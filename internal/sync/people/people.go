// Package people provides Google People (Contacts) sync engine.
package people

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

// Engine syncs Google People (Contacts).
type Engine struct {
	credentials string // OAuth credentials JSON
	token       string // OAuth token JSON (with refresh_token)
	enabled     bool
	log         *slog.Logger

	// Callback for publishing bits
	onBit BitCallback
}

// New creates a new Google People sync engine.
func New(onBit BitCallback) *Engine {
	return &Engine{
		onBit: onBit,
		log:   slog.Default(),
	}
}

func (e *Engine) Name() string        { return "google-people" }
func (e *Engine) DisplayName() string { return "Google People" }
func (e *Engine) Enabled() bool       { return e.enabled && e.credentials != "" && e.token != "" }

func (e *Engine) Configure(cfg map[string]any) error {
	if creds, ok := cfg["credentials"].(string); ok {
		e.credentials = creds
	}
	if token, ok := cfg["token"].(string); ok {
		e.token = token
	}
	if enabled, ok := cfg["enabled"].(bool); ok {
		e.enabled = enabled
	}
	return nil
}

// Sync fetches all contacts.
func (e *Engine) Sync(ctx context.Context) error {
	if !e.Enabled() {
		return fmt.Errorf("google people engine not enabled or configured")
	}

	// Get OAuth client
	client, err := e.getClient()
	if err != nil {
		return fmt.Errorf("failed to get OAuth client: %w", err)
	}

	// Fetch all contacts
	contacts, err := e.fetchContacts(ctx, client)
	if err != nil {
		return fmt.Errorf("failed to fetch contacts: %w", err)
	}

	var totalFetched int
	for _, contact := range contacts {
		b := e.contactToBit(contact)
		if b == nil {
			continue // Skip contacts without names
		}
		totalFetched++

		if e.onBit != nil {
			if err := e.onBit(ctx, b); err != nil {
				e.log.Error("failed to process contact", "contact", contact.ResourceName, "error", err)
			}
		}
	}

	e.log.Info("google people sync completed", "fetched", totalFetched)
	return nil
}

// Person represents a Google People API contact.
type Person struct {
	ResourceName string     `json:"resourceName"` // e.g., "people/c12345"
	Etag         string     `json:"etag"`
	Metadata     *Metadata  `json:"metadata"`
	Names        []Name     `json:"names"`
	EmailAddresses []Email  `json:"emailAddresses"`
	PhoneNumbers []Phone    `json:"phoneNumbers"`
	Organizations []Organization `json:"organizations"`
	Biographies  []Biography `json:"biographies"`
	Events       []Event    `json:"events"`
}

type Metadata struct {
	Sources []Source `json:"sources"`
}

type Source struct {
	Type       string `json:"type"`
	ID         string `json:"id"`
	UpdateTime string `json:"updateTime"`
}

type Name struct {
	DisplayName string `json:"displayName"`
	GivenName   string `json:"givenName"`
	FamilyName  string `json:"familyName"`
	Metadata    *FieldMetadata `json:"metadata"`
}

type FieldMetadata struct {
	Primary bool `json:"primary"`
}

type Email struct {
	Value    string         `json:"value"`
	Type     string         `json:"type"`
	Metadata *FieldMetadata `json:"metadata"`
}

type Phone struct {
	Value    string         `json:"value"`
	Type     string         `json:"type"`
	Metadata *FieldMetadata `json:"metadata"`
}

type Organization struct {
	Name     string `json:"name"`
	Title    string `json:"title"`
	Metadata *FieldMetadata `json:"metadata"`
}

type Biography struct {
	Value       string `json:"value"`
	ContentType string `json:"contentType"`
}

type Event struct {
	Type string     `json:"type"` // anniversary, etc.
	Date *EventDate `json:"date"`
}

type EventDate struct {
	Year  int `json:"year"`
	Month int `json:"month"`
	Day   int `json:"day"`
}

type PeopleListResponse struct {
	Connections   []Person `json:"connections"`
	NextPageToken string   `json:"nextPageToken"`
	TotalPeople   int      `json:"totalPeople"`
}

func (e *Engine) fetchContacts(ctx context.Context, client *http.Client) ([]Person, error) {
	var allContacts []Person
	pageToken := ""

	for {
		params := url.Values{}
		params.Set("personFields", "names,emailAddresses,phoneNumbers,organizations,biographies,events,metadata")
		params.Set("pageSize", "1000")
		params.Set("sortOrder", "LAST_MODIFIED_DESCENDING")
		if pageToken != "" {
			params.Set("pageToken", pageToken)
		}

		apiURL := fmt.Sprintf("https://people.googleapis.com/v1/people/me/connections?%s", params.Encode())

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
			return nil, fmt.Errorf("people API error: %d", resp.StatusCode)
		}

		var result PeopleListResponse
		if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
			resp.Body.Close()
			return nil, fmt.Errorf("failed to decode response: %w", err)
		}
		resp.Body.Close()

		allContacts = append(allContacts, result.Connections...)

		if result.NextPageToken == "" {
			break
		}
		pageToken = result.NextPageToken
	}

	return allContacts, nil
}

func (e *Engine) contactToBit(person Person) *bit.Bit {
	// Extract display name
	displayName := e.extractName(person)
	if displayName == "" {
		return nil // Skip contacts without names
	}

	// Build canonical URI
	masterURI := bit.BuildPeopleURI(person.ResourceName)

	// Create the Bit
	b := bit.New(masterURI, bit.SystemPeople)

	// Parse update time from metadata
	if person.Metadata != nil && len(person.Metadata.Sources) > 0 {
		if updateTime := person.Metadata.Sources[0].UpdateTime; updateTime != "" {
			if t, err := time.Parse(time.RFC3339, updateTime); err == nil {
				b.UpdatedAt = t
			}
		}
	}

	// Set frontmatter
	b.Set("title", displayName)
	b.Set("external_id", fmt.Sprintf("gcontacts_%s", person.ResourceName))
	b.Set("type", "contact")
	b.Set("source", "google-people")

	// Primary email
	email := e.extractPrimaryEmail(person)
	if email != "" {
		b.Set("email", email)
	}

	// Primary phone
	phone := e.extractPrimaryPhone(person)
	if phone != "" {
		b.Set("phone", phone)
	}

	// Organization
	if len(person.Organizations) > 0 {
		org := person.Organizations[0]
		if org.Name != "" {
			b.Set("organization", org.Name)
		}
		if org.Title != "" {
			b.Set("job_title", org.Title)
		}
	}

	// Notes/biography
	if len(person.Biographies) > 0 && person.Biographies[0].Value != "" {
		b.Content = person.Biographies[0].Value
	}

	// Anniversary
	anniversary := e.extractAnniversary(person)
	if anniversary != "" {
		b.Set("anniversary", anniversary)
	}

	// Compute hash
	b.UpdateHash()

	return b
}

func (e *Engine) extractName(person Person) string {
	if len(person.Names) == 0 {
		return ""
	}

	// Prefer primary name
	for _, name := range person.Names {
		if name.Metadata != nil && name.Metadata.Primary {
			if name.DisplayName != "" {
				return name.DisplayName
			}
			return strings.TrimSpace(name.GivenName + " " + name.FamilyName)
		}
	}

	// Fall back to first name
	name := person.Names[0]
	if name.DisplayName != "" {
		return name.DisplayName
	}
	return strings.TrimSpace(name.GivenName + " " + name.FamilyName)
}

func (e *Engine) extractPrimaryEmail(person Person) string {
	if len(person.EmailAddresses) == 0 {
		return ""
	}

	// Prefer primary email
	for _, email := range person.EmailAddresses {
		if email.Metadata != nil && email.Metadata.Primary {
			return email.Value
		}
	}

	// Fall back to first email
	return person.EmailAddresses[0].Value
}

func (e *Engine) extractPrimaryPhone(person Person) string {
	if len(person.PhoneNumbers) == 0 {
		return ""
	}

	// Prefer primary phone
	for _, phone := range person.PhoneNumbers {
		if phone.Metadata != nil && phone.Metadata.Primary {
			return phone.Value
		}
	}

	// Fall back to first phone
	return person.PhoneNumbers[0].Value
}

func (e *Engine) extractAnniversary(person Person) string {
	for _, event := range person.Events {
		if event.Type == "anniversary" && event.Date != nil {
			d := event.Date
			if d.Year > 0 {
				return fmt.Sprintf("%04d-%02d-%02d", d.Year, d.Month, d.Day)
			}
			// Year might be 0 for recurring anniversaries
			return fmt.Sprintf("--%02d-%02d", d.Month, d.Day)
		}
	}
	return ""
}

func (e *Engine) getClient() (*http.Client, error) {
	// Parse credentials
	config, err := google.ConfigFromJSON([]byte(e.credentials),
		"https://www.googleapis.com/auth/contacts.readonly",
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
