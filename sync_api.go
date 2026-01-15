package main

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"thymer-bar/internal/config"
	"thymer-bar/internal/secrets"
	"thymer-bar/internal/sqlite"
	isync "thymer-bar/internal/sync"
	"thymer-bar/internal/sync/github"
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
	case "google-calendar":
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
	var tokenKey string
	switch id {
	case "github":
		tokenKey = secrets.GitHubToken
	case "google-calendar":
		tokenKey = secrets.CalendarOAuth
	case "readwise":
		tokenKey = secrets.ReadwiseToken
	case "linear":
		tokenKey = secrets.LinearToken
	default:
		return fmt.Errorf("unknown source: %s", id)
	}

	if err := secrets.Delete(tokenKey); err != nil {
		return err
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
	switch id {
	case "github":
		return a.syncGitHub()
	default:
		return fmt.Errorf("sync not implemented for source: %s", id)
	}
}

// syncGitHub runs the GitHub sync through the event pipeline.
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
		if state, err := a.db.GetSyncState(context.Background(), "github"); err == nil && state.LastSourceUpdatedAt != nil {
			since = state.LastSourceUpdatedAt
			slog.Info("incremental sync", "since", since.Format(time.RFC3339))
		} else {
			slog.Info("full sync", "reason", "no previous sync state")
		}
	}

	var maxSourceUpdatedAt time.Time

	// Create callback that processes through the pipeline
	onRecord := func(ctx context.Context, record *isync.Record) error {
		progress.processed++
		progress.phase = "fetching"
		a.emitSyncProgress(progress)

		// Track latest source timestamp for next incremental sync
		if record.UpdatedAt.After(maxSourceUpdatedAt) {
			maxSourceUpdatedAt = record.UpdatedAt
		}

		// Process through pipeline: map -> hash -> change detection -> render
		slog.Debug("processing record", "num", progress.processed, "title", record.Title, "id", record.ExternalID)
		changed, err := a.processRecordPipeline(ctx, record)
		if err != nil {
			progress.errors++
			slog.Error("failed to process record", "id", record.ExternalID, "error", err)
			return nil // Continue with other records
		}

		if changed == "created" {
			progress.created++
			slog.Info("record created", "title", record.Title)
		} else if changed == "updated" {
			progress.updated++
			slog.Info("record updated", "title", record.Title)
		} else {
			progress.unchanged++
			slog.Debug("record unchanged", "title", record.Title)
		}

		a.emitSyncProgress(progress)
		return nil
	}

	// Create and configure the GitHub engine
	engine := github.New(onRecord)
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

	// Update sync state with latest source timestamp
	if a.db != nil && !maxSourceUpdatedAt.IsZero() {
		a.db.UpdateSyncState(ctx, &sqlite.SyncState{
			Source:              "github",
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

// processRecordPipeline processes a record through the sync pipeline.
// Returns "created", "updated", or "" (unchanged).
func (a *App) processRecordPipeline(ctx context.Context, record *isync.Record) (string, error) {
	// Map to normalized format
	mapped := a.mapGitHubRecord(record)

	// Compute content hash of mapped record
	newHash := computeMappedHash(mapped)

	// Check for changes via SQLite
	var oldHash string
	if a.db != nil {
		oldHash, _ = a.db.GetSourceRecordHash(ctx, record.Source, record.ExternalID)
	}

	changeType := ""
	if oldHash == "" {
		changeType = "created"
	} else if oldHash != newHash {
		changeType = "updated"
	} else {
		// No change
		return "", nil
	}

	// Sync to Thymer (single upsert call)
	thymerUUID, err := a.renderToThymer(ctx, mapped, changeType)
	if err != nil {
		return changeType, err
	}

	// Store hash and UUID in SQLite
	if a.db != nil {
		// Update source_records for change detection
		a.db.UpsertSourceRecord(ctx, &sqlite.SourceRecord{
			Source:          record.Source,
			ExternalID:      record.ExternalID,
			ContentHash:     newHash,
			SourceUpdatedAt: &record.UpdatedAt,
		})

		// Update records table with thymer UUID for future reference
		now := time.Now()
		a.db.UpsertRecord(ctx, &sqlite.Record{
			ID:          fmt.Sprintf("%s_%s", record.Source, record.ExternalID),
			Source:      record.Source,
			ExternalID:  record.ExternalID,
			ThymerUUID:  thymerUUID,
			ContentHash: newHash,
			SyncedAt:    &now,
		})
	}

	return changeType, nil
}

// mappedRecord is the normalized record format.
type mappedRecord struct {
	Source        string
	ExternalID    string
	Collection    string
	Title         string
	Content       string // Markdown body content
	ExternalState string
	Type          string
	URL           string
	Repo          string
	Number        int
	Author        string
	Assignee      string
	CreatedAt     time.Time
	UpdatedAt     time.Time
	Labels        []string
}

// mapGitHubRecord converts a sync.Record to our normalized format.
// Uses Labels (not IDs) for choice fields to match Thymer's API.
func (a *App) mapGitHubRecord(record *isync.Record) *mappedRecord {
	mapped := &mappedRecord{
		Source:     "GitHub", // Label, not ID
		ExternalID: record.ExternalID,
		Collection: "Issues",
		Title:      record.Title,
		Content:    record.Content, // Markdown body
		URL:        record.URL,
		CreatedAt:  record.CreatedAt,
		UpdatedAt:  record.UpdatedAt,
	}

	// External state - use Labels
	mapped.ExternalState = "Open"
	if record.CompletedAt != nil {
		mapped.ExternalState = "Closed"
	}
	if state, ok := record.Fields["state"].(string); ok {
		if state == "closed" || state == "merged" {
			mapped.ExternalState = "Closed"
		}
	}

	// Type - use Labels
	if t, ok := record.Fields["type"].(string); ok {
		switch t {
		case "pull_request":
			mapped.Type = "PR"
		case "issue":
			mapped.Type = "Issue"
		default:
			mapped.Type = "Issue"
		}
	} else {
		mapped.Type = "Issue"
	}

	// Repo
	if repo, ok := record.Fields["repo"].(string); ok {
		mapped.Repo = repo
	}

	// Number
	if num, ok := record.Fields["number"].(int); ok {
		mapped.Number = num
	}

	// Author
	if author, ok := record.Fields["author"].(string); ok {
		mapped.Author = author
	}

	// Assignee
	if assignees, ok := record.Fields["assignees"].([]string); ok && len(assignees) > 0 {
		mapped.Assignee = assignees[0]
	}

	// Labels
	if labels, ok := record.Fields["labels"].([]string); ok {
		mapped.Labels = labels
	}

	return mapped
}

// computeMappedHash computes a content hash from the mapped record.
func computeMappedHash(m *mappedRecord) string {
	parts := []string{
		m.Title,
		m.Content, // Include body content in hash
		m.ExternalState,
		m.Type,
		m.Repo,
		fmt.Sprintf("%d", m.Number),
		m.Author,
		m.Assignee,
		m.URL,
	}
	if len(m.Labels) > 0 {
		parts = append(parts, strings.Join(m.Labels, ","))
	}
	h := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(h[:16])
}

// renderToThymer syncs a mapped record to Thymer using the upsert-based SyncRecord.
// Returns the Thymer UUID for storage.
func (a *App) renderToThymer(ctx context.Context, m *mappedRecord, changeType string) (string, error) {
	if a.thymer == nil || !a.thymer.Available() {
		slog.Warn("thymer not connected", "id", m.ExternalID)
		return "", fmt.Errorf("thymer not connected")
	}

	slog.Debug("syncing to thymer", "id", m.ExternalID, "collection", m.Collection, "change", changeType)

	fields := map[string]any{
		"title":          m.Title,
		"source":         m.Source,
		"external_state": m.ExternalState,
		"type":           m.Type,
		"url":            m.URL,
		"repo":           m.Repo,
		"number":         m.Number,
		"author":         m.Author,
		"assignee":       m.Assignee,
		"updated_at":     m.UpdatedAt,
		"created_at":     m.CreatedAt,
		"content":        m.Content, // Markdown body for insertion
	}

	// Set initial status for new records based on external state
	// Use Labels (not IDs) for choice fields
	if changeType == "created" {
		if m.ExternalState == "Closed" {
			fields["status"] = "Done"
		} else {
			fields["status"] = "Inbox"
		}
	}

	// SyncRecord handles the upsert - Thymer decides if it's create or update
	slog.Debug("sending syncRecord request", "collection", m.Collection, "external_id", m.ExternalID)
	result, err := a.thymer.SyncRecord(ctx, m.Collection, m.ExternalID, m.Title, fields)
	if err != nil {
		slog.Error("syncRecord failed", "error", err)
		return "", err
	}

	slog.Info("syncRecord success", "guid", result.GUID, "action", result.Action)
	return result.GUID, nil
}

// SyncAll triggers sync for all enabled sources.
func (a *App) SyncAll() error {
	sources := a.getSyncSources()
	for _, s := range sources {
		if s.Enabled && s.AuthStatus == ui.AuthAuthenticated {
			if err := a.SyncNow(s.ID); err != nil {
				return err
			}
		}
	}
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
	case "google-calendar":
		tokenKey = secrets.CalendarOAuth
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

// OAuth connection handlers (stubs for now)

func (a *App) connectGitHub() error {
	// TODO: Implement GitHub OAuth Device Flow
	// For now, user can use llog config set github-token
	return fmt.Errorf("use 'llog config set github-token <token>' to set GitHub token")
}

func (a *App) connectGoogleCalendar() error {
	// TODO: Implement Google OAuth
	return fmt.Errorf("Google Calendar OAuth not yet implemented")
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
