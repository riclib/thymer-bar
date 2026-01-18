# thymer-bar Development Guide

## Project Overview

thymer-bar is a Wails desktop application that serves as the sync engine and planning hub for Thymer. It implements event sourcing for personal productivity, with Thymer as one of multiple projection targets.

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│                     thymer-bar (Wails)                      │
├─────────────────────────────────────────────────────────────┤
│  Frontend (Web/JS)           │  Backend (Go)                │
│  ┌─────────────────────────┐ │  ┌────────────────────────┐  │
│  │ PlannerHub UI           │ │  │ Sync Engines           │  │
│  │ Flow UI                 │ │  │ - GitHub               │  │
│  └─────────────────────────┘ │  │ - Readwise             │  │
│                              │  │ - Calendar             │  │
│                              │  ├────────────────────────┤  │
│                              │  │ MCP Server             │  │
│                              │  │ (via Cloudflare Tunnel)│  │
│                              │  ├────────────────────────┤  │
│                              │  │ NATS + JetStream       │  │
│                              │  │ (embedded)             │  │
│                              │  ├────────────────────────┤  │
│                              │  │ SQLite                 │  │
│                              │  │ - records              │  │
│                              │  │ - id_map               │  │
│                              │  │ - sync_state           │  │
│                              │  ├────────────────────────┤  │
│                              │  │ Thymer WebSocket       │  │
│                              │  │ (connection pool)      │  │
│                              │  └────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
```

## Philosophy

### Event Sourcing
- **thymer-bar = Source of Truth**: Full event log with timestamps, granular state changes
- **Thymer = Projection**: Simplified status updates, daily summaries
- Events are immutable facts; projections are derived views

### Data Flow
1. Sync engines fetch from external APIs → publish to NATS stream
2. JetStream persists events (survives restarts)
3. Renderers subscribe and push to destinations (Thymer, future: Obsidian)
4. SQLite tracks: `internal_id ↔ thymer_uuid`, sync timestamps, content hashes

### Connection Management
- Multiple Thymer tabs can connect
- Push to first available, failover on failure
- If all fail, JetStream queues for retry
- Thymer's CRDT handles cross-tab sync

### Conflict Resolution
- Last write wins

## Directory Structure

```
thymer-bar/
├── cmd/thymer-bar/        # CLI entry point (if needed)
├── internal/
│   ├── nats/              # Embedded NATS + JetStream
│   ├── sqlite/            # SQLite storage, migrations
│   ├── mcp/               # MCP server + Cloudflare tunnel
│   ├── thymer/            # WebSocket client, connection pool
│   ├── tunnel/            # Cloudflare tunnel management
│   └── sync/              # Sync engines
│       ├── github/
│       ├── readwise/
│       └── calendar/
├── frontend/              # Wails frontend (Vite + vanilla JS)
│   └── src/
│       ├── planner/       # PlannerHub UI
│       └── flow/          # Flow UI
├── build/                 # Wails build config
├── app.go                 # Wails app struct
├── main.go                # Entry point
└── wails.json             # Wails config
```

## Data Storage

Location: `~/.local/thymer-bar/` (or platform equivalent)
```
~/.local/thymer-bar/
├── nats/                  # JetStream storage
├── thymer-bar.db          # SQLite database
└── config.toml            # User configuration
```

## Key Patterns

### Sync Engine Pattern
```go
type SyncEngine interface {
    Name() string
    Sync(ctx context.Context) error
    Configure(cfg map[string]any) error
}
```

### Renderer Pattern (Markdown-shaped hole)
```go
type Renderer interface {
    Name() string
    Render(record Record) error
    Available() bool  // Check if destination is connected
}
```

### Event Types
```go
type Event struct {
    ID        string
    Type      string    // task.started, task.paused, task.completed, etc.
    Timestamp time.Time
    RecordID  string
    Data      map[string]any
}
```

## Reference Code

The `reference/` directory contains git submodules with working reference implementations.
Use `make submodules` to initialize them, then look at:
- `reference/thymer-synchub/desktop/` - Go WebSocket server, MCP integration
- `reference/thymer-synchub/plugins/` - Sync plugins (GitHub, Calendar, Readwise)
- `reference/thymer-synchub/plannerhub/` - PlannerHub UI

## MCP Server

Exposed via Cloudflare Tunnel for Claude Desktop access:
- Unique endpoint per installation
- OAuth callback support
- Tools: lifelog, planner, sync controls

## Thymer Integration

### WebSocket Protocol
- Connect to Thymer's desktop-bridge plugin
- Push records to collections
- Receive UUIDs back for ID mapping
- Plugin auto-update capability via Thymer's plugin install API

### Projections to Thymer
- Status updates on tasks/issues
- Daily note summaries (end of day)
- Deep links back to thymer-bar: `thymer-bar://focus/2024-01-13`

## Development

```bash
# First time setup (submodules + deps)
make setup

# Run in dev mode
make dev

# Build for production
make build

# Run tests
make test

# See all targets
make help
```

## Thymer Plugin Development

### Modifying Plugins
When modifying any plugin in `plugins/`:
1. **ALWAYS bump the version** in `collection.json` (for collection plugins) or `app.json` (for app plugins)
2. Update the version comment in `style.css` if it exists
3. Run `make plugins` to rebuild the concatenated `plugin.js`

The version number in `collection.json`/`app.json` (`"ver": N`) is what Thymer uses to detect updates. If you don't bump it, the changes won't be picked up.

### Plugin Structure
```
plugins/
├── app/
│   └── synchub/           # App plugin (SyncHub)
│       ├── collection.json # Config with "ver" field
│       ├── style.css
│       ├── plugin.js       # Generated - DO NOT EDIT
│       └── src/            # Source files (concatenated by build)
└── collections/
    └── issues/            # Collection plugin
        ├── collection.json
        └── ...
```

## Thymer Plugin SDK Reference

**CANONICAL API REFERENCE**: `/home/riclib/src/thymer-bar/reference/thymer-sdk/types.d.ts`

When writing or modifying Thymer plugins (SyncHub, Issues, etc.), always consult this file for:
- `CollectionPlugin` and `AppPlugin` base classes
- `DataAPI` methods (`getAllCollections`, `getAllGlobalPlugins`, `createCollection`, etc.)
- `Collection` methods (`createRecord`, `getAllRecords`, `savePlugin`, etc.)
- `Record` methods (`prop`, `text`, `getName`, `getLineItems`, etc.)
- `UI` methods (`addStatusBarItem`, `addCommandPaletteCommand`, `addToaster`, etc.)
- Line item types, segment formats, property accessors

The `reference/` directory contains git submodules with SDK documentation and reference implementations:
- `reference/thymer-sdk/` - Thymer Plugin SDK types and interfaces
- `reference/thymer-synchub/` - Previous thymer-synchub implementation (working reference code)

These are read-only reference materials. Use `make submodules` to initialize them.

### Reference Code in thymer-synchub
When porting or rewriting functionality, consult:
- `reference/thymer-synchub/desktop/` - Go WebSocket server, MCP integration
- `reference/thymer-synchub/plugins/` - Sync plugins (GitHub, Calendar, etc.)
- `reference/thymer-synchub/plannerhub/` - PlannerHub UI

## Dependencies

Core:
- github.com/wailsapp/wails/v2
- github.com/nats-io/nats-server/v2 (embedded)
- github.com/nats-io/nats.go
- modernc.org/sqlite (pure Go SQLite)
- github.com/cloudflare/cloudflared (tunnel)

Sync:
- github.com/google/go-github/v66
- golang.org/x/oauth2
