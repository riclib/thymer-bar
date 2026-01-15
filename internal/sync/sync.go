// Package sync provides the sync engine interface and registry.
package sync

import (
	"context"
	"time"
)

// Engine is the interface that sync engines must implement.
type Engine interface {
	// Name returns the unique identifier for this engine.
	Name() string

	// DisplayName returns a human-readable name.
	DisplayName() string

	// Sync performs a sync operation.
	Sync(ctx context.Context) error

	// Configure applies configuration to the engine.
	Configure(cfg map[string]any) error

	// Enabled returns true if the engine is configured and enabled.
	Enabled() bool
}

// Record represents a generic record from any sync source.
type Record struct {
	ID          string         `json:"id"`
	Source      string         `json:"source"`
	ExternalID  string         `json:"external_id"`
	Type        string         `json:"type"`                  // issue, pr, highlight, event, etc.
	Title       string         `json:"title"`
	Content     string         `json:"content"`
	URL         string         `json:"url,omitempty"`
	Fields      map[string]any `json:"fields"`                // Source-specific fields
	CreatedAt   time.Time      `json:"created_at"`
	UpdatedAt   time.Time      `json:"updated_at"`
	CompletedAt *time.Time     `json:"completed_at,omitempty"` // When closed/merged/done
}

// Event represents a sync event published to NATS.
type Event struct {
	Type      string    `json:"type"`      // record.created, record.updated, record.deleted
	Source    string    `json:"source"`    // github, readwise, etc.
	Record    *Record   `json:"record"`
	Timestamp time.Time `json:"timestamp"`
}

// Registry manages sync engines.
type Registry struct {
	engines map[string]Engine
}

// NewRegistry creates a new engine registry.
func NewRegistry() *Registry {
	return &Registry{
		engines: make(map[string]Engine),
	}
}

// Register adds an engine to the registry.
func (r *Registry) Register(e Engine) {
	r.engines[e.Name()] = e
}

// Get returns an engine by name.
func (r *Registry) Get(name string) Engine {
	return r.engines[name]
}

// All returns all registered engines.
func (r *Registry) All() []Engine {
	engines := make([]Engine, 0, len(r.engines))
	for _, e := range r.engines {
		engines = append(engines, e)
	}
	return engines
}

// Enabled returns all enabled engines.
func (r *Registry) Enabled() []Engine {
	engines := make([]Engine, 0)
	for _, e := range r.engines {
		if e.Enabled() {
			engines = append(engines, e)
		}
	}
	return engines
}
