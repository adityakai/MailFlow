package main

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"net/http"
	"strings"
	"sync"
	"time"
)

type sessionManager struct {
	secret []byte
	mu     sync.RWMutex
	users  map[string]string
}

func newSessionManager(secret string) *sessionManager {
	return &sessionManager{secret: []byte(secret), users: map[string]string{}}
}

func (s *sessionManager) getUserID(r *http.Request) string {
	cookie, err := r.Cookie("mailflow.sid")
	if err != nil {
		return ""
	}
	parts := strings.Split(cookie.Value, ".")
	if len(parts) != 2 || !s.valid(parts[0], parts[1]) {
		return ""
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.users[parts[0]]
}

func (s *sessionManager) set(w http.ResponseWriter, userID string, secure bool) {
	id := newID()
	sig := s.sign(id)
	s.mu.Lock()
	s.users[id] = userID
	s.mu.Unlock()
	http.SetCookie(w, &http.Cookie{
		Name:     "mailflow.sid",
		Value:    id + "." + sig,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int((7 * 24 * time.Hour).Seconds()),
	})
}

func (s *sessionManager) clear(w http.ResponseWriter, r *http.Request) {
	if cookie, err := r.Cookie("mailflow.sid"); err == nil {
		if id := strings.Split(cookie.Value, ".")[0]; id != "" {
			s.mu.Lock()
			delete(s.users, id)
			s.mu.Unlock()
		}
	}
	http.SetCookie(w, &http.Cookie{Name: "mailflow.sid", Value: "", Path: "/", MaxAge: -1, HttpOnly: true, SameSite: http.SameSiteLaxMode})
}

func (s *sessionManager) sign(value string) string {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(value))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func (s *sessionManager) valid(value, sig string) bool {
	expected, err := base64.RawURLEncoding.DecodeString(s.sign(value))
	if err != nil {
		return false
	}
	got, err := base64.RawURLEncoding.DecodeString(sig)
	if err != nil {
		return false
	}
	return hmac.Equal(got, expected)
}
