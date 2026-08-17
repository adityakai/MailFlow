package main

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"time"
)

type gmailThreadResponse struct {
	Messages []gmailMessage `json:"messages"`
}

type gmailMessage struct {
	ID      string `json:"id"`
	Payload struct {
		Headers []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"headers"`
		Body struct {
			Data string `json:"data"`
		} `json:"body"`
		Parts []gmailPart `json:"parts"`
	} `json:"payload"`
}

type gmailPart struct {
	MimeType string `json:"mimeType"`
	Body     struct {
		Data string `json:"data"`
	} `json:"body"`
	Parts []gmailPart `json:"parts"`
}

func (a *app) startPoller(interval time.Duration) {
	go func() {
		a.pollAllUsers()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for range ticker.C {
			a.pollAllUsers()
		}
	}()
}

func (a *app) pollAllUsers() {
	rows, err := a.store.db.Query(a.store.query("SELECT id,email,name,gmail_token FROM users WHERE gmail_token IS NOT NULL"))
	if err != nil {
		return
	}
	defer rows.Close()
	for rows.Next() {
		var user User
		if rows.Scan(&user.ID, &user.Email, &user.Name, &user.GmailToken) == nil {
			_ = a.fetchInboundReplies(&user)
		}
	}
}

func (a *app) fetchInboundReplies(user *User) error {
	tokens, err := a.tokensFor(user)
	if err != nil {
		return err
	}
	rows, err := a.store.db.Query(a.store.query("SELECT id,gmail_thread_id,subject FROM threads WHERE owner_id=? AND gmail_thread_id IS NOT NULL"), user.ID)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var threadID, gmailThreadID, subject string
		if rows.Scan(&threadID, &gmailThreadID, &subject) != nil {
			continue
		}
		req, _ := http.NewRequest(http.MethodGet, "https://gmail.googleapis.com/gmail/v1/users/me/threads/"+gmailThreadID+"?format=full", nil)
		req.Header.Set("Authorization", "Bearer "+tokens.AccessToken)
		var gt gmailThreadResponse
		if err := a.doJSON(req, &gt); err != nil {
			continue
		}
		for _, msg := range gt.Messages {
			var existing string
			err := a.store.db.QueryRow(a.store.query("SELECT id FROM messages WHERE gmail_msg_id=?"), msg.ID).Scan(&existing)
			if err == nil {
				continue
			}
			headers := gmailHeaders(msg)
			fromName, fromEmail := parseFrom(headers["from"])
			if strings.EqualFold(fromEmail, firstNonEmpty(tokens.Email, user.Email)) {
				continue
			}
			body := strings.TrimSpace(extractGmailBody(msg.Payload.Body.Data, msg.Payload.Parts))
			if body == "" {
				continue
			}
			now := unixNow()
			_ = a.store.exec("INSERT INTO messages (id,thread_id,gmail_msg_id,role,from_name,from_email,body,message_id_header,in_reply_to,references_header,sent_at) VALUES (?,?,?,'inbound',?,?,?,?,?,?,?)", newID(), threadID, msg.ID, firstNonEmpty(fromName, fromEmail), fromEmail, body, headers["message-id"], headers["in-reply-to"], headers["references"], now)
			_ = a.store.exec("UPDATE threads SET updated_at=?, is_primary=1 WHERE id=?", now, threadID)
			fmt.Println("New reply in thread", subject, "from", fromEmail)
		}
	}
	return nil
}

func gmailHeaders(msg gmailMessage) map[string]string {
	headers := map[string]string{}
	for _, h := range msg.Payload.Headers {
		headers[strings.ToLower(h.Name)] = h.Value
	}
	return headers
}

func extractGmailBody(data string, parts []gmailPart) string {
	if data != "" {
		return decodeGmailData(data)
	}
	for _, p := range parts {
		if p.MimeType == "text/plain" && p.Body.Data != "" {
			return decodeGmailData(p.Body.Data)
		}
	}
	for _, p := range parts {
		if p.MimeType == "text/html" && p.Body.Data != "" {
			return stripHTML(decodeGmailData(p.Body.Data))
		}
	}
	for _, p := range parts {
		if body := extractGmailBody(p.Body.Data, p.Parts); body != "" {
			return body
		}
	}
	return ""
}

func decodeGmailData(data string) string {
	raw, err := base64.RawURLEncoding.DecodeString(data)
	if err != nil {
		raw, err = base64.URLEncoding.DecodeString(data)
	}
	if err != nil {
		return ""
	}
	return string(raw)
}
