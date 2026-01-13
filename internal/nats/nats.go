// Package nats provides an embedded NATS server with JetStream for event persistence.
package nats

import (
	"context"
	"fmt"
	"path/filepath"
	"time"

	"github.com/nats-io/nats-server/v2/server"
	"github.com/nats-io/nats.go"
	"github.com/nats-io/nats.go/jetstream"
)

// Server wraps an embedded NATS server with JetStream.
type Server struct {
	ns     *server.Server
	nc     *nats.Conn
	js     jetstream.JetStream
	dataDir string
}

// Config holds NATS server configuration.
type Config struct {
	DataDir string // Directory for JetStream storage
	Port    int    // NATS port (default: 4222, use -1 for random)
}

// New creates and starts an embedded NATS server with JetStream.
func New(cfg Config) (*Server, error) {
	if cfg.Port == 0 {
		cfg.Port = -1 // Random port for embedded use
	}

	opts := &server.Options{
		Port:      cfg.Port,
		JetStream: true,
		StoreDir:  filepath.Join(cfg.DataDir, "nats"),
		NoLog:     true,
		NoSigs:    true,
	}

	ns, err := server.NewServer(opts)
	if err != nil {
		return nil, fmt.Errorf("failed to create NATS server: %w", err)
	}

	go ns.Start()

	if !ns.ReadyForConnections(10 * time.Second) {
		return nil, fmt.Errorf("NATS server not ready for connections")
	}

	// Connect to the embedded server
	nc, err := nats.Connect(ns.ClientURL())
	if err != nil {
		ns.Shutdown()
		return nil, fmt.Errorf("failed to connect to NATS: %w", err)
	}

	// Get JetStream context
	js, err := jetstream.New(nc)
	if err != nil {
		nc.Close()
		ns.Shutdown()
		return nil, fmt.Errorf("failed to create JetStream context: %w", err)
	}

	return &Server{
		ns:      ns,
		nc:      nc,
		js:      js,
		dataDir: cfg.DataDir,
	}, nil
}

// JetStream returns the JetStream context for creating streams and consumers.
func (s *Server) JetStream() jetstream.JetStream {
	return s.js
}

// Conn returns the NATS connection.
func (s *Server) Conn() *nats.Conn {
	return s.nc
}

// EnsureStream creates a stream if it doesn't exist.
func (s *Server) EnsureStream(ctx context.Context, name string, subjects []string) (jetstream.Stream, error) {
	stream, err := s.js.CreateOrUpdateStream(ctx, jetstream.StreamConfig{
		Name:      name,
		Subjects:  subjects,
		Storage:   jetstream.FileStorage,
		Retention: jetstream.LimitsPolicy,
		MaxAge:    30 * 24 * time.Hour, // Keep events for 30 days
	})
	if err != nil {
		return nil, fmt.Errorf("failed to ensure stream %s: %w", name, err)
	}
	return stream, nil
}

// Close shuts down the NATS server and connection.
func (s *Server) Close() error {
	if s.nc != nil {
		s.nc.Close()
	}
	if s.ns != nil {
		s.ns.Shutdown()
		s.ns.WaitForShutdown()
	}
	return nil
}
