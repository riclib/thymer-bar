// Package bit provides the universal Bit type for sync operations.
// A Bit is the core primitive: frontmatter + markdown content with URI-based identity.
package bit

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

// Bit is the universal sync primitive.
// Duck-typed via frontmatter fields - a Bit with "repo" and "number" can sync to GitHub.
type Bit struct {
	ID           string         `json:"id"`            // Local UUID
	MasterURI    string         `json:"master_uri"`    // Canonical URI: github://owner/repo/issues/123
	MasterSystem string         `json:"master_system"` // github, thymer, local
	Frontmatter  map[string]any `json:"frontmatter"`   // Duck-typed metadata
	Content      string         `json:"content"`       // Markdown body
	Hash         string         `json:"hash"`          // Content hash for change detection
	CreatedAt    time.Time      `json:"created_at"`
	UpdatedAt    time.Time      `json:"updated_at"`
}

// New creates a new Bit with a generated ID.
func New(masterURI, masterSystem string) *Bit {
	return &Bit{
		ID:           uuid.New().String(),
		MasterURI:    masterURI,
		MasterSystem: masterSystem,
		Frontmatter:  make(map[string]any),
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}
}

// URI Scheme: system://path
// Examples:
//   github://riclib/thymer-bar/issues/123
//   github://riclib/thymer-bar/pulls/456
//   thymer://collections/Issues/records/{uuid}
//   local://bits/{id}

// ParseURI extracts system and path from a master_uri.
func ParseURI(uri string) (system, path string, err error) {
	parts := strings.SplitN(uri, "://", 2)
	if len(parts) != 2 {
		return "", "", fmt.Errorf("invalid URI: %s", uri)
	}
	return parts[0], parts[1], nil
}

// BuildGitHubURI creates a GitHub URI for an issue or PR.
func BuildGitHubURI(owner, repo string, number int, itemType string) string {
	// itemType: "issues" or "pulls"
	return fmt.Sprintf("github://%s/%s/%s/%d", owner, repo, itemType, number)
}

// BuildLocalURI creates a local URI for a native bit.
func BuildLocalURI(id string) string {
	return fmt.Sprintf("local://bits/%s", id)
}

// ComputeHash computes content hash for change detection.
// Hash includes frontmatter + content to detect any meaningful change.
func (b *Bit) ComputeHash() string {
	// Serialize frontmatter deterministically
	fmJSON, _ := json.Marshal(b.Frontmatter)
	data := string(fmJSON) + "|" + b.Content
	h := sha256.Sum256([]byte(data))
	return hex.EncodeToString(h[:16]) // 128-bit hash as hex
}

// UpdateHash recomputes and stores the hash.
func (b *Bit) UpdateHash() {
	b.Hash = b.ComputeHash()
}

// HasField checks if frontmatter has a specific field.
func (b *Bit) HasField(name string) bool {
	_, ok := b.Frontmatter[name]
	return ok
}

// Get returns a frontmatter field value.
func (b *Bit) Get(name string) any {
	return b.Frontmatter[name]
}

// GetString returns a frontmatter field as string, or empty if not present/not string.
func (b *Bit) GetString(name string) string {
	if v, ok := b.Frontmatter[name].(string); ok {
		return v
	}
	return ""
}

// GetInt returns a frontmatter field as int, or 0 if not present/not int.
func (b *Bit) GetInt(name string) int {
	switch v := b.Frontmatter[name].(type) {
	case int:
		return v
	case int64:
		return int(v)
	case float64:
		return int(v)
	}
	return 0
}

// GetStrings returns a frontmatter field as []string, or nil if not present.
func (b *Bit) GetStrings(name string) []string {
	switch v := b.Frontmatter[name].(type) {
	case []string:
		return v
	case []any:
		result := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				result = append(result, s)
			}
		}
		return result
	}
	return nil
}

// Set sets a frontmatter field.
func (b *Bit) Set(name string, value any) {
	if b.Frontmatter == nil {
		b.Frontmatter = make(map[string]any)
	}
	b.Frontmatter[name] = value
}

// Delete removes a frontmatter field.
func (b *Bit) Delete(name string) {
	delete(b.Frontmatter, name)
}

// Duck typing helpers - determine what a Bit can do based on frontmatter.

// CanSyncToGitHub returns true if this Bit has the required GitHub fields.
func (b *Bit) CanSyncToGitHub() bool {
	return b.HasField("repo") && b.HasField("number")
}

// CanSyncToThymer returns true if this Bit can be rendered to Thymer.
func (b *Bit) CanSyncToThymer() bool {
	return b.HasField("title")
}

// IsIssue returns true if this Bit represents an issue (GitHub, Linear, etc).
func (b *Bit) IsIssue() bool {
	t := b.GetString("type")
	return t == "issue" || t == "pr" || t == "pull_request"
}

// IsCompleted returns true if this Bit has a completed timestamp.
func (b *Bit) IsCompleted() bool {
	return b.HasField("completed_at")
}

// Title returns the title from frontmatter.
func (b *Bit) Title() string {
	return b.GetString("title")
}

// State returns the external state (open, closed, merged).
func (b *Bit) State() string {
	return b.GetString("state")
}
