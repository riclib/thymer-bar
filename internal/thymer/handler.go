package thymer

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true // Allow all origins (local only)
	},
}

// Handler returns an HTTP handler for WebSocket connections.
func (c *Client) Handler(log *slog.Logger) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ws, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			log.Error("WebSocket upgrade failed", "error", err)
			return
		}

		// Generate connection ID
		connID := uuid.New().String()[:8]
		log.Info("New Thymer connection", "id", connID, "remote", r.RemoteAddr)

		// Add to client pool
		c.AddConnection(ws, connID)

		// Log when disconnected (AddConnection starts readLoop which calls RemoveConnection on close)
		// The readLoop handles the connection lifecycle
	}
}

// HandleIncoming processes incoming messages and returns responses.
// This is called by readLoop for messages that aren't responses to our requests.
func (c *Client) HandleIncoming(connID string, msgType string, data []byte) ([]byte, error) {
	switch msgType {
	case "register":
		// Client registering itself
		fmt.Printf("[Thymer] Client %s registered\n", connID)
		return []byte(`{"type":"ack","status":"ok"}`), nil

	case "tools":
		// Client pushing available tools
		fmt.Printf("[Thymer] Client %s pushed tools\n", connID)
		// TODO: Store tools for MCP exposure
		return []byte(`{"type":"ack","status":"ok"}`), nil

	case "plugins":
		// Client pushing available plugins
		fmt.Printf("[Thymer] Client %s pushed plugins\n", connID)
		return []byte(`{"type":"ack","status":"ok"}`), nil

	default:
		return nil, fmt.Errorf("unknown message type: %s", msgType)
	}
}
