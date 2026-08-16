package store

import "time"

type WorkItem struct {
	ID               string     `json:"id"`
	Type             string     `json:"type"`
	Body             string     `json:"body"`
	Status           string     `json:"status"`
	AttemptCount     int        `json:"attempt_count"`
	MaxAttempts      int        `json:"max_attempts"`
	NextRetryAt      *time.Time `json:"next_retry_at,omitempty"`
	AssignedWorker   *string    `json:"assigned_worker,omitempty"`
	IdempotencyToken string     `json:"idempotency_token"`
	DeadLetterReason *string    `json:"dead_letter_reason,omitempty"`
	CreatedAt        time.Time  `json:"created_at"`
	UpdatedAt        time.Time  `json:"updated_at"`
}

type Event struct {
	ID              int       `json:"id"`
	Ts              time.Time `json:"ts"`
	EntityType      string    `json:"entity_type"`
	EntityID        string    `json:"entity_id"`
	Action          string    `json:"action"`
	Reason          *string   `json:"reason,omitempty"`
	CausedByEventID *int      `json:"caused_by_event_id,omitempty"`
}

type Release struct {
	ID              string    `json:"id"`
	Version         string    `json:"version"`
	PreviousVersion string    `json:"previous_version"`
	Status          string    `json:"status"`
	StartedAt       time.Time `json:"started_at"`
	WatchUntil      time.Time `json:"watch_until"`
}
