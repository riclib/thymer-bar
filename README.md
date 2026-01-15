# thymer-bar

A Wails desktop application that serves as the sync engine and planning hub for [Thymer](https://thymer.com).

## Philosophy

thymer-bar implements **event sourcing for personal productivity**:

- **thymer-bar = Source of Truth**: Full event log with timestamps, granular state changes
- **Thymer = Projection**: Simplified status updates, daily summaries
- Events are immutable facts; projections are derived views

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
│                              │  │ NATS + JetStream       │  │
│                              │  │ (embedded)             │  │
│                              │  ├────────────────────────┤  │
│                              │  │ SQLite                 │  │
│                              │  ├────────────────────────┤  │
│                              │  │ MCP Server             │  │
│                              │  │ (Cloudflare Tunnel)    │  │
│                              │  └────────────────────────┘  │
└─────────────────────────────────────────────────────────────┘
                               │
                           WebSocket
                               │
┌──────────────────────────────▼──────────────────────────────┐
│  Thymer (Browser)                                           │
│  - desktop-bridge plugin (WebSocket endpoint)               │
│  - Collections (semantic schemas + query tools)             │
└─────────────────────────────────────────────────────────────┘
```

## Features

- **Sync Engines**: Pull data from GitHub, Readwise, Google Calendar
- **Event Logging**: Full history of focus sessions, task state changes
- **Multi-target Rendering**: Push projections to Thymer (and future: Obsidian)
- **MCP Server**: Expose tools to Claude Desktop via Cloudflare Tunnel
- **PlannerHub**: Daily planning with Kanban view and timeline
- **Flow**: Focus session tracking with analytics

## Data Storage

Follows [XDG Base Directory](https://specifications.freedesktop.org/basedir-spec/latest/) on all platforms for consistency.

### Linux & macOS

```
~/.config/thymer-bar/
└── config.json              # Settings, API tokens, repo list

~/.local/share/thymer-bar/
├── thymer-bar.db            # SQLite projection
└── jetstream/               # NATS JetStream (event log)

~/.local/state/thymer-bar/
└── sync-state.json          # Sync cursors, timestamps

~/.cache/thymer-bar/
└── ...                      # Temporary files
```

### Data Architecture

- **JetStream** = source of truth (immutable event log)
- **SQLite** = projection (queryable, rebuildable from events)
- **Config** = user settings (versionable, shareable)
- **State** = runtime state (cursors, not critical)

If SQLite corrupts, rebuild from JetStream. If JetStream is lost, SQLite still has the projection.

### Environment Variables

Override paths via XDG environment variables:
- `XDG_CONFIG_HOME` (default: `~/.config`)
- `XDG_DATA_HOME` (default: `~/.local/share`)
- `XDG_STATE_HOME` (default: `~/.local/state`)
- `XDG_CACHE_HOME` (default: `~/.cache`)

## Development

### Prerequisites

- Go 1.21+
- Wails CLI (`go install github.com/wailsapp/wails/v2/cmd/wails@latest`)
- Node.js 18+

### Run in Development

```bash
wails dev
```

### Build

```bash
wails build
```

### Project Structure

```
thymer-bar/
├── internal/
│   ├── nats/          # Embedded NATS + JetStream
│   ├── sqlite/        # SQLite storage
│   ├── mcp/           # MCP server
│   ├── thymer/        # WebSocket client
│   └── sync/          # Sync engines
│       ├── github/
│       ├── readwise/
│       └── calendar/
├── frontend/          # Wails frontend
├── app.go             # Wails app
├── main.go            # Entry point
└── CLAUDE.md          # AI development guide
```

## Related Projects

- [thymer-synchub](https://github.com/riclib/thymer-synchub) - Browser-based plugins (being migrated to thymer-bar)
- [thymer-inbox](https://github.com/riclib/thymer-inbox) - Original Go sync implementation

## License

MIT
