package main

import (
	"context"
	"database/sql"
	"errors"
	"net/http"
)

type app struct {
	cfg      config
	store    *store
	sessions *sessionManager
	client   *http.Client
}

type contextKey string

const (
	userKey   contextKey = "user"
	threadKey contextKey = "thread"
)

func (a *app) currentUser(r *http.Request) (*User, error) {
	userID := a.sessions.getUserID(r)
	if userID == "" {
		return nil, errors.New("not authenticated")
	}
	return a.getUser(userID)
}

func (a *app) getUser(userID string) (*User, error) {
	row := a.store.db.QueryRow(a.store.query("SELECT id,email,name,gmail_token FROM users WHERE id=?"), userID)
	var u User
	if err := row.Scan(&u.ID, &u.Email, &u.Name, &u.GmailToken); err != nil {
		return nil, err
	}
	return &u, nil
}

func (a *app) requireAuth(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, err := a.currentUser(r)
		if err != nil || user == nil {
			errorJSON(w, http.StatusUnauthorized, "Not authenticated")
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), userKey, user)))
	}
}

func userFrom(r *http.Request) *User {
	user, _ := r.Context().Value(userKey).(*User)
	return user
}

func threadFrom(r *http.Request) *Thread {
	thread, _ := r.Context().Value(threadKey).(*Thread)
	return thread
}

func (a *app) loadThread(threadID string) (*Thread, error) {
	row := a.store.db.QueryRow(a.store.query(`SELECT id,gmail_thread_id,contact_id,owner_id,subject,status,ai_mode,claimed_by,is_primary,deliv_score,created_at,updated_at FROM threads WHERE id=?`), threadID)
	var t Thread
	if err := row.Scan(&t.ID, &t.GmailThreadID, &t.ContactID, &t.OwnerID, &t.Subject, &t.Status, &t.AIMode, &t.ClaimedBy, &t.IsPrimary, &t.Deliverability, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, err
	}
	return &t, nil
}

func (a *app) requireThreadAccess(next http.HandlerFunc) http.HandlerFunc {
	return a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		user := userFrom(r)
		thread, err := a.loadThread(pathValue(r, "threadId"))
		if errors.Is(err, sql.ErrNoRows) {
			errorJSON(w, http.StatusNotFound, "Thread not found")
			return
		}
		if err != nil {
			errorJSON(w, http.StatusInternalServerError, "Failed to load thread")
			return
		}
		if thread.OwnerID != user.ID && (!thread.ClaimedBy.Valid || thread.ClaimedBy.String != user.ID) {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "Access denied", "reason": "Only the thread owner or the assigned agent can view this conversation."})
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), threadKey, thread)))
	})
}

func (a *app) requireThreadOwner(next http.HandlerFunc) http.HandlerFunc {
	return a.requireAuth(func(w http.ResponseWriter, r *http.Request) {
		user := userFrom(r)
		thread, err := a.loadThread(pathValue(r, "threadId"))
		if errors.Is(err, sql.ErrNoRows) {
			errorJSON(w, http.StatusNotFound, "Thread not found")
			return
		}
		if err != nil {
			errorJSON(w, http.StatusInternalServerError, "Failed to load thread")
			return
		}
		if thread.OwnerID != user.ID {
			writeJSON(w, http.StatusForbidden, map[string]any{"error": "Access denied", "reason": "Only the agent who started this thread can claim or modify it."})
			return
		}
		next(w, r.WithContext(context.WithValue(r.Context(), threadKey, thread)))
	})
}

func pathValue(r *http.Request, key string) string {
	return r.PathValue(key)
}
