package main

import (
	"bytes"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/mail"
	"net/url"
	"strings"
	"time"
)

var gmailScopes = []string{
	"https://www.googleapis.com/auth/gmail.send",
	"https://www.googleapis.com/auth/gmail.readonly",
	"https://www.googleapis.com/auth/gmail.modify",
	"https://www.googleapis.com/auth/userinfo.email",
	"https://www.googleapis.com/auth/userinfo.profile",
}

type googleTokens struct {
	AccessToken  string `json:"access_token"`
	RefreshToken string `json:"refresh_token,omitempty"`
	TokenType    string `json:"token_type,omitempty"`
	ExpiresIn    int64  `json:"expires_in,omitempty"`
	ExpiryDate   int64  `json:"expiry_date,omitempty"`
	Email        string `json:"email,omitempty"`
}

type googleProfile struct {
	Email string `json:"email"`
	Name  string `json:"name"`
}

func (a *app) googleAuthURL(redirectURI string) string {
	values := url.Values{}
	values.Set("client_id", a.cfg.GmailClientID)
	values.Set("redirect_uri", redirectURI)
	values.Set("response_type", "code")
	values.Set("access_type", "offline")
	values.Set("prompt", "consent")
	values.Set("scope", strings.Join(gmailScopes, " "))
	return "https://accounts.google.com/o/oauth2/v2/auth?" + values.Encode()
}

func (a *app) exchangeCode(code, redirectURI string) (*googleTokens, error) {
	values := url.Values{}
	values.Set("code", code)
	values.Set("client_id", a.cfg.GmailClientID)
	values.Set("client_secret", a.cfg.GmailClientSecret)
	values.Set("redirect_uri", redirectURI)
	values.Set("grant_type", "authorization_code")
	req, _ := http.NewRequest(http.MethodPost, "https://oauth2.googleapis.com/token", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var tokens googleTokens
	if err := a.doJSON(req, &tokens); err != nil {
		return nil, err
	}
	tokens.ExpiryDate = time.Now().Add(time.Duration(tokens.ExpiresIn) * time.Second).Unix()
	return &tokens, nil
}

func (a *app) refreshTokens(user *User, tokens *googleTokens) (*googleTokens, error) {
	if tokens.AccessToken != "" && tokens.ExpiryDate > unixNow()+60 {
		return tokens, nil
	}
	if tokens.RefreshToken == "" {
		return tokens, nil
	}
	values := url.Values{}
	values.Set("client_id", a.cfg.GmailClientID)
	values.Set("client_secret", a.cfg.GmailClientSecret)
	values.Set("refresh_token", tokens.RefreshToken)
	values.Set("grant_type", "refresh_token")
	req, _ := http.NewRequest(http.MethodPost, "https://oauth2.googleapis.com/token", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	var refreshed googleTokens
	if err := a.doJSON(req, &refreshed); err != nil {
		return tokens, err
	}
	if refreshed.RefreshToken == "" {
		refreshed.RefreshToken = tokens.RefreshToken
	}
	refreshed.Email = tokens.Email
	refreshed.ExpiryDate = time.Now().Add(time.Duration(refreshed.ExpiresIn) * time.Second).Unix()
	raw, _ := json.Marshal(refreshed)
	_ = a.store.exec("UPDATE users SET gmail_token=? WHERE id=?", string(raw), user.ID)
	user.GmailToken = sql.NullString{String: string(raw), Valid: true}
	return &refreshed, nil
}

func (a *app) fetchProfile(tokens *googleTokens) (*googleProfile, error) {
	req, _ := http.NewRequest(http.MethodGet, "https://www.googleapis.com/oauth2/v2/userinfo", nil)
	req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	var profile googleProfile
	return &profile, a.doJSON(req, &profile)
}

func (a *app) doJSON(req *http.Request, dest any) error {
	res, err := a.client.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	body, _ := io.ReadAll(res.Body)
	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return fmt.Errorf("remote request failed: %s: %s", res.Status, truncate(string(body), 300))
	}
	if dest == nil {
		return nil
	}
	return json.Unmarshal(body, dest)
}

func (a *app) tokensFor(user *User) (*googleTokens, error) {
	var tokens googleTokens
	if !user.GmailToken.Valid || user.GmailToken.String == "" {
		return nil, fmt.Errorf("user has no Gmail token")
	}
	if err := json.Unmarshal([]byte(user.GmailToken.String), &tokens); err != nil {
		return nil, err
	}
	return a.refreshTokens(user, &tokens)
}

type sendEmailInput struct {
	To            string
	ToName        string
	Subject       string
	Body          string
	InReplyTo     string
	References    string
	GmailThreadID string
}

type sendEmailResult struct {
	GmailMsgID    string
	GmailThreadID string
	MessageID     string
}

func (a *app) sendEmail(user *User, input sendEmailInput) (*sendEmailResult, error) {
	tokens, err := a.tokensFor(user)
	if err != nil {
		return nil, err
	}
	from := tokens.Email
	if from == "" {
		from = user.Email
	}
	domain := "localhost"
	if at := strings.LastIndex(from, "@"); at >= 0 {
		domain = from[at+1:]
	}
	messageID := fmt.Sprintf("<mf-%d-%s@%s>", time.Now().UnixMilli(), strings.ReplaceAll(newID(), "-", ""), domain)
	raw := buildRawEmail(map[string]string{
		"from": from, "fromName": firstNonEmpty(a.cfg.FromName, user.Name), "to": input.To, "toName": input.ToName,
		"subject": input.Subject, "body": input.Body, "messageID": messageID, "inReplyTo": input.InReplyTo, "references": input.References,
	})
	payload := map[string]any{"raw": raw}
	if input.GmailThreadID != "" {
		payload["threadId"] = input.GmailThreadID
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, "https://gmail.googleapis.com/gmail/v1/users/me/messages/send", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
	req.Header.Set("Content-Type", "application/json")
	var gmailRes struct {
		ID       string `json:"id"`
		ThreadID string `json:"threadId"`
	}
	if err := a.doJSON(req, &gmailRes); err != nil {
		return nil, err
	}
	return &sendEmailResult{GmailMsgID: gmailRes.ID, GmailThreadID: gmailRes.ThreadID, MessageID: messageID}, nil
}

func buildRawEmail(v map[string]string) string {
	boundary := fmt.Sprintf("mf_%d", time.Now().UnixMilli())
	plain := stripHTML(v["body"])
	html := `<html><body style="font-family:Georgia,serif;font-size:15px;line-height:1.7;color:#1a1a1a;max-width:600px;margin:0 auto;padding:20px">` + strings.ReplaceAll(v["body"], "\n", "<br>") + `</body></html>`
	domain := "localhost"
	if at := strings.LastIndex(v["from"], "@"); at >= 0 {
		domain = v["from"][at+1:]
	}
	headers := []string{
		fmt.Sprintf("From: %s", mime.QEncoding.Encode("utf-8", v["fromName"])+" <"+v["from"]+">"),
		fmt.Sprintf("To: %s", mime.QEncoding.Encode("utf-8", firstNonEmpty(v["toName"], v["to"]))+" <"+v["to"]+">"),
		"Subject: " + mime.QEncoding.Encode("utf-8", v["subject"]),
		"Message-ID: " + v["messageID"],
		"Date: " + time.Now().UTC().Format(time.RFC1123Z),
		"MIME-Version: 1.0",
		`Content-Type: multipart/alternative; boundary="` + boundary + `"`,
	}
	if v["inReplyTo"] != "" {
		headers = append(headers, "In-Reply-To: "+v["inReplyTo"])
	}
	if v["references"] != "" {
		headers = append(headers, "References: "+v["references"])
	}
	headers = append(headers,
		"List-Unsubscribe: <mailto:unsubscribe@"+domain+"?subject=unsubscribe>",
		"List-Unsubscribe-Post: List-Unsubscribe=One-Click",
		"X-Mailer: MailFlow/1.0",
	)
	raw := strings.Join(headers, "\r\n") + "\r\n\r\n" +
		"--" + boundary + "\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n" + plain + "\r\n\r\n" +
		"--" + boundary + "\r\nContent-Type: text/html; charset=UTF-8\r\n\r\n" + html + "\r\n\r\n" +
		"--" + boundary + "--"
	return base64.RawURLEncoding.EncodeToString([]byte(raw))
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func parseFrom(value string) (name, email string) {
	addr, err := mail.ParseAddress(value)
	if err == nil {
		return addr.Name, addr.Address
	}
	return value, value
}
