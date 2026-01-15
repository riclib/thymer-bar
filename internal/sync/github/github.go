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

	"thymer-bar/internal/sync"
)

// SyncVersion tracks schema version - increment to force full resync
const SyncVersion = 1

// Engine syncs GitHub issues and PRs.
type Engine struct {
	client  *github.Client
	token   string
	repos   []string // "owner/repo" format
	enabled bool
	log     *slog.Logger

	// Callback for publishing records
	onRecord func(ctx context.Context, record *sync.Record) error
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

// New creates a new GitHub sync engine.
func New(onRecord func(ctx context.Context, record *sync.Record) error) *Engine {
	return &Engine{
		onRecord: onRecord,
		log:      slog.Default(),
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
// If since is provided, only items updated after that time are fetched.
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

			record := e.issueToRecord(repo, issue)
			stats.Fetched++

			if e.onRecord != nil {
				if err := e.onRecord(ctx, record); err != nil {
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

			record := e.prToRecord(repo, pr)
			stats.Fetched++

			if e.onRecord != nil {
				if err := e.onRecord(ctx, record); err != nil {
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

func (e *Engine) issueToRecord(repo string, issue *github.Issue) *sync.Record {
	// ID format: github_{owner}_{repo}_{number}
	repoSlug := strings.ReplaceAll(repo, "/", "_")
	externalID := fmt.Sprintf("github_%s_%d", repoSlug, issue.GetNumber())

	labels := make([]string, 0, len(issue.Labels))
	for _, l := range issue.Labels {
		labels = append(labels, l.GetName())
	}

	assignees := make([]string, 0, len(issue.Assignees))
	for _, a := range issue.Assignees {
		assignees = append(assignees, a.GetLogin())
	}

	// Build rich markdown content
	var content strings.Builder
	content.WriteString(fmt.Sprintf("# #%d %s\n\n", issue.GetNumber(), issue.GetTitle()))
	content.WriteString(fmt.Sprintf("**State:** %s", issue.GetState()))
	if issue.GetUser() != nil {
		content.WriteString(fmt.Sprintf(" | **Author:** %s", issue.GetUser().GetLogin()))
	}
	if len(labels) > 0 {
		content.WriteString(fmt.Sprintf(" | **Labels:** %s", strings.Join(labels, ", ")))
	}
	content.WriteString("\n\n")
	if body := strings.TrimSpace(issue.GetBody()); body != "" {
		content.WriteString(body)
		content.WriteString("\n\n")
	}
	content.WriteString(fmt.Sprintf("[View on GitHub](%s)", issue.GetHTMLURL()))

	// Determine completion state
	var completedAt *time.Time
	if issue.GetState() == "closed" && issue.ClosedAt != nil {
		t := issue.ClosedAt.Time
		completedAt = &t
	}

	return &sync.Record{
		Source:      "github",
		ExternalID:  externalID,
		Type:        "issue",
		Title:       issue.GetTitle(),
		Content:     content.String(),
		URL:         issue.GetHTMLURL(),
		CompletedAt: completedAt,
		Fields: map[string]any{
			"repo":      repo,
			"number":    issue.GetNumber(),
			"state":     issue.GetState(),
			"type":      "issue",
			"labels":    labels,
			"author":    issue.GetUser().GetLogin(),
			"assignees": assignees,
			"comments":  issue.GetComments(),
		},
		CreatedAt: issue.GetCreatedAt().Time,
		UpdatedAt: issue.GetUpdatedAt().Time,
	}
}

func (e *Engine) prToRecord(repo string, pr *github.PullRequest) *sync.Record {
	// ID format: github_{owner}_{repo}_{number}
	repoSlug := strings.ReplaceAll(repo, "/", "_")
	externalID := fmt.Sprintf("github_%s_%d", repoSlug, pr.GetNumber())

	labels := make([]string, 0, len(pr.Labels))
	for _, l := range pr.Labels {
		labels = append(labels, l.GetName())
	}

	assignees := make([]string, 0, len(pr.Assignees))
	for _, a := range pr.Assignees {
		assignees = append(assignees, a.GetLogin())
	}

	// Build rich markdown content
	var content strings.Builder
	content.WriteString(fmt.Sprintf("# PR #%d %s\n\n", pr.GetNumber(), pr.GetTitle()))
	content.WriteString(fmt.Sprintf("**State:** %s", pr.GetState()))
	if pr.GetMerged() {
		content.WriteString(" (merged)")
	}
	if pr.GetUser() != nil {
		content.WriteString(fmt.Sprintf(" | **Author:** %s", pr.GetUser().GetLogin()))
	}
	if len(labels) > 0 {
		content.WriteString(fmt.Sprintf(" | **Labels:** %s", strings.Join(labels, ", ")))
	}
	if pr.Additions != nil && pr.Deletions != nil {
		content.WriteString(fmt.Sprintf(" | **Changes:** +%d/-%d", pr.GetAdditions(), pr.GetDeletions()))
	}
	content.WriteString("\n\n")
	if body := strings.TrimSpace(pr.GetBody()); body != "" {
		content.WriteString(body)
		content.WriteString("\n\n")
	}
	content.WriteString(fmt.Sprintf("[View on GitHub](%s)", pr.GetHTMLURL()))

	// Determine completion state
	var completedAt *time.Time
	state := pr.GetState()
	if pr.GetMerged() && pr.MergedAt != nil {
		t := pr.MergedAt.Time
		completedAt = &t
		state = "merged"
	} else if pr.GetState() == "closed" && pr.ClosedAt != nil {
		t := pr.ClosedAt.Time
		completedAt = &t
	}

	return &sync.Record{
		Source:      "github",
		ExternalID:  externalID,
		Type:        "pr",
		Title:       pr.GetTitle(),
		Content:     content.String(),
		URL:         pr.GetHTMLURL(),
		CompletedAt: completedAt,
		Fields: map[string]any{
			"repo":          repo,
			"number":        pr.GetNumber(),
			"state":         state,
			"type":          "pull_request",
			"labels":        labels,
			"author":        pr.GetUser().GetLogin(),
			"assignees":     assignees,
			"comments":      pr.GetComments(),
			"merged":        pr.GetMerged(),
			"additions":     pr.GetAdditions(),
			"deletions":     pr.GetDeletions(),
			"changed_files": pr.GetChangedFiles(),
		},
		CreatedAt: pr.GetCreatedAt().Time,
		UpdatedAt: pr.GetUpdatedAt().Time,
	}
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
