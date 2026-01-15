package cmd

import (
	"fmt"

	"github.com/spf13/cobra"

	"thymer-bar/internal/config"
	"thymer-bar/internal/secrets"
)

var githubCmd = &cobra.Command{
	Use:   "github",
	Short: "Manage GitHub sync configuration",
	Run: func(cmd *cobra.Command, args []string) {
		cfg, _ := config.Load()
		fmt.Printf("Enabled: %v\n", cfg.GitHub.Enabled)
		fmt.Printf("Repos:   %v\n", cfg.GitHub.Repos)
		fmt.Printf("Token:   %v\n", secrets.Has(secrets.GitHubToken))
	},
}

var githubEnableCmd = &cobra.Command{
	Use:   "enable",
	Short: "Enable GitHub sync",
	Run: func(cmd *cobra.Command, args []string) {
		cfg, _ := config.Load()
		cfg.GitHub.Enabled = true
		if err := cfg.Save(); err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}
		fmt.Println("GitHub sync enabled")
	},
}

var githubDisableCmd = &cobra.Command{
	Use:   "disable",
	Short: "Disable GitHub sync",
	Run: func(cmd *cobra.Command, args []string) {
		cfg, _ := config.Load()
		cfg.GitHub.Enabled = false
		if err := cfg.Save(); err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}
		fmt.Println("GitHub sync disabled")
	},
}

var githubAddCmd = &cobra.Command{
	Use:   "add <owner/repo>",
	Short: "Add a repository to sync",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		repo := args[0]
		cfg, _ := config.Load()

		// Check if already exists
		for _, r := range cfg.GitHub.Repos {
			if r == repo {
				fmt.Printf("Repo %q already configured\n", repo)
				return
			}
		}

		cfg.GitHub.Repos = append(cfg.GitHub.Repos, repo)
		if err := cfg.Save(); err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}
		fmt.Printf("Added %q\n", repo)
	},
}

var githubRemoveCmd = &cobra.Command{
	Use:   "remove <owner/repo>",
	Short: "Remove a repository from sync",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		repo := args[0]
		cfg, _ := config.Load()

		newRepos := make([]string, 0, len(cfg.GitHub.Repos))
		found := false
		for _, r := range cfg.GitHub.Repos {
			if r != repo {
				newRepos = append(newRepos, r)
			} else {
				found = true
			}
		}

		if !found {
			fmt.Printf("Repo %q not found\n", repo)
			return
		}

		cfg.GitHub.Repos = newRepos
		if err := cfg.Save(); err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}
		fmt.Printf("Removed %q\n", repo)
	},
}

func init() {
	rootCmd.AddCommand(githubCmd)
	githubCmd.AddCommand(githubEnableCmd)
	githubCmd.AddCommand(githubDisableCmd)
	githubCmd.AddCommand(githubAddCmd)
	githubCmd.AddCommand(githubRemoveCmd)
}
