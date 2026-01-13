// Package render - Lifelog renderer for publishing to lifelog.my
package render

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"thymer-bar/internal/sync"
)

// LifelogRenderer publishes records and events to lifelog.my.
type LifelogRenderer struct {
	baseURL   string // e.g., "https://lifelog.my"
	username  string // e.g., "riclib"
	apiKey    string
	storyline string // Default storyline for posts
	enabled   bool
	client    *http.Client
}

// LifelogPost represents a post to publish.
type LifelogPost struct {
	Title     string            `json:"title"`
	Content   string            `json:"content"`
	Storyline string            `json:"storyline,omitempty"`
	Published time.Time         `json:"published,omitempty"`
	Metadata  map[string]string `json:"metadata,omitempty"`
}

// LifelogEvent represents an event to log.
type LifelogEvent struct {
	Type      string         `json:"type"`
	Timestamp time.Time      `json:"timestamp"`
	RecordID  string         `json:"record_id,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
}

// NewLifelogRenderer creates a new Lifelog renderer.
func NewLifelogRenderer() *LifelogRenderer {
	return &LifelogRenderer{
		baseURL: "https://lifelog.my",
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

func (r *LifelogRenderer) Name() string        { return "lifelog" }
func (r *LifelogRenderer) DisplayName() string { return "Lifelog" }

func (r *LifelogRenderer) Available() bool {
	return r.enabled && r.apiKey != "" && r.username != ""
}

func (r *LifelogRenderer) Configure(cfg map[string]any) error {
	if baseURL, ok := cfg["base_url"].(string); ok {
		r.baseURL = baseURL
	}
	if username, ok := cfg["username"].(string); ok {
		r.username = username
	}
	if apiKey, ok := cfg["api_key"].(string); ok {
		r.apiKey = apiKey
	}
	if storyline, ok := cfg["storyline"].(string); ok {
		r.storyline = storyline
	}
	if enabled, ok := cfg["enabled"].(bool); ok {
		r.enabled = enabled
	}
	return nil
}

// Render publishes a record as a Lifelog post.
// Not all records should be published - use selectively.
func (r *LifelogRenderer) Render(ctx context.Context, record *sync.Record) (string, error) {
	if !r.Available() {
		return "", fmt.Errorf("lifelog renderer not available")
	}

	post := r.recordToPost(record)
	return r.publish(ctx, post)
}

// RenderEvent logs an event to Lifelog's event stream.
// Events are used for the activity timeline and analytics.
func (r *LifelogRenderer) RenderEvent(ctx context.Context, event *Event) error {
	if !r.Available() {
		return fmt.Errorf("lifelog renderer not available")
	}

	timestamp, _ := time.Parse(time.RFC3339, event.Timestamp)
	lifelogEvent := &LifelogEvent{
		Type:      event.Type,
		Timestamp: timestamp,
		RecordID:  event.RecordID,
		Data:      event.Data,
	}

	return r.logEvent(ctx, lifelogEvent)
}

// recordToPost converts a sync record to a Lifelog post.
func (r *LifelogRenderer) recordToPost(record *sync.Record) *LifelogPost {
	// Determine storyline based on record source/type
	storyline := r.storyline
	if storyline == "" {
		storyline = r.storylineForRecord(record)
	}

	return &LifelogPost{
		Title:     record.Title,
		Content:   record.Content,
		Storyline: storyline,
		Published: record.UpdatedAt,
		Metadata: map[string]string{
			"source":      record.Source,
			"external_id": record.ExternalID,
			"url":         record.URL,
		},
	}
}

// storylineForRecord determines the storyline for a record.
func (r *LifelogRenderer) storylineForRecord(record *sync.Record) string {
	// Could be configured per-source or use tags/labels from the record
	switch record.Source {
	case "github":
		// Could use repo name or labels to determine storyline
		if repo, ok := record.Fields["repo"].(string); ok {
			return repo
		}
	}
	return "journal" // Default storyline
}

// publish posts content to Lifelog.
func (r *LifelogRenderer) publish(ctx context.Context, post *LifelogPost) (string, error) {
	url := fmt.Sprintf("%s/api/%s/posts", r.baseURL, r.username)

	body, err := json.Marshal(post)
	if err != nil {
		return "", fmt.Errorf("failed to marshal post: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return "", fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+r.apiKey)

	resp, err := r.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to publish: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return "", fmt.Errorf("publish failed with status %d", resp.StatusCode)
	}

	// Parse response to get post ID
	var result struct {
		ID string `json:"id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	return result.ID, nil
}

// logEvent logs an event to Lifelog's event stream.
func (r *LifelogRenderer) logEvent(ctx context.Context, event *LifelogEvent) error {
	url := fmt.Sprintf("%s/api/%s/events", r.baseURL, r.username)

	body, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("failed to marshal event: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+r.apiKey)

	resp, err := r.client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to log event: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusCreated {
		return fmt.Errorf("log event failed with status %d", resp.StatusCode)
	}

	return nil
}

// PublishDailySummary publishes a daily summary post.
// Called at end of day to summarize focus sessions, tasks completed, etc.
func (r *LifelogRenderer) PublishDailySummary(ctx context.Context, date time.Time, summary string, events []*Event) (string, error) {
	post := &LifelogPost{
		Title:     fmt.Sprintf("Daily Log: %s", date.Format("January 2, 2006")),
		Content:   summary,
		Storyline: "daily-log",
		Published: date,
		Metadata: map[string]string{
			"type":   "daily-summary",
			"events": fmt.Sprintf("%d", len(events)),
		},
	}

	return r.publish(ctx, post)
}
