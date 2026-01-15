package main

import (
	"bytes"
	"context"
	"sync"
	"time"

	"thymer-bar/internal/ui"
	"thymer-bar/plugins"
)

// In-flight install tracking to prevent duplicate requests
var (
	installMu     sync.Mutex
	installFlight = make(map[string]time.Time)
)

// Plugin mode state
var (
	devMode = true // Default to dev mode during development
)

// GetPluginManagerHTML returns the rendered Plugin Manager HTML.
// Called by frontend JS on load to render the initial UI.
func (a *App) GetPluginManagerHTML() (string, error) {
	pluginInfos := a.getPluginInfos()
	a.cachedPluginInfos = pluginInfos // Cache for GetPluginInfoHTML
	connected := a.thymer != nil && a.thymer.ConnectionCount() > 0

	var buf bytes.Buffer
	err := ui.PluginManager(pluginInfos, connected, devMode).Render(context.Background(), &buf)
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

// GetPluginInfoHTML returns the info panel HTML for a specific plugin.
// Reloads plugin from disk to get fresh version info.
func (a *App) GetPluginInfoHTML(id string) (string, error) {
	// Reload fresh to get current disk version
	pluginInfos := a.getPluginInfos()
	a.cachedPluginInfos = pluginInfos

	for _, p := range pluginInfos {
		if p.ID == id || p.Name == id {
			var buf bytes.Buffer
			err := ui.InfoPanelPlugin(p).Render(context.Background(), &buf)
			if err != nil {
				return "", err
			}
			return buf.String(), nil
		}
	}
	return "", nil
}

// GetDefaultInfoHTML returns the default info panel HTML.
func (a *App) GetDefaultInfoHTML() (string, error) {
	connected := a.thymer != nil && a.thymer.ConnectionCount() > 0
	var buf bytes.Buffer
	err := ui.InfoPanelDefault(connected, devMode).Render(context.Background(), &buf)
	if err != nil {
		return "", err
	}
	return buf.String(), nil
}

// SetDevMode sets the plugin mode (true = dev, false = stable).
func (a *App) SetDevMode(dev bool) {
	devMode = dev
	plugins.DevMode = dev
}

// GetDevMode returns the current plugin mode.
func (a *App) GetDevMode() bool {
	return devMode
}

// RefreshPluginList returns refreshed plugin list HTML (reloads from disk in dev mode).
func (a *App) RefreshPluginList() (string, error) {
	return a.GetPluginManagerHTML()
}

// GetPlugins returns a list of available plugins with their status.
func (a *App) GetPlugins() []ui.PluginInfo {
	return a.getPluginInfos()
}

// InstallPlugin installs a plugin to the connected Thymer instance.
func (a *App) InstallPlugin(id string) error {
	return a.pushPlugin(id)
}

// UpdatePlugin updates/pushes a plugin to the connected Thymer instance.
func (a *App) UpdatePlugin(id string) error {
	return a.pushPlugin(id)
}

// UpdateAllPlugins updates all installed plugins.
func (a *App) UpdateAllPlugins() error {
	for _, p := range a.getPluginInfos() {
		if p.Installed {
			if err := a.pushPlugin(p.ID); err != nil {
				return err
			}
		}
	}
	return nil
}

// getPluginInfos builds the list of plugins with their current status.
func (a *App) getPluginInfos() []ui.PluginInfo {
	var result []ui.PluginInfo

	// Query Thymer for installed plugins
	installed := make(map[string]ui.InstalledInfo)
	if a.thymer != nil && a.thymer.Available() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		thymerPlugins, err := a.thymer.GetInstalledPlugins(ctx)
		if err == nil {
			for _, tp := range thymerPlugins {
				installed[tp.Name] = ui.InstalledInfo{
					GUID: tp.GUID,
					Ver:  tp.Ver,
				}
			}
		}
	}

	// App plugins
	appPlugins, _ := plugins.ListApp()
	for _, name := range appPlugins {
		p, err := loadApp(name)
		if err != nil {
			continue
		}

		configName := getString(p.Config, "name")
		if configName == "" {
			configName = name
		}

		info := ui.PluginInfo{
			ID:           name,
			Name:         configName,
			Description:  getString(p.Config, "description"),
			Type:         "app",
			AvailableVer: p.Version(),
			AutoUpdate:   true,
			HasLocalFiles: true,
		}

		// Check if installed in Thymer
		if inst, ok := installed[configName]; ok {
			info.Installed = true
			info.InstalledVer = inst.Ver
			info.GUID = inst.GUID
			if inst.Ver < p.Version() {
				info.Status = "outdated"
			} else {
				info.Status = "current"
			}
			delete(installed, configName) // Mark as matched
		} else {
			info.Status = "available"
		}

		result = append(result, info)
	}

	// Collection plugins
	collections, _ := plugins.ListCollections()
	for _, name := range collections {
		p, err := loadCollection(name)
		if err != nil {
			continue
		}

		configName := getString(p.Config, "name")
		if configName == "" {
			configName = name
		}

		info := ui.PluginInfo{
			ID:           name,
			Name:         configName,
			Description:  getString(p.Config, "description"),
			Type:         "collection",
			AvailableVer: p.Version(),
			AutoUpdate:   true,
			HasLocalFiles: true,
		}

		// Check if installed in Thymer
		if inst, ok := installed[configName]; ok {
			info.Installed = true
			info.InstalledVer = inst.Ver
			info.GUID = inst.GUID
			if inst.Ver < p.Version() {
				info.Status = "outdated"
			} else {
				info.Status = "current"
			}
			delete(installed, configName) // Mark as matched
		} else {
			info.Status = "available"
		}

		result = append(result, info)
	}

	// Note: We don't show Thymer-only plugins (user's own collections)
	// thymer-bar only manages plugins it has local files for

	return result
}

// pushPlugin sends a plugin to the connected Thymer instance.
func (a *App) pushPlugin(id string) error {
	// Check for in-flight install (prevent duplicate requests)
	installMu.Lock()
	if lastTime, ok := installFlight[id]; ok {
		if time.Since(lastTime) < 10*time.Second {
			installMu.Unlock()
			return nil // Already in flight, ignore
		}
	}
	installFlight[id] = time.Now()
	installMu.Unlock()

	// Clean up after timeout
	defer func() {
		go func() {
			time.Sleep(10 * time.Second)
			installMu.Lock()
			delete(installFlight, id)
			installMu.Unlock()
		}()
	}()

	// Try loading as app plugin first, then collection
	p, err := loadApp(id)
	if err != nil {
		p, err = loadCollection(id)
		if err != nil {
			return err
		}
	}

	// Send via WebSocket to ONE Thymer connection (not all, to avoid duplicates)
	if a.thymer != nil {
		a.thymer.SendToOne(map[string]any{
			"type":   "install_plugin",
			"id":     id,
			"config": p.Config,
			"code":   p.Code,
			"css":    p.CSS,
		})
	}

	return nil
}

func getString(m map[string]any, key string) string {
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

// loadApp loads an app plugin, using dev loader if DevMode is enabled.
func loadApp(name string) (*plugins.Plugin, error) {
	if plugins.DevMode {
		return plugins.LoadAppDev(name)
	}
	return plugins.LoadApp(name)
}

// loadCollection loads a collection plugin, using dev loader if DevMode is enabled.
func loadCollection(name string) (*plugins.Plugin, error) {
	if plugins.DevMode {
		return plugins.LoadCollectionDev(name)
	}
	return plugins.LoadCollection(name)
}
