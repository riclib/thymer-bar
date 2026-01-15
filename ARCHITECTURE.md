# thymer-bar Architecture Notes

> **Future**: Eventually rename to **lifelog** - a personal data hub for capturing, syncing, and publishing your life.
>
> (lifelog already exists - this is a rebuild. thymer-bar is the stealth name.)

---

## The Deeper Vision: Quantum Blockchain Foundation

thymer-bar isn't just a sync hub. It's the **primitive** for quantum blockchain.

### The Insight

Every actor (human, agent, device, org) has their own **immutable lifelog chain**.
Shared events create **entangled blocks** that exist in multiple chains simultaneously.

```
Ricardo's chain:  [A] → [B] → [C] → [D]
                            ↑
                    entangled block
                            ↓
Agent's chain:    [X] → [Y] → [C] → [Z]
```

Block C exists in BOTH chains (superposition). Created by multi-party signatures.

### Why This Matters for thymer-bar

Every NATS event we design should be thought of as a **future blockchain entry**:
- Immutable (append-only)
- Signed (who created it)
- Timestamped (when)
- Hash-chained (links to previous)

SQLite isn't just a cache - it's the **local chain projection**.

### The Flow

```
External event (GitHub issue created)
       ↓
NATS event: issue.created {hash, signature, timestamp, payload}
       ↓
SQLite: append to local chain
       ↓
Thymer: project to collection
       ↓
Future: entangle with GitHub's chain, agent's chain, etc.
```

### Agent Consciousness

```go
type Agent struct {
    ID       string
    Lifelog  *Blockchain  // ← CONSCIOUSNESS
    State    AgentState
}

// Agent has private thoughts
agent.Lifelog.AppendPrivate(Block{...})

// Agent participates in multi-party blocks
sharedBlock.Sign(userKey)
sharedBlock.Sign(agentKey)
// Block exists in BOTH chains
```

### What We're Really Building

| Layer | Now (thymer-bar) | Future (quantum blockchain) |
|-------|------------------|----------------------------|
| Event bus | NATS | Hash-chained NATS |
| Storage | SQLite projection | Immutable chain |
| Sync | Push to Thymer | Multi-party entanglement |
| Actions | Close GH issue | Signed cross-chain action |
| MCP | Smart queries | Agent with own lifelog |

### Design Principle

**Every event should be designed as if it will become a blockchain entry.**

Even if we don't implement signing/hashing yet, the structure should support it:
- Unique ID
- Timestamp
- Actor (who)
- Payload
- Parent reference (for chaining)

---

## Core Principles

1. **Event-driven** - NATS as the backbone, not decoration
2. **Reactive** - Users expect reactivity, components subscribe to events
3. **SQLite as projection** - Read model derived from events, not source of truth
4. **Eventually consistent** - Like Thymer itself

## Event Flow

```
┌─────────────────────────────────────────────────────────────┐
│                         NATS                                │
│                    (Event Bus)                              │
└──────▲──────────────▲──────────────▲──────────────▲────────┘
       │              │              │              │
   publish        publish       subscribe      subscribe
       │              │              │              │
┌──────┴─────┐ ┌──────┴─────┐ ┌──────┴─────┐ ┌──────┴─────┐
│  Webhooks  │ │  Pollers   │ │   SQLite   │ │   Thymer   │
│  (GitHub)  │ │ (GitHub)   │ │(Projection)│ │   (Sync)   │
└────────────┘ └────────────┘ └────────────┘ └────────────┘
```

## Current State (v0)

### What we built:
- Wails app + system tray
- WebSocket bridge to Thymer
- Plugin manager UI (dev mode, push, version detection)
- SyncHub plugin (tool registry, markdown utils)
- Issues collection plugin (query tools)
- Modular plugin build system (`src/_*.js` → `plugin.js`)

### Scaffolded but dormant:
- MCP server (placeholder tools)
- NATS embedded server
- SQLite database
- Sync registry (GitHub engine registered, not active)
- Webhook server framework
- Cloudflare tunnel

### Smells in current implementation:
- Plugin manager uses request-response, not reactive
- `GetPluginInfoHTML()` reloads everything on click
- No file watching for dev mode auto-refresh
- UI queries live data, not projection

## Target Architecture

### Events (examples):
```
# Plugin lifecycle
plugin.local.changed      - file watcher detects change
plugin.installed          - Thymer reports installation
plugin.updated            - version changed

# Connection
thymer.connected
thymer.disconnected

# Sync sources (future)
issue.created
issue.updated
issue.closed
pr.merged
```

### SQLite Projection:
- Fast queries for UI
- Built from events
- Tables derived from event subscribers

### UI Pattern:
- Subscribes to relevant events
- Queries projection, not live sources
- Reacts to events, doesn't poll

---

## Vision

### What is thymer-bar?

A **personal data hub** with Thymer and Markdown as projections (outputs, not source of truth).

### Core Concepts

1. **Sync Hub**
   - Bidirectional sync with external sources (GitHub, Linear, Calendar, etc.)
   - **Actions**: Close GH issue when dragging to Done, etc.
   - Local projection of everything synced (change detection on re-receive)

2. **Real-time + Polling**
   - Prefer webhooks/real-time
   - Pragmatic: implement both for most sources
   - Webhook for speed, polling for reliability/catch-up

3. **Thymer Integration**
   - **Daily note = info stream** (ephemeral, time-based)
   - **Collections = data sinks** (structured, persistent)
   - Reimplement all thymer-synchub plugins

4. **CLI Tool (`llog`)**
   - `cat spec.md | llog` - pipe markdown in
   - `llog add "quick thought"` - capture from terminal
   - `llog list` - query recent
   - Start building the brand now

5. **Smart MCP**
   - AI-friendly API to Thymer data
   - **Contextual indexing** (BM25? vectors?) to help MCP tools find relevant data
   - Not just CRUD - smart search and context

6. **Life Dashboard**
   - Unified view of everything (planner hub / sync hub)
   - The control center

### Data Flow

```
External Sources          thymer-bar                    Outputs
─────────────────       ─────────────────            ─────────────

GitHub      ─────┐      ┌─────────────┐
Linear      ─────┼──────▶    NATS     │
Calendar    ─────┤      │  (events)   │
Webhooks    ─────┤      └──────┬──────┘
                │              │
                │              ▼
                │      ┌─────────────┐      ┌─────────────┐
                │      │   SQLite    │──────▶   Thymer    │
                │      │(projection) │      │(collections)│
                │      │             │      └─────────────┘
                │      │ - issues    │
                │      │ - PRs       │      ┌─────────────┐
                │      │ - events    │──────▶  Markdown   │
                │      │ - index     │      │   (files)   │
                │      └──────┬──────┘      └─────────────┘
                │             │
                │             ▼
                │      ┌─────────────┐
                └──────▶    MCP      │◀───── AI tools
                       │  (smart)   │
                       └─────────────┘
```

### Two-Way Sync

```
Thymer action (drag to Done)
       │
       ▼
  NATS event: task.completed {guid, source: "github", external_id: "123"}
       │
       ▼
  GitHub subscriber: closes issue #123
       │
       ▼
  Webhook receives close confirmation
       │
       ▼
  NATS event: issue.closed (no-op, already done)
```

### Projections

| Projection | Purpose |
|------------|---------|
| SQLite | Fast queries, change detection, indexing |
| Thymer | User-facing collections, daily notes |
| Markdown | File-based export (optional) |

### Thymer as Data Source

Subscribe to Thymer collections → treat as event source:
- Drag issue to Done → `task.completed` event → close GitHub issue
- Edit a record → `record.updated` event → sync back to source
- Thymer becomes bidirectional, not just a sink

### Publishing (llog)

- Beautiful blog with storylines
- Thymer data → published content
- Another projection output

### LLM Integration

| Where | Purpose |
|-------|---------|
| Thymer (AgentHub) | In-app AI interactions |
| thymer-bar | Summarize captures, process incoming data |
| Claude Code SDK | Smart automation, code-aware actions |

### Claude Code SDK

Connect thymer-bar to Claude Code for:
- Smarter MCP responses (context-aware)
- Automated workflows
- Code + personal data bridge

---

## Parallel Thread: The Chain (Solid/SolidMemDB)

Building the same primitives at enterprise scale. What we learn there applies here.

### The Primitives

**Bit** - append-only markdown with frontmatter:
```yaml
---
type: audit_event
actionName: tokenLogin
timestamp: 2025-01-09T15:04:05Z
---
Optional content here.
```

**Frontmatter IS the structured data.** ComplyDB is just a projection of bits with no content.

### Entanglement = Pointer

User's stream and Agent's stream are entangled by a simple pointer:
```go
type Bit struct {
    ID            string
    Frontmatter   map[string]any
    Content       string
    EntangledWith *EntanglementRef  // ← just a pointer, not a join
}
```

Click "How?" → follow pointer → render AgentTurn. No reconstruction needed.

### Three-Layer Storage (SolidMemDB)

```
HOT (memory)      →    WAL (JetStream)    →    COLD (Parquet)
microseconds           durable                 hash-chained
losable                replayable              analytical
```

Query doesn't know which layer answered.

### The Lesson for thymer-bar

**Don't be the Squirrel.** Don't ship fast to the wrong place with 17 abstractions.

Think first:
- What is a sync event?
- What are the projections?
- How does entanglement work?

Then ship once. ~100 lines. Not a rewrite.

### Mapping to thymer-bar

| Solid | thymer-bar |
|-------|------------|
| Bit | NATS event |
| Frontmatter | Event payload |
| SolidMemDB | SQLite projection |
| Parquet | Future: hash-chained archive |
| User stream | User's lifelog |
| Agent stream | Future: agent lifelog |

---

## Priority Order

### First: GitHub Issues Sync

Why:
- Most needed personally
- Issues collection already exists in Thymer
- Webhook scaffolding in place
- Polling engine registered (dormant)
- Two-way potential (drag to Done → close issue)

The flow:
```
GitHub (webhook/poll)
       ↓
NATS: issue.created / issue.updated / issue.closed
       ↓
SQLite: upsert to issues table (change detection)
       ↓
Thymer: push to Issues collection
       ↓
Future: Thymer action → NATS → close GitHub issue
```

### Then: The rest emerges from use

Build. Use. Discover what's missing. Repeat.
