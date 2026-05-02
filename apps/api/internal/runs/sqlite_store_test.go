// Package runs — tests for the SQLite-backed Store implementation.
//
// Purpose:
//
//	Verifies parity with MemoryStore (Create / Get / List / UpdateStatus /
//	UpdateProgress / SetSummary / HasActive / ErrConflict) plus the two
//	SQLite-specific behaviours: persistence across reopens and orphan
//	reaping at startup.
//
// Related files:
//   - apps/api/internal/runs/sqlite_store.go
//   - apps/api/internal/runs/store.go
//
// Briefing: .orchestration/briefings/3b-api-full.md
//
// Contract: internal.
package runs

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newTempSQLiteStore opens a fresh on-disk SQLite store under t.TempDir and
// registers a Close cleanup. Returns the store and the on-disk db path so
// tests that exercise reopen semantics can build a second store on the same
// file.
func newTempSQLiteStore(t *testing.T) (*SQLiteStore, string) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "runs.db")
	s, err := NewSQLiteStore(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s.Close() })
	return s, path
}

func TestSQLiteStore_CreateAndGet(t *testing.T) {
	s, _ := newTempSQLiteStore(t)
	r, err := s.Create("hello", minimalConfig())
	require.NoError(t, err)
	require.NotNil(t, r)
	assert.NotEmpty(t, r.ID)
	assert.Equal(t, "hello", r.Name)
	assert.Equal(t, StatusQueued, r.Status)
	assert.Equal(t, 42, r.Progress.SubscribersTotal)

	got, err := s.Get(r.ID)
	require.NoError(t, err)
	assert.Equal(t, r.ID, got.ID)
	assert.Equal(t, r.Name, got.Name)
}

func TestSQLiteStore_Get_NotFound(t *testing.T) {
	s, _ := newTempSQLiteStore(t)
	_, err := s.Get("does-not-exist")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestSQLiteStore_Conflict(t *testing.T) {
	s, _ := newTempSQLiteStore(t)
	_, err := s.Create("", minimalConfig())
	require.NoError(t, err)
	_, err = s.Create("", minimalConfig())
	assert.ErrorIs(t, err, ErrConflict)
}

func TestSQLiteStore_HasActive_Transitions(t *testing.T) {
	s, _ := newTempSQLiteStore(t)
	r, err := s.Create("", minimalConfig())
	require.NoError(t, err)
	assert.True(t, s.HasActive())

	_, err = s.UpdateStatus(r.ID, StatusSucceeded)
	require.NoError(t, err)
	assert.False(t, s.HasActive())
}

func TestSQLiteStore_UpdateStatus_SetsTimestamps(t *testing.T) {
	s, _ := newTempSQLiteStore(t)
	r, err := s.Create("", minimalConfig())
	require.NoError(t, err)
	require.Nil(t, r.StartedAt)

	r2, err := s.UpdateStatus(r.ID, StatusRunning)
	require.NoError(t, err)
	require.NotNil(t, r2.StartedAt)
	assert.Nil(t, r2.FinishedAt)

	r3, err := s.UpdateStatus(r.ID, StatusSucceeded)
	require.NoError(t, err)
	require.NotNil(t, r3.FinishedAt)
	require.NotNil(t, r3.StartedAt, "StartedAt should survive across UpdateStatus")
}

func TestSQLiteStore_List_RespectsLimitAndStatus(t *testing.T) {
	s, _ := newTempSQLiteStore(t)
	for i := 0; i < 5; i++ {
		r, err := s.Create("", minimalConfig())
		require.NoError(t, err)
		_, _ = s.UpdateStatus(r.ID, StatusSucceeded)
	}
	all := s.List(0, "")
	assert.Len(t, all, 5)

	limited := s.List(2, "")
	assert.Len(t, limited, 2)

	none := s.List(0, "queued")
	assert.Len(t, none, 0)

	succeeded := s.List(0, "succeeded")
	assert.Len(t, succeeded, 5)
}

func TestSQLiteStore_UpdateProgressAndSummary(t *testing.T) {
	s, _ := newTempSQLiteStore(t)
	r, err := s.Create("", minimalConfig())
	require.NoError(t, err)

	_, err = s.UpdateProgress(r.ID, Progress{ElapsedMs: 1234, SubscribersActivated: 5})
	require.NoError(t, err)

	got, err := s.Get(r.ID)
	require.NoError(t, err)
	assert.Equal(t, int64(1234), got.Progress.ElapsedMs)
	assert.Equal(t, 5, got.Progress.SubscribersActivated)

	_, err = s.SetSummary(r.ID, map[string]any{"outcome": "succeeded"})
	require.NoError(t, err)
	got, _ = s.Get(r.ID)
	assert.Equal(t, "succeeded", got.Summary["outcome"])
}

func TestSQLiteStore_UpdateStatus_NotFound(t *testing.T) {
	s, _ := newTempSQLiteStore(t)
	_, err := s.UpdateStatus("nope", StatusRunning)
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestSQLiteStore_UpdateProgress_NotFound(t *testing.T) {
	s, _ := newTempSQLiteStore(t)
	_, err := s.UpdateProgress("nope", Progress{})
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestSQLiteStore_SetSummary_NotFound(t *testing.T) {
	s, _ := newTempSQLiteStore(t)
	_, err := s.SetSummary("nope", map[string]any{"k": "v"})
	assert.ErrorIs(t, err, ErrNotFound)
}

// TestSQLiteStore_PersistsAcrossReopen seeds a row, closes the store, then
// reopens the same db file and verifies the row is still there.
func TestSQLiteStore_PersistsAcrossReopen(t *testing.T) {
	s, path := newTempSQLiteStore(t)
	r, err := s.Create("", minimalConfig())
	require.NoError(t, err)
	id := r.ID
	// Mark succeeded so reopen doesn't reap it as orphaned.
	_, err = s.UpdateStatus(id, StatusSucceeded)
	require.NoError(t, err)
	require.NoError(t, s.Close())

	s2, err := NewSQLiteStore(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s2.Close() })

	got, err := s2.Get(id)
	require.NoError(t, err)
	assert.Equal(t, id, got.ID)
	assert.Equal(t, StatusSucceeded, got.Status)
}

// TestSQLiteStore_ReapsOrphansOnReopen verifies the startup orphan reaper
// flips queued/running/cancelling rows from a prior crash to failed.
func TestSQLiteStore_ReapsOrphansOnReopen(t *testing.T) {
	s, path := newTempSQLiteStore(t)

	// Create three runs in active states then close without finishing them.
	queued, err := s.Create("", minimalConfig())
	require.NoError(t, err)
	require.NoError(t, s.Close())

	// Force the prior run row to look orphaned (queued is already a no-op
	// for the conflict check on the next open). Flip to running directly
	// via a fresh handle so the reaper sees the row.
	s2, err := NewSQLiteStore(path)
	require.NoError(t, err)
	got, err := s2.Get(queued.ID)
	require.NoError(t, err)
	// After first reopen the row was marked failed by the reaper. Verify.
	assert.Equal(t, StatusFailed, got.Status, "queued orphan should be reaped to failed")
	require.NoError(t, s2.Close())

	// Now create a fresh row, mark it running, and reopen — the reaper
	// should flip it to failed too.
	s3, err := NewSQLiteStore(path)
	require.NoError(t, err)
	r2, err := s3.Create("", minimalConfig())
	require.NoError(t, err)
	_, err = s3.UpdateStatus(r2.ID, StatusRunning)
	require.NoError(t, err)
	require.NoError(t, s3.Close())

	s4, err := NewSQLiteStore(path)
	require.NoError(t, err)
	t.Cleanup(func() { _ = s4.Close() })
	got2, err := s4.Get(r2.ID)
	require.NoError(t, err)
	assert.Equal(t, StatusFailed, got2.Status, "running orphan should be reaped to failed")
	require.NotNil(t, got2.FinishedAt, "reaped orphan should have a FinishedAt timestamp")
}
