package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"time"

	"thymer-bar/internal/bit"
)

// Bits table schema migration.
func (d *DB) migrateBits() error {
	migrations := []string{
		// bits: canonical storage for all content
		`CREATE TABLE IF NOT EXISTS bits (
			id TEXT PRIMARY KEY,
			master_uri TEXT UNIQUE NOT NULL,
			master_system TEXT NOT NULL,
			frontmatter TEXT NOT NULL DEFAULT '{}',
			content TEXT NOT NULL DEFAULT '',
			hash TEXT NOT NULL,
			embedding BLOB,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)`,
		`CREATE INDEX IF NOT EXISTS idx_bits_master_system ON bits(master_system)`,
		`CREATE INDEX IF NOT EXISTS idx_bits_hash ON bits(hash)`,

		// refs: external references for bidirectional sync
		`CREATE TABLE IF NOT EXISTS refs (
			master_uri TEXT NOT NULL,
			system TEXT NOT NULL,
			external_id TEXT NOT NULL,
			their_hash TEXT NOT NULL DEFAULT '',
			our_hash TEXT NOT NULL DEFAULT '',
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (master_uri, system)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_refs_system ON refs(system)`,
		`CREATE INDEX IF NOT EXISTS idx_refs_external_id ON refs(system, external_id)`,

		// sync_cursors: track sync position per system
		`CREATE TABLE IF NOT EXISTS sync_cursors (
			system TEXT PRIMARY KEY,
			cursor TEXT,
			last_sync DATETIME,
			last_source_updated_at DATETIME,
			metadata TEXT
		)`,
	}

	for _, m := range migrations {
		if _, err := d.db.Exec(m); err != nil {
			if !isDuplicateColumnError(err) {
				return err
			}
		}
	}

	return nil
}

// UpsertBit inserts or updates a Bit.
func (d *DB) UpsertBit(ctx context.Context, b *bit.Bit) error {
	fm, err := json.Marshal(b.Frontmatter)
	if err != nil {
		return err
	}

	_, err = d.db.ExecContext(ctx, `
		INSERT INTO bits (id, master_uri, master_system, frontmatter, content, hash, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(master_uri) DO UPDATE SET
			frontmatter = excluded.frontmatter,
			content = excluded.content,
			hash = excluded.hash,
			updated_at = excluded.updated_at
	`, b.ID, b.MasterURI, b.MasterSystem, string(fm), b.Content, b.Hash, b.CreatedAt, b.UpdatedAt)
	return err
}

// GetBitByURI retrieves a Bit by its master_uri.
func (d *DB) GetBitByURI(ctx context.Context, masterURI string) (*bit.Bit, error) {
	b := &bit.Bit{}
	var fm string
	var createdAt, updatedAt sql.NullString

	err := d.db.QueryRowContext(ctx, `
		SELECT id, master_uri, master_system, frontmatter, content, hash, created_at, updated_at
		FROM bits WHERE master_uri = ?
	`, masterURI).Scan(&b.ID, &b.MasterURI, &b.MasterSystem, &fm, &b.Content, &b.Hash, &createdAt, &updatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal([]byte(fm), &b.Frontmatter); err != nil {
		return nil, err
	}

	if createdAt.Valid {
		b.CreatedAt, _ = time.Parse(time.RFC3339, createdAt.String)
	}
	if updatedAt.Valid {
		b.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt.String)
	}

	return b, nil
}

// GetBitByID retrieves a Bit by its local ID.
func (d *DB) GetBitByID(ctx context.Context, id string) (*bit.Bit, error) {
	b := &bit.Bit{}
	var fm string
	var createdAt, updatedAt sql.NullString

	err := d.db.QueryRowContext(ctx, `
		SELECT id, master_uri, master_system, frontmatter, content, hash, created_at, updated_at
		FROM bits WHERE id = ?
	`, id).Scan(&b.ID, &b.MasterURI, &b.MasterSystem, &fm, &b.Content, &b.Hash, &createdAt, &updatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if err := json.Unmarshal([]byte(fm), &b.Frontmatter); err != nil {
		return nil, err
	}

	if createdAt.Valid {
		b.CreatedAt, _ = time.Parse(time.RFC3339, createdAt.String)
	}
	if updatedAt.Valid {
		b.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt.String)
	}

	return b, nil
}

// GetBitsBySystem retrieves all Bits from a given master_system.
func (d *DB) GetBitsBySystem(ctx context.Context, system string) ([]*bit.Bit, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, master_uri, master_system, frontmatter, content, hash, created_at, updated_at
		FROM bits WHERE master_system = ?
		ORDER BY updated_at DESC
	`, system)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bits []*bit.Bit
	for rows.Next() {
		b := &bit.Bit{}
		var fm string
		var createdAt, updatedAt sql.NullString

		if err := rows.Scan(&b.ID, &b.MasterURI, &b.MasterSystem, &fm, &b.Content, &b.Hash, &createdAt, &updatedAt); err != nil {
			return nil, err
		}

		if err := json.Unmarshal([]byte(fm), &b.Frontmatter); err != nil {
			return nil, err
		}

		if createdAt.Valid {
			b.CreatedAt, _ = time.Parse(time.RFC3339, createdAt.String)
		}
		if updatedAt.Valid {
			b.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt.String)
		}

		bits = append(bits, b)
	}

	return bits, rows.Err()
}

// DeleteBit removes a Bit and all its refs.
func (d *DB) DeleteBit(ctx context.Context, masterURI string) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	// Delete refs first
	if _, err := tx.ExecContext(ctx, `DELETE FROM refs WHERE master_uri = ?`, masterURI); err != nil {
		return err
	}

	// Delete bit
	if _, err := tx.ExecContext(ctx, `DELETE FROM bits WHERE master_uri = ?`, masterURI); err != nil {
		return err
	}

	return tx.Commit()
}

// UpsertRef inserts or updates a Ref.
func (d *DB) UpsertRef(ctx context.Context, r *bit.Ref) error {
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO refs (master_uri, system, external_id, their_hash, our_hash, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(master_uri, system) DO UPDATE SET
			external_id = excluded.external_id,
			their_hash = excluded.their_hash,
			our_hash = excluded.our_hash,
			updated_at = excluded.updated_at
	`, r.MasterURI, r.System, r.ExternalID, r.TheirHash, r.OurHash, r.CreatedAt, r.UpdatedAt)
	return err
}

// GetRef retrieves a Ref by master_uri and system.
func (d *DB) GetRef(ctx context.Context, masterURI, system string) (*bit.Ref, error) {
	r := &bit.Ref{}
	var createdAt, updatedAt sql.NullString

	err := d.db.QueryRowContext(ctx, `
		SELECT master_uri, system, external_id, their_hash, our_hash, created_at, updated_at
		FROM refs WHERE master_uri = ? AND system = ?
	`, masterURI, system).Scan(&r.MasterURI, &r.System, &r.ExternalID, &r.TheirHash, &r.OurHash, &createdAt, &updatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if createdAt.Valid {
		r.CreatedAt, _ = time.Parse(time.RFC3339, createdAt.String)
	}
	if updatedAt.Valid {
		r.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt.String)
	}

	return r, nil
}

// GetRefByExternalID retrieves a Ref by system and external_id.
// Useful for finding a Bit from an external system's ID (e.g., Thymer UUID).
func (d *DB) GetRefByExternalID(ctx context.Context, system, externalID string) (*bit.Ref, error) {
	r := &bit.Ref{}
	var createdAt, updatedAt sql.NullString

	err := d.db.QueryRowContext(ctx, `
		SELECT master_uri, system, external_id, their_hash, our_hash, created_at, updated_at
		FROM refs WHERE system = ? AND external_id = ?
	`, system, externalID).Scan(&r.MasterURI, &r.System, &r.ExternalID, &r.TheirHash, &r.OurHash, &createdAt, &updatedAt)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	if createdAt.Valid {
		r.CreatedAt, _ = time.Parse(time.RFC3339, createdAt.String)
	}
	if updatedAt.Valid {
		r.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt.String)
	}

	return r, nil
}

// GetRefsByMasterURI retrieves all refs for a Bit.
func (d *DB) GetRefsByMasterURI(ctx context.Context, masterURI string) ([]*bit.Ref, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT master_uri, system, external_id, their_hash, our_hash, created_at, updated_at
		FROM refs WHERE master_uri = ?
	`, masterURI)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var refs []*bit.Ref
	for rows.Next() {
		r := &bit.Ref{}
		var createdAt, updatedAt sql.NullString

		if err := rows.Scan(&r.MasterURI, &r.System, &r.ExternalID, &r.TheirHash, &r.OurHash, &createdAt, &updatedAt); err != nil {
			return nil, err
		}

		if createdAt.Valid {
			r.CreatedAt, _ = time.Parse(time.RFC3339, createdAt.String)
		}
		if updatedAt.Valid {
			r.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt.String)
		}

		refs = append(refs, r)
	}

	return refs, rows.Err()
}

// DeleteRef removes a Ref.
func (d *DB) DeleteRef(ctx context.Context, masterURI, system string) error {
	_, err := d.db.ExecContext(ctx, `DELETE FROM refs WHERE master_uri = ? AND system = ?`, masterURI, system)
	return err
}

// SyncCursor represents sync state for a system.
type SyncCursor struct {
	System              string
	Cursor              string
	LastSync            *time.Time
	LastSourceUpdatedAt *time.Time
}

// GetSyncCursorForSystem retrieves sync cursor for a system.
func (d *DB) GetSyncCursorForSystem(ctx context.Context, system string) (*SyncCursor, error) {
	c := &SyncCursor{System: system}
	var cursor, lastSync, lastSourceUpdatedAt sql.NullString

	err := d.db.QueryRowContext(ctx, `
		SELECT cursor, last_sync, last_source_updated_at
		FROM sync_cursors WHERE system = ?
	`, system).Scan(&cursor, &lastSync, &lastSourceUpdatedAt)

	if err == sql.ErrNoRows {
		return c, nil
	}
	if err != nil {
		return nil, err
	}

	c.Cursor = cursor.String
	if lastSync.Valid {
		t, _ := time.Parse(time.RFC3339, lastSync.String)
		c.LastSync = &t
	}
	if lastSourceUpdatedAt.Valid {
		t, _ := time.Parse(time.RFC3339, lastSourceUpdatedAt.String)
		c.LastSourceUpdatedAt = &t
	}

	return c, nil
}

// UpdateSyncCursor updates sync cursor for a system.
func (d *DB) UpdateSyncCursor(ctx context.Context, c *SyncCursor) error {
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO sync_cursors (system, cursor, last_sync, last_source_updated_at)
		VALUES (?, ?, CURRENT_TIMESTAMP, ?)
		ON CONFLICT(system) DO UPDATE SET
			cursor = excluded.cursor,
			last_sync = CURRENT_TIMESTAMP,
			last_source_updated_at = excluded.last_source_updated_at
	`, c.System, c.Cursor, c.LastSourceUpdatedAt)
	return err
}

// GetAllBits retrieves all Bits from the database.
func (d *DB) GetAllBits(ctx context.Context) ([]*bit.Bit, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, master_uri, master_system, frontmatter, content, hash, created_at, updated_at
		FROM bits
		ORDER BY updated_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var bits []*bit.Bit
	for rows.Next() {
		b := &bit.Bit{}
		var fm string
		var createdAt, updatedAt sql.NullString

		if err := rows.Scan(&b.ID, &b.MasterURI, &b.MasterSystem, &fm, &b.Content, &b.Hash, &createdAt, &updatedAt); err != nil {
			return nil, err
		}

		if err := json.Unmarshal([]byte(fm), &b.Frontmatter); err != nil {
			return nil, err
		}

		if createdAt.Valid {
			b.CreatedAt, _ = time.Parse(time.RFC3339, createdAt.String)
		}
		if updatedAt.Valid {
			b.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt.String)
		}

		bits = append(bits, b)
	}

	return bits, rows.Err()
}

// ClearAllForResync clears bits, refs, and sync_cursors tables for a full resync.
func (d *DB) ClearAllForResync(ctx context.Context) error {
	tx, err := d.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `DELETE FROM refs`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM bits`); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM sync_cursors`); err != nil {
		return err
	}

	return tx.Commit()
}

// ClearAllRefs clears all refs for a full rerender.
func (d *DB) ClearAllRefs(ctx context.Context) error {
	_, err := d.db.ExecContext(ctx, `DELETE FROM refs`)
	return err
}

// CountBits returns the number of bits in the database.
func (d *DB) CountBits(ctx context.Context) (int, error) {
	var count int
	err := d.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM bits`).Scan(&count)
	return count, err
}

// CountRefs returns the number of refs in the database.
func (d *DB) CountRefs(ctx context.Context) (int, error) {
	var count int
	err := d.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM refs`).Scan(&count)
	return count, err
}
