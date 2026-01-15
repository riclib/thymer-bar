package cmd

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"thymer-bar/internal/bit"
	"thymer-bar/internal/config"
	"thymer-bar/internal/markdown"
	"thymer-bar/internal/secrets"
	"thymer-bar/internal/sync/github"
)

var lifelogDir string

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Sync all enabled sources",
	Run: func(cmd *cobra.Command, args []string) {
		// For now, just sync GitHub
		syncGitHub()
	},
}

var syncGitHubCmd = &cobra.Command{
	Use:   "github",
	Short: "Sync GitHub issues and PRs",
	Run: func(cmd *cobra.Command, args []string) {
		syncGitHub()
	},
}

func syncGitHub() {
	cfg, err := config.Load()
	if err != nil {
		fmt.Printf("Error loading config: %v\n", err)
		return
	}

	if !cfg.GitHub.Enabled {
		fmt.Println("GitHub sync not enabled. Run: llog github enable")
		return
	}

	if len(cfg.GitHub.Repos) == 0 {
		fmt.Println("No repos configured. Run: llog github add <owner/repo>")
		return
	}

	token, err := secrets.Get(secrets.GitHubToken)
	if err != nil {
		fmt.Printf("Error getting token: %v\n", err)
		return
	}
	if token == "" {
		fmt.Println("No GitHub token. Run: llog config set github-token <token>")
		return
	}

	// Setup markdown projector if lifelog dir is set
	var projector *markdown.Projector
	if lifelogDir != "" {
		projector = markdown.New(lifelogDir)
		fmt.Printf("Writing to %s\n", lifelogDir)
	}

	fmt.Printf("Syncing %d repos...\n", len(cfg.GitHub.Repos))

	count := 0
	engine := github.New(func(ctx context.Context, b *bit.Bit) error {
		count++
		state := b.GetString("state")
		typ := b.GetString("type")
		fmt.Printf("  [%s] #%v %s (%s)\n", typ, b.GetInt("number"), b.Title(), state)

		// Write to markdown if projector is set
		if projector != nil {
			if err := projector.WriteBit(b); err != nil {
				fmt.Printf("    Warning: failed to write markdown: %v\n", err)
			}
		}
		return nil
	})

	repos := make([]any, len(cfg.GitHub.Repos))
	for i, r := range cfg.GitHub.Repos {
		repos[i] = r
	}
	engine.Configure(map[string]any{
		"token":   token,
		"repos":   repos,
		"enabled": true,
	})

	ctx := context.Background()
	if err := engine.Sync(ctx); err != nil {
		fmt.Printf("Sync error: %v\n", err)
		return
	}

	fmt.Printf("\nSynced %d items\n", count)
}

func init() {
	rootCmd.AddCommand(syncCmd)
	syncCmd.AddCommand(syncGitHubCmd)

	// Default lifelog dir
	home, _ := os.UserHomeDir()
	defaultDir := filepath.Join(home, "lifelog")

	syncCmd.PersistentFlags().StringVarP(&lifelogDir, "dir", "d", defaultDir, "lifelog directory for markdown output")
}
