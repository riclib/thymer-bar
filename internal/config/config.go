package config

import (
	"encoding/json"
	"os"
)

// Config holds all application configuration.
type Config struct {
	GitHub   GitHubConfig   `json:"github"`
	Calendar CalendarConfig `json:"calendar"`
	People   PeopleConfig   `json:"people"`
}

// GitHubConfig holds GitHub sync settings.
// Token is stored in system keychain, not here.
type GitHubConfig struct {
	Enabled bool     `json:"enabled"`
	Repos   []string `json:"repos"` // "owner/repo" format
}

// CalendarConfig holds Google Calendar sync settings.
// OAuth credentials and tokens are stored in system keychain.
type CalendarConfig struct {
	Enabled   bool     `json:"enabled"`
	Calendars []string `json:"calendars"` // Calendar IDs (empty = primary only)
}

// PeopleConfig holds Google People (Contacts) sync settings.
// OAuth credentials and tokens are stored in system keychain.
type PeopleConfig struct {
	Enabled bool `json:"enabled"`
}

// Load reads config from disk. Returns empty config if file doesn't exist.
func Load() (*Config, error) {
	cfg := &Config{}

	data, err := os.ReadFile(ConfigFile())
	if os.IsNotExist(err) {
		return cfg, nil
	}
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal(data, cfg); err != nil {
		return nil, err
	}

	return cfg, nil
}

// Save writes config to disk.
func (c *Config) Save() error {
	if err := EnsureDirs(); err != nil {
		return err
	}

	data, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(ConfigFile(), data, 0600)
}
