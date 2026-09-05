package main

import (
	"bufio"
	"os"
	"strings"
)

type config struct {
	Port              string
	AppEnv            string
	SessionSecret     string
	DatabaseURL       string
	GmailClientID     string
	GmailClientSecret string
	GmailRedirectURI  string
	FromName          string
	GroqAPIKey        string
	GroqModel         string
	FrontendDir       string
	SQLitePath        string
	IsProduction      bool
	IsPostgres        bool
}

func loadConfig() config {
	loadDotEnv(".env")
	env := firstEnv("APP_ENV", "NODE_ENV")
	if env == "" {
		env = "development"
	}
	dbURL := os.Getenv("DATABASE_URL")
	return config{
		Port:              getEnv("PORT", "3000"),
		AppEnv:            env,
		SessionSecret:     getEnv("SESSION_SECRET", "dev-secret-change-me"),
		DatabaseURL:       dbURL,
		GmailClientID:     firstEnv("GMAIL_CLIENT_ID", "GOOGLE_CLIENT_ID"),
		GmailClientSecret: firstEnv("GMAIL_CLIENT_SECRET", "GOOGLE_CLIENT_SECRET"),
		GmailRedirectURI:  firstEnv("GMAIL_REDIRECT_URI", "GOOGLE_REDIRECT_URI"),
		FromName:          os.Getenv("FROM_NAME"),
		GroqAPIKey:        os.Getenv("GROQ_API_KEY"),
		GroqModel:         getEnv("GROQ_MODEL", "openai/gpt-oss-120b"),
		FrontendDir:       getEnv("FRONTEND_DIR", "frontend"),
		SQLitePath:        getEnv("SQLITE_PATH", "mailflow.db"),
		IsProduction:      env == "production",
		IsPostgres:        strings.HasPrefix(dbURL, "postgres://") || strings.HasPrefix(dbURL, "postgresql://"),
	}
}

func loadDotEnv(path string) {
	file, err := os.Open(path)
	if err != nil {
		return
	}
	defer file.Close()

	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") || !strings.Contains(line, "=") {
			continue
		}
		parts := strings.SplitN(line, "=", 2)
		key := strings.TrimSpace(parts[0])
		value := strings.Trim(strings.TrimSpace(parts[1]), `"'`)
		if key != "" && os.Getenv(key) == "" {
			_ = os.Setenv(key, value)
		}
	}
}

func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func firstEnv(keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(os.Getenv(key)); value != "" {
			return value
		}
	}
	return ""
}
