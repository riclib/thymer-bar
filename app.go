package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"thymer-bar/internal/mcp"
	internalnats "thymer-bar/internal/nats"
	"thymer-bar/internal/sqlite"
	"thymer-bar/internal/sync"
	"thymer-bar/internal/sync/github"
	"thymer-bar/internal/thymer"
)

// App struct holds the application state and services.
type App struct {
	ctx context.Context

	// Core services
	nats   *internalnats.Server
	db     *sqlite.DB
	thymer *thymer.Client
	mcp    *mcp.Server

	// Sync engines
	syncRegistry *sync.Registry

	// Configuration
	dataDir string
}

// NewApp creates a new App application struct.
func NewApp() *App {
	return &App{
		syncRegistry: sync.NewRegistry(),
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
	a.initMCP()

	fmt.Println("thymer-bar started")
}

// shutdown is called when the app is closing.
func (a *App) shutdown(ctx context.Context) {
	if a.mcp != nil {
		a.mcp.Stop(ctx)
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
	// Register GitHub engine
	githubEngine := github.New(func(ctx context.Context, record *sync.Record) error {
		// TODO: Publish to NATS and sync to Thymer
		fmt.Printf("GitHub record: %s\n", record.Title)
		return nil
	})
	a.syncRegistry.Register(githubEngine)

	// TODO: Register other engines (readwise, calendar)

	fmt.Printf("Registered %d sync engines\n", len(a.syncRegistry.All()))
}

func (a *App) initMCP() {
	a.mcp = mcp.New(9850)

	// Register tools
	a.registerMCPTools()

	// Start MCP server in background
	go func() {
		if err := a.mcp.Start(); err != nil {
			fmt.Printf("MCP server error: %v\n", err)
		}
	}()

	fmt.Println("MCP server started on :9850")
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
}

// Greet returns a greeting for the given name (Wails demo method).
func (a *App) Greet(name string) string {
	return fmt.Sprintf("Hello %s from thymer-bar!", name)
}

// GetStatus returns the current status of all services.
func (a *App) GetStatus() map[string]any {
	return map[string]any{
		"nats":              a.nats != nil,
		"sqlite":            a.db != nil,
		"thymerConnections": a.thymer.ConnectionCount(),
		"syncEngines":       len(a.syncRegistry.All()),
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
