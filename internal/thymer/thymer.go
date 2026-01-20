// Package thymer provides WebSocket client for connecting to Thymer's desktop-bridge plugin.
package thymer

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// DailyNoteSnapshot represents a full snapshot of today's daily note lines.
// This is the proper event sourcing format - observer sends observations,
// thymer-bar compares with projection and publishes events for changes.
type DailyNoteSnapshot struct {
	Date        string             `json:"date"`                  // YYYY-MM-DD
	JournalGUID string             `json:"journalGuid,omitempty"` // Journal record GUID (needed for status updates)
	ObservedAt  string             `json:"observedAt"`            // ISO timestamp
	Lines       []DailyNoteLineObs `json:"lines"`                 // All current lines in order
}

// DailyNoteLineObs represents an observed line from Thymer.
type DailyNoteLineObs struct {
	GUID      string    `json:"guid"`
	Markdown  string    `json:"markdown"`
	Refs      []TaskRef `json:"refs,omitempty"`
	Status    string    `json:"status"`
	UpdatedAt string    `json:"updatedAt"` // Thymer's timestamp for this line
}

// TaskRef represents a reference link in a task.
type TaskRef struct {
	GUID  string `json:"guid"`
	Title string `json:"title"`
}

// Client manages WebSocket connections to Thymer instances.
type Client struct {
	mu          sync.RWMutex
	connections []*connection
	msgID       int64

	// Callback when connection count changes
	OnConnectionChange func(count int)

	// Callback when daily note snapshot is pushed from Thymer
	OnDailyNoteSnapshot func(snapshot *DailyNoteSnapshot)
}

type connection struct {
	ws       *websocket.Conn
	id       string
	lastSeen time.Time
	pending  map[int64]chan *Response
	mu       sync.Mutex
}

// Message represents a WebSocket message to/from Thymer.
type Message struct {
	ID     int64           `json:"id,omitempty"`
	Type   string          `json:"type"`
	Action string          `json:"action,omitempty"`
	Data   json.RawMessage `json:"data,omitempty"`
}

// Response represents a response from Thymer.
type Response struct {
	ID     int64           `json:"id"`
	Type   string          `json:"type"`
	Data   json.RawMessage `json:"data,omitempty"`
	Error  string          `json:"error,omitempty"`
}

// Record represents a Thymer record.
type Record struct {
	GUID       string         `json:"guid,omitempty"`
	Collection string         `json:"collection"`
	Name       string         `json:"name"`
	Fields     map[string]any `json:"fields,omitempty"`
}

// New creates a new Thymer client.
func New() *Client {
	return &Client{
		connections: make([]*connection, 0),
	}
}

// AddConnection adds a new WebSocket connection from a Thymer instance.
func (c *Client) AddConnection(ws *websocket.Conn, id string) {
	slog.Info("client connected", "client", id[:8])
	conn := &connection{
		ws:       ws,
		id:       id,
		lastSeen: time.Now(),
		pending:  make(map[int64]chan *Response),
	}

	c.mu.Lock()
	c.connections = append(c.connections, conn)
	count := len(c.connections)
	c.mu.Unlock()

	slog.Info("connection count updated", "total", count)

	// Notify callback
	if c.OnConnectionChange != nil {
		c.OnConnectionChange(count)
	}

	// Start reading from this connection
	go c.readLoop(conn)
}

// RemoveConnection removes a connection by ID.
func (c *Client) RemoveConnection(id string) {
	c.mu.Lock()
	var count int
	for i, conn := range c.connections {
		if conn.id == id {
			conn.ws.Close()
			c.connections = append(c.connections[:i], c.connections[i+1:]...)
			count = len(c.connections)
			c.mu.Unlock()

			// Notify callback
			if c.OnConnectionChange != nil {
				c.OnConnectionChange(count)
			}
			return
		}
	}
	c.mu.Unlock()
}

// ConnectionCount returns the number of active connections.
func (c *Client) ConnectionCount() int {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return len(c.connections)
}

// readLoop reads messages from a connection.
func (c *Client) readLoop(conn *connection) {
	defer c.RemoveConnection(conn.id)

	for {
		_, data, err := conn.ws.ReadMessage()
		if err != nil {
			return
		}

		conn.lastSeen = time.Now()

		// Try to parse as a response first (has ID field)
		var resp Response
		if err := json.Unmarshal(data, &resp); err == nil && resp.ID != 0 {
			conn.mu.Lock()
			if ch, ok := conn.pending[resp.ID]; ok {
				ch <- &resp
				delete(conn.pending, resp.ID)
			}
			conn.mu.Unlock()
			continue
		}

		// Otherwise, it's an incoming message from the client
		var msg Message
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}

		// Handle incoming message
		response, err := c.HandleIncoming(conn.id, msg.Type, data)
		if err != nil {
			fmt.Printf("[Thymer] Error handling message: %v\n", err)
			continue
		}

		// Send response if any (must lock to prevent concurrent writes)
		if response != nil {
			conn.mu.Lock()
			conn.ws.WriteMessage(websocket.TextMessage, response)
			conn.mu.Unlock()
		}
	}
}

// send sends a message and waits for response, with failover.
func (c *Client) send(ctx context.Context, msg *Message) (*Response, error) {
	c.mu.Lock()
	c.msgID++
	msg.ID = c.msgID
	connections := make([]*connection, len(c.connections))
	copy(connections, c.connections)
	c.mu.Unlock()

	if len(connections) == 0 {
		slog.Warn("no connections available", "type", msg.Type, "action", msg.Action)
		return nil, fmt.Errorf("no Thymer connections available")
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal message: %w", err)
	}

	slog.Debug("sending message", "id", msg.ID, "type", msg.Type, "action", msg.Action, "connections", len(connections))

	// Try each connection until one succeeds
	var lastErr error
	for i, conn := range connections {
		slog.Debug("trying connection", "attempt", i+1, "total", len(connections), "client", conn.id[:8])
		resp, err := c.sendToConnection(ctx, conn, msg.ID, data)
		if err != nil {
			slog.Warn("connection failed", "client", conn.id[:8], "error", err)
			lastErr = err
			continue
		}
		slog.Debug("got response", "id", msg.ID)
		return resp, nil
	}

	return nil, fmt.Errorf("all connections failed, last error: %w", lastErr)
}

func (c *Client) sendToConnection(ctx context.Context, conn *connection, id int64, data []byte) (*Response, error) {
	respCh := make(chan *Response, 1)

	conn.mu.Lock()
	conn.pending[id] = respCh
	err := conn.ws.WriteMessage(websocket.TextMessage, data)
	conn.mu.Unlock()

	if err != nil {
		conn.mu.Lock()
		delete(conn.pending, id)
		conn.mu.Unlock()
		return nil, fmt.Errorf("failed to send: %w", err)
	}

	slog.Debug("waiting for response", "id", id)
	select {
	case resp := <-respCh:
		if resp.Error != "" {
			slog.Error("response error", "id", id, "error", resp.Error)
			return nil, fmt.Errorf("thymer error: %s", resp.Error)
		}
		return resp, nil
	case <-ctx.Done():
		slog.Warn("context cancelled", "id", id)
		conn.mu.Lock()
		delete(conn.pending, id)
		conn.mu.Unlock()
		return nil, ctx.Err()
	case <-time.After(30 * time.Second):
		slog.Error("timeout waiting for response", "id", id, "timeout", "30s")
		conn.mu.Lock()
		delete(conn.pending, id)
		conn.mu.Unlock()
		return nil, fmt.Errorf("timeout waiting for response")
	}
}

// CreateRecord creates a record in Thymer and returns the UUID.
func (c *Client) CreateRecord(ctx context.Context, collection string, name string, fields map[string]any) (string, error) {
	data, _ := json.Marshal(map[string]any{
		"collection": collection,
		"name":       name,
		"fields":     fields,
	})

	resp, err := c.send(ctx, &Message{
		Type:   "request",
		Action: "createRecord",
		Data:   data,
	})
	if err != nil {
		return "", err
	}

	var result struct {
		GUID string `json:"guid"`
	}
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	return result.GUID, nil
}

// UpdateRecord updates a record in Thymer.
func (c *Client) UpdateRecord(ctx context.Context, guid string, fields map[string]any) error {
	data, _ := json.Marshal(map[string]any{
		"guid":   guid,
		"fields": fields,
	})

	_, err := c.send(ctx, &Message{
		Type:   "request",
		Action: "updateRecord",
		Data:   data,
	})
	return err
}

// TaskStatus represents the numeric status values for Thymer tasks.
type TaskStatus int

const (
	TaskStatusUnchecked  TaskStatus = 0 // or null to clear
	TaskStatusInProgress TaskStatus = 1
	TaskStatusBlocked    TaskStatus = 2
	TaskStatusDone       TaskStatus = 8
)

// UpdateLineItemStatus updates a task's status in the daily note.
// recordGUID is the journal/daily note GUID, lineItemGUID is the task GUID.
// Status values: 1=In Progress, 2=Blocked/Waiting, 8=Done, 0 or nil=Unchecked
func (c *Client) UpdateLineItemStatus(ctx context.Context, recordGUID, lineItemGUID string, status TaskStatus) error {
	data, _ := json.Marshal(map[string]any{
		"record_guid":   recordGUID,
		"lineitem_guid": lineItemGUID,
		"status":        status,
	})

	_, err := c.send(ctx, &Message{
		Type:   "request",
		Action: "updateLineItem",
		Data:   data,
	})
	return err
}

// SyncResult represents the result of a sync operation.
type SyncResult struct {
	GUID   string `json:"guid"`
	Action string `json:"action"` // "created" or "updated"
}

// SyncRecord syncs a record to Thymer (upsert by external_id).
// Returns the GUID and whether it was created or updated.
func (c *Client) SyncRecord(ctx context.Context, collection, externalID, name string, fields map[string]any) (*SyncResult, error) {
	data, _ := json.Marshal(map[string]any{
		"collection":  collection,
		"external_id": externalID,
		"name":        name,
		"fields":      fields,
	})

	resp, err := c.send(ctx, &Message{
		Type:   "request",
		Action: "syncRecord",
		Data:   data,
	})
	if err != nil {
		return nil, err
	}

	var result SyncResult
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &result, nil
}

// FindRecord finds a record by external ID.
func (c *Client) FindRecord(ctx context.Context, collection, externalID string) (*Record, error) {
	data, _ := json.Marshal(map[string]any{
		"collection": collection,
		"externalId": externalID,
	})

	resp, err := c.send(ctx, &Message{
		Type:   "request",
		Action: "findRecord",
		Data:   data,
	})
	if err != nil {
		return nil, err
	}

	var record Record
	if err := json.Unmarshal(resp.Data, &record); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if record.GUID == "" {
		return nil, nil // Not found
	}

	return &record, nil
}

// InstallPlugin installs or updates a plugin in Thymer.
func (c *Client) InstallPlugin(ctx context.Context, pluginData []byte) error {
	_, err := c.send(ctx, &Message{
		Type:   "request",
		Action: "installPlugin",
		Data:   pluginData,
	})
	return err
}

// InstalledPlugin represents a plugin installed in Thymer.
type InstalledPlugin struct {
	Name string `json:"name"`
	GUID string `json:"guid"`
	Type string `json:"type"`
	Ver  int    `json:"ver"`
}

// GetInstalledPlugins queries Thymer for all installed plugins.
func (c *Client) GetInstalledPlugins(ctx context.Context) ([]InstalledPlugin, error) {
	resp, err := c.send(ctx, &Message{
		Type: "get_installed_plugins",
	})
	if err != nil {
		return nil, err
	}

	var result struct {
		Plugins []InstalledPlugin `json:"plugins"`
		Error   string            `json:"error,omitempty"`
	}
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if result.Error != "" {
		return nil, fmt.Errorf("thymer error: %s", result.Error)
	}

	return result.Plugins, nil
}

// Available returns true if at least one Thymer connection is available.
func (c *Client) Available() bool {
	return c.ConnectionCount() > 0
}

// =============================================================================
// Daily Note / Journal Operations (READ path - direct WebSocket)
// =============================================================================

// LineItem represents a task/item in a Thymer record.
type LineItem struct {
	GUID       string     `json:"guid"`
	ParentGUID string     `json:"parent_guid,omitempty"`
	Type       string     `json:"type"` // "task", "bullet", "heading", etc.
	Checked    bool       `json:"checked"`
	Text       string     `json:"text"` // Markdown text with [title](thymer:uuid) links
	Children   []LineItem `json:"children,omitempty"`
}

// JournalRecord represents a daily note/journal entry.
type JournalRecord struct {
	GUID      string     `json:"guid"`
	Name      string     `json:"name"`
	Date      string     `json:"date"` // YYYY-MM-DD
	LineItems []LineItem `json:"line_items"`
}

// GetTodayJournal retrieves or creates today's daily note.
func (c *Client) GetTodayJournal(ctx context.Context) (*JournalRecord, error) {
	resp, err := c.send(ctx, &Message{
		Type:   "request",
		Action: "getTodayJournal",
	})
	if err != nil {
		return nil, err
	}

	var result JournalRecord
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return &result, nil
}

// GetLineItems retrieves line items (tasks) from a record.
func (c *Client) GetLineItems(ctx context.Context, recordGUID string) ([]LineItem, error) {
	data, _ := json.Marshal(map[string]any{
		"guid": recordGUID,
	})

	resp, err := c.send(ctx, &Message{
		Type:   "request",
		Action: "getLineItems",
		Data:   data,
	})
	if err != nil {
		return nil, err
	}

	var result struct {
		Items []LineItem `json:"items"`
	}
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return result.Items, nil
}

// GetRecordByDate finds a journal record by date (YYYY-MM-DD format).
func (c *Client) GetRecordByDate(ctx context.Context, collection, date string) (*Record, error) {
	data, _ := json.Marshal(map[string]any{
		"collection": collection,
		"date":       date,
	})

	resp, err := c.send(ctx, &Message{
		Type:   "request",
		Action: "getRecordByDate",
		Data:   data,
	})
	if err != nil {
		return nil, err
	}

	var record Record
	if err := json.Unmarshal(resp.Data, &record); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	if record.GUID == "" {
		return nil, nil // Not found
	}

	return &record, nil
}

// =============================================================================
// Line Item Operations (WRITE path - called via JetStream consumer)
// =============================================================================

// UpdateLineItem updates a line item's properties (e.g., toggle checked).
func (c *Client) UpdateLineItem(ctx context.Context, recordGUID, lineItemGUID string, updates map[string]any) error {
	payload := map[string]any{
		"record_guid":   recordGUID,
		"lineitem_guid": lineItemGUID,
	}
	for k, v := range updates {
		payload[k] = v
	}

	data, _ := json.Marshal(payload)
	_, err := c.send(ctx, &Message{
		Type:   "request",
		Action: "updateLineItem",
		Data:   data,
	})
	return err
}

// AddLineItem adds a new line item to a record.
func (c *Client) AddLineItem(ctx context.Context, recordGUID, itemType string, segments []map[string]any) (string, error) {
	data, _ := json.Marshal(map[string]any{
		"record_guid": recordGUID,
		"type":        itemType,
		"segments":    segments,
	})

	resp, err := c.send(ctx, &Message{
		Type:   "request",
		Action: "addLineItem",
		Data:   data,
	})
	if err != nil {
		return "", err
	}

	var result struct {
		GUID string `json:"guid"`
	}
	if err := json.Unmarshal(resp.Data, &result); err != nil {
		return "", fmt.Errorf("failed to parse response: %w", err)
	}

	return result.GUID, nil
}

// SendToAll broadcasts a message to all connected Thymer instances (fire-and-forget).
func (c *Client) SendToAll(msg any) {
	c.mu.RLock()
	connections := make([]*connection, len(c.connections))
	copy(connections, c.connections)
	c.mu.RUnlock()

	data, err := json.Marshal(msg)
	if err != nil {
		return
	}

	for _, conn := range connections {
		conn.mu.Lock()
		conn.ws.WriteMessage(websocket.TextMessage, data)
		conn.mu.Unlock()
	}
}

// SendToOne sends a message to the first available Thymer connection (fire-and-forget).
// Use this for operations that should only happen once (like plugin installation).
func (c *Client) SendToOne(msg any) {
	c.mu.RLock()
	var conn *connection
	if len(c.connections) > 0 {
		conn = c.connections[0]
	}
	c.mu.RUnlock()

	if conn == nil {
		return
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return
	}

	conn.mu.Lock()
	conn.ws.WriteMessage(websocket.TextMessage, data)
	conn.mu.Unlock()
}

// SendRaw sends raw JSON bytes to the first available Thymer connection (fire-and-forget).
// Returns an error if no connections are available.
func (c *Client) SendRaw(ctx context.Context, data []byte) error {
	c.mu.RLock()
	var conn *connection
	if len(c.connections) > 0 {
		conn = c.connections[0]
	}
	c.mu.RUnlock()

	if conn == nil {
		return fmt.Errorf("no Thymer connections available")
	}

	conn.mu.Lock()
	err := conn.ws.WriteMessage(websocket.TextMessage, data)
	conn.mu.Unlock()

	return err
}
