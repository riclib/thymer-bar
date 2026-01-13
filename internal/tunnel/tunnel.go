// Package tunnel provides Cloudflare Tunnel integration for public endpoint exposure.
package tunnel

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os/exec"
	"strings"
	"sync"
	"time"
)

// Tunnel manages a Cloudflare Tunnel connection.
type Tunnel struct {
	localPort  int
	tunnelName string // Named tunnel (uses `cloudflared tunnel run`)
	domain     string // Custom domain (e.g., "hub.lifelog.my")
	publicURL  string
	cmd        *exec.Cmd
	log        *slog.Logger
	mu         sync.RWMutex
	running    bool
}

// Config holds tunnel configuration.
type Config struct {
	LocalPort  int    // Local port to tunnel
	TunnelName string // Named tunnel (pre-configured in Cloudflare dashboard)
	Domain     string // Custom domain for named tunnel (e.g., "hub.lifelog.my")
}

// New creates a new tunnel manager.
func New(cfg Config, log *slog.Logger) *Tunnel {
	return &Tunnel{
		localPort:  cfg.LocalPort,
		tunnelName: cfg.TunnelName,
		domain:     cfg.Domain,
		log:        log,
	}
}

// Start starts the Cloudflare Tunnel.
// If TunnelName is set, uses named tunnel (`cloudflared tunnel run`).
// Otherwise, uses quick tunnel (`cloudflared tunnel --url`) - no account needed.
func (t *Tunnel) Start(ctx context.Context) error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.running {
		return nil
	}

	// Check if cloudflared is available
	if _, err := exec.LookPath("cloudflared"); err != nil {
		return fmt.Errorf("cloudflared not found in PATH: %w", err)
	}

	// Named tunnel or quick tunnel?
	if t.tunnelName != "" {
		// Named tunnel: `cloudflared tunnel run <name>`
		// Requires pre-configuration in Cloudflare dashboard
		t.cmd = exec.CommandContext(ctx, "cloudflared", "tunnel", "run", t.tunnelName)
		// Set public URL immediately since we know the domain
		if t.domain != "" {
			t.publicURL = "https://" + t.domain
		}
	} else {
		// Quick tunnel: random trycloudflare.com URL
		t.cmd = exec.CommandContext(ctx, "cloudflared", "tunnel", "--url", fmt.Sprintf("http://localhost:%d", t.localPort))
	}

	// Capture output to find the public URL
	stdout, err := t.cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("failed to get stdout pipe: %w", err)
	}
	stderr, err := t.cmd.StderrPipe()
	if err != nil {
		return fmt.Errorf("failed to get stderr pipe: %w", err)
	}

	if err := t.cmd.Start(); err != nil {
		return fmt.Errorf("failed to start cloudflared: %w", err)
	}

	t.running = true

	// Parse output for public URL (cloudflared prints to stderr)
	go t.parseOutput(stdout)
	go t.parseOutput(stderr)

	// Wait a bit for URL to be available
	go func() {
		for i := 0; i < 30; i++ {
			time.Sleep(time.Second)
			if t.PublicURL() != "" {
				t.log.Info("Tunnel established", "url", t.PublicURL())
				return
			}
		}
		t.log.Warn("Tunnel URL not found after 30 seconds")
	}()

	return nil
}

func (t *Tunnel) parseOutput(r interface{ Read([]byte) (int, error) }) {
	buf := make([]byte, 4096)
	for {
		n, err := r.Read(buf)
		if err != nil {
			return
		}
		output := string(buf[:n])

		// Look for the trycloudflare.com URL
		if strings.Contains(output, "trycloudflare.com") {
			// Extract URL from output
			lines := strings.Split(output, "\n")
			for _, line := range lines {
				if strings.Contains(line, "https://") && strings.Contains(line, "trycloudflare.com") {
					// Extract just the URL
					start := strings.Index(line, "https://")
					if start >= 0 {
						url := line[start:]
						// Trim any trailing whitespace or characters
						if end := strings.IndexAny(url, " \t\n\r"); end > 0 {
							url = url[:end]
						}
						t.mu.Lock()
						t.publicURL = strings.TrimSpace(url)
						t.mu.Unlock()
					}
				}
			}
		}
	}
}

// Stop stops the Cloudflare Tunnel.
func (t *Tunnel) Stop() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if !t.running {
		return nil
	}

	if t.cmd != nil && t.cmd.Process != nil {
		t.cmd.Process.Kill()
		t.cmd.Wait()
	}

	t.running = false
	t.publicURL = ""
	t.log.Info("Tunnel stopped")
	return nil
}

// PublicURL returns the public URL of the tunnel.
func (t *Tunnel) PublicURL() string {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.publicURL
}

// Running returns true if the tunnel is running.
func (t *Tunnel) Running() bool {
	t.mu.RLock()
	defer t.mu.RUnlock()
	return t.running
}

// WebhookURL returns the full URL for a webhook endpoint.
func (t *Tunnel) WebhookURL(path string) string {
	base := t.PublicURL()
	if base == "" {
		return ""
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	return base + path
}

// Handler returns an HTTP handler that shows tunnel status.
func (t *Tunnel) StatusHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		t.mu.RLock()
		defer t.mu.RUnlock()

		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"running":%t,"url":%q}`, t.running, t.publicURL)
	}
}
