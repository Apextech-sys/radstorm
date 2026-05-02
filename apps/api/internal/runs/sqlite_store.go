// Package runs — SQLite-backed implementation of Store.
//
// Purpose:
//
//	Persists run metadata to a SQLite file at <data-dir>/runs.db so runs
//	survive API server restarts. Uses modernc.org/sqlite (pure Go, no CGO)
//	so the binary builds on Windows hosts without a C toolchain.
//
//	On startup the store reaps orphaned `running`/`queued`/`cancelling` rows
//	left over from a previous crashed instance, marking them `failed` with
//	a reason recorded in progress_json so the UI can render the cause.
//
// Related files:
//   - apps/api/internal/runs/store.go (Store interface + types)
//   - apps/api/internal/runs/runner.go (writes status transitions)
//   - apps/api/cmd/radstorm-api/main.go (constructs SQLiteStore)
//   - .orchestration/contracts/rest-api.md (Run shape on the wire)
//   - .orchestration/briefings/3b-api-full.md
//
// Briefing: .orchestration/briefings/3b-api-full.md
//
// Contract: implements runs.Store; on-disk schema is internal but stable
// across restarts.
package runs

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	_ "modernc.org/sqlite"

	"github.com/Apextech-sys/reflex-radstorm/pkg/config"
)

// schemaSQL is applied at NewSQLiteStore. CREATE TABLE IF NOT EXISTS keeps
// it idempotent across restarts. The `runs` table mirrors the in-memory Run
// type; JSON-encoded blobs hold Config / Progress / Summary so we don't have
// to track schema changes when those structs evolve.
const schemaSQL = `
CREATE TABLE IF NOT EXISTS runs (
	id            TEXT PRIMARY KEY,
	name          TEXT NOT NULL,
	status        TEXT NOT NULL,
	created_at    TEXT NOT NULL,
	started_at    TEXT,
	finished_at   TEXT,
	config_json   TEXT NOT NULL,
	progress_json TEXT NOT NULL,
	summary_json  TEXT
);

CREATE INDEX IF NOT EXISTS idx_runs_created_at ON runs(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_runs_status     ON runs(status);
`

// SQLiteStore implements Store on top of modernc.org/sqlite.
//
// A single goroutine-safe *sql.DB is shared across handlers; SQLite
// internally serialises writes. We additionally hold a sync.Mutex around
// HasActive-then-INSERT in Create() to make the single-tenant check
// race-free without relying on SQLite-specific advisory locks.
type SQLiteStore struct {
	db *sql.DB
	mu sync.Mutex
}

// NewSQLiteStore opens (creating if necessary) a SQLite database at the
// given path, applies the schema, and reaps orphaned in-flight rows from
// any prior crashed server instance.
//
// path may be ":memory:" for tests; in that case orphan reaping is a no-op
// against an empty schema.
func NewSQLiteStore(path string) (*SQLiteStore, error) {
	dsn := path
	// Always enable foreign keys + a sane busy timeout. PRAGMAs go in the
	// query string for modernc.org/sqlite.
	if path != ":memory:" {
		dsn = fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(on)", path)
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open sqlite at %q: %w", path, err)
	}
	// modernc.org/sqlite recommends a single-writer pool for WAL. Allow many
	// readers but cap writers via the mutex above.
	db.SetMaxOpenConns(1)

	if _, err := db.Exec(schemaSQL); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("apply schema: %w", err)
	}

	s := &SQLiteStore{db: db}
	if err := s.reapOrphans(); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("reap orphans: %w", err)
	}
	return s, nil
}

// Close releases the underlying DB handle.
func (s *SQLiteStore) Close() error { return s.db.Close() }

// reapOrphans flips any rows in queued/running/cancelling to failed with a
// progress.note explaining what happened. Called once at startup.
func (s *SQLiteStore) reapOrphans() error {
	rows, err := s.db.Query(`SELECT id, progress_json FROM runs WHERE status IN (?, ?, ?)`,
		StatusQueued, StatusRunning, StatusCancelling)
	if err != nil {
		return fmt.Errorf("query orphans: %w", err)
	}
	defer rows.Close()

	type orphan struct {
		id       string
		progress string
	}
	var orphans []orphan
	for rows.Next() {
		var o orphan
		if err := rows.Scan(&o.id, &o.progress); err != nil {
			return fmt.Errorf("scan orphan: %w", err)
		}
		orphans = append(orphans, o)
	}
	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate orphans: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339Nano)
	for _, o := range orphans {
		// Decorate the progress JSON with a `reason` field so the UI can
		// surface why the run failed without a separate column.
		var p map[string]any
		if o.progress != "" {
			_ = json.Unmarshal([]byte(o.progress), &p)
		}
		if p == nil {
			p = map[string]any{}
		}
		p["reason"] = "orphaned (server restart)"
		newProg, _ := json.Marshal(p)
		if _, err := s.db.Exec(
			`UPDATE runs SET status = ?, finished_at = COALESCE(finished_at, ?), progress_json = ? WHERE id = ?`,
			StatusFailed, now, string(newProg), o.id,
		); err != nil {
			return fmt.Errorf("mark orphan %s failed: %w", o.id, err)
		}
	}
	return nil
}

// Create allocates a new Run with a generated id and persists it. Rejects
// with ErrConflict if any row is in a terminal-not-yet status.
func (s *SQLiteStore) Create(name string, cfg *config.Config) (*Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	active, err := s.hasActiveLocked()
	if err != nil {
		return nil, err
	}
	if active {
		return nil, ErrConflict
	}

	now := time.Now().UTC()
	id := generateID(now)
	if name == "" {
		name = id
	}
	r := &Run{
		ID:        id,
		Name:      name,
		Status:    StatusQueued,
		CreatedAt: now,
		Config:    cfg,
		Progress: Progress{
			SubscribersTotal: subscriberCount(cfg),
		},
	}
	cfgJSON, err := json.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("marshal config: %w", err)
	}
	progJSON, _ := json.Marshal(r.Progress)
	if _, err := s.db.Exec(
		`INSERT INTO runs (id, name, status, created_at, config_json, progress_json) VALUES (?,?,?,?,?,?)`,
		r.ID, r.Name, r.Status, r.CreatedAt.Format(time.RFC3339Nano), string(cfgJSON), string(progJSON),
	); err != nil {
		return nil, fmt.Errorf("insert run: %w", err)
	}
	return r, nil
}

// Get returns the run with the given id, or ErrNotFound.
func (s *SQLiteStore) Get(id string) (*Run, error) {
	row := s.db.QueryRow(
		`SELECT id, name, status, created_at, started_at, finished_at, config_json, progress_json, summary_json FROM runs WHERE id = ?`,
		id,
	)
	r, err := scanRun(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return r, nil
}

// List returns up to `limit` runs most-recent-first, optionally filtered by
// status. limit <= 0 means no limit (capped to 10000 defensively).
func (s *SQLiteStore) List(limit int, status string) []*Run {
	if limit <= 0 || limit > 10000 {
		limit = 10000
	}
	var (
		rows *sql.Rows
		err  error
	)
	if status != "" {
		rows, err = s.db.Query(
			`SELECT id, name, status, created_at, started_at, finished_at, config_json, progress_json, summary_json
			   FROM runs WHERE status = ? ORDER BY created_at DESC LIMIT ?`,
			status, limit,
		)
	} else {
		rows, err = s.db.Query(
			`SELECT id, name, status, created_at, started_at, finished_at, config_json, progress_json, summary_json
			   FROM runs ORDER BY created_at DESC LIMIT ?`,
			limit,
		)
	}
	if err != nil {
		// List has no error path in the interface; return empty on failure.
		return nil
	}
	defer rows.Close()

	out := make([]*Run, 0, 16)
	for rows.Next() {
		r, scanErr := scanRun(rows)
		if scanErr != nil {
			continue
		}
		out = append(out, r)
	}
	return out
}

// UpdateStatus transitions a run to a new status, setting StartedAt /
// FinishedAt automatically as appropriate.
func (s *SQLiteStore) UpdateStatus(id string, st Status) (*Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	r.Status = st
	switch st {
	case StatusRunning:
		if r.StartedAt == nil {
			t := now
			r.StartedAt = &t
		}
	case StatusSucceeded, StatusFailed, StatusCancelled:
		if r.FinishedAt == nil {
			t := now
			r.FinishedAt = &t
		}
	}
	startedAt := nullableTime(r.StartedAt)
	finishedAt := nullableTime(r.FinishedAt)
	if _, err := s.db.Exec(
		`UPDATE runs SET status = ?, started_at = ?, finished_at = ? WHERE id = ?`,
		r.Status, startedAt, finishedAt, r.ID,
	); err != nil {
		return nil, fmt.Errorf("update status: %w", err)
	}
	return r, nil
}

// UpdateProgress overwrites the progress block.
func (s *SQLiteStore) UpdateProgress(id string, p Progress) (*Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	r.Progress = p
	progJSON, _ := json.Marshal(p)
	if _, err := s.db.Exec(
		`UPDATE runs SET progress_json = ? WHERE id = ?`,
		string(progJSON), r.ID,
	); err != nil {
		return nil, fmt.Errorf("update progress: %w", err)
	}
	return r, nil
}

// SetSummary attaches the summary blob to a run.
func (s *SQLiteStore) SetSummary(id string, summary map[string]any) (*Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, err := s.Get(id)
	if err != nil {
		return nil, err
	}
	r.Summary = summary
	sumJSON, err := json.Marshal(summary)
	if err != nil {
		return nil, fmt.Errorf("marshal summary: %w", err)
	}
	if _, err := s.db.Exec(
		`UPDATE runs SET summary_json = ? WHERE id = ?`,
		string(sumJSON), r.ID,
	); err != nil {
		return nil, fmt.Errorf("update summary: %w", err)
	}
	return r, nil
}

// HasActive returns true if any run is in queued/running/cancelling.
func (s *SQLiteStore) HasActive() bool {
	active, err := s.hasActiveLocked()
	if err != nil {
		return false
	}
	return active
}

func (s *SQLiteStore) hasActiveLocked() (bool, error) {
	var n int
	err := s.db.QueryRow(
		`SELECT COUNT(*) FROM runs WHERE status IN (?, ?, ?)`,
		StatusQueued, StatusRunning, StatusCancelling,
	).Scan(&n)
	if err != nil {
		return false, fmt.Errorf("count active: %w", err)
	}
	return n > 0, nil
}

// rowScanner is the common subset of *sql.Row and *sql.Rows used by scanRun.
type rowScanner interface {
	Scan(dest ...any) error
}

// scanRun decodes a row from the runs table into a *Run, tolerating NULLs.
func scanRun(row rowScanner) (*Run, error) {
	var (
		id, name, status, createdAt string
		startedAt, finishedAt       sql.NullString
		cfgJSON, progJSON           string
		summaryJSON                 sql.NullString
	)
	if err := row.Scan(&id, &name, &status, &createdAt, &startedAt, &finishedAt, &cfgJSON, &progJSON, &summaryJSON); err != nil {
		return nil, err
	}
	r := &Run{
		ID:     id,
		Name:   name,
		Status: Status(status),
	}
	if t, err := time.Parse(time.RFC3339Nano, createdAt); err == nil {
		r.CreatedAt = t
	}
	if startedAt.Valid {
		if t, err := time.Parse(time.RFC3339Nano, startedAt.String); err == nil {
			r.StartedAt = &t
		}
	}
	if finishedAt.Valid {
		if t, err := time.Parse(time.RFC3339Nano, finishedAt.String); err == nil {
			r.FinishedAt = &t
		}
	}
	if cfgJSON != "" {
		var cfg config.Config
		if err := json.Unmarshal([]byte(cfgJSON), &cfg); err == nil {
			r.Config = &cfg
		}
	}
	if progJSON != "" {
		_ = json.Unmarshal([]byte(progJSON), &r.Progress)
	}
	if summaryJSON.Valid && summaryJSON.String != "" {
		_ = json.Unmarshal([]byte(summaryJSON.String), &r.Summary)
	}
	return r, nil
}

// nullableTime turns a *time.Time into a sql-compatible value: NULL when nil.
func nullableTime(t *time.Time) any {
	if t == nil {
		return nil
	}
	return t.UTC().Format(time.RFC3339Nano)
}
