// Package plugins provides bundled Thymer plugins and collections.
package plugins

import (
	"embed"
	"encoding/json"
	"fmt"
	"path"
)

//go:embed all:collections all:app
var embedded embed.FS

// Plugin represents a bundled plugin.
type Plugin struct {
	Name   string         // Plugin name (directory name)
	Type   string         // "collection" or "app"
	Config map[string]any // Parsed config.json
	Code   string         // plugin.js content
	CSS    string         // style.css content (optional)
}

// LoadCollection loads a bundled collection plugin by name.
func LoadCollection(name string) (*Plugin, error) {
	return load("collections", name)
}

// LoadApp loads a bundled app plugin by name.
func LoadApp(name string) (*Plugin, error) {
	return load("app", name)
}

// ListCollections returns names of all bundled collection plugins.
func ListCollections() ([]string, error) {
	return list("collections")
}

// ListApp returns names of all bundled app plugins.
func ListApp() ([]string, error) {
	return list("app")
}

func load(typ, name string) (*Plugin, error) {
	base := path.Join(typ, name)

	// Load config.json
	configData, err := embedded.ReadFile(path.Join(base, "config.json"))
	if err != nil {
		return nil, fmt.Errorf("failed to read config.json: %w", err)
	}

	var config map[string]any
	if err := json.Unmarshal(configData, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config.json: %w", err)
	}

	// Load plugin.js
	code, err := embedded.ReadFile(path.Join(base, "plugin.js"))
	if err != nil {
		return nil, fmt.Errorf("failed to read plugin.js: %w", err)
	}

	// Load style.css (optional)
	css, _ := embedded.ReadFile(path.Join(base, "style.css"))

	return &Plugin{
		Name:   name,
		Type:   typ,
		Config: config,
		Code:   string(code),
		CSS:    string(css),
	}, nil
}

func list(typ string) ([]string, error) {
	entries, err := embedded.ReadDir(typ)
	if err != nil {
		return nil, err
	}

	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	return names, nil
}

// Version returns the version of a plugin config.
func (p *Plugin) Version() int {
	if ver, ok := p.Config["ver"].(float64); ok {
		return int(ver)
	}
	return 0
}
