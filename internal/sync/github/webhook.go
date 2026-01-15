package github

import (
	"context"
	"strings"
	"time"

	"thymer-bar/internal/bit"
	"thymer-bar/internal/webhook"
)

// WebhookHandler handles GitHub webhooks.
type WebhookHandler struct {
	onBit BitCallback
}

// NewWebhookHandler creates a new GitHub webhook handler.
func NewWebhookHandler(onBit BitCallback) *WebhookHandler {
	return &WebhookHandler{onBit: onBit}
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

	b := webhookIssueToBit(event.Repository.FullName, event.Issue)
	if h.onBit != nil {
		return h.onBit(ctx, b)
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

	b := webhookPRToBit(event.Repository.FullName, event.PullRequest)
	if h.onBit != nil {
		return h.onBit(ctx, b)
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
	b := webhookIssueToBit(event.Repository.FullName, event.Issue)
	if h.onBit != nil {
		return h.onBit(ctx, b)
	}
	return nil
}

// webhookIssueToBit converts a webhook issue to a Bit.
func webhookIssueToBit(repo string, issue *webhook.GitHubIssue) *bit.Bit {
	// Parse repo into owner/name
	parts := strings.SplitN(repo, "/", 2)
	owner, name := parts[0], parts[1]

	// Build canonical URI
	masterURI := bit.BuildGitHubURI(owner, name, issue.Number, "issues")

	// Extract labels
	labels := make([]string, 0, len(issue.Labels))
	for _, l := range issue.Labels {
		labels = append(labels, l.Name)
	}

	// Create the Bit
	b := bit.New(masterURI, bit.SystemGitHub)
	b.Content = issue.Body

	createdAt, _ := time.Parse(time.RFC3339, issue.CreatedAt)
	updatedAt, _ := time.Parse(time.RFC3339, issue.UpdatedAt)
	b.CreatedAt = createdAt
	b.UpdatedAt = updatedAt

	// Set frontmatter
	b.Set("title", issue.Title)
	b.Set("repo", repo)
	b.Set("number", issue.Number)
	b.Set("state", issue.State)
	b.Set("type", "issue")
	b.Set("url", issue.HTMLURL)
	b.Set("labels", labels)
	b.Set("author", issue.User.Login)

	// Track completion (state only - webhook doesn't include closed_at timestamp)
	if issue.State == "closed" {
		b.Set("completed_at", time.Now()) // Use current time as approximation
	}

	b.UpdateHash()
	return b
}

// webhookPRToBit converts a webhook PR to a Bit.
func webhookPRToBit(repo string, pr *webhook.GitHubPR) *bit.Bit {
	// Parse repo into owner/name
	parts := strings.SplitN(repo, "/", 2)
	owner, name := parts[0], parts[1]

	// Build canonical URI
	masterURI := bit.BuildGitHubURI(owner, name, pr.Number, "pulls")

	// Extract labels
	labels := make([]string, 0, len(pr.Labels))
	for _, l := range pr.Labels {
		labels = append(labels, l.Name)
	}

	// Determine state
	state := pr.State
	if pr.Merged {
		state = "merged"
	}

	// Create the Bit
	b := bit.New(masterURI, bit.SystemGitHub)
	b.Content = pr.Body

	createdAt, _ := time.Parse(time.RFC3339, pr.CreatedAt)
	updatedAt, _ := time.Parse(time.RFC3339, pr.UpdatedAt)
	b.CreatedAt = createdAt
	b.UpdatedAt = updatedAt

	// Set frontmatter
	b.Set("title", pr.Title)
	b.Set("repo", repo)
	b.Set("number", pr.Number)
	b.Set("state", state)
	b.Set("type", "pr")
	b.Set("url", pr.HTMLURL)
	b.Set("labels", labels)
	b.Set("author", pr.User.Login)
	b.Set("merged", pr.Merged)

	// Track completion (state only - webhook doesn't include timestamps)
	if pr.Merged || pr.State == "closed" {
		b.Set("completed_at", time.Now()) // Use current time as approximation
	}

	b.UpdateHash()
	return b
}
