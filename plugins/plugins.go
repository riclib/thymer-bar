// Package plugins provides bundled Thymer plugins and collections.
package plugins

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"sort"
	"strings"
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

	// Load collection.json (for CollectionPlugins) or config.json (for AppPlugins)
	configData, err := embedded.ReadFile(path.Join(base, "collection.json"))
	if err != nil {
		// Fall back to config.json for global/app plugins
		configData, err = embedded.ReadFile(path.Join(base, "config.json"))
		if err != nil {
			return nil, fmt.Errorf("failed to read collection.json or config.json: %w", err)
		}
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

// DevMode enables loading plugins from disk instead of embedded.
// Set this to true during development.
var DevMode = false

// DevPluginsDir is the path to the plugins directory on disk.
// Used when DevMode is true.
var DevPluginsDir = ""

// LoadCollectionDev loads a collection plugin from disk, auto-building if needed.
func LoadCollectionDev(name string) (*Plugin, error) {
	return loadDev("collections", name)
}

// LoadAppDev loads an app plugin from disk, auto-building if needed.
func LoadAppDev(name string) (*Plugin, error) {
	return loadDev("app", name)
}

func loadDev(typ, name string) (*Plugin, error) {
	if DevPluginsDir == "" {
		return nil, fmt.Errorf("DevPluginsDir not set")
	}

	base := filepath.Join(DevPluginsDir, typ, name)

	// Check if src/ directory exists - if so, build first
	srcDir := filepath.Join(base, "src")
	if info, err := os.Stat(srcDir); err == nil && info.IsDir() {
		if err := buildPlugin(base); err != nil {
			return nil, fmt.Errorf("failed to build plugin: %w", err)
		}
	}

	// Load collection.json or config.json
	configData, err := os.ReadFile(filepath.Join(base, "collection.json"))
	if err != nil {
		configData, err = os.ReadFile(filepath.Join(base, "config.json"))
		if err != nil {
			return nil, fmt.Errorf("failed to read collection.json or config.json: %w", err)
		}
	}

	var config map[string]any
	if err := json.Unmarshal(configData, &config); err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	// Load plugin.js
	code, err := os.ReadFile(filepath.Join(base, "plugin.js"))
	if err != nil {
		return nil, fmt.Errorf("failed to read plugin.js: %w", err)
	}

	// Load style.css (optional)
	css, _ := os.ReadFile(filepath.Join(base, "style.css"))

	return &Plugin{
		Name:   name,
		Type:   typ,
		Config: config,
		Code:   string(code),
		CSS:    string(css),
	}, nil
}

// buildPlugin concatenates src/_*.js files into plugin.js
// Files are sorted alphabetically - use numeric prefixes for ordering:
//
//	_00_helpers.js, _01_utils.js, ..., _99_plugin.js
func buildPlugin(pluginDir string) error {
	srcDir := filepath.Join(pluginDir, "src")
	output := filepath.Join(pluginDir, "plugin.js")

	// Get version and name from config
	name, version := getPluginMeta(pluginDir)

	// Collect and sort all _*.js files
	files, err := filepath.Glob(filepath.Join(srcDir, "_*.js"))
	if err != nil {
		return err
	}
	sort.Strings(files)

	// Concatenate
	var sb strings.Builder
	fmt.Fprintf(&sb, "/* %s v%d - Generated from src/ - DO NOT EDIT DIRECTLY */\n", name, version)
	sb.WriteString("/* Run: make plugins */\n\n")

	for _, f := range files {
		content, err := os.ReadFile(f)
		if err != nil {
			return fmt.Errorf("failed to read %s: %w", f, err)
		}
		fmt.Fprintf(&sb, "// === %s ===\n", filepath.Base(f))
		sb.Write(content)
		sb.WriteString("\n\n")
	}

	return os.WriteFile(output, []byte(sb.String()), 0644)
}

// getPluginMeta reads name and version from collection.json or config.json
func getPluginMeta(pluginDir string) (name string, version int) {
	name = "unknown"
	version = 0

	// Try collection.json first, then config.json
	configPaths := []string{
		filepath.Join(pluginDir, "collection.json"),
		filepath.Join(pluginDir, "config.json"),
	}

	for _, configPath := range configPaths {
		data, err := os.ReadFile(configPath)
		if err != nil {
			continue
		}

		var config map[string]any
		if err := json.Unmarshal(data, &config); err != nil {
			continue
		}

		if n, ok := config["name"].(string); ok {
			name = n
		}
		if v, ok := config["ver"].(float64); ok {
			version = int(v)
		}
		break
	}

	return name, version
}

// BuildAllPlugins builds all plugins that have src/ directories.
// This can be called from main() or a CLI command.
func BuildAllPlugins(pluginsDir string) error {
	types := []string{"app", "collections"}

	for _, typ := range types {
		typeDir := filepath.Join(pluginsDir, typ)
		entries, err := os.ReadDir(typeDir)
		if err != nil {
			continue
		}

		for _, entry := range entries {
			if !entry.IsDir() {
				continue
			}

			pluginDir := filepath.Join(typeDir, entry.Name())
			srcDir := filepath.Join(pluginDir, "src")

			if info, err := os.Stat(srcDir); err == nil && info.IsDir() {
				fmt.Printf("Building: %s/%s\n", typ, entry.Name())
				if err := buildPlugin(pluginDir); err != nil {
					return fmt.Errorf("failed to build %s/%s: %w", typ, entry.Name(), err)
				}
			}
		}
	}

	return nil
}

// RunBuildScript runs the external build script (for compatibility).
func RunBuildScript(scriptPath string) error {
	cmd := exec.Command(scriptPath)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}
