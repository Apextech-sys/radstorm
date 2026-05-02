// Package handlers — tests.
//
// Purpose:
//
//	httptest-based coverage for every handler in the package: success path,
//	404 on missing run, 400 on invalid config, 409 on second concurrent run,
//	and a smoke check of the SSE stream end-to-end.
//
// Related files:
//   - all handlers in this package
//   - apps/api/internal/runs/store.go
//
// Briefing: .orchestration/briefings/1f-api-skeleton.md
//
// Contract: internal — exercises the on-the-wire shapes from rest-api.md.
package handlers

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Apextech-sys/reflex-radstorm/pkg/config"
	"github.com/Apextech-sys/reflex-radstorm/apps/api/internal/mockdata"
	"github.com/Apextech-sys/reflex-radstorm/apps/api/internal/runs"
)

// silentLogger discards everything; tests don't care about log output.
func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// validConfig returns a minimal config that passes Validate. It clones the
// first mockdata template and points its CredentialsFile at a temp CSV so the
// `file` validator (which os.Stat's the path) succeeds.
func validConfig() *config.Config {
	tmpl := mockdata.Templates()[0]
	cfg := *tmpl.Config // shallow copy is enough; we only mutate Subscribers
	tmp, err := os.CreateTemp("", "creds-*.csv")
	if err != nil {
		panic(err)
	}
	defer tmp.Close()
	if _, err := tmp.WriteString("username,password\nu1,p1\n"); err != nil {
		panic(err)
	}
	cfg.Subscribers.CredentialsFile = tmp.Name()
	return &cfg
}

// makeRunsHandler returns a RunsHandler with a fresh in-memory store and a
// drastically shortened fake-run duration so tests don't sleep for 2s.
func makeRunsHandler(t *testing.T) (*RunsHandler, *runs.MemoryStore) {
	t.Helper()
	prevDur := FakeRunDuration
	FakeRunDuration = 50 * time.Millisecond
	t.Cleanup(func() { FakeRunDuration = prevDur })
	store := runs.NewMemoryStore()
	return NewRunsHandler(store), store
}

func TestHealth(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	rec := httptest.NewRecorder()
	Health(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, "ok", body["status"])
	assert.Equal(t, Version, body["version"])
}

func TestTemplates(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/api/v1/scenarios/templates", nil)
	rec := httptest.NewRecorder()
	Templates(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var list []map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &list))
	require.GreaterOrEqual(t, len(list), 3, "must have at least the 3 required templates")
	ids := make(map[string]bool)
	for _, tmpl := range list {
		ids[tmpl["id"].(string)] = true
	}
	assert.True(t, ids["smoke-100"])
	assert.True(t, ids["cold-start-1k"])
	assert.True(t, ids["uniform-1k"])
}

func TestCreateRun_ValidConfig(t *testing.T) {
	h, store := makeRunsHandler(t)
	body, err := json.Marshal(map[string]any{
		"name":   "unit-test",
		"config": validConfig(),
	})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code)
	var out runs.Run
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	assert.NotEmpty(t, out.ID)
	assert.Equal(t, runs.StatusQueued, out.Status)
	assert.Equal(t, "unit-test", out.Name)

	// Verify persisted to store.
	got, err := store.Get(out.ID)
	require.NoError(t, err)
	assert.Equal(t, out.ID, got.ID)
}

func TestCreateRun_InvalidConfig_400(t *testing.T) {
	h, _ := makeRunsHandler(t)
	body, err := json.Marshal(map[string]any{
		"config": &config.Config{}, // missing required fields
	})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	assert.Equal(t, http.StatusBadRequest, rec.Code)
	var env ErrorBody
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &env))
	assert.NotEmpty(t, env.Error)
	require.NotNil(t, env.Details)
}

func TestCreateRun_MissingConfig_400(t *testing.T) {
	h, _ := makeRunsHandler(t)
	body := []byte(`{"name":"no-config"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestCreateRun_BadJSON_400(t *testing.T) {
	h, _ := makeRunsHandler(t)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs", strings.NewReader("{not-json"))
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestCreateRun_Conflict_409(t *testing.T) {
	h, _ := makeRunsHandler(t)
	// First create succeeds.
	body, _ := json.Marshal(map[string]any{"config": validConfig()})
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/runs", bytes.NewReader(body))
	rec1 := httptest.NewRecorder()
	h.Create(rec1, req1)
	require.Equal(t, http.StatusCreated, rec1.Code)

	// Second create immediately must conflict.
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/runs", bytes.NewReader(body))
	rec2 := httptest.NewRecorder()
	h.Create(rec2, req2)
	assert.Equal(t, http.StatusConflict, rec2.Code)
}

func TestListRuns(t *testing.T) {
	h, store := makeRunsHandler(t)
	// Seed three runs directly via the store; flip statuses so HasActive
	// doesn't reject the next Create.
	for i := 0; i < 3; i++ {
		r, err := store.Create("", validConfig())
		require.NoError(t, err)
		_, _ = store.UpdateStatus(r.ID, runs.StatusSucceeded)
	}

	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var body struct {
		Runs []runs.Run `json:"runs"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Len(t, body.Runs, 3)
}

func TestGetRun_OK(t *testing.T) {
	store := runs.NewMemoryStore()
	r, err := store.Create("", validConfig())
	require.NoError(t, err)

	router := newTestRouter(store)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/"+r.ID, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var out runs.Run
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	assert.Equal(t, r.ID, out.ID)
}

func TestGetRun_404(t *testing.T) {
	router := newTestRouter(runs.NewMemoryStore())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/does-not-exist", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestCancelRun_Flow(t *testing.T) {
	prevDur := FakeRunDuration
	FakeRunDuration = 5 * time.Second
	t.Cleanup(func() { FakeRunDuration = prevDur })

	store := runs.NewMemoryStore()
	r, err := store.Create("", validConfig())
	require.NoError(t, err)

	router := newTestRouter(store)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs/"+r.ID+"/cancel", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, string(runs.StatusCancelling), body["status"])
}

func TestResults_Endpoints(t *testing.T) {
	store := runs.NewMemoryStore()
	r, err := store.Create("", validConfig())
	require.NoError(t, err)

	router := newTestRouter(store)

	cases := []struct {
		name string
		url  string
		key  string // top-level key we expect to see
	}{
		{"summary", "/api/v1/runs/" + r.ID + "/results", "subscribers"},
		{"establishment", "/api/v1/runs/" + r.ID + "/results/establishment-curve", "points"},
		{"latency", "/api/v1/runs/" + r.ID + "/results/latency-histogram", "buckets"},
		{"artifacts", "/api/v1/runs/" + r.ID + "/artifacts", "artifacts"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, tc.url, nil)
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			assert.Equal(t, http.StatusOK, rec.Code, "url=%s body=%s", tc.url, rec.Body.String())
			var body map[string]any
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			assert.Contains(t, body, tc.key)
		})
	}
}

func TestSSE_Stream_EmitsProgressAndComplete(t *testing.T) {
	prev := SSEStepInterval
	SSEStepInterval = 5 * time.Millisecond
	t.Cleanup(func() { SSEStepInterval = prev })

	store := runs.NewMemoryStore()
	r, err := store.Create("", validConfig())
	require.NoError(t, err)

	srv := httptest.NewServer(newTestRouter(store))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/api/v1/runs/" + r.ID + "/events")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	var events []string
	var sawComplete bool
	deadline := time.After(5 * time.Second)
	done := make(chan struct{})
	go func() {
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "event: ") {
				ev := strings.TrimPrefix(line, "event: ")
				events = append(events, ev)
				if ev == "complete" {
					// Read one more line (data) then we're done.
					if scanner.Scan() {
						sawComplete = true
					}
					close(done)
					return
				}
			}
		}
		close(done)
	}()
	select {
	case <-done:
	case <-deadline:
		t.Fatal("SSE stream did not complete within deadline")
	}
	assert.True(t, sawComplete, "expected to see complete event")
	// Should have at least one progress event before complete.
	var sawProgress bool
	for _, ev := range events {
		if ev == "progress" {
			sawProgress = true
			break
		}
	}
	assert.True(t, sawProgress, "expected at least one progress event")
}

func TestSSE_Stream_404OnMissingRun(t *testing.T) {
	router := newTestRouter(runs.NewMemoryStore())
	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/missing/events", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestArtifact_404(t *testing.T) {
	store := runs.NewMemoryStore()
	r, err := store.Create("", validConfig())
	require.NoError(t, err)
	router := newTestRouter(store)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/"+r.ID+"/artifacts/summary.json", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// TestCreateRun_FakeStateMachine_Succeeds drives a run end-to-end through
// the fake state machine and asserts it lands on succeeded with a summary.
func TestCreateRun_FakeStateMachine_Succeeds(t *testing.T) {
	prev := FakeRunDuration
	FakeRunDuration = 200 * time.Millisecond
	t.Cleanup(func() { FakeRunDuration = prev })

	h, store := func() (*RunsHandler, *runs.MemoryStore) {
		s := runs.NewMemoryStore()
		return NewRunsHandler(s), s
	}()

	body, _ := json.Marshal(map[string]any{"config": validConfig()})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code)

	var created runs.Run
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))

	// Wait for the fake state machine to land on succeeded.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		got, err := store.Get(created.ID)
		require.NoError(t, err)
		if got.Status == runs.StatusSucceeded {
			require.NotNil(t, got.Summary)
			assert.Equal(t, "succeeded", got.Summary["outcome"])
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
	t.Fatalf("run did not reach succeeded in time")
}

// TestCancel_AfterCreate_StopsFakeStateMachine verifies cancellation flips
// status and the fake state machine respects it.
func TestCancel_AfterCreate_StopsFakeStateMachine(t *testing.T) {
	prev := FakeRunDuration
	FakeRunDuration = 1 * time.Second
	t.Cleanup(func() { FakeRunDuration = prev })

	store := runs.NewMemoryStore()
	h := NewRunsHandler(store)

	body, _ := json.Marshal(map[string]any{"config": validConfig()})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code)

	var created runs.Run
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))

	// Cancel immediately via the router (so URL params are populated).
	router := newTestRouter(store)
	creq := httptest.NewRequest(http.MethodPost, "/api/v1/runs/"+created.ID+"/cancel", nil)
	crec := httptest.NewRecorder()
	router.ServeHTTP(crec, creq)
	assert.Equal(t, http.StatusOK, crec.Code)

	// Eventually the run should be cancelled, not succeeded.
	deadline := time.Now().Add(3 * time.Second)
	var final runs.Status
	for time.Now().Before(deadline) {
		got, _ := store.Get(created.ID)
		final = got.Status
		if final == runs.StatusCancelled {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	assert.Equal(t, runs.StatusCancelled, final)
}

// TestCancel_AlreadyFinished_409 verifies the handler rejects cancel on a
// run that has already finished.
func TestCancel_AlreadyFinished_409(t *testing.T) {
	store := runs.NewMemoryStore()
	r, err := store.Create("", validConfig())
	require.NoError(t, err)
	_, err = store.UpdateStatus(r.ID, runs.StatusSucceeded)
	require.NoError(t, err)

	router := newTestRouter(store)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs/"+r.ID+"/cancel", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusConflict, rec.Code)
}

// TestCancel_NotFound_404 verifies cancel on an unknown run id is a 404.
func TestCancel_NotFound_404(t *testing.T) {
	router := newTestRouter(runs.NewMemoryStore())
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs/missing/cancel", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// TestResults_NotFound_404 verifies results endpoints 404 for unknown runs.
func TestResults_NotFound_404(t *testing.T) {
	router := newTestRouter(runs.NewMemoryStore())
	urls := []string{
		"/api/v1/runs/missing/results",
		"/api/v1/runs/missing/results/establishment-curve",
		"/api/v1/runs/missing/results/latency-histogram",
		"/api/v1/runs/missing/artifacts",
		"/api/v1/runs/missing/artifacts/foo",
	}
	for _, u := range urls {
		req := httptest.NewRequest(http.MethodGet, u, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusNotFound, rec.Code, u)
	}
}

// TestList_RespectsLimitAndStatusFilter exercises the limit / status query
// params on GET /runs.
func TestList_RespectsLimitAndStatusFilter(t *testing.T) {
	store := runs.NewMemoryStore()
	for i := 0; i < 4; i++ {
		r, err := store.Create("", validConfig())
		require.NoError(t, err)
		_, _ = store.UpdateStatus(r.ID, runs.StatusFailed)
	}
	h := NewRunsHandler(store)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs?limit=2&status=failed", nil)
	rec := httptest.NewRecorder()
	h.List(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var body struct {
		Runs []runs.Run `json:"runs"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Len(t, body.Runs, 2)
	for _, r := range body.Runs {
		assert.Equal(t, runs.StatusFailed, r.Status)
	}
}

// _ keeps silentLogger reachable in case future tests need it.
var _ = silentLogger
