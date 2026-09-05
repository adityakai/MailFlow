package main

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"
)

func (a *app) routes() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /auth/google", a.handleGoogleLogin)
	mux.HandleFunc("GET /auth/callback", a.handleGoogleCallback)
	mux.HandleFunc("GET /api/me", a.handleMe)
	mux.HandleFunc("GET /api/auth-debug", a.handleAuthDebug)
	mux.HandleFunc("GET /api/oauth-config-debug", a.handleOAuthDebug)
	mux.HandleFunc("GET /api/ai-config-debug", a.handleAIDebug)
	mux.HandleFunc("POST /api/logout", a.handleLogout)
	mux.HandleFunc("GET /api/threads", a.requireAuth(a.handleListThreads))
	mux.HandleFunc("POST /api/threads", a.requireAuth(a.handleCreateThread))
	mux.HandleFunc("GET /api/threads/{threadId}", a.requireThreadAccess(a.handleThreadDetail))
	mux.HandleFunc("DELETE /api/threads/{threadId}", a.requireThreadOwner(a.handleDeleteThread))
	mux.HandleFunc("POST /api/threads/{threadId}/claim", a.requireThreadOwner(a.handleClaimThread))
	mux.HandleFunc("POST /api/threads/{threadId}/release", a.requireThreadOwner(a.handleReleaseThread))
	mux.HandleFunc("POST /api/threads/{threadId}/toggle-ai", a.requireThreadOwner(a.handleToggleAI))
	mux.HandleFunc("POST /api/threads/{threadId}/reply", a.requireThreadAccess(a.handleReply))
	mux.HandleFunc("POST /api/threads/{threadId}/draft", a.requireThreadAccess(a.handleDraft))
	mux.HandleFunc("POST /api/bulk-send", a.requireAuth(a.handleBulkSend))
	mux.HandleFunc("POST /api/poll", a.handleManualPoll)
	mux.HandleFunc("GET /health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "env": a.cfg.AppEnv})
	})
	mux.Handle("/", http.FileServer(http.Dir(a.cfg.FrontendDir)))
	return a.withCommonHeaders(a.fallbackIndex(mux))
}

func (a *app) withCommonHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", r.Header.Get("Origin"))
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		w.Header().Set("Access-Control-Allow-Methods", "GET,POST,DELETE,OPTIONS")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *app) fallbackIndex(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/auth/") || r.URL.Path == "/health" {
			next.ServeHTTP(w, r)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (a *app) handleGoogleLogin(w http.ResponseWriter, r *http.Request) {
	http.Redirect(w, r, a.googleAuthURL(a.oauthRedirectURI(r)), http.StatusFound)
}

func (a *app) handleGoogleCallback(w http.ResponseWriter, r *http.Request) {
	if r.URL.Query().Get("error") != "" {
		http.Redirect(w, r, "/?error=oauth_denied", http.StatusFound)
		return
	}
	tokens, err := a.exchangeCode(r.URL.Query().Get("code"), a.oauthRedirectURI(r))
	if err != nil {
		http.Redirect(w, r, "/?error=oauth_token_exchange_failed", http.StatusFound)
		return
	}
	profile, err := a.fetchProfile(tokens)
	if err != nil {
		http.Redirect(w, r, "/?error=oauth_profile_failed", http.StatusFound)
		return
	}
	tokens.Email = profile.Email
	raw, _ := json.Marshal(tokens)
	userID := newID()
	var existingID string
	err = a.store.db.QueryRow(a.store.query("SELECT id FROM users WHERE email=?"), profile.Email).Scan(&existingID)
	if err == nil {
		userID = existingID
		err = a.store.exec("UPDATE users SET name=?, gmail_token=? WHERE id=?", profile.Name, string(raw), userID)
	} else if errors.Is(err, sql.ErrNoRows) {
		err = a.store.exec("INSERT INTO users (id,email,name,gmail_token,created_at) VALUES (?,?,?,?,?)", userID, profile.Email, profile.Name, string(raw), unixNow())
	}
	if err != nil {
		http.Redirect(w, r, "/?error=oauth_user_save_failed", http.StatusFound)
		return
	}
	a.sessions.set(w, userID, a.secureCookie(r))
	http.Redirect(w, r, "/", http.StatusFound)
}

func (a *app) secureCookie(r *http.Request) bool {
	if isLocalHost(r.Host) {
		return false
	}
	return a.cfg.IsProduction || r.Header.Get("X-Forwarded-Proto") == "https"
}

func (a *app) oauthRedirectURI(r *http.Request) string {
	return a.externalBaseURL(r) + "/auth/callback"
}

func (a *app) externalBaseURL(r *http.Request) string {
	proto := r.Header.Get("X-Forwarded-Proto")
	if proto == "" {
		if r.TLS != nil {
			proto = "https"
		} else {
			proto = "http"
		}
	}
	host := r.Header.Get("X-Forwarded-Host")
	if host == "" {
		host = r.Host
	}
	return proto + "://" + host
}

func isLocalHost(host string) bool {
	host = strings.Split(host, ":")[0]
	return host == "localhost" || host == "127.0.0.1" || host == "[::1]" || host == "::1"
}

func (a *app) handleMe(w http.ResponseWriter, r *http.Request) {
	user, err := a.currentUser(r)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"user": nil})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"user": userMap(user)})
}

func (a *app) handleAuthDebug(w http.ResponseWriter, r *http.Request) {
	userID := a.sessions.getUserID(r)
	_, err := a.getUser(userID)
	writeJSON(w, http.StatusOK, map[string]any{
		"hasCookieHeader":  r.Header.Get("Cookie") != "",
		"hasSessionUserId": userID != "",
		"userExists":       err == nil,
		"sessionIdPrefix":  truncate(userID, 8),
		"protocol":         r.URL.Scheme,
		"secure":           a.secureCookie(r),
		"forwardedProto":   r.Header.Get("X-Forwarded-Proto"),
		"appEnv":           a.cfg.AppEnv,
	})
}

func (a *app) handleOAuthDebug(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"hasClientId":               a.cfg.GmailClientID != "",
		"clientIdPrefix":            truncate(a.cfg.GmailClientID, 12),
		"clientIdSuffix":            suffix(a.cfg.GmailClientID, 24),
		"clientIdHasWhitespace":     a.cfg.GmailClientID != strings.TrimSpace(a.cfg.GmailClientID),
		"hasClientSecret":           a.cfg.GmailClientSecret != "",
		"clientSecretHasWhitespace": a.cfg.GmailClientSecret != strings.TrimSpace(a.cfg.GmailClientSecret),
		"redirectUri":               a.cfg.GmailRedirectURI,
		"activeRedirectUri":         a.oauthRedirectURI(r),
		"redirectUriHasWhitespace":  a.cfg.GmailRedirectURI != strings.TrimSpace(a.cfg.GmailRedirectURI),
		"appEnv":                    a.cfg.AppEnv,
	})
}

func (a *app) handleAIDebug(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"hasGroqApiKey": a.cfg.GroqAPIKey != "",
		"groqModel":     a.cfg.GroqModel,
	})
}

func (a *app) handleLogout(w http.ResponseWriter, r *http.Request) {
	a.sessions.clear(w, r)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *app) handleListThreads(w http.ResponseWriter, r *http.Request) {
	user := userFrom(r)
	rows, err := a.store.db.Query(a.store.query(`
SELECT t.id,t.gmail_thread_id,t.contact_id,t.owner_id,t.subject,t.status,t.ai_mode,t.claimed_by,t.is_primary,t.deliv_score,t.created_at,t.updated_at,
       c.name,c.email,u.name,cu.name,
       (SELECT COUNT(*) FROM messages m WHERE m.thread_id=t.id),
       (SELECT m.body FROM messages m WHERE m.thread_id=t.id ORDER BY m.sent_at DESC LIMIT 1)
FROM threads t
JOIN contacts c ON c.id=t.contact_id
JOIN users u ON u.id=t.owner_id
LEFT JOIN users cu ON cu.id=t.claimed_by
WHERE t.owner_id=? OR t.claimed_by=?
ORDER BY t.updated_at DESC`), user.ID, user.ID)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "Could not load threads")
		return
	}
	defer rows.Close()
	threads := []map[string]any{}
	for rows.Next() {
		var t Thread
		if err := rows.Scan(&t.ID, &t.GmailThreadID, &t.ContactID, &t.OwnerID, &t.Subject, &t.Status, &t.AIMode, &t.ClaimedBy, &t.IsPrimary, &t.Deliverability, &t.CreatedAt, &t.UpdatedAt, &t.ContactName, &t.ContactEmail, &t.OwnerName, &t.ClaimedByName, &t.MessageCount, &t.LastMessagePreview); err == nil {
			item := threadMap(&t)
			item["last_message_preview"] = truncate(ns(t.LastMessagePreview), 80)
			threads = append(threads, item)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"threads": threads})
}

func (a *app) handleThreadDetail(w http.ResponseWriter, r *http.Request) {
	thread := threadFrom(r)
	contact, err := a.loadContact(thread.ContactID)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "Could not load contact")
		return
	}
	messages, err := a.loadMessages(thread.ID)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "Could not load messages")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"thread": threadMap(thread), "contact": contactMap(contact), "messages": messageMaps(messages)})
}

func (a *app) loadContact(id string) (*Contact, error) {
	row := a.store.db.QueryRow(a.store.query("SELECT id,email,name,company FROM contacts WHERE id=?"), id)
	var c Contact
	err := row.Scan(&c.ID, &c.Email, &c.Name, &c.Company)
	return &c, err
}

func (a *app) loadMessages(threadID string) ([]Message, error) {
	rows, err := a.store.db.Query(a.store.query("SELECT id,thread_id,gmail_msg_id,role,from_name,from_email,body,message_id_header,references_header,in_reply_to,sent_at FROM messages WHERE thread_id=? ORDER BY sent_at ASC"), threadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var messages []Message
	for rows.Next() {
		var m Message
		if err := rows.Scan(&m.ID, &m.ThreadID, &m.GmailMsgID, &m.Role, &m.FromName, &m.FromEmail, &m.Body, &m.MessageIDHeader, &m.ReferencesHeader, &m.InReplyTo, &m.SentAt); err != nil {
			return nil, err
		}
		messages = append(messages, m)
	}
	return messages, nil
}

func (a *app) handleDeleteThread(w http.ResponseWriter, r *http.Request) {
	threadID := pathValue(r, "threadId")
	_ = a.store.exec("DELETE FROM messages WHERE thread_id=?", threadID)
	_ = a.store.exec("DELETE FROM thread_access WHERE thread_id=?", threadID)
	_ = a.store.exec("DELETE FROM threads WHERE id=?", threadID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *app) handleClaimThread(w http.ResponseWriter, r *http.Request) {
	user := userFrom(r)
	_ = a.store.exec("UPDATE threads SET status='human', claimed_by=?, updated_at=? WHERE id=?", user.ID, unixNow(), pathValue(r, "threadId"))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "claimedBy": user.Name})
}

func (a *app) handleReleaseThread(w http.ResponseWriter, r *http.Request) {
	_ = a.store.exec("UPDATE threads SET status='ai', claimed_by=NULL, ai_mode=1, updated_at=? WHERE id=?", unixNow(), pathValue(r, "threadId"))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *app) handleToggleAI(w http.ResponseWriter, r *http.Request) {
	var req struct {
		AIMode bool `json:"aiMode"`
	}
	_ = readJSON(r, &req)
	mode, status := 0, "human"
	if req.AIMode {
		mode, status = 1, "ai"
	}
	_ = a.store.exec("UPDATE threads SET ai_mode=?, status=?, updated_at=? WHERE id=?", mode, status, unixNow(), pathValue(r, "threadId"))
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "aiMode": req.AIMode})
}

func suffix(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}

func (a *app) handleCreateThread(w http.ResponseWriter, r *http.Request) {
	user := userFrom(r)
	var req struct {
		ContactEmail   string `json:"contactEmail"`
		ContactName    string `json:"contactName"`
		ContactCompany string `json:"contactCompany"`
		Subject        string `json:"subject"`
		Body           string `json:"body"`
	}
	if err := readJSON(r, &req); err != nil || req.ContactEmail == "" || req.Subject == "" || req.Body == "" {
		errorJSON(w, http.StatusBadRequest, "Email, subject, and body are required")
		return
	}
	contact, err := a.upsertContact(req.ContactEmail, req.ContactName, req.ContactCompany)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "Failed to save contact")
		return
	}
	sent, err := a.sendEmail(user, sendEmailInput{To: contact.Email, ToName: ns(contact.Name), Subject: req.Subject, Body: req.Body})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Failed to create thread", "detail": err.Error()})
		return
	}
	threadID := newID()
	now := unixNow()
	if err := a.store.exec("INSERT INTO threads (id,gmail_thread_id,contact_id,owner_id,subject,status,ai_mode,created_at,updated_at) VALUES (?,?,?,?,?,'ai',1,?,?)", threadID, sent.GmailThreadID, contact.ID, user.ID, req.Subject, now, now); err != nil {
		errorJSON(w, http.StatusInternalServerError, "Failed to save thread")
		return
	}
	_ = a.store.exec("INSERT INTO thread_access (thread_id,user_id,role,granted_at) VALUES (?,?,'owner',?)", threadID, user.ID, now)
	_ = a.store.exec("INSERT INTO messages (id,thread_id,gmail_msg_id,role,from_name,from_email,body,message_id_header,sent_at) VALUES (?,?,?,'outbound-ai',?,?,?,?,?)", newID(), threadID, sent.GmailMsgID, user.Name, user.Email, req.Body, sent.MessageID, now)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "threadId": threadID})
}

func (a *app) handleBulkSend(w http.ResponseWriter, r *http.Request) {
	user := userFrom(r)
	var req struct {
		Contacts []struct {
			Email   string `json:"email"`
			Name    string `json:"name"`
			Company string `json:"company"`
		} `json:"contacts"`
		Subject string `json:"subject"`
		Body    string `json:"body"`
		DelayMs int    `json:"delayMs"`
	}
	if err := readJSON(r, &req); err != nil {
		errorJSON(w, http.StatusBadRequest, "Invalid request")
		return
	}
	if len(req.Contacts) == 0 {
		errorJSON(w, http.StatusBadRequest, "No contacts provided")
		return
	}
	if len(req.Contacts) > 100 {
		errorJSON(w, http.StatusBadRequest, "Max 100 contacts per bulk send")
		return
	}
	if req.Subject == "" || req.Body == "" {
		errorJSON(w, http.StatusBadRequest, "Subject and body are required")
		return
	}
	if req.DelayMs <= 0 {
		req.DelayMs = 2000
	}
	failures := []map[string]string{}
	sentCount := 0
	for i, in := range req.Contacts {
		name := firstNonEmpty(in.Name, strings.Split(in.Email, "@")[0])
		subject := personalize(req.Subject, name, in.Company)
		body := personalize(req.Body, name, in.Company)
		contact, err := a.upsertContact(in.Email, name, in.Company)
		if err == nil {
			var sent *sendEmailResult
			sent, err = a.sendEmail(user, sendEmailInput{To: contact.Email, ToName: ns(contact.Name), Subject: subject, Body: body})
			if err == nil {
				threadID := newID()
				now := unixNow()
				err = a.store.exec("INSERT INTO threads (id,gmail_thread_id,contact_id,owner_id,subject,status,ai_mode,created_at,updated_at) VALUES (?,?,?,?,?,'ai',1,?,?)", threadID, sent.GmailThreadID, contact.ID, user.ID, subject, now, now)
				if err == nil {
					_ = a.store.exec("INSERT INTO thread_access (thread_id,user_id,role,granted_at) VALUES (?,?,'owner',?)", threadID, user.ID, now)
					_ = a.store.exec("INSERT INTO messages (id,thread_id,gmail_msg_id,role,from_name,from_email,body,message_id_header,sent_at) VALUES (?,?,?,'outbound-ai',?,?,?,?,?)", newID(), threadID, sent.GmailMsgID, user.Name, user.Email, body, sent.MessageID, now)
					sentCount++
				}
			}
		}
		if err != nil {
			failures = append(failures, map[string]string{"email": in.Email, "error": err.Error()})
		}
		if i < len(req.Contacts)-1 {
			timeSleep(req.DelayMs)
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "total": len(req.Contacts), "sent": sentCount, "failed": len(failures), "failures": failures})
}

func (a *app) handleReply(w http.ResponseWriter, r *http.Request) {
	user, thread := userFrom(r), threadFrom(r)
	var req struct {
		Body string `json:"body"`
		Role string `json:"role"`
	}
	if err := readJSON(r, &req); err != nil || strings.TrimSpace(req.Body) == "" {
		errorJSON(w, http.StatusBadRequest, "Body is required")
		return
	}
	contact, err := a.loadContact(thread.ContactID)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "Could not load contact")
		return
	}
	last, refs := a.threadHeaders(thread.ID)
	sent, err := a.sendEmail(user, sendEmailInput{To: contact.Email, ToName: ns(contact.Name), Subject: "Re: " + thread.Subject, Body: req.Body, InReplyTo: last, References: refs, GmailThreadID: ns(thread.GmailThreadID)})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Failed to send", "detail": err.Error()})
		return
	}
	role := req.Role
	if role == "" {
		if thread.AIMode == 1 {
			role = "outbound-ai"
		} else {
			role = "outbound-human"
		}
	}
	now := unixNow()
	_ = a.store.exec("INSERT INTO messages (id,thread_id,gmail_msg_id,role,from_name,from_email,body,message_id_header,references_header,in_reply_to,sent_at) VALUES (?,?,?,?,?,?,?,?,?,?,?)", newID(), thread.ID, sent.GmailMsgID, role, user.Name, user.Email, req.Body, sent.MessageID, refs, last, now)
	_ = a.store.exec("UPDATE threads SET updated_at=? WHERE id=?", now, thread.ID)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *app) handleDraft(w http.ResponseWriter, r *http.Request) {
	user, thread := userFrom(r), threadFrom(r)
	contact, err := a.loadContact(thread.ContactID)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "Could not load contact")
		return
	}
	messages, err := a.loadMessages(thread.ID)
	if err != nil {
		errorJSON(w, http.StatusInternalServerError, "Could not load messages")
		return
	}
	draft, err := a.generateDraft(messages, *contact, user)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]any{"error": "Failed to generate draft", "detail": err.Error()})
		return
	}
	escalation := map[string]any{"escalate": false, "reason": ""}
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "inbound" {
			escalation = a.shouldEscalate(ns(contact.Name), messages[i].Body)
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"draft": draft, "escalation": escalation})
}

func (a *app) handleManualPoll(w http.ResponseWriter, r *http.Request) {
	go a.pollAllUsers()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *app) upsertContact(email, name, company string) (*Contact, error) {
	if name == "" && strings.Contains(email, "@") {
		name = strings.Split(email, "@")[0]
	}
	var c Contact
	err := a.store.db.QueryRow(a.store.query("SELECT id,email,name,company FROM contacts WHERE email=?"), email).Scan(&c.ID, &c.Email, &c.Name, &c.Company)
	if err == nil {
		return &c, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	id := newID()
	if err := a.store.exec("INSERT INTO contacts (id,email,name,company,created_at) VALUES (?,?,?,?,?)", id, email, name, company, unixNow()); err != nil {
		return nil, err
	}
	c = Contact{ID: id, Email: email, Name: sql.NullString{String: name, Valid: name != ""}, Company: sql.NullString{String: company, Valid: company != ""}}
	return &c, nil
}

func (a *app) threadHeaders(threadID string) (last string, refs string) {
	rows, err := a.store.db.Query(a.store.query("SELECT message_id_header FROM messages WHERE thread_id=? AND message_id_header IS NOT NULL ORDER BY sent_at ASC"), threadID)
	if err != nil {
		return "", ""
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if rows.Scan(&id) == nil && id != "" {
			ids = append(ids, id)
		}
	}
	if len(ids) > 0 {
		last = ids[len(ids)-1]
		refs = strings.Join(ids, " ")
	}
	return
}

func personalize(value, name, company string) string {
	value = strings.ReplaceAll(value, "{{name}}", name)
	value = strings.ReplaceAll(value, "{{Name}}", name)
	value = strings.ReplaceAll(value, "{{company}}", company)
	value = strings.ReplaceAll(value, "{{Company}}", company)
	return value
}

func timeSleep(delayMs int) {
	time.Sleep(time.Duration(delayMs) * time.Millisecond)
}

func ns(s sql.NullString) string {
	if s.Valid {
		return s.String
	}
	return ""
}

func nullable(s sql.NullString) any {
	if s.Valid {
		return s.String
	}
	return nil
}

func userMap(u *User) map[string]any {
	return map[string]any{"id": u.ID, "email": u.Email, "name": u.Name}
}

func contactMap(c *Contact) map[string]any {
	return map[string]any{"id": c.ID, "email": c.Email, "name": nullable(c.Name), "company": nullable(c.Company)}
}

func threadMap(t *Thread) map[string]any {
	return map[string]any{
		"id": t.ID, "gmail_thread_id": nullable(t.GmailThreadID), "contact_id": t.ContactID, "owner_id": t.OwnerID,
		"subject": t.Subject, "status": t.Status, "ai_mode": t.AIMode, "claimed_by": nullable(t.ClaimedBy),
		"is_primary": t.IsPrimary, "deliv_score": t.Deliverability, "created_at": t.CreatedAt, "updated_at": t.UpdatedAt,
		"contact_name": nullable(t.ContactName), "contact_email": nullable(t.ContactEmail), "owner_name": nullable(t.OwnerName),
		"claimed_by_name": nullable(t.ClaimedByName), "message_count": t.MessageCount,
	}
}

func messageMaps(messages []Message) []map[string]any {
	out := make([]map[string]any, 0, len(messages))
	for _, m := range messages {
		out = append(out, map[string]any{
			"id": m.ID, "thread_id": m.ThreadID, "gmail_msg_id": nullable(m.GmailMsgID), "role": m.Role,
			"from_name": m.FromName, "from_email": m.FromEmail, "body": m.Body, "message_id_header": nullable(m.MessageIDHeader),
			"references_header": nullable(m.ReferencesHeader), "in_reply_to": nullable(m.InReplyTo), "sent_at": m.SentAt,
		})
	}
	return out
}
