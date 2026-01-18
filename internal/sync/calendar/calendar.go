// Package calendar provides Google Calendar sync engine.
package calendar

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"

	"thymer-bar/internal/bit"
	"thymer-bar/internal/sync"
)

// BitCallback is called for each fetched bit.
type BitCallback func(ctx context.Context, b *bit.Bit) error

// Engine syncs Google Calendar events.
type Engine struct {
	credentials string   // OAuth credentials JSON
	token       string   // OAuth token JSON (with refresh_token)
	calendars   []string // Calendar IDs to sync (empty = primary)
	enabled     bool
	log         *slog.Logger

	// Callback for publishing bits
	onBit BitCallback
}

// New creates a new Google Calendar sync engine.
func New(onBit BitCallback) *Engine {
	return &Engine{
		onBit: onBit,
		log:   slog.Default(),
	}
}

func (e *Engine) Name() string        { return "google-calendar" }
func (e *Engine) DisplayName() string { return "Google Calendar" }
func (e *Engine) Enabled() bool       { return e.enabled && e.credentials != "" && e.token != "" }

func (e *Engine) Configure(cfg map[string]any) error {
	if creds, ok := cfg["credentials"].(string); ok {
		e.credentials = creds
	}
	if token, ok := cfg["token"].(string); ok {
		e.token = token
	}
	if calendars, ok := cfg["calendars"].([]any); ok {
		e.calendars = make([]string, 0, len(calendars))
		for _, c := range calendars {
			if s, ok := c.(string); ok {
				e.calendars = append(e.calendars, s)
			}
		}
	}
	if enabled, ok := cfg["enabled"].(bool); ok {
		e.enabled = enabled
	}
	return nil
}

// Sync fetches calendar events from the configured time window.
func (e *Engine) Sync(ctx context.Context) error {
	return e.SyncSince(ctx, nil)
}

// SyncSince fetches events, using incremental window if since is provided.
func (e *Engine) SyncSince(ctx context.Context, since *time.Time) error {
	if !e.Enabled() {
		return fmt.Errorf("google calendar engine not enabled or configured")
	}

	// Get OAuth client
	client, err := e.getClient()
	if err != nil {
		return fmt.Errorf("failed to get OAuth client: %w", err)
	}

	// Determine time window
	now := time.Now()
	var timeMin, timeMax time.Time
	if since != nil {
		// Incremental: past 7 days to future 30 days
		timeMin = now.AddDate(0, 0, -7)
		timeMax = now.AddDate(0, 0, 30)
	} else {
		// Full sync: past 30 days to future 90 days
		timeMin = now.AddDate(0, 0, -30)
		timeMax = now.AddDate(0, 0, 90)
	}

	// Get calendars to sync
	calendars := e.calendars
	if len(calendars) == 0 {
		calendars = []string{"primary"}
	}

	var totalFetched int
	for _, calID := range calendars {
		events, err := e.fetchEvents(ctx, client, calID, timeMin, timeMax)
		if err != nil {
			e.log.Error("failed to fetch calendar events", "calendar", calID, "error", err)
			continue
		}

		calendarName := calID
		if calID == "primary" {
			calendarName = "Primary Calendar"
		}

		for _, event := range events {
			b := e.eventToBit(calID, calendarName, event)
			totalFetched++

			if e.onBit != nil {
				if err := e.onBit(ctx, b); err != nil {
					e.log.Error("failed to process event", "event", event.ID, "error", err)
				}
			}
		}
	}

	e.log.Info("google calendar sync completed", "fetched", totalFetched)
	return nil
}

// CalendarEvent represents a Google Calendar event from the API.
type CalendarEvent struct {
	ID          string `json:"id"`
	Summary     string `json:"summary"`
	Description string `json:"description"`
	Location    string `json:"location"`
	HTMLLink    string `json:"htmlLink"`
	Status      string `json:"status"` // confirmed, tentative, cancelled
	Created     string `json:"created"`
	Updated     string `json:"updated"`
	Start       *EventDateTime `json:"start"`
	End         *EventDateTime `json:"end"`
	Attendees   []Attendee     `json:"attendees"`
	ConferenceData *ConferenceData `json:"conferenceData"`
	Organizer   *Organizer `json:"organizer"`
}

type EventDateTime struct {
	DateTime string `json:"dateTime"` // RFC3339 for timed events
	Date     string `json:"date"`     // YYYY-MM-DD for all-day events
	TimeZone string `json:"timeZone"`
}

type Attendee struct {
	Email          string `json:"email"`
	DisplayName    string `json:"displayName"`
	ResponseStatus string `json:"responseStatus"`
	Self           bool   `json:"self"`
}

type ConferenceData struct {
	EntryPoints []EntryPoint `json:"entryPoints"`
}

type EntryPoint struct {
	EntryPointType string `json:"entryPointType"` // video, phone, etc.
	URI            string `json:"uri"`
}

type Organizer struct {
	Email       string `json:"email"`
	DisplayName string `json:"displayName"`
	Self        bool   `json:"self"`
}

type CalendarListResponse struct {
	Items         []CalendarEvent `json:"items"`
	NextPageToken string          `json:"nextPageToken"`
}

func (e *Engine) fetchEvents(ctx context.Context, client *http.Client, calendarID string, timeMin, timeMax time.Time) ([]CalendarEvent, error) {
	var allEvents []CalendarEvent
	pageToken := ""

	for {
		params := url.Values{}
		params.Set("timeMin", timeMin.Format(time.RFC3339))
		params.Set("timeMax", timeMax.Format(time.RFC3339))
		params.Set("singleEvents", "true")
		params.Set("orderBy", "startTime")
		params.Set("maxResults", "250")
		if pageToken != "" {
			params.Set("pageToken", pageToken)
		}

		apiURL := fmt.Sprintf("https://www.googleapis.com/calendar/v3/calendars/%s/events?%s",
			url.PathEscape(calendarID), params.Encode())

		req, err := http.NewRequestWithContext(ctx, "GET", apiURL, nil)
		if err != nil {
			return nil, err
		}

		resp, err := client.Do(req)
		if err != nil {
			return nil, err
		}

		if resp.StatusCode != 200 {
			resp.Body.Close()
			return nil, fmt.Errorf("calendar API error: %d", resp.StatusCode)
		}

		var result CalendarListResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}
		resp.Body.Close()

		allEvents = append(allEvents, result.Items...)

		if result.NextPageToken == "" {
			break
		}
		pageToken = result.NextPageToken
	}

	return allEvents, nil
}

func (e *Engine) eventToBit(calendarID, calendarName string, event CalendarEvent) *bit.Bit {
	// Build canonical URI
	masterURI := bit.BuildCalendarURI(calendarID, event.ID)

	// Create the Bit
	b := bit.New(masterURI, bit.SystemCalendar)
	b.Content = event.Description

	// Parse timestamps
	if event.Created != "" {
		if t, err := time.Parse(time.RFC3339, event.Created); err == nil {
			b.CreatedAt = t
		}
	}
	if event.Updated != "" {
		if t, err := time.Parse(time.RFC3339, event.Updated); err == nil {
			b.UpdatedAt = t
		}
	}

	// Set frontmatter
	title := event.Summary
	if title == "" {
		title = "(No title)"
	}
	b.Set("title", title)
	b.Set("external_id", fmt.Sprintf("gcal_%s", event.ID))
	b.Set("type", "event")
	b.Set("source", "google-calendar")
	b.Set("calendar", calendarName)
	b.Set("calendar_id", calendarID)
	b.Set("status", event.Status)
	b.Set("url", event.HTMLLink)
	b.Set("location", event.Location)

	// Determine if all-day event
	isAllDay := event.Start != nil && event.Start.Date != ""
	b.Set("all_day", isAllDay)

	// Parse start/end times
	if event.Start != nil {
		if isAllDay {
			b.Set("start_date", event.Start.Date)
		} else if event.Start.DateTime != "" {
			if t, err := time.Parse(time.RFC3339, event.Start.DateTime); err == nil {
				b.Set("start_time", t)
			}
		}
	}
	if event.End != nil {
		if isAllDay {
			b.Set("end_date", event.End.Date)
		} else if event.End.DateTime != "" {
			if t, err := time.Parse(time.RFC3339, event.End.DateTime); err == nil {
				b.Set("end_time", t)
			}
		}
	}

	// Determine timing (past/upcoming)
	now := time.Now()
	var eventTime time.Time
	if startTime, ok := b.Get("start_time").(time.Time); ok {
		eventTime = startTime
	} else if startDate := b.GetString("start_date"); startDate != "" {
		if t, err := time.Parse("2006-01-02", startDate); err == nil {
			eventTime = t
		}
	}
	if !eventTime.IsZero() {
		if eventTime.Before(now) {
			b.Set("timing", "past")
		} else {
			b.Set("timing", "upcoming")
		}
	}

	// Extract attendees (limit to 5)
	var attendees []string
	for i, a := range event.Attendees {
		if i >= 5 {
			break
		}
		if a.DisplayName != "" {
			attendees = append(attendees, a.DisplayName)
		} else if a.Email != "" {
			attendees = append(attendees, a.Email)
		}
	}
	if len(attendees) > 0 {
		b.Set("attendees", attendees)
	}

	// Extract meeting link
	meetLink := e.extractMeetingLink(event)
	if meetLink != "" {
		b.Set("meet_link", meetLink)
	}

	// Compute hash
	b.UpdateHash()

	return b
}

func (e *Engine) extractMeetingLink(event CalendarEvent) string {
	// Check conference data first
	if event.ConferenceData != nil {
		for _, ep := range event.ConferenceData.EntryPoints {
			if ep.EntryPointType == "video" && ep.URI != "" {
				return ep.URI
			}
		}
	}

	// Check location for meeting URLs
	if event.Location != "" {
		if strings.Contains(event.Location, "meet.google.com") ||
			strings.Contains(event.Location, "zoom.us") ||
			strings.Contains(event.Location, "teams.microsoft.com") {
			return event.Location
		}
	}

	// Check description for meeting URLs
	if event.Description != "" {
		for _, pattern := range []string{"meet.google.com", "zoom.us", "teams.microsoft.com"} {
			if idx := strings.Index(event.Description, pattern); idx != -1 {
				// Extract URL from description (simplified)
				start := strings.LastIndex(event.Description[:idx], "http")
				if start != -1 {
					end := strings.IndexAny(event.Description[start:], " \n\r\t<>")
					if end == -1 {
						end = len(event.Description) - start
					}
					return event.Description[start : start+end]
				}
			}
		}
	}

	return ""
}

func (e *Engine) getClient() (*http.Client, error) {
	// Parse credentials
	config, err := google.ConfigFromJSON([]byte(e.credentials),
		"https://www.googleapis.com/auth/calendar.readonly",
	)
	if err != nil {
		return nil, fmt.Errorf("failed to parse credentials: %w", err)
	}

	// Parse stored token
	var token oauth2.Token
	if err := json.Unmarshal([]byte(e.token), &token); err != nil {
		return nil, fmt.Errorf("failed to parse token: %w", err)
	}

	// Create client with automatic token refresh
	return config.Client(context.Background(), &token), nil
}

// Ensure Engine implements sync.Engine
var _ sync.Engine = (*Engine)(nil)
