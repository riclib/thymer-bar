// Package pipeline defines the sync event pipeline types and subjects.
package pipeline

import "time"

// JetStream subjects for the sync pipeline
const (
	// SubjectFetched is published when records are fetched from a source.
	// Format: sync.{source}.fetched (e.g., sync.github.fetched)
	SubjectFetched = "sync.%s.fetched"

	// SubjectChanged is published when a record has actually changed.
	// This is source-agnostic - all sources publish here after change detection.
	SubjectChanged = "sync.records.changed"

	// SubjectRendered is published after a record is rendered to Thymer.
	SubjectRendered = "sync.records.rendered"
)

// FetchedEvent is published when records are fetched from a source.
// This contains the raw-ish data before mapping.
type FetchedEvent struct {
	Source     string    `json:"source"`      // github, linear, calendar, etc.
	ExternalID string    `json:"external_id"` // ID in source system
	Type       string    `json:"type"`        // issue, pr, event, etc.
	FetchedAt  time.Time `json:"fetched_at"`

	// Raw fields from the source (will be mapped)
	Title     string         `json:"title"`
	Content   string         `json:"content,omitempty"`
	URL       string         `json:"url,omitempty"`
	State     string         `json:"state,omitempty"` // open, closed, merged
	Fields    map[string]any `json:"fields"`          // Source-specific fields
	CreatedAt time.Time      `json:"created_at"`
	UpdatedAt time.Time      `json:"updated_at"`
}

// MappedRecord is the normalized record after mapping from source format.
// This is what gets hashed for change detection.
type MappedRecord struct {
	Source     string `json:"source"`
	ExternalID string `json:"external_id"`
	Collection string `json:"collection"` // Issues, Calendar, etc.

	// Normalized fields (schema matches Thymer collections)
	Title         string    `json:"title"`
	Status        string    `json:"status,omitempty"`         // inbox, backlog, next, doing, done, cancelled
	ExternalState string    `json:"external_state,omitempty"` // open, closed (from source)
	Type          string    `json:"type,omitempty"`           // issue, pr, task, event
	URL           string    `json:"url,omitempty"`
	Repo          string    `json:"repo,omitempty"`
	Number        int       `json:"number,omitempty"`
	Author        string    `json:"author,omitempty"`
	Assignee      string    `json:"assignee,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`

	// Extended fields (source-specific, passed through)
	Extra map[string]any `json:"extra,omitempty"`
}

// ChangedEvent is published when a record has changed (new or updated).
type ChangedEvent struct {
	ChangeType string       `json:"change_type"` // created, updated, deleted
	Record     MappedRecord `json:"record"`
	OldHash    string       `json:"old_hash,omitempty"` // Previous hash (for updates)
	NewHash    string       `json:"new_hash"`           // Current hash
	ChangedAt  time.Time    `json:"changed_at"`
}

// RenderedEvent is published after a record is rendered to Thymer.
type RenderedEvent struct {
	Source     string    `json:"source"`
	ExternalID string    `json:"external_id"`
	ThymerGUID string    `json:"thymer_guid"`
	ChangeType string    `json:"change_type"` // created, updated
	RenderedAt time.Time `json:"rendered_at"`
}

// SyncProgress tracks sync operation progress for UI feedback.
type SyncProgress struct {
	Source     string    `json:"source"`
	Phase      string    `json:"phase"` // fetching, mapping, rendering
	Total      int       `json:"total,omitempty"`
	Processed  int       `json:"processed"`
	Created    int       `json:"created"`
	Updated    int       `json:"updated"`
	Unchanged  int       `json:"unchanged"`
	Errors     int       `json:"errors"`
	StartedAt  time.Time `json:"started_at"`
	LastUpdate time.Time `json:"last_update"`
}
