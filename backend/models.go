package main

import "database/sql"

type User struct {
	ID         string         `json:"id"`
	Email      string         `json:"email"`
	Name       string         `json:"name"`
	GmailToken sql.NullString `json:"-"`
}

type Contact struct {
	ID      string         `json:"id"`
	Email   string         `json:"email"`
	Name    sql.NullString `json:"name"`
	Company sql.NullString `json:"company"`
}

type Thread struct {
	ID                 string         `json:"id"`
	GmailThreadID      sql.NullString `json:"gmail_thread_id"`
	ContactID          string         `json:"contact_id"`
	OwnerID            string         `json:"owner_id"`
	Subject            string         `json:"subject"`
	Status             string         `json:"status"`
	AIMode             int            `json:"ai_mode"`
	ClaimedBy          sql.NullString `json:"claimed_by"`
	IsPrimary          int            `json:"is_primary"`
	Deliverability     int            `json:"deliv_score"`
	CreatedAt          int64          `json:"created_at"`
	UpdatedAt          int64          `json:"updated_at"`
	ContactName        sql.NullString `json:"contact_name,omitempty"`
	ContactEmail       sql.NullString `json:"contact_email,omitempty"`
	OwnerName          sql.NullString `json:"owner_name,omitempty"`
	ClaimedByName      sql.NullString `json:"claimed_by_name,omitempty"`
	MessageCount       int            `json:"message_count,omitempty"`
	LastMessagePreview sql.NullString `json:"last_message_preview,omitempty"`
}

type Message struct {
	ID               string         `json:"id"`
	ThreadID         string         `json:"thread_id"`
	GmailMsgID       sql.NullString `json:"gmail_msg_id"`
	Role             string         `json:"role"`
	FromName         string         `json:"from_name"`
	FromEmail        string         `json:"from_email"`
	Body             string         `json:"body"`
	MessageIDHeader  sql.NullString `json:"message_id_header"`
	ReferencesHeader sql.NullString `json:"references_header"`
	InReplyTo        sql.NullString `json:"in_reply_to"`
	SentAt           int64          `json:"sent_at"`
}
