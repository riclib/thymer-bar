package pipeline

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"time"

	"thymer-bar/internal/sqlite"
	"thymer-bar/internal/sync"

	"github.com/nats-io/nats.go/jetstream"
)

// Orchestrator coordinates the sync pipeline.
type Orchestrator struct {
	db              *sqlite.DB
	js              jetstream.JetStream
	thymer          ThymerClient
	sqliteProjector *SQLiteProjector
	thymerProjector *ThymerProjector
	log             *slog.Logger

	// Progress callback for UI updates
	onProgress func(progress *SyncProgress)
}

// NewOrchestrator creates a new sync orchestrator.
func NewOrchestrator(db *sqlite.DB, js jetstream.JetStream, thymer ThymerClient, log *slog.Logger) *Orchestrator {
	o := &Orchestrator{
		db:     db,
		js:     js,
		thymer: thymer,
		log:    log,
	}

	o.sqliteProjector = NewSQLiteProjector(db, js, log)
	o.thymerProjector = NewThymerProjector(thymer, db, js, log)

	return o
}

// SetProgressCallback sets the callback for progress updates.
func (o *Orchestrator) SetProgressCallback(fn func(progress *SyncProgress)) {
	o.onProgress = fn
}

// SyncGitHub runs the GitHub sync pipeline.
func (o *Orchestrator) SyncGitHub(ctx context.Context, token string, repos []string) (*SyncProgress, error) {
	progress := &SyncProgress{
		Source:    "github",
		Phase:     "fetching",
		StartedAt: time.Now(),
	}
	o.updateProgress(progress)

	// Get last sync time for incremental fetch
	syncState, err := o.db.GetSyncState(ctx, "github")
	if err != nil {
		return progress, fmt.Errorf("failed to get sync state: %w", err)
	}

	// Create callback for processing fetched records
	var maxSourceUpdatedAt time.Time
	onRecord := func(ctx context.Context, record *sync.Record) error {
		// Track the newest source updated_at for next incremental sync
		if record.UpdatedAt.After(maxSourceUpdatedAt) {
			maxSourceUpdatedAt = record.UpdatedAt
		}

		// Convert sync.Record to FetchedEvent
		fetchedEvent := &FetchedEvent{
			Source:     record.Source,
			ExternalID: record.ExternalID,
			Type:       record.Type,
			FetchedAt:  time.Now(),
			Title:      record.Title,
			Content:    record.Content,
			URL:        record.URL,
			State:      getState(record),
			Fields:     record.Fields,
			CreatedAt:  record.CreatedAt,
			UpdatedAt:  record.UpdatedAt,
		}

		// Publish to JetStream (if available)
		if o.js != nil {
			subject := fmt.Sprintf(SubjectFetched, record.Source)
			data, _ := json.Marshal(fetchedEvent)
			if _, err := o.js.Publish(ctx, subject, data); err != nil {
				o.log.Warn("failed to publish fetched event", "error", err)
			}
		}

		// Process through SQLite projector (change detection)
		progress.Phase = "mapping"
		progress.Processed++
		o.updateProgress(progress)

		changeEvent, err := o.sqliteProjector.ProcessFetched(ctx, fetchedEvent)
		if err != nil {
			o.log.Error("failed to process fetched event", "error", err, "external_id", record.ExternalID)
			progress.Errors++
			return nil // Continue processing other records
		}

		if changeEvent == nil {
			// No change
			progress.Unchanged++
			o.updateProgress(progress)
			return nil
		}

		// Publish change event to JetStream
		if o.js != nil {
			if err := o.sqliteProjector.PublishChange(ctx, changeEvent); err != nil {
				o.log.Warn("failed to publish change event", "error", err)
			}
		}

		// Process through Thymer projector
		progress.Phase = "rendering"
		o.updateProgress(progress)

		if err := o.thymerProjector.ProcessChange(ctx, changeEvent); err != nil {
			o.log.Error("failed to render to Thymer", "error", err, "external_id", record.ExternalID)
			progress.Errors++
			return nil
		}

		if changeEvent.ChangeType == "created" {
			progress.Created++
		} else {
			progress.Updated++
		}
		o.updateProgress(progress)

		return nil
	}

	// Create and run GitHub engine
	engine := createGitHubEngine(token, repos, onRecord)

	// Run sync (with since time for incremental)
	if syncState.LastSourceUpdatedAt != nil {
		o.log.Info("running incremental sync", "since", syncState.LastSourceUpdatedAt)
		err = engine.SyncSince(ctx, syncState.LastSourceUpdatedAt)
	} else {
		o.log.Info("running full sync")
		err = engine.Sync(ctx)
	}

	if err != nil {
		progress.Phase = "error"
		o.updateProgress(progress)
		return progress, fmt.Errorf("github sync failed: %w", err)
	}

	// Update sync state with latest source timestamp
	if !maxSourceUpdatedAt.IsZero() {
		if err := o.db.UpdateSyncState(ctx, &sqlite.SyncState{
			Source:              "github",
			LastSourceUpdatedAt: &maxSourceUpdatedAt,
		}); err != nil {
			o.log.Warn("failed to update sync state", "error", err)
		}
	}

	progress.Phase = "complete"
	progress.LastUpdate = time.Now()
	o.updateProgress(progress)

	o.log.Info("sync completed",
		"source", "github",
		"processed", progress.Processed,
		"created", progress.Created,
		"updated", progress.Updated,
		"unchanged", progress.Unchanged,
		"errors", progress.Errors,
		"duration", time.Since(progress.StartedAt))

	return progress, nil
}

func (o *Orchestrator) updateProgress(progress *SyncProgress) {
	progress.LastUpdate = time.Now()
	if o.onProgress != nil {
		o.onProgress(progress)
	}
}

func getState(record *sync.Record) string {
	if state, ok := record.Fields["state"].(string); ok {
		return state
	}
	if record.CompletedAt != nil {
		return "closed"
	}
	return "open"
}

// createGitHubEngine creates a configured GitHub sync engine.
func createGitHubEngine(token string, repos []string, onRecord func(context.Context, *sync.Record) error) *githubEngine {
	return &githubEngine{
		token:    token,
		repos:    repos,
		onRecord: onRecord,
	}
}

// githubEngine is a minimal wrapper to avoid import cycles.
// It delegates to the actual github.Engine.
type githubEngine struct {
	token    string
	repos    []string
	onRecord func(context.Context, *sync.Record) error
}

func (e *githubEngine) Sync(ctx context.Context) error {
	return e.SyncSince(ctx, nil)
}

func (e *githubEngine) SyncSince(ctx context.Context, since *time.Time) error {
	// Import and use the actual github engine
	// We need to do this dynamically to avoid import cycles
	// For now, use the internal/sync/github package directly

	// This is a placeholder - the actual implementation will be wired in sync_api.go
	return fmt.Errorf("github engine not wired - use SyncGitHubWithEngine instead")
}

// SyncGitHubWithEngine runs sync using a pre-configured engine.
// This avoids import cycle issues.
func (o *Orchestrator) SyncGitHubWithEngine(ctx context.Context, engine sync.Engine) (*SyncProgress, error) {
	progress := &SyncProgress{
		Source:    "github",
		Phase:     "fetching",
		StartedAt: time.Now(),
	}
	o.updateProgress(progress)

	err := engine.Sync(ctx)

	if err != nil {
		progress.Phase = "error"
		o.updateProgress(progress)
		return progress, err
	}

	progress.Phase = "complete"
	progress.LastUpdate = time.Now()
	o.updateProgress(progress)

	return progress, nil
}
