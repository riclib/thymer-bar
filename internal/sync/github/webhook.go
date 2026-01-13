package github

import (
	"context"
	"fmt"
	"time"

	"thymer-bar/internal/sync"
	"thymer-bar/internal/webhook"
)

// WebhookHandler handles GitHub webhooks.
type WebhookHandler struct {
	onRecord func(ctx context.Context, record *sync.Record) error
}

// NewWebhookHandler creates a new GitHub webhook handler.
func NewWebhookHandler(onRecord func(ctx context.Context, record *sync.Record) error) *WebhookHandler {
	return &WebhookHandler{onRecord: onRecord}
}

func (h *WebhookHandler) Source() string {
	return "github"
}

func (h *WebhookHandler) Handle(ctx context.Context, event string, payload []byte) error {
	switch event {
	case "issues":
		return h.handleIssues(ctx, payload)
	case "pull_request":
		return h.handlePullRequest(ctx, payload)
	case "issue_comment":
		return h.handleIssueComment(ctx, payload)
	case "ping":
		// GitHub sends ping when webhook is first configured
		return nil
	default:
		// Ignore other events
		return nil
	}
}

func (h *WebhookHandler) handleIssues(ctx context.Context, payload []byte) error {
	event, err := webhook.ParseGitHubEvent(payload)
	if err != nil {
		return err
	}

	if event.Issue == nil {
		return nil
	}

	// Only process relevant actions
	switch event.Action {
	case "opened", "closed", "reopened", "edited", "labeled", "unlabeled":
		// Process these
	default:
		return nil
	}

	record := issueToRecord(event.Repository.FullName, event.Issue)
	if h.onRecord != nil {
		return h.onRecord(ctx, record)
	}
	return nil
}

func (h *WebhookHandler) handlePullRequest(ctx context.Context, payload []byte) error {
	event, err := webhook.ParseGitHubEvent(payload)
	if err != nil {
		return err
	}

	if event.PullRequest == nil {
		return nil
	}

	// Only process relevant actions
	switch event.Action {
	case "opened", "closed", "reopened", "edited", "labeled", "unlabeled", "merged":
		// Process these
	default:
		return nil
	}

	record := prToRecord(event.Repository.FullName, event.PullRequest)
	if h.onRecord != nil {
		return h.onRecord(ctx, record)
	}
	return nil
}

func (h *WebhookHandler) handleIssueComment(ctx context.Context, payload []byte) error {
	// For now, we just trigger a refresh of the issue
	// Could be extended to track comments
	event, err := webhook.ParseGitHubEvent(payload)
	if err != nil {
		return err
	}

	if event.Issue == nil {
		return nil
	}

	// Treat as issue update
	record := issueToRecord(event.Repository.FullName, event.Issue)
	if h.onRecord != nil {
		return h.onRecord(ctx, record)
	}
	return nil
}

func issueToRecord(repo string, issue *webhook.GitHubIssue) *sync.Record {
	labels := make([]string, 0, len(issue.Labels))
	for _, l := range issue.Labels {
		labels = append(labels, l.Name)
	}

	createdAt, _ := time.Parse(time.RFC3339, issue.CreatedAt)
	updatedAt, _ := time.Parse(time.RFC3339, issue.UpdatedAt)

	return &sync.Record{
		Source:     "github",
		ExternalID: fmt.Sprintf("%s#%d", repo, issue.Number),
		Type:       "issue",
		Title:      issue.Title,
		Content:    issue.Body,
		URL:        issue.HTMLURL,
		Fields: map[string]any{
			"repo":   repo,
			"number": issue.Number,
			"state":  issue.State,
			"labels": labels,
			"author": issue.User.Login,
		},
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}
}

func prToRecord(repo string, pr *webhook.GitHubPR) *sync.Record {
	labels := make([]string, 0, len(pr.Labels))
	for _, l := range pr.Labels {
		labels = append(labels, l.Name)
	}

	state := pr.State
	if pr.Merged {
		state = "merged"
	}

	createdAt, _ := time.Parse(time.RFC3339, pr.CreatedAt)
	updatedAt, _ := time.Parse(time.RFC3339, pr.UpdatedAt)

	return &sync.Record{
		Source:     "github",
		ExternalID: fmt.Sprintf("%s#%d", repo, pr.Number),
		Type:       "pr",
		Title:      pr.Title,
		Content:    pr.Body,
		URL:        pr.HTMLURL,
		Fields: map[string]any{
			"repo":   repo,
			"number": pr.Number,
			"state":  state,
			"labels": labels,
			"author": pr.User.Login,
			"merged": pr.Merged,
		},
		CreatedAt: createdAt,
		UpdatedAt: updatedAt,
	}
}
