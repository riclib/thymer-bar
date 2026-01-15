package main

import (
	"bytes"
	"context"
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

	"thymer-bar/internal/bit"
	"thymer-bar/internal/config"
	"thymer-bar/internal/secrets"
	"thymer-bar/internal/sqlite"
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

	slog.Debug("syncing to thymer", "uri", b.MasterURI, "collection", collection, "change", changeType)

	// Format markdown content for better readability
	formattedContent := ensureBlankLinesBeforeHeadings(b.Content)

	fields := map[string]any{
		"title":          b.Title(),
		"source":         "GitHub", // Label for Thymer
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

// collectionFromBit determines the Thymer collection for a Bit.
func (a *App) collectionFromBit(b *bit.Bit) string {
	// Duck typing: check frontmatter fields
	if b.HasField("repo") && b.HasField("number") {
		return "Issues"
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
	for _, s := range sources {
		if s.Enabled && s.AuthStatus == ui.AuthAuthenticated {
			if err := a.SyncNow(s.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// ResyncAll clears all data and performs a full sync from all sources.
// This clears bits, refs, and sync_cursors, then fetches everything fresh.
func (a *App) ResyncAll() error {
	ctx := context.Background()

	slog.Info("resync all: clearing database")

	// Clear all tables
	if a.db != nil {
		if err := a.db.ClearAllForResync(ctx); err != nil {
			return fmt.Errorf("failed to clear database: %w", err)
		}
	}

	slog.Info("resync all: starting fresh sync")

	// Trigger full sync
	return a.SyncAll()
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
