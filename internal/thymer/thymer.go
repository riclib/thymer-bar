// Package thymer provides WebSocket client for connecting to Thymer's desktop-bridge plugin.
package thymer

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// Client manages WebSocket connections to Thymer instances.
type Client struct {
	mu          sync.RWMutex
	connections []*connection
	msgID       int64
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
	conn := &connection{
		ws:       ws,
		id:       id,
		lastSeen: time.Now(),
		pending:  make(map[int64]chan *Response),
	}

	c.mu.Lock()
	c.connections = append(c.connections, conn)
	c.mu.Unlock()

	// Start reading from this connection
	go c.readLoop(conn)
}

// RemoveConnection removes a connection by ID.
func (c *Client) RemoveConnection(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	for i, conn := range c.connections {
		if conn.id == id {
			conn.ws.Close()
			c.connections = append(c.connections[:i], c.connections[i+1:]...)
			return
		}
	}
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

		var resp Response
		if err := json.Unmarshal(data, &resp); err != nil {
			continue
		}

		conn.mu.Lock()
		if ch, ok := conn.pending[resp.ID]; ok {
			ch <- &resp
			delete(conn.pending, resp.ID)
		}
		conn.mu.Unlock()

		conn.lastSeen = time.Now()
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
		return nil, fmt.Errorf("no Thymer connections available")
	}

	data, err := json.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal message: %w", err)
	}

	// Try each connection until one succeeds
	var lastErr error
	for _, conn := range connections {
		resp, err := c.sendToConnection(ctx, conn, msg.ID, data)
		if err != nil {
			lastErr = err
			continue
		}
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

	select {
	case resp := <-respCh:
		if resp.Error != "" {
			return nil, fmt.Errorf("thymer error: %s", resp.Error)
		}
		return resp, nil
	case <-ctx.Done():
		conn.mu.Lock()
		delete(conn.pending, id)
		conn.mu.Unlock()
		return nil, ctx.Err()
	case <-time.After(30 * time.Second):
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

// Available returns true if at least one Thymer connection is available.
func (c *Client) Available() bool {
	return c.ConnectionCount() > 0
}
