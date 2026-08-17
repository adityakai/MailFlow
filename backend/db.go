package main

import (
	"database/sql"
	"fmt"
	"regexp"
	"strings"
	"time"

	_ "github.com/lib/pq"
	_ "modernc.org/sqlite"
)

type store struct {
	db         *sql.DB
	isPostgres bool
}

const schema = `
CREATE TABLE IF NOT EXISTS users (
  id TEXT PRIMARY KEY,
  email TEXT UNIQUE NOT NULL,
  name TEXT NOT NULL,
  gmail_token TEXT,
  created_at BIGINT DEFAULT 0
);
CREATE TABLE IF NOT EXISTS contacts (
  id TEXT PRIMARY KEY,
  email TEXT UNIQUE NOT NULL,
  name TEXT,
  company TEXT,
  created_at BIGINT DEFAULT 0
);
CREATE TABLE IF NOT EXISTS threads (
  id TEXT PRIMARY KEY,
  gmail_thread_id TEXT UNIQUE,
  contact_id TEXT NOT NULL,
  owner_id TEXT NOT NULL,
  subject TEXT NOT NULL,
  status TEXT DEFAULT 'ai',
  ai_mode INTEGER DEFAULT 1,
  claimed_by TEXT,
  is_primary INTEGER DEFAULT 1,
  deliv_score INTEGER DEFAULT 94,
  created_at BIGINT DEFAULT 0,
  updated_at BIGINT DEFAULT 0
);
CREATE TABLE IF NOT EXISTS messages (
  id TEXT PRIMARY KEY,
  thread_id TEXT NOT NULL,
  gmail_msg_id TEXT,
  role TEXT NOT NULL,
  from_name TEXT NOT NULL,
  from_email TEXT NOT NULL,
  body TEXT NOT NULL,
  message_id_header TEXT,
  references_header TEXT,
  in_reply_to TEXT,
  sent_at BIGINT DEFAULT 0
);
CREATE TABLE IF NOT EXISTS thread_access (
  thread_id TEXT NOT NULL,
  user_id TEXT NOT NULL,
  role TEXT NOT NULL DEFAULT 'owner',
  granted_at BIGINT DEFAULT 0,
  PRIMARY KEY (thread_id, user_id)
);`

func openStore(cfg config) (*store, error) {
	driver, dsn := "sqlite", cfg.SQLitePath
	if cfg.IsPostgres {
		driver, dsn = "postgres", cfg.DatabaseURL
		if cfg.IsProduction && !strings.Contains(dsn, "sslmode=") {
			if strings.Contains(dsn, "?") {
				dsn += "&sslmode=require"
			} else {
				dsn += "?sslmode=require"
			}
		}
	}

	db, err := sql.Open(driver, dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)

	s := &store{db: db, isPostgres: cfg.IsPostgres}
	for _, stmt := range splitSQL(schema) {
		if _, err := db.Exec(s.query(stmt)); err != nil {
			return nil, fmt.Errorf("schema: %w", err)
		}
	}
	if err := s.backfillTimestamps(); err != nil {
		return nil, err
	}
	return s, nil
}

func splitSQL(sqlText string) []string {
	parts := strings.Split(sqlText, ";")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if stmt := strings.TrimSpace(part); stmt != "" {
			out = append(out, stmt)
		}
	}
	return out
}

func (s *store) query(q string) string {
	if !s.isPostgres {
		return q
	}
	i := 0
	return regexp.MustCompile(`\?`).ReplaceAllStringFunc(q, func(string) string {
		i++
		return fmt.Sprintf("$%d", i)
	})
}

func (s *store) exec(q string, args ...any) error {
	_, err := s.db.Exec(s.query(q), args...)
	return err
}

func (s *store) backfillTimestamps() error {
	now := unixNow()
	statements := []string{
		"UPDATE users SET created_at=? WHERE created_at=0",
		"UPDATE contacts SET created_at=? WHERE created_at=0",
		"UPDATE threads SET created_at=? WHERE created_at=0",
		"UPDATE threads SET updated_at=? WHERE updated_at=0",
		"UPDATE messages SET sent_at=? WHERE sent_at=0",
		"UPDATE thread_access SET granted_at=? WHERE granted_at=0",
	}
	for _, stmt := range statements {
		if err := s.exec(stmt, now); err != nil {
			return err
		}
	}
	return nil
}
