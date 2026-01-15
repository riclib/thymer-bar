package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"thymer-bar/internal/sqlite"

	"github.com/nats-io/nats.go/jetstream"
)

// SQLiteProjector handles change detection and publishes change events.
type SQLiteProjector struct {
	db       *sqlite.DB
	js       jetstream.JetStream
	log      *slog.Logger
	progress *SyncProgress
}

// NewSQLiteProjector creates a new SQLite projector.
func NewSQLiteProjector(db *sqlite.DB, js jetstream.JetStream, log *slog.Logger) *SQLiteProjector {
	return &SQLiteProjector{
		db:  db,
		js:  js,
		log: log,
	}
}

// ProcessFetched handles a fetched event, performs change detection,
// and publishes a change event if the record has changed.
func (p *SQLiteProjector) ProcessFetched(ctx context.Context, event *FetchedEvent) (*ChangedEvent, error) {
	// Get the appropriate mapper
	mapper := GetMapper(event.Source)
	if mapper == nil {
		return nil, fmt.Errorf("no mapper for source: %s", event.Source)
	}

	// Map to normalized record
	mapped, err := mapper.Map(event)
	if err != nil {
		return nil, fmt.Errorf("mapping failed: %w", err)
	}

	// Compute hash of mapped record
	newHash := ComputeHash(mapped)

	// Get existing hash from SQLite
	oldHash, err := p.db.GetSourceRecordHash(ctx, event.Source, event.ExternalID)
	if err != nil {
		return nil, fmt.Errorf("failed to get existing hash: %w", err)
	}

	// Determine change type
	var changeType string
	if oldHash == "" {
		changeType = "created"
	} else if oldHash != newHash {
		changeType = "updated"
	} else {
		// No change
		p.log.Debug("record unchanged", "source", event.Source, "external_id", event.ExternalID)
		return nil, nil
	}

	// Update source_records with new hash
	sourceUpdatedAt := event.UpdatedAt
	if err := p.db.UpsertSourceRecord(ctx, &sqlite.SourceRecord{
		Source:          event.Source,
		ExternalID:      event.ExternalID,
		ContentHash:     newHash,
		SourceUpdatedAt: &sourceUpdatedAt,
	}); err != nil {
		return nil, fmt.Errorf("failed to update source record: %w", err)
	}

	// Create change event
	changeEvent := &ChangedEvent{
		ChangeType: changeType,
		Record:     *mapped,
		OldHash:    oldHash,
		NewHash:    newHash,
		ChangedAt:  time.Now(),
	}

	p.log.Info("record changed",
		"type", changeType,
		"source", event.Source,
		"external_id", event.ExternalID,
		"title", mapped.Title)

	return changeEvent, nil
}

// PublishChange publishes a change event to JetStream.
func (p *SQLiteProjector) PublishChange(ctx context.Context, event *ChangedEvent) error {
	data, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal change event: %w", err)
	}

	_, err = p.js.Publish(ctx, SubjectChanged, data)
	if err != nil {
		return fmt.Errorf("failed to publish change event: %w", err)
	}

	return nil
}

// ThymerProjector renders changed records to Thymer.
type ThymerProjector struct {
	thymer ThymerClient
	db     *sqlite.DB
	js     jetstream.JetStream
	log    *slog.Logger
}

// ThymerClient interface for Thymer operations.
type ThymerClient interface {
	Available() bool
	FindRecord(ctx context.Context, collection, externalID string) (*ThymerRecord, error)
	CreateRecord(ctx context.Context, collection, name string, fields map[string]any) (string, error)
	UpdateRecord(ctx context.Context, guid string, fields map[string]any) error
}

// ThymerRecord represents a record in Thymer.
type ThymerRecord struct {
	GUID   string
	Fields map[string]any
}

// NewThymerProjector creates a new Thymer projector.
func NewThymerProjector(thymer ThymerClient, db *sqlite.DB, js jetstream.JetStream, log *slog.Logger) *ThymerProjector {
	return &ThymerProjector{
		thymer: thymer,
		db:     db,
		js:     js,
		log:    log,
	}
}

// ProcessChange handles a change event and renders to Thymer.
func (p *ThymerProjector) ProcessChange(ctx context.Context, event *ChangedEvent) error {
	if !p.thymer.Available() {
		return fmt.Errorf("thymer not connected")
	}

	record := &event.Record

	// Build fields for Thymer
	fields := map[string]any{
		"external_id":    record.ExternalID,
		"title":          record.Title,
		"source":         record.Source,
		"external_state": record.ExternalState,
		"type":           record.Type,
		"url":            record.URL,
		"repo":           record.Repo,
		"number":         record.Number,
		"author":         record.Author,
		"assignee":       record.Assignee,
		"updated_at":     record.UpdatedAt,
		"created_at":     record.CreatedAt,
	}

	// Find existing record in Thymer
	existing, err := p.thymer.FindRecord(ctx, record.Collection, record.ExternalID)
	if err != nil {
		return fmt.Errorf("failed to find record in Thymer: %w", err)
	}

	var thymerGUID string
	var renderType string

	if existing == nil {
		// New record - set initial status based on external state
		if record.ExternalState == "closed" {
			fields["status"] = "done"
		} else {
			fields["status"] = "inbox"
		}

		guid, err := p.thymer.CreateRecord(ctx, record.Collection, record.Title, fields)
		if err != nil {
			return fmt.Errorf("failed to create record in Thymer: %w", err)
		}
		thymerGUID = guid
		renderType = "created"

		p.log.Info("created in Thymer",
			"guid", guid,
			"title", record.Title,
			"status", fields["status"])
	} else {
		// Existing record - check for auto-close
		oldExternalState, _ := existing.Fields["external_state"].(string)
		currentStatus, _ := existing.Fields["status"].(string)

		// If external state changed to closed and not already cancelled, mark as done
		if record.ExternalState == "closed" && oldExternalState != "closed" {
			if currentStatus != "cancelled" {
				fields["status"] = "done"
				p.log.Info("auto-closing in Thymer",
					"guid", existing.GUID,
					"title", record.Title,
					"was", currentStatus)
			}
		}
		// Otherwise don't touch status - preserve user's choice

		if err := p.thymer.UpdateRecord(ctx, existing.GUID, fields); err != nil {
			return fmt.Errorf("failed to update record in Thymer: %w", err)
		}
		thymerGUID = existing.GUID
		renderType = "updated"

		p.log.Info("updated in Thymer",
			"guid", existing.GUID,
			"title", record.Title)
	}

	// Publish rendered event
	if p.js != nil {
		renderedEvent := &RenderedEvent{
			Source:     record.Source,
			ExternalID: record.ExternalID,
			ThymerGUID: thymerGUID,
			ChangeType: renderType,
			RenderedAt: time.Now(),
		}
		data, _ := json.Marshal(renderedEvent)
		p.js.Publish(ctx, SubjectRendered, data)
	}

	return nil
}
