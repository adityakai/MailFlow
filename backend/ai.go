package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

func (a *app) generateDraft(messages []Message, contact Contact, user *User) (string, error) {
	var transcript []string
	contactName := contact.Name.String
	for _, m := range messages {
		side := contactName
		if m.Role != "inbound" {
			side = user.Name + " (you)"
		}
		transcript = append(transcript, fmt.Sprintf("[%s]\n%s", side, m.Body))
	}
	system := fmt.Sprintf("You are %s, a professional outreach agent replying to %s (%s).\nBe warm, concise, and human. Write ONLY the reply body. No subject line. Keep under 150 words unless detail is needed. Do not add a sign-off.", user.Name, contactName, contact.Email)
	userPrompt := "Thread so far:\n\n" + strings.Join(transcript, "\n\n---\n\n") + "\n\nWrite a reply to " + contactName + "'s latest message."
	text, err := a.groqChat(system, userPrompt, 600)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(text) + "\n\nBest,\n" + user.Name, nil
}

func (a *app) shouldEscalate(contactName, latestMessage string) map[string]any {
	system := `Respond ONLY with valid JSON, no markdown: {"escalate": true or false, "reason": "one sentence"}. Escalate if: frustration or complaints, pricing negotiation, legal or compliance questions, or explicit request to speak to a human.`
	text, err := a.groqChat(system, "Contact: "+contactName+"\nMessage: "+latestMessage, 80)
	if err != nil {
		return map[string]any{"escalate": false, "reason": ""}
	}
	var out map[string]any
	if err := json.Unmarshal([]byte(strings.TrimSpace(text)), &out); err != nil {
		return map[string]any{"escalate": false, "reason": ""}
	}
	return out
}

func (a *app) groqChat(system, userPrompt string, maxTokens int) (string, error) {
	if a.cfg.GroqAPIKey == "" {
		return "", fmt.Errorf("GROQ_API_KEY is not configured")
	}
	payload := map[string]any{
		"model":      "llama-3.3-70b-versatile",
		"max_tokens": maxTokens,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": userPrompt},
		},
	}
	body, _ := json.Marshal(payload)
	req, _ := http.NewRequest(http.MethodPost, "https://api.groq.com/openai/v1/chat/completions", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+a.cfg.GroqAPIKey)
	req.Header.Set("Content-Type", "application/json")
	var response struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := a.doJSON(req, &response); err != nil {
		return "", err
	}
	if len(response.Choices) == 0 {
		return "", nil
	}
	return response.Choices[0].Message.Content, nil
}
