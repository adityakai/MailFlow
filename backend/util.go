package main

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

func unixNow() int64 {
	return time.Now().Unix()
}

func newID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return hex.EncodeToString([]byte(time.Now().Format(time.RFC3339Nano)))
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return hex.EncodeToString(b[0:4]) + "-" + hex.EncodeToString(b[4:6]) + "-" + hex.EncodeToString(b[6:8]) + "-" + hex.EncodeToString(b[8:10]) + "-" + hex.EncodeToString(b[10:16])
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func readJSON(r *http.Request, dest any) error {
	defer r.Body.Close()
	return json.NewDecoder(r.Body).Decode(dest)
}

func errorJSON(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{"error": message})
}

func truncate(s string, limit int) string {
	if len(s) <= limit {
		return s
	}
	return s[:limit]
}

func stringValue(s any) string {
	switch v := s.(type) {
	case string:
		return v
	default:
		return ""
	}
}

func stripHTML(s string) string {
	var out strings.Builder
	inTag := false
	for _, r := range s {
		switch r {
		case '<':
			inTag = true
		case '>':
			inTag = false
		default:
			if !inTag {
				out.WriteRune(r)
			}
		}
	}
	return strings.TrimSpace(strings.NewReplacer("&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">").Replace(out.String()))
}
