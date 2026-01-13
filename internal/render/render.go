// Package render provides the renderer interface for outputting records to various destinations.
package render

import (
	"context"

	"thymer-bar/internal/sync"
)

// Renderer outputs records to a destination (Thymer, Obsidian, Lifelog, etc.)
type Renderer interface {
	// Name returns the unique identifier for this renderer.
	Name() string

	// DisplayName returns a human-readable name.
	DisplayName() string

	// Render outputs a record to the destination.
	// Returns the destination's ID for the record (e.g., Thymer UUID, file path, post ID).
	Render(ctx context.Context, record *sync.Record) (string, error)

	// RenderEvent outputs an event (for event-sourced destinations like Lifelog).
	RenderEvent(ctx context.Context, event *Event) error

	// Available returns true if the destination is currently reachable.
	Available() bool

	// Configure applies configuration to the renderer.
	Configure(cfg map[string]any) error
}

// Event represents an event to be rendered (for event-sourced systems).
type Event struct {
	Type      string         `json:"type"`      // task.started, task.completed, note.added, etc.
	RecordID  string         `json:"record_id"` // Related record
	Timestamp string         `json:"timestamp"` // ISO8601
	Data      map[string]any `json:"data"`      // Event-specific data
}

// Registry manages renderers.
type Registry struct {
	renderers map[string]Renderer
}

// NewRegistry creates a new renderer registry.
func NewRegistry() *Registry {
	return &Registry{
		renderers: make(map[string]Renderer),
	}
}

// Register adds a renderer to the registry.
func (r *Registry) Register(renderer Renderer) {
	r.renderers[renderer.Name()] = renderer
}

// Get returns a renderer by name.
func (r *Registry) Get(name string) Renderer {
	return r.renderers[name]
}

// All returns all registered renderers.
func (r *Registry) All() []Renderer {
	renderers := make([]Renderer, 0, len(r.renderers))
	for _, renderer := range r.renderers {
		renderers = append(renderers, renderer)
	}
	return renderers
}

// Available returns all available (reachable) renderers.
func (r *Registry) Available() []Renderer {
	renderers := make([]Renderer, 0)
	for _, renderer := range r.renderers {
		if renderer.Available() {
			renderers = append(renderers, renderer)
		}
	}
	return renderers
}

// RenderToAll renders a record to all available renderers.
// Returns a map of renderer name -> destination ID.
func (r *Registry) RenderToAll(ctx context.Context, record *sync.Record) map[string]string {
	results := make(map[string]string)
	for _, renderer := range r.Available() {
		if id, err := renderer.Render(ctx, record); err == nil {
			results[renderer.Name()] = id
		}
	}
	return results
}
