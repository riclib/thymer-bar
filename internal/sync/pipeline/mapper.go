package pipeline

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
)

// Mapper transforms fetched events into mapped records.
type Mapper interface {
	Map(event *FetchedEvent) (*MappedRecord, error)
}

// GitHubMapper maps GitHub issues/PRs to normalized records.
type GitHubMapper struct{}

func (m *GitHubMapper) Map(event *FetchedEvent) (*MappedRecord, error) {
	if event.Source != "github" {
		return nil, fmt.Errorf("GitHubMapper cannot map source: %s", event.Source)
	}

	record := &MappedRecord{
		Source:     event.Source,
		ExternalID: event.ExternalID,
		Collection: "Issues",
		Title:      event.Title,
		URL:        event.URL,
		CreatedAt:  event.CreatedAt,
		UpdatedAt:  event.UpdatedAt,
	}

	// Map external state
	record.ExternalState = "open"
	if event.State == "closed" || event.State == "merged" {
		record.ExternalState = "closed"
	}

	// Map type
	if t, ok := event.Fields["type"].(string); ok {
		record.Type = t
	} else {
		record.Type = "issue"
	}

	// Map repo
	if repo, ok := event.Fields["repo"].(string); ok {
		record.Repo = repo
	}

	// Map number
	if num, ok := event.Fields["number"].(int); ok {
		record.Number = num
	} else if numFloat, ok := event.Fields["number"].(float64); ok {
		record.Number = int(numFloat)
	}

	// Map author
	if author, ok := event.Fields["author"].(string); ok {
		record.Author = author
	}

	// Map assignee (take first if multiple)
	if assignees, ok := event.Fields["assignees"].([]string); ok && len(assignees) > 0 {
		record.Assignee = assignees[0]
	} else if assignees, ok := event.Fields["assignees"].([]any); ok && len(assignees) > 0 {
		if s, ok := assignees[0].(string); ok {
			record.Assignee = s
		}
	}

	// Store extra fields that might be useful
	record.Extra = map[string]any{}
	if labels, ok := event.Fields["labels"]; ok {
		record.Extra["labels"] = labels
	}
	if comments, ok := event.Fields["comments"]; ok {
		record.Extra["comments"] = comments
	}

	return record, nil
}

// ComputeHash computes a content hash for a mapped record.
// This is used for change detection - only fields that matter are hashed.
func ComputeHash(record *MappedRecord) string {
	// Hash the mapped fields that represent meaningful changes
	// Deliberately exclude: timestamps, thymer-side status
	parts := []string{
		record.Title,
		record.ExternalState,
		record.Type,
		record.Repo,
		fmt.Sprintf("%d", record.Number),
		record.Author,
		record.Assignee,
		record.URL,
	}

	// Include labels if present (sorted for consistency)
	if labels, ok := record.Extra["labels"].([]string); ok {
		sortedLabels := make([]string, len(labels))
		copy(sortedLabels, labels)
		// Simple sort
		for i := range sortedLabels {
			for j := i + 1; j < len(sortedLabels); j++ {
				if sortedLabels[i] > sortedLabels[j] {
					sortedLabels[i], sortedLabels[j] = sortedLabels[j], sortedLabels[i]
				}
			}
		}
		parts = append(parts, strings.Join(sortedLabels, ","))
	}

	h := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(h[:16]) // 32 hex chars is plenty
}

// GetMapper returns the appropriate mapper for a source.
func GetMapper(source string) Mapper {
	switch source {
	case "github":
		return &GitHubMapper{}
	default:
		return nil
	}
}

// MappedRecordToJSON converts a mapped record to JSON for storage.
func MappedRecordToJSON(record *MappedRecord) ([]byte, error) {
	return json.Marshal(record)
}

// JSONToMappedRecord parses a mapped record from JSON.
func JSONToMappedRecord(data []byte) (*MappedRecord, error) {
	var record MappedRecord
	if err := json.Unmarshal(data, &record); err != nil {
		return nil, err
	}
	return &record, nil
}
