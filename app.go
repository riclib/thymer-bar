package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"

	"thymer-bar/internal/mcp"
	internalnats "thymer-bar/internal/nats"
	"thymer-bar/internal/sqlite"
	"thymer-bar/internal/sync"
	"thymer-bar/internal/sync/github"
	"thymer-bar/internal/thymer"
	"thymer-bar/internal/tunnel"
	"thymer-bar/internal/webhook"
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

	// HTTP server (serves MCP + webhooks)
	httpServer *http.Server

	// Sync engines
	syncRegistry *sync.Registry

	// Configuration
	dataDir string
	config  *Config
}

// Config holds application configuration.
type Config struct {
	HTTPPort       int               `json:"http_port"`       // Port for HTTP server (MCP + webhooks)
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
		HTTPPort:       9850,
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

	fmt.Println("thymer-bar started")
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
	fmt.Println("Thymer client initialized (waiting for connections)")
}

func (a *App) initSyncEngines() {
	// Callback for processing records from sync engines
	onRecord := func(ctx context.Context, record *sync.Record) error {
		// TODO: Publish to NATS and sync to Thymer
		fmt.Printf("[%s] Record: %s\n", record.Source, record.Title)
		return nil
	}

	// Register GitHub polling engine
	githubEngine := github.New(onRecord)
	a.syncRegistry.Register(githubEngine)

	// TODO: Register other engines (readwise, calendar)

	fmt.Printf("Registered %d sync engines\n", len(a.syncRegistry.All()))
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
	onRecord := func(ctx context.Context, record *sync.Record) error {
		fmt.Printf("[webhook:%s] Record: %s\n", record.Source, record.Title)
		// TODO: Publish to NATS
		return nil
	}
	a.webhook.Register(github.NewWebhookHandler(onRecord))

	// Mount handlers
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
		LocalPort: a.config.HTTPPort,
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
