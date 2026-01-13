// Package render - Obsidian renderer for markdown vault output.
package render

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"thymer-bar/internal/sync"
)

// ObsidianRenderer renders records to an Obsidian vault as markdown files.
type ObsidianRenderer struct {
	vaultPath string
	folder    string // Subfolder within vault (e.g., "thymer-bar")
	enabled   bool
}

// NewObsidianRenderer creates a new Obsidian renderer.
func NewObsidianRenderer() *ObsidianRenderer {
	return &ObsidianRenderer{
		folder: "thymer-bar",
	}
}

func (r *ObsidianRenderer) Name() string        { return "obsidian" }
func (r *ObsidianRenderer) DisplayName() string { return "Obsidian" }

func (r *ObsidianRenderer) Available() bool {
	if !r.enabled || r.vaultPath == "" {
		return false
	}
	// Check if vault path exists
	if _, err := os.Stat(r.vaultPath); os.IsNotExist(err) {
		return false
	}
	return true
}

func (r *ObsidianRenderer) Configure(cfg map[string]any) error {
	if vaultPath, ok := cfg["vault_path"].(string); ok {
		r.vaultPath = vaultPath
	}
	if folder, ok := cfg["folder"].(string); ok {
		r.folder = folder
	}
	if enabled, ok := cfg["enabled"].(bool); ok {
		r.enabled = enabled
	}
	return nil
}

// Render writes a record to the Obsidian vault as a markdown file.
func (r *ObsidianRenderer) Render(ctx context.Context, record *sync.Record) (string, error) {
	if !r.Available() {
		return "", fmt.Errorf("obsidian renderer not available")
	}

	// Determine folder and filename
	folder := r.folderForRecord(record)
	filename := r.filenameForRecord(record)
	fullPath := filepath.Join(r.vaultPath, r.folder, folder, filename)

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
		return "", fmt.Errorf("failed to create directory: %w", err)
	}

	// Generate markdown content
	content := r.recordToMarkdown(record)

	// Write file
	if err := os.WriteFile(fullPath, []byte(content), 0644); err != nil {
		return "", fmt.Errorf("failed to write file: %w", err)
	}

	return fullPath, nil
}

// RenderEvent appends an event to the daily note.
func (r *ObsidianRenderer) RenderEvent(ctx context.Context, event *Event) error {
	if !r.Available() {
		return fmt.Errorf("obsidian renderer not available")
	}

	// Parse timestamp
	timestamp, _ := time.Parse(time.RFC3339, event.Timestamp)
	if timestamp.IsZero() {
		timestamp = time.Now()
	}

	// Append to daily note
	dailyPath := r.dailyNotePath(timestamp)

	// Ensure directory exists
	if err := os.MkdirAll(filepath.Dir(dailyPath), 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Read existing content or create new
	var existing []byte
	if data, err := os.ReadFile(dailyPath); err == nil {
		existing = data
	}

	// Format event line
	eventLine := fmt.Sprintf("- %s %s", timestamp.Format("15:04"), r.formatEvent(event))

	// Append to file
	f, err := os.OpenFile(dailyPath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return fmt.Errorf("failed to open daily note: %w", err)
	}
	defer f.Close()

	// Add events header if this is a new file or section doesn't exist
	if !strings.Contains(string(existing), "## Events") {
		f.WriteString("\n## Events\n")
	}
	f.WriteString(eventLine + "\n")

	return nil
}

// folderForRecord determines the subfolder for a record.
func (r *ObsidianRenderer) folderForRecord(record *sync.Record) string {
	switch record.Source {
	case "github":
		return "issues"
	case "readwise":
		return "captures"
	case "calendar":
		return "calendar"
	default:
		return "misc"
	}
}

// filenameForRecord generates a filename for a record.
func (r *ObsidianRenderer) filenameForRecord(record *sync.Record) string {
	// Sanitize title for filename
	title := sanitizeFilename(record.Title)
	if len(title) > 50 {
		title = title[:50]
	}
	return fmt.Sprintf("%s.md", title)
}

// dailyNotePath returns the path to the daily note for a given date.
func (r *ObsidianRenderer) dailyNotePath(date time.Time) string {
	filename := date.Format("2006-01-02") + ".md"
	return filepath.Join(r.vaultPath, r.folder, "daily", filename)
}

// recordToMarkdown converts a record to markdown with frontmatter.
func (r *ObsidianRenderer) recordToMarkdown(record *sync.Record) string {
	var sb strings.Builder

	// Frontmatter
	sb.WriteString("---\n")
	sb.WriteString(fmt.Sprintf("title: %q\n", record.Title))
	sb.WriteString(fmt.Sprintf("source: %s\n", record.Source))
	sb.WriteString(fmt.Sprintf("external_id: %s\n", record.ExternalID))
	if record.URL != "" {
		sb.WriteString(fmt.Sprintf("url: %s\n", record.URL))
	}
	sb.WriteString(fmt.Sprintf("type: %s\n", record.Type))
	sb.WriteString(fmt.Sprintf("created: %s\n", record.CreatedAt.Format(time.RFC3339)))
	sb.WriteString(fmt.Sprintf("updated: %s\n", record.UpdatedAt.Format(time.RFC3339)))

	// Source-specific fields
	for k, v := range record.Fields {
		switch val := v.(type) {
		case string:
			sb.WriteString(fmt.Sprintf("%s: %q\n", k, val))
		case []string:
			sb.WriteString(fmt.Sprintf("%s: [%s]\n", k, strings.Join(val, ", ")))
		default:
			sb.WriteString(fmt.Sprintf("%s: %v\n", k, val))
		}
	}
	sb.WriteString("---\n\n")

	// Title
	sb.WriteString(fmt.Sprintf("# %s\n\n", record.Title))

	// Content
	if record.Content != "" {
		sb.WriteString(record.Content)
		sb.WriteString("\n")
	}

	// Link
	if record.URL != "" {
		sb.WriteString(fmt.Sprintf("\n---\n[View on %s](%s)\n", record.Source, record.URL))
	}

	return sb.String()
}

// formatEvent formats an event for the daily note.
func (r *ObsidianRenderer) formatEvent(event *Event) string {
	switch event.Type {
	case "task.started":
		return fmt.Sprintf("Started: [[%s]]", event.RecordID)
	case "task.paused":
		return fmt.Sprintf("Paused: [[%s]]", event.RecordID)
	case "task.completed":
		return fmt.Sprintf("Completed: [[%s]]", event.RecordID)
	default:
		return fmt.Sprintf("%s: %s", event.Type, event.RecordID)
	}
}

// sanitizeFilename removes or replaces characters invalid in filenames.
func sanitizeFilename(name string) string {
	// Replace invalid characters
	replacer := strings.NewReplacer(
		"/", "-",
		"\\", "-",
		":", "-",
		"*", "",
		"?", "",
		"\"", "",
		"<", "",
		">", "",
		"|", "",
	)
	return strings.TrimSpace(replacer.Replace(name))
}
