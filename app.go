package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"thymer-bar/internal/bit"
	"thymer-bar/internal/config"
	"thymer-bar/internal/mcp"
	internalnats "thymer-bar/internal/nats"
	"thymer-bar/internal/secrets"
	"thymer-bar/internal/sqlite"
	"thymer-bar/internal/sync"
	"thymer-bar/internal/sync/calendar"
	"thymer-bar/internal/sync/github"
	"thymer-bar/internal/sync/people"
	"thymer-bar/internal/thymer"
	"thymer-bar/internal/tray"
	"thymer-bar/internal/tunnel"
	"thymer-bar/internal/ui"
	"thymer-bar/internal/webhook"
	"thymer-bar/plugins"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct holds the application state and services.
type App struct {
	ctx context.Context
	log *slog.Logger

	// Core services
	nats    *internalnats.Server
	db      *sqlite.DB
	thymer  *thymer.Client
	mcp     *mcp.Server
	webhook *webhook.Server
	tunnel  *tunnel.Tunnel
	tray    *tray.Tray

	// HTTP server (serves MCP + webhooks)
	httpServer *http.Server

	// Sync engines
	syncRegistry *sync.Registry

	// Configuration
	dataDir string
	config  *Config

	// Cached plugin infos for info panel lookups
	cachedPluginInfos []ui.PluginInfo
}

// Config holds application configuration.
type Config struct {
	HTTPPort       int               `json:"http_port"`       // Port for HTTP server (MCP + webhooks)
	TunnelName     string            `json:"tunnel_name"`     // Named Cloudflare Tunnel (pre-configured)
	TunnelDomain   string            `json:"tunnel_domain"`   // Custom domain (e.g., "hub.lifelog.my")
	EnableTunnel   bool              `json:"enable_tunnel"`   // Enable Cloudflare Tunnel
	WebhookSecrets map[string]string `json:"webhook_secrets"` // source -> secret
	GitHub         struct {
		Token string   `json:"token"`
		Repos []string `json:"repos"`
	} `json:"github"`
}

// DefaultConfig returns default configuration.
func DefaultConfig() *Config {
	return &Config{
		HTTPPort:       8496, // "thym" in T9
		EnableTunnel:   false,
		WebhookSecrets: make(map[string]string),
	}
}

// NewApp creates a new App application struct.
func NewApp() *App {
	return &App{
		syncRegistry: sync.NewRegistry(),
		config:       DefaultConfig(),
		log:          slog.Default(),
	}
}

// startup is called when the app starts.
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	// Initialize dev mode - check if plugins/app exists relative to executable
	a.initDevMode()

	// Determine data directory
	a.dataDir = getDataDir()

	// Ensure data directory exists
	if err := os.MkdirAll(a.dataDir, 0755); err != nil {
		fmt.Printf("Failed to create data directory: %v\n", err)
		return
	}

	// Initialize services (errors are logged but don't prevent startup)
	a.initNATS()
	a.initSQLite()
	a.initThymer()
	a.initSyncEngines()
	a.initHTTPServer()
	a.initTray()

	fmt.Println("thymer-bar started")
}

func (a *App) initTray() {
	a.tray = tray.New(tray.Config{
		OnShow: func() {
			runtime.WindowShow(a.ctx)
		},
		OnQuit: func() {
			runtime.Quit(a.ctx)
		},
		OnSync: func() {
			// TODO: Trigger sync all
			fmt.Println("Sync all triggered from tray")
		},
	})

	// Start tray in background (doesn't block)
	a.tray.RunAsync()

	// Update status
	a.tray.UpdateStatus("Running")
}

// shutdown is called when the app is closing.
func (a *App) shutdown(ctx context.Context) {
	if a.tunnel != nil {
		a.tunnel.Stop()
	}
	if a.httpServer != nil {
		a.httpServer.Shutdown(ctx)
	}
	if a.nats != nil {
		a.nats.Close()
	}
	if a.db != nil {
		a.db.Close()
	}
	fmt.Println("thymer-bar stopped")
}

func (a *App) initNATS() {
	ns, err := internalnats.New(internalnats.Config{
		DataDir: a.dataDir,
	})
	if err != nil {
		fmt.Printf("Failed to start NATS: %v\n", err)
		return
	}
	a.nats = ns

	// Create event streams
	ctx := context.Background()
	_, err = ns.EnsureStream(ctx, "SYNC", []string{"sync.>"})
	if err != nil {
		fmt.Printf("Failed to create SYNC stream: %v\n", err)
	}
	_, err = ns.EnsureStream(ctx, "EVENTS", []string{"events.>"})
	if err != nil {
		fmt.Printf("Failed to create EVENTS stream: %v\n", err)
	}

	fmt.Println("NATS initialized")
}

func (a *App) initSQLite() {
	db, err := sqlite.New(sqlite.Config{
		DataDir: a.dataDir,
	})
	if err != nil {
		fmt.Printf("Failed to open database: %v\n", err)
		return
	}
	a.db = db
	fmt.Println("SQLite initialized")
}

func (a *App) initThymer() {
	a.thymer = thymer.New()

	// Emit events to frontend when connection status changes
	a.thymer.OnConnectionChange = func(count int) {
		runtime.EventsEmit(a.ctx, "thymer:connection", map[string]any{
			"connected": count > 0,
			"count":     count,
		})
	}

	fmt.Println("Thymer client initialized (waiting for connections)")
}

func (a *App) initSyncEngines() {
	// Callback for processing Bits from sync engines
	onBit := func(ctx context.Context, b *bit.Bit) error {
		// TODO: Publish to NATS and sync to Thymer
		fmt.Printf("[%s] Bit: %s\n", b.MasterSystem, b.Title())
		return nil
	}

	// Load config from disk
	cfg, _ := config.Load()

	// Register GitHub polling engine
	githubEngine := github.New(onBit)
	githubToken, _ := secrets.Get(secrets.GitHubToken)
	githubEngine.Configure(map[string]any{
		"token":   githubToken,
		"repos":   toAnySlice(cfg.GitHub.Repos),
		"enabled": cfg.GitHub.Enabled,
	})
	a.syncRegistry.Register(githubEngine)

	// Register Google Calendar engine
	calendarEngine := calendar.New(onBit)
	googleCreds, _ := secrets.Get(secrets.GoogleCredentials)
	googleToken, _ := secrets.Get(secrets.GoogleOAuthToken)
	calendarEngine.Configure(map[string]any{
		"credentials": googleCreds,
		"token":       googleToken,
		"calendars":   toAnySlice(cfg.Calendar.Calendars),
		"enabled":     cfg.Calendar.Enabled,
	})
	a.syncRegistry.Register(calendarEngine)

	// Register Google People engine
	peopleEngine := people.New(onBit)
	peopleEngine.Configure(map[string]any{
		"credentials": googleCreds,
		"token":       googleToken,
		"enabled":     cfg.People.Enabled,
	})
	a.syncRegistry.Register(peopleEngine)

	fmt.Printf("Registered %d sync engines\n", len(a.syncRegistry.All()))
}

// toAnySlice converts []string to []any for Configure()
func toAnySlice(ss []string) []any {
	result := make([]any, len(ss))
	for i, s := range ss {
		result[i] = s
	}
	return result
}

func (a *App) initHTTPServer() {
	// Create unified HTTP mux
	mux := http.NewServeMux()

	// Initialize MCP server
	a.mcp = mcp.New(a.config.HTTPPort)
	a.registerMCPTools()

	// Initialize webhook server
	a.webhook = webhook.New(webhook.Config{
		Secrets: a.config.WebhookSecrets,
	}, a.log)

	// Register webhook handlers
	onBitWebhook := func(ctx context.Context, b *bit.Bit) error {
		fmt.Printf("[webhook:%s] Bit: %s\n", b.MasterSystem, b.Title())
		// TODO: Publish to NATS
		return nil
	}
	a.webhook.Register(github.NewWebhookHandler(onBitWebhook))

	// Mount handlers
	mux.HandleFunc("/ws", a.thymer.Handler(a.log))
	mux.HandleFunc("/mcp", a.mcp.HandleRequest)
	mux.Handle("/webhooks/", a.webhook.Handler())
	mux.HandleFunc("/status", a.handleStatus)

	// Start HTTP server
	a.httpServer = &http.Server{
		Addr:    fmt.Sprintf(":%d", a.config.HTTPPort),
		Handler: mux,
	}

	go func() {
		fmt.Printf("HTTP server started on :%d\n", a.config.HTTPPort)
		if err := a.httpServer.ListenAndServe(); err != http.ErrServerClosed {
			fmt.Printf("HTTP server error: %v\n", err)
		}
	}()

	// Start tunnel if enabled
	if a.config.EnableTunnel {
		a.initTunnel()
	}
}

func (a *App) initTunnel() {
	a.tunnel = tunnel.New(tunnel.Config{
		LocalPort:  a.config.HTTPPort,
		TunnelName: a.config.TunnelName,
		Domain:     a.config.TunnelDomain,
	}, a.log)

	go func() {
		if err := a.tunnel.Start(a.ctx); err != nil {
			fmt.Printf("Failed to start tunnel: %v\n", err)
			return
		}
	}()

	fmt.Println("Cloudflare Tunnel starting...")
}

func (a *App) handleStatus(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")

	tunnelURL := ""
	if a.tunnel != nil {
		tunnelURL = a.tunnel.PublicURL()
	}

	fmt.Fprintf(w, `{
	"nats": %t,
	"sqlite": %t,
	"thymer_connections": %d,
	"sync_engines": %d,
	"tunnel_url": %q,
	"webhook_github": %q
}`,
		a.nats != nil,
		a.db != nil,
		a.thymer.ConnectionCount(),
		len(a.syncRegistry.All()),
		tunnelURL,
		tunnelURL+"/webhooks/github",
	)
}

func (a *App) registerMCPTools() {
	// Lifelog tools (placeholder)
	a.mcp.RegisterTool(&mcp.Tool{
		Name:        "lifelog_list",
		Description: "List recent blocks or filter by bucket",
		Parameters: map[string]*mcp.Parameter{
			"bucket": {Type: "string", Description: "Filter by bucket: NEXT, SOON, LATER, DONE"},
			"limit":  {Type: "number", Description: "Maximum number of blocks to return"},
		},
		Handler: func(ctx context.Context, params map[string]any) (any, error) {
			// TODO: Implement
			return "lifelog_list not yet implemented", nil
		},
	})

	a.mcp.RegisterTool(&mcp.Tool{
		Name:        "lifelog_add",
		Description: "Add a new block (note/journal entry)",
		Parameters: map[string]*mcp.Parameter{
			"content": {Type: "string", Description: "Block content", Required: true},
		},
		Handler: func(ctx context.Context, params map[string]any) (any, error) {
			// TODO: Implement
			return "lifelog_add not yet implemented", nil
		},
	})

	// Sync tools
	a.mcp.RegisterTool(&mcp.Tool{
		Name:        "sync_status",
		Description: "Get sync status for all engines",
		Parameters:  map[string]*mcp.Parameter{},
		Handler: func(ctx context.Context, params map[string]any) (any, error) {
			engines := a.syncRegistry.All()
			status := make([]map[string]any, 0, len(engines))
			for _, e := range engines {
				status = append(status, map[string]any{
					"name":    e.Name(),
					"display": e.DisplayName(),
					"enabled": e.Enabled(),
				})
			}
			return status, nil
		},
	})

	// Tunnel tools
	a.mcp.RegisterTool(&mcp.Tool{
		Name:        "tunnel_status",
		Description: "Get Cloudflare Tunnel status and public URL",
		Parameters:  map[string]*mcp.Parameter{},
		Handler: func(ctx context.Context, params map[string]any) (any, error) {
			if a.tunnel == nil {
				return map[string]any{"enabled": false}, nil
			}
			return map[string]any{
				"enabled": true,
				"running": a.tunnel.Running(),
				"url":     a.tunnel.PublicURL(),
				"webhook_github": a.tunnel.WebhookURL("/webhooks/github"),
			}, nil
		},
	})
}

// Greet returns a greeting for the given name (Wails demo method).
func (a *App) Greet(name string) string {
	return fmt.Sprintf("Hello %s from thymer-bar!", name)
}

// GetStatus returns the current status of all services.
func (a *App) GetStatus() map[string]any {
	tunnelURL := ""
	if a.tunnel != nil {
		tunnelURL = a.tunnel.PublicURL()
	}

	return map[string]any{
		"nats":              a.nats != nil,
		"sqlite":            a.db != nil,
		"thymerConnections": a.thymer.ConnectionCount(),
		"syncEngines":       len(a.syncRegistry.All()),
		"tunnelURL":         tunnelURL,
	}
}

// EnableTunnel enables and starts the Cloudflare Tunnel.
func (a *App) EnableTunnel() error {
	a.config.EnableTunnel = true
	if a.tunnel == nil {
		a.initTunnel()
	}
	return nil
}

// GetWebhookURLs returns the webhook URLs (requires tunnel to be running).
func (a *App) GetWebhookURLs() map[string]string {
	if a.tunnel == nil || !a.tunnel.Running() {
		return map[string]string{
			"error": "Tunnel not running. Enable tunnel first.",
		}
	}

	return map[string]string{
		"github":   a.tunnel.WebhookURL("/webhooks/github"),
		"calendar": a.tunnel.WebhookURL("/webhooks/calendar"),
	}
}

// HideWindow hides the plugin manager window (back to tray).
func (a *App) HideWindow() {
	runtime.WindowHide(a.ctx)
}

// getDataDir returns the platform-appropriate data directory.
func getDataDir() string {
	// Check XDG_DATA_HOME first
	if xdg := os.Getenv("XDG_DATA_HOME"); xdg != "" {
		return filepath.Join(xdg, "thymer-bar")
	}

	// Fall back to ~/.local/share on Linux, or platform equivalent
	home, err := os.UserHomeDir()
	if err != nil {
		return "./data" // Fallback to current directory
	}

	return filepath.Join(home, ".local", "share", "thymer-bar")
}

// initDevMode checks if we're running in dev mode and sets up the plugins package accordingly.
// Dev mode is detected by checking if THYMER_DEV_PLUGINS env var is set, or if plugins/app
// exists relative to the executable.
func (a *App) initDevMode() {
	// Check env var first
	if devDir := os.Getenv("THYMER_DEV_PLUGINS"); devDir != "" {
		plugins.DevMode = true
		plugins.DevPluginsDir = devDir
		devMode = true
		fmt.Printf("Dev mode enabled via THYMER_DEV_PLUGINS: %s\n", devDir)
		return
	}

	// Check relative to executable
	exe, err := os.Executable()
	if err != nil {
		return
	}

	// Try relative to executable directory (for wails dev)
	exeDir := filepath.Dir(exe)
	candidates := []string{
		filepath.Join(exeDir, "..", "..", "plugins"),       // build/bin/thymer-bar -> plugins
		filepath.Join(exeDir, "plugins"),                   // same dir
		filepath.Join(filepath.Dir(exeDir), "plugins"),     // parent dir
	}

	for _, dir := range candidates {
		if info, err := os.Stat(filepath.Join(dir, "app")); err == nil && info.IsDir() {
			absDir, _ := filepath.Abs(dir)
			plugins.DevMode = true
			plugins.DevPluginsDir = absDir
			devMode = true
			fmt.Printf("Dev mode enabled: %s\n", absDir)
			return
		}
	}
}

// =============================================================================
// Configuration API (exposed to frontend)
// =============================================================================

// SetGitHubToken validates and stores the GitHub token securely in the system keychain.
func (a *App) SetGitHubToken(token string) error {
	if token == "" {
		return fmt.Errorf("token cannot be empty")
	}

	// Validate token by making a test API call
	req, err := http.NewRequest("GET", "https://api.github.com/user", nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")

	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to validate token: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return fmt.Errorf("invalid token (status %d)", resp.StatusCode)
	}

	// Token is valid - store it
	if err := secrets.Set(secrets.GitHubToken, token); err != nil {
		return fmt.Errorf("failed to store token: %w", err)
	}

	// Enable the source in sync state
	if syncState != nil {
		syncState.mu.Lock()
		if state := syncState.Sources["github"]; state != nil {
			state.Enabled = true
		}
		syncState.mu.Unlock()
		syncState.save()
	}

	return nil
}

// GetGitHubToken retrieves the GitHub token from the system keychain.
// Returns empty string if not set.
func (a *App) GetGitHubToken() string {
	token, _ := secrets.Get(secrets.GitHubToken)
	return token
}

// HasGitHubToken checks if a GitHub token is configured.
func (a *App) HasGitHubToken() bool {
	token, _ := secrets.Get(secrets.GitHubToken)
	return token != ""
}

// GetGitHubConfig returns the GitHub configuration (repos, enabled status).
func (a *App) GetGitHubConfig() map[string]any {
	cfg, _ := config.Load()
	return map[string]any{
		"enabled":  cfg.GitHub.Enabled,
		"repos":    cfg.GitHub.Repos,
		"hasToken": a.HasGitHubToken(),
	}
}

// SetGitHubConfig updates the GitHub configuration.
func (a *App) SetGitHubConfig(enabled bool, repos []string) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	cfg.GitHub.Enabled = enabled
	cfg.GitHub.Repos = repos
	return cfg.Save()
}

// GetConfigPaths returns all configuration paths for debugging.
func (a *App) GetConfigPaths() map[string]string {
	return map[string]string{
		"config":    config.ConfigDir(),
		"data":      config.DataDir(),
		"state":     config.StateDir(),
		"cache":     config.CacheDir(),
		"database":  config.DatabaseFile(),
		"jetstream": config.JetStreamDir(),
	}
}
