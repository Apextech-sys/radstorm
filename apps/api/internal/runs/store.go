// Package runs — run lifecycle store.
//
// Purpose:
//
//	Defines the Run record and a Store interface for persisting/listing runs.
//	Provides an in-memory implementation used by the Wave 1 API skeleton; a
//	SQLite-backed implementation will replace it in Wave 3 without changing
//	the interface or the handlers.
//
// Related files:
//   - apps/api/internal/server/handlers/runs.go (consumer)
//   - .orchestration/contracts/rest-api.md (Run shape on the wire)
//   - .orchestration/contracts/results-schema.md (Summary shape)
//
// Briefing: .orchestration/briefings/1f-api-skeleton.md
//
// Contract: internal Go interface — handlers depend on Store, not on a
// concrete impl, so swapping to SQLite later is a one-line wiring change.
package runs

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/Apextech-sys/reflex-radstorm/apps/api/internal/config"
)

// Status enumerates the run lifecycle states defined in rest-api.md.
type Status string

const (
	StatusQueued     Status = "queued"
	StatusRunning    Status = "running"
	StatusSucceeded  Status = "succeeded"
	StatusFailed     Status = "failed"
	StatusCancelling Status = "cancelling"
	StatusCancelled  Status = "cancelled"
)

// Progress mirrors the `progress` block in GET /runs/{id}.
type Progress struct {
	ElapsedMs               int64 `json:"elapsed_ms"`
	SubscribersTotal        int   `json:"subscribers_total"`
	SubscribersActivated    int   `json:"subscribers_activated"`
	SubscribersEstablished  int   `json:"subscribers_established"`
	SubscribersFailed       int   `json:"subscribers_failed"`
}

// Run is the in-memory representation of a run; it serialises directly to the
// JSON shape documented in rest-api.md.
type Run struct {
	ID         string                 `json:"id"`
	Name       string                 `json:"name"`
	Status     Status                 `json:"status"`
	CreatedAt  time.Time              `json:"created_at"`
	StartedAt  *time.Time             `json:"started_at,omitempty"`
	FinishedAt *time.Time             `json:"finished_at,omitempty"`
	Config     *config.Config         `json:"config"`
	Progress   Progress               `json:"progress"`
	Summary    map[string]any         `json:"summary,omitempty"`
}

// ErrNotFound is returned when a run id does not exist in the store.
var ErrNotFound = errors.New("run not found")

// ErrConflict is returned when a new run is rejected because another run is
// already in flight (single-tenant API per rest-api.md §implementation notes).
var ErrConflict = errors.New("another run is already in progress")

// Store is the abstraction handlers depend on. Both the in-memory impl below
// and the future SQLite impl satisfy this interface.
type Store interface {
	Create(name string, cfg *config.Config) (*Run, error)
	Get(id string) (*Run, error)
	List(limit int, status string) []*Run
	UpdateStatus(id string, s Status) (*Run, error)
	UpdateProgress(id string, p Progress) (*Run, error)
	SetSummary(id string, summary map[string]any) (*Run, error)
	HasActive() bool
}

// MemoryStore is a goroutine-safe in-memory implementation.
type MemoryStore struct {
	mu   sync.RWMutex
	runs map[string]*Run
	// preserves creation order for predictable List output
	order []string
}

// NewMemoryStore returns an empty MemoryStore.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{runs: make(map[string]*Run)}
}

// Create allocates a new Run with a generated id and stores it.
func (s *MemoryStore) Create(name string, cfg *config.Config) (*Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.hasActiveLocked() {
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
	s.runs[id] = r
	s.order = append(s.order, id)
	return r, nil
}

// Get returns the run with the given id, or ErrNotFound.
func (s *MemoryStore) Get(id string) (*Run, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	r, ok := s.runs[id]
	if !ok {
		return nil, ErrNotFound
	}
	return r, nil
}

// List returns up to `limit` runs, most-recent-first, optionally filtered by
// status. limit <= 0 means unlimited.
func (s *MemoryStore) List(limit int, status string) []*Run {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]*Run, 0, len(s.order))
	for i := len(s.order) - 1; i >= 0; i-- {
		r := s.runs[s.order[i]]
		if r == nil {
			continue
		}
		if status != "" && string(r.Status) != status {
			continue
		}
		out = append(out, r)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	// Stable order — already most-recent-first via reverse iteration; sort by
	// CreatedAt desc as a defensive fallback.
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	return out
}

// UpdateStatus transitions a run to a new status, setting StartedAt /
// FinishedAt automatically as appropriate.
func (s *MemoryStore) UpdateStatus(id string, st Status) (*Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.runs[id]
	if !ok {
		return nil, ErrNotFound
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
	return r, nil
}

// UpdateProgress overwrites the run's progress block.
func (s *MemoryStore) UpdateProgress(id string, p Progress) (*Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.runs[id]
	if !ok {
		return nil, ErrNotFound
	}
	r.Progress = p
	return r, nil
}

// SetSummary attaches the summary blob to a finished run.
func (s *MemoryStore) SetSummary(id string, summary map[string]any) (*Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	r, ok := s.runs[id]
	if !ok {
		return nil, ErrNotFound
	}
	r.Summary = summary
	return r, nil
}

// HasActive returns true if there is a run in queued/running/cancelling.
func (s *MemoryStore) HasActive() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.hasActiveLocked()
}

func (s *MemoryStore) hasActiveLocked() bool {
	for _, r := range s.runs {
		switch r.Status {
		case StatusQueued, StatusRunning, StatusCancelling:
			return true
		}
	}
	return false
}

// generateID builds a run identifier of the shape used in rest-api.md
// examples: "run-2026-05-02T01-23-45Z-7f3a".
func generateID(now time.Time) string {
	var buf [2]byte
	_, _ = rand.Read(buf[:])
	suffix := hex.EncodeToString(buf[:])
	return fmt.Sprintf("run-%s-%s", now.Format("2006-01-02T15-04-05Z"), suffix)
}

func subscriberCount(c *config.Config) int {
	if c == nil {
		return 0
	}
	return c.Subscribers.Count
}
