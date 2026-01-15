package cmd

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"thymer-bar/internal/config"
	"thymer-bar/internal/secrets"
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Manage configuration and secrets",
	Long:  `View and modify thymer-bar configuration and secrets stored in the system keychain.`,
}

var configListCmd = &cobra.Command{
	Use:   "list",
	Short: "List all secrets",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Secrets:")
		for _, key := range secrets.KnownKeys() {
			if secrets.Has(key) {
				value, _ := secrets.Get(key)
				fmt.Printf("  ✓ %s = %s\n", key, mask(value))
			} else {
				fmt.Printf("  ✗ %s (not set)\n", key)
			}
		}
	},
}

var configSetCmd = &cobra.Command{
	Use:   "set <key> <value>",
	Short: "Store a secret in keychain",
	Args:  cobra.ExactArgs(2),
	Run: func(cmd *cobra.Command, args []string) {
		key, value := args[0], args[1]
		if err := secrets.Set(key, value); err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}
		fmt.Printf("Stored %q in system keychain\n", key)
	},
}

var configGetCmd = &cobra.Command{
	Use:   "get <key>",
	Short: "Get a secret (masked)",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		key := args[0]
		value, err := secrets.Get(key)
		if err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}
		if value == "" {
			fmt.Printf("%s: (not set)\n", key)
		} else {
			fmt.Printf("%s: %s\n", key, mask(value))
		}
	},
}

var configDeleteCmd = &cobra.Command{
	Use:   "delete <key>",
	Short: "Remove a secret from keychain",
	Args:  cobra.ExactArgs(1),
	Run: func(cmd *cobra.Command, args []string) {
		key := args[0]
		if err := secrets.Delete(key); err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}
		fmt.Printf("Deleted %q from keychain\n", key)
	},
}

var configPathsCmd = &cobra.Command{
	Use:   "paths",
	Short: "Show configuration paths",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("Config:    %s\n", config.ConfigDir())
		fmt.Printf("Data:      %s\n", config.DataDir())
		fmt.Printf("State:     %s\n", config.StateDir())
		fmt.Printf("Cache:     %s\n", config.CacheDir())
		fmt.Printf("Database:  %s\n", config.DatabaseFile())
		fmt.Printf("JetStream: %s\n", config.JetStreamDir())
	},
}

var configInitCmd = &cobra.Command{
	Use:   "init",
	Short: "Create all directories",
	Run: func(cmd *cobra.Command, args []string) {
		if err := config.EnsureDirs(); err != nil {
			fmt.Printf("Error: %v\n", err)
			return
		}
		fmt.Println("Directories created")
	},
}

func init() {
	rootCmd.AddCommand(configCmd)
	configCmd.AddCommand(configListCmd)
	configCmd.AddCommand(configSetCmd)
	configCmd.AddCommand(configGetCmd)
	configCmd.AddCommand(configDeleteCmd)
	configCmd.AddCommand(configPathsCmd)
	configCmd.AddCommand(configInitCmd)
}

func mask(s string) string {
	if len(s) <= 8 {
		return "****"
	}
	return s[:4] + "..." + s[len(s)-4:]
}

// For help text
func knownKeysHelp() string {
	return strings.Join(secrets.KnownKeys(), ", ")
}
