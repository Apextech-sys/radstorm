// Package runs — tests for the in-memory store.
//
// Purpose:
//
//	Verifies Create/Get/List/UpdateStatus/UpdateProgress/SetSummary plus
//	HasActive/conflict semantics. SQLite parity tests will live in their
//	own file once the SQLite impl lands in Wave 3.
//
// Related files:
//   - apps/api/internal/runs/store.go
//
// Briefing: .orchestration/briefings/1f-api-skeleton.md
//
// Contract: internal.
package runs

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Apextech-sys/reflex-radstorm/pkg/config"
)

func minimalConfig() *config.Config {
	return &config.Config{
		Subscribers: config.Subscribers{Count: 42},
	}
}

func TestMemoryStore_CreateAndGet(t *testing.T) {
	s := NewMemoryStore()
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
}

func TestMemoryStore_Get_NotFound(t *testing.T) {
	s := NewMemoryStore()
	_, err := s.Get("nope")
	assert.ErrorIs(t, err, ErrNotFound)
}

func TestMemoryStore_Conflict(t *testing.T) {
	s := NewMemoryStore()
	_, err := s.Create("", minimalConfig())
	require.NoError(t, err)
	_, err = s.Create("", minimalConfig())
	assert.ErrorIs(t, err, ErrConflict)
}

func TestMemoryStore_HasActive_Transitions(t *testing.T) {
	s := NewMemoryStore()
	r, err := s.Create("", minimalConfig())
	require.NoError(t, err)
	assert.True(t, s.HasActive())

	_, err = s.UpdateStatus(r.ID, StatusSucceeded)
	require.NoError(t, err)
	assert.False(t, s.HasActive())
}

func TestMemoryStore_UpdateStatus_SetsTimestamps(t *testing.T) {
	s := NewMemoryStore()
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
}

func TestMemoryStore_List_RespectsLimitAndStatus(t *testing.T) {
	s := NewMemoryStore()
	for i := 0; i < 5; i++ {
		r, err := s.Create("", minimalConfig())
		require.NoError(t, err)
		_, _ = s.UpdateStatus(r.ID, StatusSucceeded)
	}
	all := s.List(0, "")
	assert.Len(t, all, 5)

	limited := s.List(2, "")
	assert.Len(t, limited, 2)

	// Filter by status that none have.
	none := s.List(0, "queued")
	assert.Len(t, none, 0)

	succeeded := s.List(0, "succeeded")
	assert.Len(t, succeeded, 5)
}

func TestMemoryStore_UpdateProgressAndSummary(t *testing.T) {
	s := NewMemoryStore()
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

func TestMemoryStore_UpdateStatus_NotFound(t *testing.T) {
	s := NewMemoryStore()
	_, err := s.UpdateStatus("nope", StatusRunning)
	assert.ErrorIs(t, err, ErrNotFound)
}
