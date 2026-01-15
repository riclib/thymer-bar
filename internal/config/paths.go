// Package config provides configuration management with XDG path support.
package config

import (
	"os"
	"path/filepath"
)

const appName = "thymer-bar"

// ConfigDir returns the config directory (~/.config/thymer-bar).
func ConfigDir() string {
	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".config")
	}
	return filepath.Join(base, appName)
}

// DataDir returns the data directory (~/.local/share/thymer-bar).
func DataDir() string {
	base := os.Getenv("XDG_DATA_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".local", "share")
	}
	return filepath.Join(base, appName)
}

// StateDir returns the state directory (~/.local/state/thymer-bar).
func StateDir() string {
	base := os.Getenv("XDG_STATE_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(base, appName)
}

// CacheDir returns the cache directory (~/.cache/thymer-bar).
func CacheDir() string {
	base := os.Getenv("XDG_CACHE_HOME")
	if base == "" {
		home, _ := os.UserHomeDir()
		base = filepath.Join(home, ".cache")
	}
	return filepath.Join(base, appName)
}

// Paths within directories

// ConfigFile returns the path to config.json.
func ConfigFile() string {
	return filepath.Join(ConfigDir(), "config.json")
}

// DatabaseFile returns the path to the SQLite database.
func DatabaseFile() string {
	return filepath.Join(DataDir(), "thymer-bar.db")
}

// JetStreamDir returns the path to JetStream storage.
func JetStreamDir() string {
	return filepath.Join(DataDir(), "jetstream")
}

// EnsureDirs creates all required directories.
func EnsureDirs() error {
	dirs := []string{
		ConfigDir(),
		DataDir(),
		StateDir(),
		CacheDir(),
		JetStreamDir(),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
	}
	return nil
}
