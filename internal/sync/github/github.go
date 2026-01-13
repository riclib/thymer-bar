// Package github provides GitHub issues and PRs sync engine.
package github

import (
	"context"
	"fmt"

	"github.com/google/go-github/v66/github"
	"golang.org/x/oauth2"

	"thymer-bar/internal/sync"
)

// Engine syncs GitHub issues and PRs.
type Engine struct {
	client  *github.Client
	token   string
	repos   []string // "owner/repo" format
	enabled bool

	// Callbacks for publishing events
	onRecord func(ctx context.Context, record *sync.Record) error
}

// Config holds GitHub sync configuration.
type Config struct {
	Token string   `json:"token"`
	Repos []string `json:"repos"` // List of "owner/repo" to sync
}

// New creates a new GitHub sync engine.
func New(onRecord func(ctx context.Context, record *sync.Record) error) *Engine {
	return &Engine{
		onRecord: onRecord,
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

func (e *Engine) Sync(ctx context.Context) error {
	if !e.Enabled() {
		return fmt.Errorf("github engine not enabled or configured")
	}

	for _, repo := range e.repos {
		if err := e.syncRepo(ctx, repo); err != nil {
			return fmt.Errorf("failed to sync %s: %w", repo, err)
		}
	}

	return nil
}

func (e *Engine) syncRepo(ctx context.Context, repo string) error {
	// Parse owner/repo
	var owner, name string
	if _, err := fmt.Sscanf(repo, "%s/%s", &owner, &name); err != nil {
		// Try splitting on /
		for i, c := range repo {
			if c == '/' {
				owner = repo[:i]
				name = repo[i+1:]
				break
			}
		}
	}

	if owner == "" || name == "" {
		return fmt.Errorf("invalid repo format: %s (expected owner/repo)", repo)
	}

	// Fetch issues (includes PRs)
	opts := &github.IssueListByRepoOptions{
		State:     "all",
		Sort:      "updated",
		Direction: "desc",
		ListOptions: github.ListOptions{
			PerPage: 100,
		},
	}

	issues, _, err := e.client.Issues.ListByRepo(ctx, owner, name, opts)
	if err != nil {
		return fmt.Errorf("failed to list issues: %w", err)
	}

	for _, issue := range issues {
		record := e.issueToRecord(repo, issue)
		if e.onRecord != nil {
			if err := e.onRecord(ctx, record); err != nil {
				return fmt.Errorf("failed to process issue %d: %w", issue.GetNumber(), err)
			}
		}
	}

	return nil
}

func (e *Engine) issueToRecord(repo string, issue *github.Issue) *sync.Record {
	recordType := "issue"
	if issue.IsPullRequest() {
		recordType = "pr"
	}

	state := issue.GetState()
	if issue.IsPullRequest() && issue.GetPullRequestLinks() != nil {
		// Could check if merged, but would need another API call
	}

	labels := make([]string, 0, len(issue.Labels))
	for _, l := range issue.Labels {
		labels = append(labels, l.GetName())
	}

	return &sync.Record{
		Source:     "github",
		ExternalID: fmt.Sprintf("%s#%d", repo, issue.GetNumber()),
		Type:       recordType,
		Title:      issue.GetTitle(),
		Content:    issue.GetBody(),
		URL:        issue.GetHTMLURL(),
		Fields: map[string]any{
			"repo":   repo,
			"number": issue.GetNumber(),
			"state":  state,
			"labels": labels,
			"author": issue.GetUser().GetLogin(),
		},
		CreatedAt: issue.GetCreatedAt().Time,
		UpdatedAt: issue.GetUpdatedAt().Time,
	}
}
