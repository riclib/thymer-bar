// Package mcp provides the MCP (Model Context Protocol) server.
package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
)

// Server implements an MCP server.
type Server struct {
	tools    map[string]*Tool
	mu       sync.RWMutex
	port     int
	server   *http.Server
}

// Tool represents an MCP tool.
type Tool struct {
	Name        string                 `json:"name"`
	Description string                 `json:"description"`
	Parameters  map[string]*Parameter  `json:"parameters"`
	Handler     ToolHandler            `json:"-"`
}

// Parameter describes a tool parameter.
type Parameter struct {
	Type        string `json:"type"`
	Description string `json:"description"`
	Required    bool   `json:"required"`
}

// ToolHandler is a function that handles tool invocations.
type ToolHandler func(ctx context.Context, params map[string]any) (any, error)

// Request represents an MCP request.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      any             `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

// Response represents an MCP response.
type Response struct {
	JSONRPC string `json:"jsonrpc"`
	ID      any    `json:"id"`
	Result  any    `json:"result,omitempty"`
	Error   *Error `json:"error,omitempty"`
}

// Error represents an MCP error.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// New creates a new MCP server.
func New(port int) *Server {
	return &Server{
		tools: make(map[string]*Tool),
		port:  port,
	}
}

// RegisterTool registers a tool with the server.
func (s *Server) RegisterTool(tool *Tool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools[tool.Name] = tool
}

// Start starts the MCP server.
func (s *Server) Start() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/", s.handleRequest)

	s.server = &http.Server{
		Addr:    fmt.Sprintf(":%d", s.port),
		Handler: mux,
	}

	return s.server.ListenAndServe()
}

// Stop stops the MCP server.
func (s *Server) Stop(ctx context.Context) error {
	if s.server != nil {
		return s.server.Shutdown(ctx)
	}
	return nil
}

func (s *Server) handleRequest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req Request
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.sendError(w, nil, -32700, "Parse error")
		return
	}

	switch req.Method {
	case "initialize":
		s.handleInitialize(w, &req)
	case "tools/list":
		s.handleToolsList(w, &req)
	case "tools/call":
		s.handleToolsCall(w, r.Context(), &req)
	default:
		s.sendError(w, req.ID, -32601, "Method not found")
	}
}

func (s *Server) handleInitialize(w http.ResponseWriter, req *Request) {
	s.sendResult(w, req.ID, map[string]any{
		"protocolVersion": "2024-11-05",
		"serverInfo": map[string]any{
			"name":    "thymer-bar",
			"version": "0.1.0",
		},
		"capabilities": map[string]any{
			"tools": map[string]any{},
		},
	})
}

func (s *Server) handleToolsList(w http.ResponseWriter, req *Request) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tools := make([]map[string]any, 0, len(s.tools))
	for _, tool := range s.tools {
		tools = append(tools, map[string]any{
			"name":        tool.Name,
			"description": tool.Description,
			"inputSchema": map[string]any{
				"type":       "object",
				"properties": tool.Parameters,
			},
		})
	}

	s.sendResult(w, req.ID, map[string]any{
		"tools": tools,
	})
}

func (s *Server) handleToolsCall(w http.ResponseWriter, ctx context.Context, req *Request) {
	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(req.Params, &params); err != nil {
		s.sendError(w, req.ID, -32602, "Invalid params")
		return
	}

	s.mu.RLock()
	tool, ok := s.tools[params.Name]
	s.mu.RUnlock()

	if !ok {
		s.sendError(w, req.ID, -32602, fmt.Sprintf("Unknown tool: %s", params.Name))
		return
	}

	result, err := tool.Handler(ctx, params.Arguments)
	if err != nil {
		s.sendError(w, req.ID, -32000, err.Error())
		return
	}

	s.sendResult(w, req.ID, map[string]any{
		"content": []map[string]any{
			{
				"type": "text",
				"text": fmt.Sprintf("%v", result),
			},
		},
	})
}

func (s *Server) sendResult(w http.ResponseWriter, id any, result any) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(Response{
		JSONRPC: "2.0",
		ID:      id,
		Result:  result,
	})
}

func (s *Server) sendError(w http.ResponseWriter, id any, code int, message string) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(Response{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &Error{Code: code, Message: message},
	})
}
