// Package markdown provides markdown file projection for lifelog.
package markdown

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"

	"thymer-bar/internal/sync"
)

// Projector writes sync records as markdown files.
type Projector struct {
	baseDir string
}

// New creates a markdown projector writing to the given directory.
func New(baseDir string) *Projector {
	return &Projector{baseDir: baseDir}
}

// WriteRecord writes a sync record as a markdown file.
// Files are organized by type: Issues/, PRs/, etc.
func (p *Projector) WriteRecord(record *sync.Record) error {
	// Determine folder based on type
	folder := typeToFolder(record.Type)
	dir := filepath.Join(p.baseDir, folder)

	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory: %w", err)
	}

	// Filename from external ID
	filename := sanitizeFilename(record.ExternalID) + ".md"
	path := filepath.Join(dir, filename)

	// Build frontmatter
	fm := buildFrontmatter(record)

	// Marshal frontmatter to YAML
	fmBytes, err := yaml.Marshal(fm)
	if err != nil {
		return fmt.Errorf("failed to marshal frontmatter: %w", err)
	}

	// Build full content
	var content strings.Builder
	content.WriteString("---\n")
	content.Write(fmBytes)
	content.WriteString("---\n\n")
	content.WriteString(record.Content)

	// Write file
	if err := os.WriteFile(path, []byte(content.String()), 0644); err != nil {
		return fmt.Errorf("failed to write file: %w", err)
	}

	return nil
}

// buildFrontmatter creates frontmatter map from record.
func buildFrontmatter(record *sync.Record) map[string]any {
	fm := map[string]any{
		"source":      record.Source,
		"external_id": record.ExternalID,
		"type":        record.Type,
		"title":       record.Title,
		"created":     record.CreatedAt.Format(time.RFC3339),
		"updated":     record.UpdatedAt.Format(time.RFC3339),
	}

	if record.URL != "" {
		fm["url"] = record.URL
	}

	if record.CompletedAt != nil {
		fm["completed"] = record.CompletedAt.Format(time.RFC3339)
	}

	// Add source-specific fields
	for k, v := range record.Fields {
		// Skip fields already in frontmatter
		if k == "type" {
			continue
		}
		fm[k] = v
	}

	return fm
}

// typeToFolder maps record types to folder names.
func typeToFolder(t string) string {
	switch t {
	case "issue":
		return "Issues"
	case "pr":
		return "PRs"
	case "capture":
		return "Captures"
	case "highlight":
		return "Highlights"
	case "event":
		return "Events"
	default:
		return "Notes"
	}
}

// sanitizeFilename makes a string safe for use as a filename.
func sanitizeFilename(s string) string {
	// Replace problematic characters
	replacer := strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
	)
	return replacer.Replace(s)
}
