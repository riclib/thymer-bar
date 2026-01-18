package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"thymer-bar/internal/bit"
	"thymer-bar/internal/config"
	"thymer-bar/internal/secrets"
	"thymer-bar/internal/sqlite"
	"thymer-bar/internal/sync/calendar"
	"thymer-bar/internal/sync/github"
	"thymer-bar/internal/sync/people"
	"thymer-bar/internal/ui"
)

// Sync source registry
var syncSources = []ui.SyncSource{
	{
		ID:          "github",
		Name:        "GitHub",
		Description: "Sync issues and pull requests from your repositories",
		Icon:        "github",
	},
	{
		ID:          "google-calendar",
		Name:        "Google Calendar",
		Description: "Sync events from your Google calendars",
		Icon:        "google",
	},
	{
		ID:          "google-people",
		Name:        "Google Contacts",
		Description: "Sync contacts from Google People",
		Icon:        "google",
	},
	{
		ID:          "readwise",
		Name:        "Readwise",
		Description: "Sync highlights and articles from Readwise",
		Icon:        "readwise",
	},
	{
		ID:          "linear",
		Name:        "Linear",
		Description: "Sync issues from Linear workspaces",
		Icon:        "linear",
	},
}

// Sync state (in-memory, persisted to state file)
type SyncState struct {
	mu       sync.RWMutex
	Sources  map[string]*SourceState `json:"sources"`
	History  []ui.SyncHistoryEntry   `json:"history"`
	filePath string
}

type SourceState struct {
	Enabled     bool       `json:"enabled"`
	LastSync    *time.Time `json:"last_sync,omitempty"`
	LastError   string     `json:"last_error,omitempty"`
	ItemCount   int        `json:"item_count"`
	ConfigItems []string   `json:"config_items,omitempty"`
}

var syncState *SyncState

func initSyncState() error {
	stateDir := config.StateDir()
	if err := os.MkdirAll(stateDir, 0755); err != nil {
		return err
	}

	syncState = &SyncState{
		Sources:  make(map[string]*SourceState),
		History:  []ui.SyncHistoryEntry{},
		filePath: filepath.Join(stateDir, "sync.json"),
	}

	// Load existing state
	data, err := os.ReadFile(syncState.filePath)
	if err == nil {
		json.Unmarshal(data, syncState)
	}

	// Ensure all sources have state
	for _, src := range syncSources {
		if syncState.Sources[src.ID] == nil {
			syncState.Sources[src.ID] = &SourceState{}
		}
	}

	return nil
}

func (s *SyncState) save() error {
	s.mu.RLock()
	defer s.mu.RUnlock()

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.filePath, data, 0644)
}

// GetSyncManagerHTML returns the rendered Sync Manager HTML.
func (a *App) GetSyncManagerHTML() (string, error) {
	if syncState == nil {
		if err := initSyncState(); err != nil {
			return "", err
		}
	}

	sources := a.getSyncSources()
	history := syncState.History

	var buf bytes.Buffer
	err := ui.SyncManager(sources, history).Render(context.Background(), &buf)
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

// GetSyncSourceInfoHTML returns the info panel HTML for a specific source.
func (a *App) GetSyncSourceInfoHTML(id string) (string, error) {
	sources := a.getSyncSources()

	for _, s := range sources {
		if s.ID == id {
			var buf bytes.Buffer
			err := ui.SyncInfoSource(s).Render(context.Background(), &buf)
			if err != nil {
				return "", err
			}
			return buf.String(), nil
		}
	}
	return "", fmt.Errorf("source not found: %s", id)
}

// GetSyncDefaultInfoHTML returns the default info panel HTML.
func (a *App) GetSyncDefaultInfoHTML() (string, error) {
	sources := a.getSyncSources()
	history := syncState.History

	var buf bytes.Buffer
	err := ui.SyncInfoDefault(sources, history).Render(context.Background(), &buf)
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

// ConnectSource initiates OAuth flow for a source.
func (a *App) ConnectSource(id string) error {
	switch id {
	case "github":
		return a.connectGitHub()
	case "google-calendar", "google-people":
		// Both use the same Google OAuth
		return a.connectGoogleCalendar()
	case "readwise":
		return a.connectReadwise()
	case "linear":
		return a.connectLinear()
	default:
		return fmt.Errorf("unknown source: %s", id)
	}
}

// DisconnectSource removes auth for a source.
func (a *App) DisconnectSource(id string) error {
	var tokenKeys []string
	switch id {
	case "github":
		tokenKeys = []string{secrets.GitHubToken}
	case "google-calendar", "google-people":
		// Both share Google OAuth - delete credentials and token
		tokenKeys = []string{secrets.GoogleCredentials, secrets.GoogleOAuthToken}
	case "readwise":
		tokenKeys = []string{secrets.ReadwiseToken}
	case "linear":
		tokenKeys = []string{secrets.LinearToken}
	default:
		return fmt.Errorf("unknown source: %s", id)
	}

	for _, key := range tokenKeys {
		secrets.Delete(key) // Ignore errors for missing keys
	}

	// Disable the source
	syncState.mu.Lock()
	if state := syncState.Sources[id]; state != nil {
		state.Enabled = false
		state.ConfigItems = nil
	}
	syncState.mu.Unlock()

	return syncState.save()
}

// SetSourceEnabled enables or disables a sync source.
func (a *App) SetSourceEnabled(id string, enabled bool) error {
	syncState.mu.Lock()
	if state := syncState.Sources[id]; state != nil {
		state.Enabled = enabled
	}
	syncState.mu.Unlock()

	return syncState.save()
}

// SyncNow triggers an immediate sync for a source.
func (a *App) SyncNow(id string) error {
	slog.Info("SyncNow called", "source", id)
	var err error
	switch id {
	case "github":
		err = a.syncGitHub()
	case "google-calendar":
		err = a.syncGoogleCalendar()
	case "google-people":
		err = a.syncGooglePeople()
	default:
		err = fmt.Errorf("sync not implemented for source: %s", id)
	}
	if err != nil {
		slog.Error("SyncNow failed", "source", id, "error", err)
	} else {
		slog.Info("SyncNow completed", "source", id)
	}
	return err
}

// syncGitHub runs the GitHub sync through the Bits pipeline.
func (a *App) syncGitHub() error {
	token, err := secrets.Get(secrets.GitHubToken)
	if err != nil || token == "" {
		return fmt.Errorf("GitHub token not configured")
	}

	// Get configured repos
	syncState.mu.RLock()
	var repos []string
	if state := syncState.Sources["github"]; state != nil {
		repos = state.ConfigItems
	}
	syncState.mu.RUnlock()

	if len(repos) == 0 {
		return fmt.Errorf("no repositories configured for sync")
	}

	startTime := time.Now()
	progress := &syncProgress{
		source: "github",
		phase:  "starting",
	}

	// Emit initial progress
	a.emitSyncProgress(progress)
	slog.Info("starting github sync", "repos", len(repos), "repo_list", repos)

	// Get last sync time for incremental fetch
	var since *time.Time
	if a.db != nil {
		if cursor, err := a.db.GetSyncCursorForSystem(context.Background(), "github"); err == nil && cursor.LastSourceUpdatedAt != nil {
			since = cursor.LastSourceUpdatedAt
			slog.Info("incremental sync", "since", since.Format(time.RFC3339))
		} else {
			slog.Info("full sync", "reason", "no previous sync state")
		}
	}

	var maxSourceUpdatedAt time.Time

	// Create callback that processes Bits through the pipeline
	onBit := func(ctx context.Context, b *bit.Bit) error {
		progress.processed++
		progress.phase = "fetching"
		a.emitSyncProgress(progress)

		// Track latest source timestamp for next incremental sync
		if b.UpdatedAt.After(maxSourceUpdatedAt) {
			maxSourceUpdatedAt = b.UpdatedAt
		}

		// Process through pipeline: hash check -> store -> render
		slog.Debug("processing bit", "num", progress.processed, "title", b.Title(), "uri", b.MasterURI)
		changed, err := a.processBitPipeline(ctx, b)
		if err != nil {
			progress.errors++
			slog.Error("failed to process bit", "uri", b.MasterURI, "error", err)
			return nil // Continue with other bits
		}

		if changed == "created" {
			progress.created++
			slog.Info("bit created", "title", b.Title())
		} else if changed == "updated" {
			progress.updated++
			slog.Info("bit updated", "title", b.Title())
		} else {
			progress.unchanged++
			slog.Debug("bit unchanged", "title", b.Title())
		}

		a.emitSyncProgress(progress)
		return nil
	}

	// Create and configure the GitHub engine
	engine := github.New(onBit)
	reposAny := make([]any, len(repos))
	for i, r := range repos {
		reposAny[i] = r
	}
	engine.Configure(map[string]any{
		"token":   token,
		"repos":   reposAny,
		"enabled": true,
	})

	// Run the sync
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	progress.phase = "fetching"
	a.emitSyncProgress(progress)

	var syncErr error
	if since != nil {
		syncErr = engine.SyncSince(ctx, since)
	} else {
		syncErr = engine.Sync(ctx)
	}

	// Update sync cursor with latest source timestamp
	if a.db != nil && !maxSourceUpdatedAt.IsZero() {
		a.db.UpdateSyncCursor(ctx, &sqlite.SyncCursor{
			System:              "github",
			LastSourceUpdatedAt: &maxSourceUpdatedAt,
		})
	}

	// Record results
	duration := time.Since(startTime)
	now := time.Now()
	status := "success"
	itemCount := progress.created + progress.updated
	message := fmt.Sprintf("Synced %d items (%d new, %d updated, %d unchanged)",
		itemCount, progress.created, progress.updated, progress.unchanged)

	if syncErr != nil {
		status = "error"
		message = syncErr.Error()
	} else if progress.errors > 0 {
		status = "partial"
		message = fmt.Sprintf("%s, %d errors", message, progress.errors)
	}

	progress.phase = "complete"
	a.emitSyncProgress(progress)

	slog.Info("sync complete", "status", status, "created", progress.created, "updated", progress.updated, "unchanged", progress.unchanged, "errors", progress.errors, "duration", duration.Round(time.Millisecond))

	// Update UI state
	syncState.mu.Lock()
	if state := syncState.Sources["github"]; state != nil {
		state.LastSync = &now
		state.ItemCount = itemCount
		if syncErr != nil {
			state.LastError = syncErr.Error()
		} else {
			state.LastError = ""
		}
	}

	syncState.History = append([]ui.SyncHistoryEntry{{
		Source:    "github",
		Timestamp: now,
		Duration:  duration,
		ItemCount: itemCount,
		Status:    status,
		Message:   message,
	}}, syncState.History...)

	if len(syncState.History) > 50 {
		syncState.History = syncState.History[:50]
	}
	syncState.mu.Unlock()

	if err := syncState.save(); err != nil {
		fmt.Printf("Failed to save sync state: %v\n", err)
	}

	return syncErr
}

// syncGoogleCalendar runs the Google Calendar sync through the Bits pipeline.
func (a *App) syncGoogleCalendar() error {
	creds, err := secrets.Get(secrets.GoogleCredentials)
	if err != nil || creds == "" {
		return fmt.Errorf("Google credentials not configured")
	}

	token, err := secrets.Get(secrets.GoogleOAuthToken)
	if err != nil || token == "" {
		return fmt.Errorf("Google OAuth not completed")
	}

	// Get configured calendars (empty = primary only)
	syncState.mu.RLock()
	var calendars []string
	if state := syncState.Sources["google-calendar"]; state != nil {
		calendars = state.ConfigItems
	}
	syncState.mu.RUnlock()

	startTime := time.Now()
	progress := &syncProgress{
		source: "google-calendar",
		phase:  "starting",
	}

	a.emitSyncProgress(progress)
	slog.Info("starting google calendar sync", "calendars", len(calendars))

	// Create callback that processes Bits through the pipeline
	onBit := func(ctx context.Context, b *bit.Bit) error {
		progress.processed++
		progress.phase = "fetching"
		a.emitSyncProgress(progress)

		slog.Debug("processing calendar bit", "num", progress.processed, "title", b.Title(), "uri", b.MasterURI)
		changed, err := a.processBitPipeline(ctx, b)
		if err != nil {
			progress.errors++
			slog.Error("failed to process bit", "uri", b.MasterURI, "error", err)
			return nil
		}

		if changed == "created" {
			progress.created++
		} else if changed == "updated" {
			progress.updated++
		} else {
			progress.unchanged++
		}

		a.emitSyncProgress(progress)
		return nil
	}

	// Create and configure the Calendar engine
	engine := calendar.New(onBit)
	calendarsAny := make([]any, len(calendars))
	for i, c := range calendars {
		calendarsAny[i] = c
	}
	engine.Configure(map[string]any{
		"credentials": creds,
		"token":       token,
		"calendars":   calendarsAny,
		"enabled":     true,
	})

	// Run the sync
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	progress.phase = "fetching"
	a.emitSyncProgress(progress)

	syncErr := engine.Sync(ctx)

	// Record results
	duration := time.Since(startTime)
	now := time.Now()
	status := "success"
	itemCount := progress.created + progress.updated
	message := fmt.Sprintf("Synced %d items (%d new, %d updated, %d unchanged)",
		itemCount, progress.created, progress.updated, progress.unchanged)

	if syncErr != nil {
		status = "error"
		message = syncErr.Error()
	} else if progress.errors > 0 {
		status = "partial"
		message = fmt.Sprintf("%s, %d errors", message, progress.errors)
	}

	progress.phase = "complete"
	a.emitSyncProgress(progress)

	slog.Info("calendar sync complete", "status", status, "created", progress.created, "updated", progress.updated, "unchanged", progress.unchanged, "errors", progress.errors, "duration", duration.Round(time.Millisecond))

	// Update UI state
	syncState.mu.Lock()
	if state := syncState.Sources["google-calendar"]; state != nil {
		state.LastSync = &now
		state.ItemCount = itemCount
		if syncErr != nil {
			state.LastError = syncErr.Error()
		} else {
			state.LastError = ""
		}
	}

	syncState.History = append([]ui.SyncHistoryEntry{{
		Source:    "google-calendar",
		Timestamp: now,
		Duration:  duration,
		ItemCount: itemCount,
		Status:    status,
		Message:   message,
	}}, syncState.History...)

	if len(syncState.History) > 50 {
		syncState.History = syncState.History[:50]
	}
	syncState.mu.Unlock()

	if err := syncState.save(); err != nil {
		fmt.Printf("Failed to save sync state: %v\n", err)
	}

	return syncErr
}

// syncGooglePeople runs the Google People (Contacts) sync through the Bits pipeline.
func (a *App) syncGooglePeople() error {
	creds, err := secrets.Get(secrets.GoogleCredentials)
	if err != nil || creds == "" {
		return fmt.Errorf("Google credentials not configured")
	}

	token, err := secrets.Get(secrets.GoogleOAuthToken)
	if err != nil || token == "" {
		return fmt.Errorf("Google OAuth not completed")
	}

	startTime := time.Now()
	progress := &syncProgress{
		source: "google-people",
		phase:  "starting",
	}

	a.emitSyncProgress(progress)
	slog.Info("starting google people sync")

	// Create callback that processes Bits through the pipeline
	onBit := func(ctx context.Context, b *bit.Bit) error {
		progress.processed++
		progress.phase = "fetching"
		a.emitSyncProgress(progress)

		slog.Debug("processing people bit", "num", progress.processed, "title", b.Title(), "uri", b.MasterURI)
		changed, err := a.processBitPipeline(ctx, b)
		if err != nil {
			progress.errors++
			slog.Error("failed to process bit", "uri", b.MasterURI, "error", err)
			return nil
		}

		if changed == "created" {
			progress.created++
		} else if changed == "updated" {
			progress.updated++
		} else {
			progress.unchanged++
		}

		a.emitSyncProgress(progress)
		return nil
	}

	// Create and configure the People engine
	engine := people.New(onBit)
	engine.Configure(map[string]any{
		"credentials": creds,
		"token":       token,
		"enabled":     true,
	})

	// Run the sync
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	progress.phase = "fetching"
	a.emitSyncProgress(progress)

	syncErr := engine.Sync(ctx)

	// Record results
	duration := time.Since(startTime)
	now := time.Now()
	status := "success"
	itemCount := progress.created + progress.updated
	message := fmt.Sprintf("Synced %d items (%d new, %d updated, %d unchanged)",
		itemCount, progress.created, progress.updated, progress.unchanged)

	if syncErr != nil {
		status = "error"
		message = syncErr.Error()
	} else if progress.errors > 0 {
		status = "partial"
		message = fmt.Sprintf("%s, %d errors", message, progress.errors)
	}

	progress.phase = "complete"
	a.emitSyncProgress(progress)

	slog.Info("people sync complete", "status", status, "created", progress.created, "updated", progress.updated, "unchanged", progress.unchanged, "errors", progress.errors, "duration", duration.Round(time.Millisecond))

	// Update UI state
	syncState.mu.Lock()
	if state := syncState.Sources["google-people"]; state != nil {
		state.LastSync = &now
		state.ItemCount = itemCount
		if syncErr != nil {
			state.LastError = syncErr.Error()
		} else {
			state.LastError = ""
		}
	}

	syncState.History = append([]ui.SyncHistoryEntry{{
		Source:    "google-people",
		Timestamp: now,
		Duration:  duration,
		ItemCount: itemCount,
		Status:    status,
		Message:   message,
	}}, syncState.History...)

	if len(syncState.History) > 50 {
		syncState.History = syncState.History[:50]
	}
	syncState.mu.Unlock()

	if err := syncState.save(); err != nil {
		fmt.Printf("Failed to save sync state: %v\n", err)
	}

	return syncErr
}

// syncProgress tracks sync progress for UI updates.
type syncProgress struct {
	source    string
	phase     string
	processed int
	created   int
	updated   int
	unchanged int
	errors    int
}

// emitSyncProgress sends progress update to the frontend.
func (a *App) emitSyncProgress(p *syncProgress) {
	// Emit to frontend via Wails runtime events
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "sync:progress", map[string]any{
			"source":    p.source,
			"phase":     p.phase,
			"processed": p.processed,
			"created":   p.created,
			"updated":   p.updated,
			"unchanged": p.unchanged,
			"errors":    p.errors,
		})
	}
}

// processBitPipeline processes a Bit through the sync pipeline.
// Returns "created", "updated", or "" (unchanged).
func (a *App) processBitPipeline(ctx context.Context, b *bit.Bit) (string, error) {
	// Check for existing Bit by master_uri
	var existing *bit.Bit
	if a.db != nil {
		existing, _ = a.db.GetBitByURI(ctx, b.MasterURI)
	}

	// Determine change type
	changeType := ""
	if existing == nil {
		changeType = "created"
	} else if existing.Hash != b.Hash {
		changeType = "updated"
	} else {
		// No change
		return "", nil
	}

	// Store the Bit
	if a.db != nil {
		if err := a.db.UpsertBit(ctx, b); err != nil {
			return changeType, fmt.Errorf("failed to store bit: %w", err)
		}
	}

	// Render to Thymer if available
	if a.thymer != nil && a.thymer.Available() && b.CanSyncToThymer() {
		ref, err := a.renderBitToThymer(ctx, b, changeType)
		if err != nil {
			slog.Warn("failed to render to thymer", "uri", b.MasterURI, "error", err)
			// Don't fail the pipeline - bit is stored, thymer can be synced later
		} else if ref != nil && a.db != nil {
			a.db.UpsertRef(ctx, ref)
		}
	}

	return changeType, nil
}

// renderBitToThymer syncs a Bit to Thymer using the upsert-based SyncRecord.
// Returns the Ref for storage.
func (a *App) renderBitToThymer(ctx context.Context, b *bit.Bit, changeType string) (*bit.Ref, error) {
	if a.thymer == nil || !a.thymer.Available() {
		return nil, fmt.Errorf("thymer not connected")
	}

	// Determine collection from Bit type
	collection := a.collectionFromBit(b)

	slog.Debug("syncing to thymer", "uri", b.MasterURI, "collection", collection, "change", changeType)

	// Build fields based on collection type
	var fields map[string]any
	switch collection {
	case "Calendar":
		fields = a.buildCalendarFields(b)
	case "People":
		fields = a.buildPeopleFields(b)
	case "Issues":
		fields = a.buildIssuesFields(b, changeType)
	default:
		fields = a.buildNotesFields(b)
	}

	// SyncRecord handles the upsert - Thymer decides if it's create or update
	// Use master_uri as the external_id for unique identification
	result, err := a.thymer.SyncRecord(ctx, collection, b.MasterURI, b.Title(), fields)
	if err != nil {
		slog.Error("syncRecord failed", "error", err)
		return nil, err
	}

	slog.Info("syncRecord success", "guid", result.GUID, "action", result.Action)

	// Create ref with bidirectional hash tracking
	ref := bit.NewRef(b.MasterURI, bit.SystemThymer, result.GUID)
	ref.MarkPushed(b.Hash)

	return ref, nil
}

// buildIssuesFields builds Thymer fields for GitHub Issues/PRs.
func (a *App) buildIssuesFields(b *bit.Bit, changeType string) map[string]any {
	// Map external state for Thymer
	externalState := "Open"
	if state := b.GetString("state"); state == "closed" || state == "merged" {
		externalState = "Closed"
	}
	if b.IsCompleted() {
		externalState = "Closed"
	}

	// Map type for Thymer
	bitType := b.GetString("type")
	thymerType := "Issue"
	if bitType == "pr" || bitType == "pull_request" {
		thymerType = "PR"
	}

	// Get first assignee
	assignee := ""
	if assignees := b.GetStrings("assignees"); len(assignees) > 0 {
		assignee = assignees[0]
	}

	// Format markdown content for better readability
	formattedContent := ensureBlankLinesBeforeHeadings(b.Content)

	fields := map[string]any{
		"title":          b.Title(),
		"source":         "GitHub",
		"external_state": externalState,
		"type":           thymerType,
		"url":            b.GetString("url"),
		"repo":           b.GetString("repo"),
		"number":         b.GetInt("number"),
		"author":         b.GetString("author"),
		"assignee":       assignee,
		"updated_at":     b.UpdatedAt,
		"created_at":     b.CreatedAt,
		"content":        formattedContent,
	}

	// Set initial status for new records based on external state
	if changeType == "created" {
		if externalState == "Closed" {
			fields["status"] = "Done"
		} else {
			fields["status"] = "Inbox"
		}
	}

	return fields
}

// buildCalendarFields builds Thymer fields for Calendar events.
func (a *App) buildCalendarFields(b *bit.Bit) map[string]any {
	fields := map[string]any{
		"title":       b.Title(),
		"external_id": b.GetString("external_id"),
		"content":     b.Content,
	}

	// Map source: google-calendar -> google (choice ID)
	source := b.GetString("source")
	if source == "google-calendar" {
		fields["source"] = "google"
	} else if source != "" {
		fields["source"] = source
	}

	// Calendar name
	if cal := b.GetString("calendar"); cal != "" {
		fields["calendar"] = cal
	}

	// Location
	if loc := b.GetString("location"); loc != "" {
		fields["location"] = loc
	}

	// Status (confirmed/tentative/cancelled)
	if status := b.GetString("status"); status != "" {
		fields["status"] = status
	}

	// Timing (upcoming/past)
	if timing := b.GetString("timing"); timing != "" {
		fields["timing"] = timing
	}

	// All day flag (choice: yes/no)
	if b.GetBool("all_day") {
		fields["all_day"] = "yes"
	} else {
		fields["all_day"] = "no"
	}

	// Build time_period as DateTimeValue with range support
	timePeriod := a.buildTimePeriod(b)
	if timePeriod != nil {
		fields["time_period"] = timePeriod
	}

	// Meet link
	if meetLink := b.GetString("meet_link"); meetLink != "" {
		fields["meet_link"] = meetLink
	}

	// Event URL
	if url := b.GetString("url"); url != "" {
		fields["url"] = url
	}

	// Attendees (comma-separated string)
	if attendees := b.GetString("attendees"); attendees != "" {
		fields["attendees"] = attendees
	}

	return fields
}

// buildTimePeriod constructs a Thymer DateTimeValue with optional range.
// Format: {"d": "YYYYMMDD", "t": {"t": "HHMM", "tz": offset}, "r": {...end...}}
func (a *App) buildTimePeriod(b *bit.Bit) map[string]any {
	isAllDay := b.GetBool("all_day")

	// Get start time
	var startTime time.Time
	if t := b.GetTime("start_time"); !t.IsZero() {
		startTime = t
	} else if dateStr := b.GetString("start_date"); dateStr != "" {
		// Parse date string like "2024-01-15"
		if t, err := time.Parse("2006-01-02", dateStr); err == nil {
			startTime = t
		}
	}

	if startTime.IsZero() {
		return nil
	}

	// Build start DateTimeValue
	dtv := map[string]any{
		"d": startTime.Format("20060102"), // YYYYMMDD
	}

	// Add time component if not all-day
	if !isAllDay {
		_, offset := startTime.Zone()
		offsetMinutes := offset / 60
		dtv["t"] = map[string]any{
			"t":  startTime.Format("1504"), // HHMM
			"tz": offsetMinutes,
		}
	}

	// Get end time for range
	var endTime time.Time
	if t := b.GetTime("end_time"); !t.IsZero() {
		endTime = t
	} else if dateStr := b.GetString("end_date"); dateStr != "" {
		if t, err := time.Parse("2006-01-02", dateStr); err == nil {
			endTime = t
		}
	}

	// Add range if end time differs from start
	if !endTime.IsZero() && !endTime.Equal(startTime) {
		rangeEnd := map[string]any{
			"d": endTime.Format("20060102"),
		}

		if !isAllDay {
			_, offset := endTime.Zone()
			offsetMinutes := offset / 60
			rangeEnd["t"] = map[string]any{
				"t":  endTime.Format("1504"),
				"tz": offsetMinutes,
			}
		}

		dtv["r"] = rangeEnd
	}

	return dtv
}

// buildPeopleFields builds Thymer fields for People/Contacts.
func (a *App) buildPeopleFields(b *bit.Bit) map[string]any {
	fields := map[string]any{
		"title":        b.Title(),
		"relationship": "contact", // Default relationship
		"content":      b.Content,
	}

	// Email
	if email := b.GetString("email"); email != "" {
		fields["email"] = email
	}

	// Phone
	if phone := b.GetString("phone"); phone != "" {
		fields["phone"] = phone
	}

	// Company (from organization)
	if org := b.GetString("organization"); org != "" {
		fields["company"] = org
	}

	// Role (from job_title)
	if title := b.GetString("job_title"); title != "" {
		fields["role"] = title
	}

	// Birthday (from anniversary - note: Google stores anniversary, not birthday separately)
	// The People API has a "birthdays" field we could add later
	if anniversary := b.GetString("anniversary"); anniversary != "" {
		// Anniversary might be in format YYYY-MM-DD or --MM-DD
		fields["birthday"] = anniversary
	}

	return fields
}

// buildNotesFields builds generic Thymer fields for Notes.
func (a *App) buildNotesFields(b *bit.Bit) map[string]any {
	return map[string]any{
		"title":   b.Title(),
		"content": ensureBlankLinesBeforeHeadings(b.Content),
	}
}

// collectionFromBit determines the Thymer collection for a Bit.
func (a *App) collectionFromBit(b *bit.Bit) string {
	// Duck typing: check frontmatter fields
	bitType := b.GetString("type")

	// GitHub issues/PRs
	if b.HasField("repo") && b.HasField("number") {
		return "Issues"
	}

	// Calendar events
	if bitType == "event" || b.HasField("start_time") || b.HasField("start_date") {
		return "Calendar"
	}

	// People/Contacts
	if bitType == "contact" || b.GetString("source") == "google-people" {
		return "People"
	}

	// Default to Notes
	return "Notes"
}

// ensureBlankLinesBeforeHeadings adds a blank line before markdown headings
// (lines starting with #, ##, ###, etc.) unless they're at the start or
// already preceded by a blank line. This helps Thymer's markdown parser
// properly recognize headings as separate blocks.
func ensureBlankLinesBeforeHeadings(content string) string {
	if content == "" {
		return content
	}

	lines := strings.Split(content, "\n")
	var result []string

	for i, line := range lines {
		// Check if this line is a heading (starts with #)
		trimmed := strings.TrimLeft(line, " \t")
		isHeading := strings.HasPrefix(trimmed, "#")

		// If it's a heading and not the first line, ensure blank line before
		if isHeading && i > 0 {
			// Check if we already have a blank line
			if len(result) > 0 && strings.TrimSpace(result[len(result)-1]) != "" {
				result = append(result, "")
			}
		}

		result = append(result, line)
	}

	return strings.Join(result, "\n")
}

// SyncAll triggers sync for all enabled sources.
func (a *App) SyncAll() error {
	sources := a.getSyncSources()
	var synced int
	for _, s := range sources {
		slog.Info("sync all: checking source", "id", s.ID, "enabled", s.Enabled, "auth", s.AuthStatus)
		if s.Enabled && s.AuthStatus == ui.AuthAuthenticated {
			slog.Info("sync all: syncing source", "id", s.ID)
			if err := a.SyncNow(s.ID); err != nil {
				slog.Error("sync all: source failed", "id", s.ID, "error", err)
				// Continue with other sources instead of failing completely
			} else {
				synced++
			}
		}
	}
	slog.Info("sync all: completed", "synced", synced, "total", len(sources))
	return nil
}

// ResyncAll clears all data and performs a full sync from all sources.
// This clears bits, refs, and sync_cursors, then fetches everything fresh.
// Unlike SyncAll, this syncs ALL authenticated sources regardless of enabled status.
func (a *App) ResyncAll() error {
	ctx := context.Background()

	slog.Info("resync all: clearing database")

	// Clear all tables
	if a.db != nil {
		if err := a.db.ClearAllForResync(ctx); err != nil {
			return fmt.Errorf("failed to clear database: %w", err)
		}
	}

	slog.Info("resync all: starting fresh sync of all authenticated sources")

	// Sync ALL authenticated sources (not just enabled ones)
	sources := a.getSyncSources()
	var synced int
	var lastErr error
	for _, s := range sources {
		slog.Info("resync all: checking source", "id", s.ID, "auth", s.AuthStatus)
		if s.AuthStatus == ui.AuthAuthenticated {
			slog.Info("resync all: syncing source", "id", s.ID)
			if err := a.SyncNow(s.ID); err != nil {
				slog.Error("resync all: source failed", "id", s.ID, "error", err)
				lastErr = err
				// Continue with other sources
			} else {
				synced++
			}
		}
	}
	slog.Info("resync all: completed", "synced", synced, "total", len(sources))
	return lastErr
}

// ResyncSource clears data for a single source and performs a fresh sync.
func (a *App) ResyncSource(id string) error {
	ctx := context.Background()

	slog.Info("resync source: clearing data", "source", id)

	// Clear bits for this source
	if a.db != nil {
		if err := a.db.ClearBitsForSource(ctx, id); err != nil {
			return fmt.Errorf("failed to clear source data: %w", err)
		}
	}

	slog.Info("resync source: starting fresh sync", "source", id)

	// Sync the source
	return a.SyncNow(id)
}

// RerenderSource re-renders all bits from a single source to Thymer.
func (a *App) RerenderSource(id string) error {
	ctx := context.Background()

	slog.Info("rerender source: starting", "source", id)

	if a.db == nil {
		return fmt.Errorf("database not initialized")
	}

	// Get all bits for this source (source ID = system name)
	bits, err := a.db.GetBitsBySystem(ctx, id)
	if err != nil {
		return fmt.Errorf("failed to get bits: %w", err)
	}

	slog.Info("rerender source: found bits", "source", id, "count", len(bits))

	var rendered int
	for _, b := range bits {
		if a.thymer != nil && a.thymer.Available() && b.CanSyncToThymer() {
			if _, err := a.renderBitToThymer(ctx, b, "updated"); err != nil {
				slog.Warn("failed to render bit", "uri", b.MasterURI, "error", err)
			} else {
				rendered++
			}
		}
	}

	slog.Info("rerender source: completed", "source", id, "rendered", rendered)
	return nil
}

// RerenderAll clears refs and re-renders all existing bits to Thymer.
// This doesn't fetch from sources - it just pushes existing bits to Thymer.
func (a *App) RerenderAll() error {
	ctx := context.Background()

	if a.db == nil {
		return fmt.Errorf("database not initialized")
	}

	if a.thymer == nil || !a.thymer.Available() {
		return fmt.Errorf("thymer not connected")
	}

	// Get count before clearing
	bitCount, err := a.db.CountBits(ctx)
	if err != nil {
		return fmt.Errorf("failed to count bits: %w", err)
	}

	slog.Info("rerender all: clearing refs", "bit_count", bitCount)

	// Clear all refs (but keep bits and cursors)
	if err := a.db.ClearAllRefs(ctx); err != nil {
		return fmt.Errorf("failed to clear refs: %w", err)
	}

	// Get all bits
	bits, err := a.db.GetAllBits(ctx)
	if err != nil {
		return fmt.Errorf("failed to get bits: %w", err)
	}

	slog.Info("rerender all: rendering bits to thymer", "count", len(bits))

	// Progress tracking
	progress := &syncProgress{
		source: "rerender",
		phase:  "rendering",
	}
	a.emitSyncProgress(progress)

	// Render each bit to Thymer
	var errors int
	for i, b := range bits {
		if !b.CanSyncToThymer() {
			continue
		}

		progress.processed = i + 1
		a.emitSyncProgress(progress)

		ref, err := a.renderBitToThymer(ctx, b, "created")
		if err != nil {
			slog.Warn("failed to render bit", "uri", b.MasterURI, "error", err)
			errors++
			continue
		}

		if ref != nil {
			if err := a.db.UpsertRef(ctx, ref); err != nil {
				slog.Warn("failed to save ref", "uri", b.MasterURI, "error", err)
			}
		}
		progress.created++
	}

	progress.phase = "complete"
	progress.errors = errors
	a.emitSyncProgress(progress)

	slog.Info("rerender all: complete", "rendered", progress.created, "errors", errors)

	return nil
}

// GetGitHubConfigHTML returns the GitHub configuration panel HTML.
func (a *App) GetGitHubConfigHTML() (string, error) {
	repos, err := a.getGitHubRepos()
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	err = ui.SyncConfigGitHub(repos, nil).Render(context.Background(), &buf)
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

// SetGitHubRepos updates the list of enabled GitHub repos.
func (a *App) SetGitHubRepos(repos []string) error {
	syncState.mu.Lock()
	if state := syncState.Sources["github"]; state != nil {
		state.ConfigItems = repos
	}
	syncState.mu.Unlock()

	return syncState.save()
}

// GetGoogleCalendarConfigHTML returns the Google Calendar configuration panel HTML.
func (a *App) GetGoogleCalendarConfigHTML() (string, error) {
	calendars, err := a.getGoogleCalendars()
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	err = ui.SyncConfigGoogleCalendar(calendars).Render(context.Background(), &buf)
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

// SetGoogleCalendars updates the list of enabled Google calendars.
func (a *App) SetGoogleCalendars(calendars []string) error {
	syncState.mu.Lock()
	if state := syncState.Sources["google-calendar"]; state != nil {
		state.ConfigItems = calendars
	}
	syncState.mu.Unlock()

	return syncState.save()
}

// Internal helpers

func (a *App) getSyncSources() []ui.SyncSource {
	result := make([]ui.SyncSource, len(syncSources))
	copy(result, syncSources)

	syncState.mu.RLock()
	defer syncState.mu.RUnlock()

	for i := range result {
		src := &result[i]

		// Check auth status
		src.AuthStatus = a.getAuthStatus(src.ID)

		// Get state
		if state := syncState.Sources[src.ID]; state != nil {
			src.Enabled = state.Enabled
			src.LastSync = state.LastSync
			src.LastError = state.LastError
			src.ItemCount = state.ItemCount
			src.ConfigItems = state.ConfigItems
		}

		// Determine sync status
		if src.AuthStatus != ui.AuthAuthenticated {
			src.SyncStatus = ui.SyncDisabled
		} else if !src.Enabled {
			src.SyncStatus = ui.SyncDisabled
		} else if src.LastError != "" {
			src.SyncStatus = ui.SyncError
		} else {
			src.SyncStatus = ui.SyncIdle
		}
	}

	return result
}

func (a *App) getAuthStatus(id string) ui.AuthStatus {
	var tokenKey string
	switch id {
	case "github":
		tokenKey = secrets.GitHubToken
	case "google-calendar", "google-people":
		// Both share the same Google OAuth token
		tokenKey = secrets.GoogleOAuthToken
	case "readwise":
		tokenKey = secrets.ReadwiseToken
	case "linear":
		tokenKey = secrets.LinearToken
	default:
		return ui.AuthNotConfigured
	}

	if secrets.Has(tokenKey) {
		return ui.AuthAuthenticated
	}
	return ui.AuthNotConfigured
}

// HasGoogleCredentials checks if Google OAuth credentials JSON is configured (but not necessarily authenticated).
func (a *App) HasGoogleCredentials() bool {
	return secrets.Has(secrets.GoogleCredentials)
}

// OAuth connection handlers (stubs for now)

func (a *App) connectGitHub() error {
	// Check if token is already set
	if secrets.Has(secrets.GitHubToken) {
		return nil
	}
	// Token not set - frontend should prompt for it
	return fmt.Errorf("GitHub token not configured - please enter your token")
}

func (a *App) connectGoogleCalendar() error {
	// Check if we have credentials
	credsJSON, err := secrets.Get(secrets.GoogleCredentials)
	if err != nil || credsJSON == "" {
		return fmt.Errorf("Google credentials not configured - please upload your credentials JSON")
	}

	// Check if we already have a valid token
	if secrets.Has(secrets.GoogleOAuthToken) {
		return nil
	}

	// Parse credentials and start OAuth flow
	return a.startGoogleOAuth(credsJSON)
}

// SetGoogleCredentials stores the Google OAuth credentials JSON and starts the OAuth flow.
func (a *App) SetGoogleCredentials(jsonStr string) error {
	if jsonStr == "" {
		return fmt.Errorf("credentials JSON cannot be empty")
	}

	// Parse to validate the JSON structure
	var creds struct {
		Installed *struct {
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
		} `json:"installed"`
		Web *struct {
			ClientID     string `json:"client_id"`
			ClientSecret string `json:"client_secret"`
		} `json:"web"`
	}

	if err := json.Unmarshal([]byte(jsonStr), &creds); err != nil {
		return fmt.Errorf("invalid JSON: %w", err)
	}

	// Check for installed (desktop) credentials
	if creds.Installed == nil || creds.Installed.ClientID == "" {
		if creds.Web != nil && creds.Web.ClientID != "" {
			return fmt.Errorf("please use a 'Desktop app' OAuth client, not 'Web application'")
		}
		return fmt.Errorf("invalid credentials JSON - missing client_id")
	}

	// Store credentials
	if err := secrets.Set(secrets.GoogleCredentials, jsonStr); err != nil {
		return fmt.Errorf("failed to store credentials: %w", err)
	}

	// Start OAuth flow
	return a.startGoogleOAuth(jsonStr)
}

// startGoogleOAuth initiates the OAuth flow with a localhost callback.
func (a *App) startGoogleOAuth(credsJSON string) error {
	// Parse credentials
	config, err := google.ConfigFromJSON([]byte(credsJSON),
		"https://www.googleapis.com/auth/calendar.readonly",
		"https://www.googleapis.com/auth/contacts.readonly",
		"https://www.googleapis.com/auth/gmail.readonly",
	)
	if err != nil {
		return fmt.Errorf("failed to parse credentials: %w", err)
	}

	// Find an available port
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return fmt.Errorf("failed to start callback server: %w", err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()

	// Set redirect URI to localhost
	config.RedirectURL = fmt.Sprintf("http://127.0.0.1:%d/callback", port)

	// Channel to receive the auth code
	codeChan := make(chan string, 1)
	errChan := make(chan error, 1)

	// Start temporary HTTP server for callback
	mux := http.NewServeMux()
	server := &http.Server{
		Addr:    fmt.Sprintf("127.0.0.1:%d", port),
		Handler: mux,
	}

	mux.HandleFunc("/callback", func(w http.ResponseWriter, r *http.Request) {
		code := r.URL.Query().Get("code")
		if code == "" {
			errMsg := r.URL.Query().Get("error")
			if errMsg == "" {
				errMsg = "no code received"
			}
			w.Header().Set("Content-Type", "text/html")
			w.WriteHeader(400)
			fmt.Fprintf(w, `<html><body><h2>Authentication Failed</h2><p>%s</p><p>You can close this tab.</p></body></html>`, errMsg)
			errChan <- fmt.Errorf("OAuth failed: %s", errMsg)
			return
		}

		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<html><body><h2>Authentication Successful!</h2><p>You can close this tab and return to thymer-bar.</p></body></html>`)
		codeChan <- code

		// Shutdown server after response is sent
		go func() {
			time.Sleep(100 * time.Millisecond)
			server.Shutdown(context.Background())
		}()
	})

	// Start server in background
	go func() {
		if err := server.ListenAndServe(); err != http.ErrServerClosed {
			errChan <- fmt.Errorf("callback server error: %w", err)
		}
	}()

	// Generate auth URL and open browser
	authURL := config.AuthCodeURL("state-token", oauth2.AccessTypeOffline, oauth2.ApprovalForce)
	runtime.BrowserOpenURL(a.ctx, authURL)

	// Wait for callback (with timeout)
	select {
	case code := <-codeChan:
		// Exchange code for token
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		token, err := config.Exchange(ctx, code)
		if err != nil {
			return fmt.Errorf("failed to exchange code for token: %w", err)
		}

		// Store refresh token (we need this for future requests)
		tokenJSON, err := json.Marshal(token)
		if err != nil {
			return fmt.Errorf("failed to serialize token: %w", err)
		}

		if err := secrets.Set(secrets.GoogleOAuthToken, string(tokenJSON)); err != nil {
			return fmt.Errorf("failed to store token: %w", err)
		}

		// Enable the source
		syncState.mu.Lock()
		if state := syncState.Sources["google-calendar"]; state != nil {
			state.Enabled = true
		}
		syncState.mu.Unlock()
		syncState.save()

		slog.Info("Google OAuth completed successfully")
		return nil

	case err := <-errChan:
		server.Shutdown(context.Background())
		return err

	case <-time.After(5 * time.Minute):
		server.Shutdown(context.Background())
		return fmt.Errorf("OAuth timed out - please try again")
	}
}

func (a *App) connectReadwise() error {
	// TODO: Prompt for API key
	return fmt.Errorf("use 'llog config set readwise-token <token>' to set Readwise token")
}

func (a *App) connectLinear() error {
	// TODO: Implement Linear OAuth
	return fmt.Errorf("use 'llog config set linear-token <token>' to set Linear token")
}

// GitHub repo fetching
func (a *App) getGitHubRepos() ([]ui.GitHubRepo, error) {
	token, err := secrets.Get(secrets.GitHubToken)
	if err != nil || token == "" {
		return nil, fmt.Errorf("GitHub not connected")
	}

	// Fetch repos from GitHub API
	apiRepos, err := fetchGitHubRepos(token)
	if err != nil {
		return nil, err
	}

	// Get currently enabled repos
	syncState.mu.RLock()
	enabledSet := make(map[string]bool)
	if state := syncState.Sources["github"]; state != nil {
		for _, name := range state.ConfigItems {
			enabledSet[name] = true
		}
	}
	syncState.mu.RUnlock()

	// Build result with enabled status
	var repos []ui.GitHubRepo
	for _, r := range apiRepos {
		repos = append(repos, ui.GitHubRepo{
			FullName: r.FullName,
			Private:  r.Private,
			Enabled:  enabledSet[r.FullName],
		})
	}

	// Sort alphabetically by full name
	sort.Slice(repos, func(i, j int) bool {
		return repos[i].FullName < repos[j].FullName
	})

	return repos, nil
}

type ghRepo struct {
	FullName string `json:"full_name"`
	Private  bool   `json:"private"`
}

func fetchGitHubRepos(token string) ([]ghRepo, error) {
	var allRepos []ghRepo
	client := &http.Client{Timeout: 30 * time.Second}
	page := 1

	for {
		url := fmt.Sprintf("https://api.github.com/user/repos?per_page=100&page=%d", page)
		req, err := http.NewRequest("GET", url, nil)
		if err != nil {
			return nil, err
		}

		req.Header.Set("Authorization", "Bearer "+token)
		req.Header.Set("Accept", "application/vnd.github+json")
		req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode != 200 {
			resp.Body.Close()
			return nil, fmt.Errorf("GitHub API error: %d", resp.StatusCode)
		}

		var repos []ghRepo
		if err := json.NewDecoder(resp.Body).Decode(&repos); err != nil {
			resp.Body.Close()
			return nil, err
		}
		resp.Body.Close()

		allRepos = append(allRepos, repos...)

		// If we got fewer than 100, we've reached the end
		if len(repos) < 100 {
			break
		}
		page++
	}

	return allRepos, nil
}

// Google Calendar fetching (stub)
func (a *App) getGoogleCalendars() ([]ui.GoogleCalendar, error) {
	// TODO: Fetch from Google Calendar API
	syncState.mu.RLock()
	defer syncState.mu.RUnlock()

	var calendars []ui.GoogleCalendar
	if state := syncState.Sources["google-calendar"]; state != nil {
		for _, name := range state.ConfigItems {
			calendars = append(calendars, ui.GoogleCalendar{
				ID:      name,
				Name:    name,
				Color:   "#4285f4",
				Enabled: true,
			})
		}
	}
	return calendars, nil
}
