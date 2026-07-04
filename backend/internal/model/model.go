// Package model defines the domain types shared by every layer. These shapes
// are the contract between the API, the store, and the pager worker — change
// them deliberately.
package model

import "time"

// PageState is the page lifecycle. Transitions only move forward:
// created → pushed → delivered → seen → acked, or out to expired/cancelled.
type PageState string

const (
	PageCreated   PageState = "created"
	PagePushed    PageState = "pushed"
	PageDelivered PageState = "delivered"
	PageSeen      PageState = "seen"
	PageAcked     PageState = "acked"
	PageExpired   PageState = "expired"
	PageCancelled PageState = "cancelled"
)

// Active reports whether the nag loop should keep pushing.
func (s PageState) Active() bool {
	switch s {
	case PageCreated, PagePushed, PageDelivered, PageSeen:
		return true
	}
	return false
}

type PairingStatus string

const (
	PairingActive  PairingStatus = "active"
	PairingBlocked PairingStatus = "blocked"
)

type User struct {
	ID          string    `json:"id"` // Cognito sub
	DisplayName string    `json:"display_name"`
	CreatedAt   time.Time `json:"created_at"`
}

type Device struct {
	UserID    string    `json:"user_id"`
	Token     string    `json:"token"` // FCM registration token
	Platform  string    `json:"platform"` // "ios" | "android"
	UpdatedAt time.Time `json:"updated_at"`
}

// Pairing is stored under BOTH users' partitions (written transactionally) so
// either side can read its contact list with one Query.
type Pairing struct {
	UserID    string        `json:"user_id"` // owner of this copy
	PeerID    string        `json:"peer_id"`
	PeerName  string        `json:"peer_name"` // denormalized display name
	Status    PairingStatus `json:"status"`
	CreatedAt time.Time     `json:"created_at"`
}

type Invite struct {
	Code      string    `json:"code"`
	CreatorID string    `json:"creator_id"`
	ExpiresAt time.Time `json:"expires_at"` // also the DynamoDB TTL
}

type Page struct {
	ID             string    `json:"id"`
	SenderID       string    `json:"sender_id"`
	RecipientID    string    `json:"recipient_id"`
	Message        string    `json:"message"`
	State          PageState `json:"state"`
	Attempt        int       `json:"attempt"` // last push attempt number
	IdempotencyKey string    `json:"-"`
	CreatedAt      time.Time `json:"created_at"`
	ExpiresAt      time.Time `json:"expires_at"`
	AckedAt        *time.Time `json:"acked_at,omitempty"`
	AckNote        string    `json:"ack_note,omitempty"` // "on my way", free text
}

// PageEvent is the append-only timeline shown to the sender.
type PageEvent struct {
	PageID string    `json:"page_id"`
	Type   string    `json:"type"` // created|push_attempt|delivered|seen|acked|expired|cancelled
	Detail string    `json:"detail,omitempty"`
	At     time.Time `json:"at"`
}
