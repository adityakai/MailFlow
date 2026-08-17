package main

import (
	"fmt"
	"log"
	"net/http"
	"time"
)

func main() {
	cfg := loadConfig()
	st, err := openStore(cfg)
	if err != nil {
		log.Fatal(err)
	}
	a := &app{
		cfg:      cfg,
		store:    st,
		sessions: newSessionManager(cfg.SessionSecret),
		client:   &http.Client{Timeout: 30 * time.Second},
	}
	a.startPoller(60 * time.Second)
	addr := ":" + cfg.Port
	fmt.Printf("MailFlow running on http://localhost:%s\n", cfg.Port)
	log.Fatal(http.ListenAndServe(addr, a.routes()))
}
