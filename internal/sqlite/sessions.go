package sqlite

import (
	"context"
	"database/sql"
	"time"
)

// Session represents a work session.
type Session struct {
	ID             string     `json:"id"`
	TaskGUID       string     `json:"task_guid"`
	TaskTitle      string     `json:"task_title"`
	TaskSource     string     `json:"task_source,omitempty"`
	StartedAt      time.Time  `json:"started_at"`
	EndedAt        *time.Time `json:"ended_at,omitempty"`
	ElapsedSeconds int        `json:"elapsed_seconds"`
	Status         string     `json:"status"` // "active", "paused", "completed", "abandoned"
	Date           string     `json:"date"`   // YYYY-MM-DD
}

// DailyStats represents aggregated stats for a day.
type DailyStats struct {
	Date           string `json:"date"` // YYYY-MM-DD
	TasksFound     int    `json:"tasks_found"`
	TasksCompleted int    `json:"tasks_completed"`
	TotalSeconds   int    `json:"total_seconds"`
	SessionCount   int    `json:"session_count"`
}

// migrateSessions runs session-related migrations.
func (d *DB) migrateSessions() error {
	migrations := []string{
		// sessions: tracks work sessions
		`CREATE TABLE IF NOT EXISTS sessions (
			id TEXT PRIMARY KEY,
			task_guid TEXT NOT NULL,
			task_title TEXT NOT NULL,
			task_source TEXT,
			started_at DATETIME NOT NULL,
			ended_at DATETIME,
			elapsed_seconds INTEGER DEFAULT 0,
			status TEXT NOT NULL DEFAULT 'active',
			date TEXT NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_date ON sessions(date)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_task_guid ON sessions(task_guid)`,
		`CREATE INDEX IF NOT EXISTS idx_sessions_status ON sessions(status)`,

		// daily_stats: aggregated stats per day
		`CREATE TABLE IF NOT EXISTS daily_stats (
			date TEXT PRIMARY KEY,
			tasks_found INTEGER DEFAULT 0,
			tasks_completed INTEGER DEFAULT 0,
			total_seconds INTEGER DEFAULT 0,
			session_count INTEGER DEFAULT 0
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

// UpsertSession inserts or updates a session.
func (d *DB) UpsertSession(ctx context.Context, s *Session) error {
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO sessions (id, task_guid, task_title, task_source, started_at, ended_at, elapsed_seconds, status, date)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			task_guid = excluded.task_guid,
			task_title = excluded.task_title,
			task_source = excluded.task_source,
			started_at = excluded.started_at,
			ended_at = excluded.ended_at,
			elapsed_seconds = excluded.elapsed_seconds,
			status = excluded.status,
			date = excluded.date
	`, s.ID, s.TaskGUID, s.TaskTitle, s.TaskSource, s.StartedAt, s.EndedAt, s.ElapsedSeconds, s.Status, s.Date)
	return err
}

// GetSession retrieves a session by ID.
func (d *DB) GetSession(ctx context.Context, id string) (*Session, error) {
	s := &Session{}
	var endedAt sql.NullString
	var taskSource sql.NullString

	err := d.db.QueryRowContext(ctx, `
		SELECT id, task_guid, task_title, task_source, started_at, ended_at, elapsed_seconds, status, date
		FROM sessions WHERE id = ?
	`, id).Scan(&s.ID, &s.TaskGUID, &s.TaskTitle, &taskSource, &s.StartedAt, &endedAt, &s.ElapsedSeconds, &s.Status, &s.Date)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	s.TaskSource = taskSource.String
	if endedAt.Valid {
		t, _ := time.Parse(time.RFC3339, endedAt.String)
		s.EndedAt = &t
	}

	return s, nil
}

// GetSessionsByDate retrieves all sessions for a given date.
func (d *DB) GetSessionsByDate(ctx context.Context, date string) ([]*Session, error) {
	rows, err := d.db.QueryContext(ctx, `
		SELECT id, task_guid, task_title, task_source, started_at, ended_at, elapsed_seconds, status, date
		FROM sessions WHERE date = ?
		ORDER BY started_at ASC
	`, date)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var sessions []*Session
	for rows.Next() {
		s := &Session{}
		var endedAt sql.NullString
		var taskSource sql.NullString

		if err := rows.Scan(&s.ID, &s.TaskGUID, &s.TaskTitle, &taskSource, &s.StartedAt, &endedAt, &s.ElapsedSeconds, &s.Status, &s.Date); err != nil {
			return nil, err
		}

		s.TaskSource = taskSource.String
		if endedAt.Valid {
			t, _ := time.Parse(time.RFC3339, endedAt.String)
			s.EndedAt = &t
		}

		sessions = append(sessions, s)
	}

	return sessions, rows.Err()
}

// GetActiveSession retrieves the currently active session (if any).
func (d *DB) GetActiveSession(ctx context.Context) (*Session, error) {
	s := &Session{}
	var endedAt sql.NullString
	var taskSource sql.NullString

	err := d.db.QueryRowContext(ctx, `
		SELECT id, task_guid, task_title, task_source, started_at, ended_at, elapsed_seconds, status, date
		FROM sessions WHERE status IN ('active', 'paused')
		ORDER BY started_at DESC LIMIT 1
	`).Scan(&s.ID, &s.TaskGUID, &s.TaskTitle, &taskSource, &s.StartedAt, &endedAt, &s.ElapsedSeconds, &s.Status, &s.Date)

	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}

	s.TaskSource = taskSource.String
	if endedAt.Valid {
		t, _ := time.Parse(time.RFC3339, endedAt.String)
		s.EndedAt = &t
	}

	return s, nil
}

// UpdateSessionStatus updates a session's status.
func (d *DB) UpdateSessionStatus(ctx context.Context, id, status string, elapsed int, endedAt *time.Time) error {
	_, err := d.db.ExecContext(ctx, `
		UPDATE sessions SET status = ?, elapsed_seconds = ?, ended_at = ?
		WHERE id = ?
	`, status, elapsed, endedAt, id)
	return err
}

// GetDailyStats retrieves stats for a given date.
func (d *DB) GetDailyStats(ctx context.Context, date string) (*DailyStats, error) {
	stats := &DailyStats{Date: date}
	err := d.db.QueryRowContext(ctx, `
		SELECT date, tasks_found, tasks_completed, total_seconds, session_count
		FROM daily_stats WHERE date = ?
	`, date).Scan(&stats.Date, &stats.TasksFound, &stats.TasksCompleted, &stats.TotalSeconds, &stats.SessionCount)

	if err == sql.ErrNoRows {
		return stats, nil // Return empty stats for the date
	}
	return stats, err
}

// UpdateDailyStats updates or inserts daily stats.
func (d *DB) UpdateDailyStats(ctx context.Context, stats *DailyStats) error {
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO daily_stats (date, tasks_found, tasks_completed, total_seconds, session_count)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(date) DO UPDATE SET
			tasks_found = excluded.tasks_found,
			tasks_completed = excluded.tasks_completed,
			total_seconds = excluded.total_seconds,
			session_count = excluded.session_count
	`, stats.Date, stats.TasksFound, stats.TasksCompleted, stats.TotalSeconds, stats.SessionCount)
	return err
}

// IncrementDailyStats increments specific stats for today.
func (d *DB) IncrementDailyStats(ctx context.Context, date string, completedDelta, secondsDelta, sessionDelta int) error {
	_, err := d.db.ExecContext(ctx, `
		INSERT INTO daily_stats (date, tasks_found, tasks_completed, total_seconds, session_count)
		VALUES (?, 0, ?, ?, ?)
		ON CONFLICT(date) DO UPDATE SET
			tasks_completed = daily_stats.tasks_completed + excluded.tasks_completed,
			total_seconds = daily_stats.total_seconds + excluded.total_seconds,
			session_count = daily_stats.session_count + excluded.session_count
	`, date, completedDelta, secondsDelta, sessionDelta)
	return err
}

// DeleteOldSessions deletes sessions older than the specified number of days.
func (d *DB) DeleteOldSessions(ctx context.Context, daysToKeep int) error {
	cutoff := time.Now().AddDate(0, 0, -daysToKeep).Format("2006-01-02")
	_, err := d.db.ExecContext(ctx, `DELETE FROM sessions WHERE date < ?`, cutoff)
	return err
}

// DeleteOldDailyStats deletes daily stats older than the specified number of days.
func (d *DB) DeleteOldDailyStats(ctx context.Context, daysToKeep int) error {
	cutoff := time.Now().AddDate(0, 0, -daysToKeep).Format("2006-01-02")
	_, err := d.db.ExecContext(ctx, `DELETE FROM daily_stats WHERE date < ?`, cutoff)
	return err
}
