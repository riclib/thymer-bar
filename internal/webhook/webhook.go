// Package webhook provides HTTP webhook receivers for real-time sync.
package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
)

// Handler processes incoming webhooks.
type Handler interface {
	// Source returns the webhook source name (github, calendar, etc.)
	Source() string
	// Handle processes the webhook payload.
	Handle(ctx context.Context, event string, payload []byte) error
}

// Server receives webhooks and dispatches to handlers.
type Server struct {
	handlers map[string]Handler
	secrets  map[string]string // source -> webhook secret
	log      *slog.Logger
	mux      *http.ServeMux
}

// Config holds webhook server configuration.
type Config struct {
	Secrets map[string]string // source -> webhook secret for validation
}

// New creates a new webhook server.
func New(cfg Config, log *slog.Logger) *Server {
	s := &Server{
		handlers: make(map[string]Handler),
		secrets:  cfg.Secrets,
		log:      log,
		mux:      http.NewServeMux(),
	}

	// Register routes
	s.mux.HandleFunc("/webhooks/github", s.handleGitHub)
	s.mux.HandleFunc("/webhooks/calendar", s.handleCalendar)
	s.mux.HandleFunc("/webhooks/health", s.handleHealth)

	return s
}

// Register adds a handler for a webhook source.
func (s *Server) Register(h Handler) {
	s.handlers[h.Source()] = h
	s.log.Info("Registered webhook handler", "source", h.Source())
}

// Handler returns the HTTP handler for mounting.
func (s *Server) Handler() http.Handler {
	return s.mux
}

// handleHealth is a simple health check endpoint.
func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

// handleGitHub processes GitHub webhooks.
func (s *Server) handleGitHub(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Read body
	body, err := io.ReadAll(r.Body)
	if err != nil {
		s.log.Error("Failed to read webhook body", "error", err)
		http.Error(w, "Failed to read body", http.StatusBadRequest)
		return
	}

	// Validate signature if secret is configured
	if secret, ok := s.secrets["github"]; ok && secret != "" {
		signature := r.Header.Get("X-Hub-Signature-256")
		if !s.validateGitHubSignature(body, signature, secret) {
			s.log.Warn("Invalid GitHub webhook signature")
			http.Error(w, "Invalid signature", http.StatusUnauthorized)
			return
		}
	}

	// Get event type
	event := r.Header.Get("X-GitHub-Event")
	if event == "" {
		http.Error(w, "Missing X-GitHub-Event header", http.StatusBadRequest)
		return
	}

	s.log.Info("Received GitHub webhook", "event", event)

	// Dispatch to handler
	if handler, ok := s.handlers["github"]; ok {
		if err := handler.Handle(r.Context(), event, body); err != nil {
			s.log.Error("Failed to handle GitHub webhook", "event", event, "error", err)
			http.Error(w, "Handler error", http.StatusInternalServerError)
			return
		}
	}

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("ok"))
}

// validateGitHubSignature validates the X-Hub-Signature-256 header.
func (s *Server) validateGitHubSignature(payload []byte, signature, secret string) bool {
	if !strings.HasPrefix(signature, "sha256=") {
		return false
	}

	sig, err := hex.DecodeString(strings.TrimPrefix(signature, "sha256="))
	if err != nil {
		return false
	}

	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	expected := mac.Sum(nil)

	return hmac.Equal(sig, expected)
}

// handleCalendar processes Google Calendar push notifications.
func (s *Server) handleCalendar(w http.ResponseWriter, r *http.Request) {
	// Google Calendar uses POST for push notifications
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	// Google sends channel info in headers
	channelID := r.Header.Get("X-Goog-Channel-ID")
	resourceID := r.Header.Get("X-Goog-Resource-ID")
	resourceState := r.Header.Get("X-Goog-Resource-State")

	s.log.Info("Received Calendar webhook",
		"channel", channelID,
		"resource", resourceID,
		"state", resourceState,
	)

	// Read body (may be empty for sync notifications)
	body, _ := io.ReadAll(r.Body)

	// Dispatch to handler
	if handler, ok := s.handlers["calendar"]; ok {
		if err := handler.Handle(r.Context(), resourceState, body); err != nil {
			s.log.Error("Failed to handle Calendar webhook", "error", err)
			http.Error(w, "Handler error", http.StatusInternalServerError)
			return
		}
	}

	w.WriteHeader(http.StatusOK)
}

// GitHubEvent represents common GitHub webhook event structure.
type GitHubEvent struct {
	Action     string          `json:"action"`
	Repository GitHubRepo      `json:"repository"`
	Sender     GitHubUser      `json:"sender"`
	Issue      *GitHubIssue    `json:"issue,omitempty"`
	PullRequest *GitHubPR      `json:"pull_request,omitempty"`
}

type GitHubRepo struct {
	FullName string `json:"full_name"`
	HTMLURL  string `json:"html_url"`
}

type GitHubUser struct {
	Login string `json:"login"`
}

type GitHubIssue struct {
	Number    int        `json:"number"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	State     string     `json:"state"`
	HTMLURL   string     `json:"html_url"`
	Labels    []GitHubLabel `json:"labels"`
	User      GitHubUser `json:"user"`
	CreatedAt string     `json:"created_at"`
	UpdatedAt string     `json:"updated_at"`
}

type GitHubPR struct {
	Number    int        `json:"number"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	State     string     `json:"state"`
	HTMLURL   string     `json:"html_url"`
	Merged    bool       `json:"merged"`
	Labels    []GitHubLabel `json:"labels"`
	User      GitHubUser `json:"user"`
	CreatedAt string     `json:"created_at"`
	UpdatedAt string     `json:"updated_at"`
}

type GitHubLabel struct {
	Name string `json:"name"`
}

// ParseGitHubEvent parses a GitHub webhook payload.
func ParseGitHubEvent(payload []byte) (*GitHubEvent, error) {
	var event GitHubEvent
	if err := json.Unmarshal(payload, &event); err != nil {
		return nil, fmt.Errorf("failed to parse GitHub event: %w", err)
	}
	return &event, nil
}
