// Package github provides GitHub issues and PRs sync engine.
package github

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/google/go-github/v66/github"
	"golang.org/x/oauth2"

	"thymer-bar/internal/bit"
	"thymer-bar/internal/sync"
)

// SyncVersion tracks schema version - increment to force full resync
const SyncVersion = 1

// BitCallback is called for each fetched bit.
type BitCallback func(ctx context.Context, b *bit.Bit) error

// Engine syncs GitHub issues and PRs.
type Engine struct {
	client  *github.Client
	token   string
	repos   []string // "owner/repo" format
	enabled bool
	log     *slog.Logger

	// Callback for publishing bits
	onBit BitCallback
}

// Config holds GitHub sync configuration.
type Config struct {
	Token string   `json:"token"`
	Repos []string `json:"repos"` // List of "owner/repo" to sync
}

// SyncStats tracks sync operation results
type SyncStats struct {
	Fetched   int
	Created   int
	Updated   int
	Unchanged int
	Errors    []error
}

// New creates a new GitHub sync engine with Bit callback.
func New(onBit BitCallback) *Engine {
	return &Engine{
		onBit: onBit,
		log:   slog.Default(),
	}
}

func (e *Engine) Name() string        { return "github" }
func (e *Engine) DisplayName() string { return "GitHub" }
func (e *Engine) Enabled() bool       { return e.enabled && e.token != "" }

func (e *Engine) Configure(cfg map[string]any) error {
	if token, ok := cfg["token"].(string); ok {
		e.token = token
	}
	if repos, ok := cfg["repos"].([]any); ok {
		e.repos = make([]string, 0, len(repos))
		for _, r := range repos {
			if s, ok := r.(string); ok {
				e.repos = append(e.repos, s)
			}
		}
	}
	if enabled, ok := cfg["enabled"].(bool); ok {
		e.enabled = enabled
	}

	if e.token != "" {
		ts := oauth2.StaticTokenSource(&oauth2.Token{AccessToken: e.token})
		tc := oauth2.NewClient(context.Background(), ts)
		e.client = github.NewClient(tc)
	}

	return nil
}

// Sync fetches all issues and PRs from configured repos.
func (e *Engine) Sync(ctx context.Context) error {
	return e.SyncSince(ctx, nil)
}

// SyncSince fetches issues/PRs updated since the given time.
func (e *Engine) SyncSince(ctx context.Context, since *time.Time) error {
	if !e.Enabled() {
		return fmt.Errorf("github engine not enabled or configured")
	}

	stats := &SyncStats{Errors: make([]error, 0)}

	for _, repo := range e.repos {
		repoStats, err := e.syncRepo(ctx, repo, since)
		if err != nil {
			e.log.Error("failed to sync repo", "repo", repo, "error", err)
			stats.Errors = append(stats.Errors, fmt.Errorf("%s: %w", repo, err))
			continue
		}
		stats.Fetched += repoStats.Fetched
		stats.Created += repoStats.Created
		stats.Updated += repoStats.Updated
		stats.Unchanged += repoStats.Unchanged
	}

	e.log.Info("github sync completed",
		"fetched", stats.Fetched,
		"created", stats.Created,
		"updated", stats.Updated,
		"unchanged", stats.Unchanged,
		"errors", len(stats.Errors))

	return nil
}

func (e *Engine) syncRepo(ctx context.Context, repo string, since *time.Time) (*SyncStats, error) {
	owner, name, err := parseRepo(repo)
	if err != nil {
		return nil, err
	}

	stats := &SyncStats{Errors: make([]error, 0)}

	// Fetch issues with pagination
	issueOpts := &github.IssueListByRepoOptions{
		State:     "all",
		Sort:      "updated",
		Direction: "desc",
		ListOptions: github.ListOptions{
			PerPage: 100,
		},
	}
	if since != nil {
		issueOpts.Since = *since
	}

	// Paginate through all issues
	for {
		issues, resp, err := e.client.Issues.ListByRepo(ctx, owner, name, issueOpts)
		if err != nil {
			if e.isRateLimited(resp) {
				e.waitForRateLimit(ctx, resp)
				continue
			}
			return stats, fmt.Errorf("failed to list issues: %w", err)
		}

		for _, issue := range issues {
			// Skip PRs - we fetch them separately for full metadata
			if issue.IsPullRequest() {
				continue
			}

			b := e.issueToBit(repo, owner, name, issue)
			stats.Fetched++

			if e.onBit != nil {
				if err := e.onBit(ctx, b); err != nil {
					stats.Errors = append(stats.Errors, err)
				}
			}
		}

		// Check for more pages
		if resp.NextPage == 0 {
			break
		}
		issueOpts.Page = resp.NextPage
	}

	// Fetch PRs with pagination (separate API for full PR metadata)
	prOpts := &github.PullRequestListOptions{
		State:     "all",
		Sort:      "updated",
		Direction: "desc",
		ListOptions: github.ListOptions{
			PerPage: 100,
		},
	}

	for {
		prs, resp, err := e.client.PullRequests.List(ctx, owner, name, prOpts)
		if err != nil {
			if e.isRateLimited(resp) {
				e.waitForRateLimit(ctx, resp)
				continue
			}
			return stats, fmt.Errorf("failed to list PRs: %w", err)
		}

		for _, pr := range prs {
			// Filter by since if provided (PR API doesn't have Since param)
			if since != nil && pr.GetUpdatedAt().Time.Before(*since) {
				continue
			}

			b := e.prToBit(repo, owner, name, pr)
			stats.Fetched++

			if e.onBit != nil {
				if err := e.onBit(ctx, b); err != nil {
					stats.Errors = append(stats.Errors, err)
				}
			}
		}

		// Check for more pages
		if resp.NextPage == 0 {
			break
		}
		prOpts.Page = resp.NextPage
	}

	return stats, nil
}

// issueToBit converts a GitHub issue to a Bit.
func (e *Engine) issueToBit(repo, owner, name string, issue *github.Issue) *bit.Bit {
	// Build canonical URI
	masterURI := bit.BuildGitHubURI(owner, name, issue.GetNumber(), "issues")

	// Extract labels
	labels := make([]string, 0, len(issue.Labels))
	for _, l := range issue.Labels {
		labels = append(labels, l.GetName())
	}

	// Extract assignees
	assignees := make([]string, 0, len(issue.Assignees))
	for _, a := range issue.Assignees {
		assignees = append(assignees, a.GetLogin())
	}

	// Build markdown content (the issue body)
	var content strings.Builder
	if body := strings.TrimSpace(issue.GetBody()); body != "" {
		content.WriteString(body)
	}

	// Create the Bit
	b := bit.New(masterURI, bit.SystemGitHub)
	b.Content = content.String()
	b.CreatedAt = issue.GetCreatedAt().Time
	b.UpdatedAt = issue.GetUpdatedAt().Time

	// Set frontmatter (duck-typed fields)
	b.Set("title", issue.GetTitle())
	b.Set("repo", repo)
	b.Set("number", issue.GetNumber())
	b.Set("state", issue.GetState())
	b.Set("type", "issue")
	b.Set("url", issue.GetHTMLURL())
	b.Set("labels", labels)
	b.Set("author", issue.GetUser().GetLogin())
	b.Set("assignees", assignees)
	b.Set("comments", issue.GetComments())

	// Track completion
	if issue.GetState() == "closed" && issue.ClosedAt != nil {
		b.Set("completed_at", issue.ClosedAt.Time)
	}

	// Compute hash for change detection
	b.UpdateHash()

	return b
}

// prToBit converts a GitHub PR to a Bit.
func (e *Engine) prToBit(repo, owner, name string, pr *github.PullRequest) *bit.Bit {
	// Build canonical URI
	masterURI := bit.BuildGitHubURI(owner, name, pr.GetNumber(), "pulls")

	// Extract labels
	labels := make([]string, 0, len(pr.Labels))
	for _, l := range pr.Labels {
		labels = append(labels, l.GetName())
	}

	// Extract assignees
	assignees := make([]string, 0, len(pr.Assignees))
	for _, a := range pr.Assignees {
		assignees = append(assignees, a.GetLogin())
	}

	// Build markdown content (the PR body)
	var content strings.Builder
	if body := strings.TrimSpace(pr.GetBody()); body != "" {
		content.WriteString(body)
	}

	// Determine state
	state := pr.GetState()
	if pr.GetMerged() {
		state = "merged"
	}

	// Create the Bit
	b := bit.New(masterURI, bit.SystemGitHub)
	b.Content = content.String()
	b.CreatedAt = pr.GetCreatedAt().Time
	b.UpdatedAt = pr.GetUpdatedAt().Time

	// Set frontmatter (duck-typed fields)
	b.Set("title", pr.GetTitle())
	b.Set("repo", repo)
	b.Set("number", pr.GetNumber())
	b.Set("state", state)
	b.Set("type", "pr")
	b.Set("url", pr.GetHTMLURL())
	b.Set("labels", labels)
	b.Set("author", pr.GetUser().GetLogin())
	b.Set("assignees", assignees)
	b.Set("comments", pr.GetComments())
	b.Set("merged", pr.GetMerged())
	b.Set("additions", pr.GetAdditions())
	b.Set("deletions", pr.GetDeletions())
	b.Set("changed_files", pr.GetChangedFiles())

	// Track completion
	if pr.GetMerged() && pr.MergedAt != nil {
		b.Set("completed_at", pr.MergedAt.Time)
	} else if pr.GetState() == "closed" && pr.ClosedAt != nil {
		b.Set("completed_at", pr.ClosedAt.Time)
	}

	// Compute hash for change detection
	b.UpdateHash()

	return b
}

// parseRepo splits "owner/repo" into components
func parseRepo(repo string) (owner, name string, err error) {
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("invalid repo format: %s (expected owner/repo)", repo)
	}
	return parts[0], parts[1], nil
}

// isRateLimited checks if response indicates rate limiting
func (e *Engine) isRateLimited(resp *github.Response) bool {
	if resp == nil {
		return false
	}
	return resp.Rate.Remaining == 0
}

// waitForRateLimit waits until rate limit resets
func (e *Engine) waitForRateLimit(ctx context.Context, resp *github.Response) {
	if resp == nil || resp.Rate.Reset.IsZero() {
		// Default wait if no reset time
		select {
		case <-ctx.Done():
		case <-time.After(60 * time.Second):
		}
		return
	}

	waitDuration := time.Until(resp.Rate.Reset.Time) + time.Second
	e.log.Warn("rate limited, waiting", "until", resp.Rate.Reset.Time, "duration", waitDuration)

	select {
	case <-ctx.Done():
	case <-time.After(waitDuration):
	}
}

// ValidateToken checks if the configured token is valid
func (e *Engine) ValidateToken(ctx context.Context) error {
	if e.client == nil {
		return fmt.Errorf("client not configured")
	}

	_, _, err := e.client.Users.Get(ctx, "")
	if err != nil {
		return fmt.Errorf("invalid token: %w", err)
	}
	return nil
}

// Legacy support: sync.Engine interface compatibility
var _ sync.Engine = (*Engine)(nil)
