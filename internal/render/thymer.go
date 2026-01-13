package render

import (
	"context"

	"thymer-bar/internal/sync"
	"thymer-bar/internal/thymer"
)

// ThymerRenderer renders records to Thymer via WebSocket.
type ThymerRenderer struct {
	client     *thymer.Client
	collection string // Default collection to render to
	enabled    bool
}

// NewThymerRenderer creates a new Thymer renderer.
func NewThymerRenderer(client *thymer.Client) *ThymerRenderer {
	return &ThymerRenderer{
		client:     client,
		collection: "issues", // Default
		enabled:    true,
	}
}

func (r *ThymerRenderer) Name() string        { return "thymer" }
func (r *ThymerRenderer) DisplayName() string { return "Thymer" }
func (r *ThymerRenderer) Available() bool     { return r.enabled && r.client.Available() }

func (r *ThymerRenderer) Configure(cfg map[string]any) error {
	if collection, ok := cfg["collection"].(string); ok {
		r.collection = collection
	}
	if enabled, ok := cfg["enabled"].(bool); ok {
		r.enabled = enabled
	}
	return nil
}

func (r *ThymerRenderer) Render(ctx context.Context, record *sync.Record) (string, error) {
	// Determine collection based on record type
	collection := r.collectionForRecord(record)

	// Check if record already exists in Thymer
	existing, err := r.client.FindRecord(ctx, collection, record.ExternalID)
	if err != nil {
		return "", err
	}

	fields := r.recordToFields(record)

	if existing != nil {
		// Update existing record
		if err := r.client.UpdateRecord(ctx, existing.GUID, fields); err != nil {
			return "", err
		}
		return existing.GUID, nil
	}

	// Create new record
	guid, err := r.client.CreateRecord(ctx, collection, record.Title, fields)
	if err != nil {
		return "", err
	}

	return guid, nil
}

func (r *ThymerRenderer) RenderEvent(ctx context.Context, event *Event) error {
	// Thymer doesn't store events directly - they're projected to record state
	// Events like task.completed update the record's status field
	return nil
}

// collectionForRecord determines the Thymer collection for a record.
func (r *ThymerRenderer) collectionForRecord(record *sync.Record) string {
	switch record.Source {
	case "github":
		return "issues"
	case "readwise":
		return "captures"
	case "calendar":
		return "calendar"
	default:
		return r.collection
	}
}

// recordToFields converts a sync record to Thymer fields.
func (r *ThymerRenderer) recordToFields(record *sync.Record) map[string]any {
	fields := map[string]any{
		"external_id": record.ExternalID,
		"url":         record.URL,
	}

	// Copy source-specific fields
	for k, v := range record.Fields {
		fields[k] = v
	}

	return fields
}
