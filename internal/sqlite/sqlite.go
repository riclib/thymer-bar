// Package sqlite provides SQLite storage for sync state and ID mappings.
package sqlite

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// DB wraps the SQLite database connection.
type DB struct {
	db *sql.DB
}

// Record represents a synced record with ID mappings.
type Record struct {
	ID          string    // Internal unique ID
	Source      string    // Source system (github, readwise, etc.)
	ExternalID  string    // ID in the source system
	ThymerUUID  string    // UUID in Thymer (if synced)
	ContentHash string    // Hash of content for change detection
	Data        []byte    // JSON blob of record data
	CreatedAt   time.Time
	UpdatedAt   time.Time
	SyncedAt    *time.Time // When last synced to Thymer
}

// Event represents a logged event.
type Event struct {
	ID        string
	Type      string    // task.started, task.paused, etc.
	RecordID  string    // Related record ID
	Timestamp time.Time
	Data      []byte    // JSON blob
}

// Config holds database configuration.
type Config struct {
	DataDir string
}

// New opens or creates the SQLite database.
func New(cfg Config) (*DB, error) {
	dbPath := filepath.Join(cfg.DataDir, "thymer-bar.db")

	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	// Enable WAL mode for better concurrency
	if _, err := db.Exec("PRAGMA journal_mode=WAL"); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to enable WAL: %w", err)
	}

	d := &DB{db: db}
	if err := d.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to migrate: %w", err)
	}

	return d, nil
}

// migrate runs database migrations.
func (d *DB) migrate() error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS records (
			id TEXT PRIMARY KEY,
			source TEXT NOT NULL,
			external_id TEXT NOT NULL,
			thymer_uuid TEXT,
			content_hash TEXT,
			data BLOB,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			synced_at DATETIME,
			UNIQUE(source, external_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_records_source ON records(source)`,
		`CREATE INDEX IF NOT EXISTS idx_records_thymer_uuid ON records(thymer_uuid)`,

		`CREATE TABLE IF NOT EXISTS events (
			id TEXT PRIMARY KEY,
			type TEXT NOT NULL,
			record_id TEXT,
			timestamp DATETIME NOT NULL,
			data BLOB,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_events_type ON events(type)`,
		`CREATE INDEX IF NOT EXISTS idx_events_record_id ON events(record_id)`,
		`CREATE INDEX IF NOT EXISTS idx_events_timestamp ON events(timestamp)`,

		`CREATE TABLE IF NOT EXISTS sync_state (
			source TEXT PRIMARY KEY,
			cursor TEXT,
			last_sync DATETIME,
			metadata BLOB
		)`,
	}

	for _, m := range migrations {
		if _, err := d.db.Exec(m); err != nil {
			return fmt.Errorf("migration failed: %w", err)
		}
	}

	return nil
}

// UpsertRecord inserts or updates a record.
func (d *DB) UpsertRecord(ctx context.Context, r *Record) error {
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO records (id, source, external_id, thymer_uuid, content_hash, data, updated_at, synced_at)
		VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP, ?)
		ON CONFLICT(source, external_id) DO UPDATE SET
			thymer_uuid = excluded.thymer_uuid,
			content_hash = excluded.content_hash,
			data = excluded.data,
			updated_at = CURRENT_TIMESTAMP,
			synced_at = excluded.synced_at
	`, r.ID, r.Source, r.ExternalID, r.ThymerUUID, r.ContentHash, r.Data, r.SyncedAt)
	return err
}

// GetRecord retrieves a record by source and external ID.
func (d *DB) GetRecord(ctx context.Context, source, externalID string) (*Record, error) {
	r := &Record{}
	err := d.db.QueryRowContext(ctx, `
		SELECT id, source, external_id, thymer_uuid, content_hash, data, created_at, updated_at, synced_at
		FROM records WHERE source = ? AND external_id = ?
	`, source, externalID).Scan(
		&r.ID, &r.Source, &r.ExternalID, &r.ThymerUUID, &r.ContentHash,
		&r.Data, &r.CreatedAt, &r.UpdatedAt, &r.SyncedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return r, err
}

// GetRecordByThymerUUID retrieves a record by its Thymer UUID.
func (d *DB) GetRecordByThymerUUID(ctx context.Context, uuid string) (*Record, error) {
	r := &Record{}
	err := d.db.QueryRowContext(ctx, `
		SELECT id, source, external_id, thymer_uuid, content_hash, data, created_at, updated_at, synced_at
		FROM records WHERE thymer_uuid = ?
	`, uuid).Scan(
		&r.ID, &r.Source, &r.ExternalID, &r.ThymerUUID, &r.ContentHash,
		&r.Data, &r.CreatedAt, &r.UpdatedAt, &r.SyncedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return r, err
}

// InsertEvent inserts a new event.
func (d *DB) InsertEvent(ctx context.Context, e *Event) error {
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO events (id, type, record_id, timestamp, data)
		VALUES (?, ?, ?, ?, ?)
	`, e.ID, e.Type, e.RecordID, e.Timestamp, e.Data)
	return err
}

// GetEvents retrieves events for a record within a time range.
func (d *DB) GetEvents(ctx context.Context, recordID string, since, until time.Time) ([]*Event, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, type, record_id, timestamp, data
		FROM events
		WHERE record_id = ? AND timestamp >= ? AND timestamp <= ?
		ORDER BY timestamp ASC
	`, recordID, since, until)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var events []*Event
	for rows.Next() {
		e := &Event{}
		if err := rows.Scan(&e.ID, &e.Type, &e.RecordID, &e.Timestamp, &e.Data); err != nil {
			return nil, err
		}
		events = append(events, e)
	}
	return events, rows.Err()
}

// GetSyncCursor retrieves the sync cursor for a source.
func (d *DB) GetSyncCursor(ctx context.Context, source string) (string, error) {
	var cursor sql.NullString
	err := d.db.QueryRowContext(ctx, `
		SELECT cursor FROM sync_state WHERE source = ?
	`, source).Scan(&cursor)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return cursor.String, err
}

// SetSyncCursor updates the sync cursor for a source.
func (d *DB) SetSyncCursor(ctx context.Context, source, cursor string) error {
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO sync_state (source, cursor, last_sync)
		VALUES (?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(source) DO UPDATE SET
			cursor = excluded.cursor,
			last_sync = CURRENT_TIMESTAMP
	`, source, cursor)
	return err
}

// Close closes the database connection.
func (d *DB) Close() error {
	return d.db.Close()
}
