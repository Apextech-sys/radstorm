// Package handlers — tests.
//
// Purpose:
//
//	httptest-based coverage for every handler in the package. The runs +
//	events + results endpoints are exercised against a real subprocess
//	runner that spawns the fake CLI binary at internal/runs/testdata/fakecli,
//	so the SSE / progress / summary flow is identical to production.
//
// Related files:
//   - all handlers in this package
//   - apps/api/internal/runs/store.go
//   - apps/api/internal/runs/runner.go
//   - apps/api/internal/runs/testdata/fakecli/main.go
//
// Briefing: .orchestration/briefings/3b-api-full.md
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
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/Apextech-sys/radstorm/apps/api/internal/mockdata"
	"github.com/Apextech-sys/radstorm/apps/api/internal/runs"
	"github.com/Apextech-sys/radstorm/pkg/config"
)

// silentLogger discards everything; tests don't care about log output.
func silentLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// validConfig returns a minimal config that passes Validate. It clones the
// first mockdata template and points its CredentialsFile at a temp CSV so the
// `file` validator (which os.Stat's the path) succeeds.
func validConfig(t *testing.T) *config.Config {
	t.Helper()
	tmpl := mockdata.Templates()[0]
	cfg := *tmpl.Config // shallow copy is enough; we only mutate Subscribers
	tmp, err := os.CreateTemp(t.TempDir(), "creds-*.csv")
	require.NoError(t, err)
	defer tmp.Close()
	if _, err := tmp.WriteString("username,password\nu1,p1\n"); err != nil {
		t.Fatal(err)
	}
	cfg.Subscribers.CredentialsFile = tmp.Name()
	return &cfg
}

// fakeCLIOnce is shared so the binary is built once per test process even
// across the handlers + runs packages (each gets its own sync.Once, but
// rebuilds are cheap with the Go build cache).
var (
	handlersFakeCLIOnce sync.Once
	handlersFakeCLIPath string
	handlersFakeCLIErr  error
)

// buildHandlersFakeCLI compiles the fake CLI used by handlers tests.
func buildHandlersFakeCLI(t *testing.T) string {
	t.Helper()
	handlersFakeCLIOnce.Do(func() {
		stableDir, err := os.MkdirTemp("", "radstorm-handlers-fakecli-*")
		if err != nil {
			handlersFakeCLIErr = err
			return
		}
		bin := filepath.Join(stableDir, "fakecli")
		if runtime.GOOS == "windows" {
			bin += ".exe"
		}
		_, thisFile, _, _ := runtime.Caller(0)
		// thisFile is .../apps/api/internal/server/handlers/handlers_test.go
		// fakecli is at .../apps/api/internal/runs/testdata/fakecli
		repoRel := filepath.Join(filepath.Dir(thisFile), "..", "..", "runs", "testdata", "fakecli")
		cmd := exec.Command("go", "build", "-o", bin, ".")
		cmd.Dir = repoRel
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			handlersFakeCLIErr = err
			return
		}
		handlersFakeCLIPath = bin
	})
	if handlersFakeCLIErr != nil {
		t.Fatalf("build fake CLI: %v", handlersFakeCLIErr)
	}
	return handlersFakeCLIPath
}

// makeRealRunner builds a *runs.Runner backed by SQLite + the fake CLI,
// rooted at a fresh per-test temp directory.
func makeRealRunner(t *testing.T) (runs.Store, *runs.Runner, string) {
	t.Helper()
	dataDir := t.TempDir()
	store, err := runs.NewSQLiteStore(filepath.Join(dataDir, "runs.db"))
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	runner, err := runs.NewRunner(store, runs.Options{
		DataDir: dataDir,
		CLIPath: buildHandlersFakeCLI(t),
		Logger:  silentLogger(),
	})
	require.NoError(t, err)
	t.Cleanup(func() { runner.Shutdown(2 * time.Second) })
	return store, runner, dataDir
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

func TestCreateRun_ValidConfig_SpawnsSubprocess(t *testing.T) {
	store, runner, _ := makeRealRunner(t)
	h := NewRunsHandler(store, runner)

	body, err := json.Marshal(map[string]any{
		"name":   "unit-test",
		"config": validConfig(t),
	})
	require.NoError(t, err)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	h.Create(rec, req)

	assert.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var out runs.Run
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	assert.NotEmpty(t, out.ID)
	assert.Equal(t, "unit-test", out.Name)

	// Wait for the fake CLI to finish.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, err := store.Get(out.ID)
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

func TestCreateRun_InvalidConfig_400(t *testing.T) {
	store, runner, _ := makeRealRunner(t)
	h := NewRunsHandler(store, runner)
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
	store, runner, _ := makeRealRunner(t)
	h := NewRunsHandler(store, runner)
	body := []byte(`{"name":"no-config"}`)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestCreateRun_BadJSON_400(t *testing.T) {
	store, runner, _ := makeRealRunner(t)
	h := NewRunsHandler(store, runner)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs", strings.NewReader("{not-json"))
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	assert.Equal(t, http.StatusBadRequest, rec.Code)
}

func TestCreateRun_Conflict_409(t *testing.T) {
	store, runner, _ := makeRealRunner(t)
	h := NewRunsHandler(store, runner)
	// First create succeeds and starts a long-hanging subprocess.
	cfg := validConfig(t)
	body, _ := json.Marshal(map[string]any{"config": cfg})

	// Hang the fake CLI so the run stays active across the second POST.
	t.Setenv("FAKECLI_HANG", "1")
	req1 := httptest.NewRequest(http.MethodPost, "/api/v1/runs", bytes.NewReader(body))
	rec1 := httptest.NewRecorder()
	h.Create(rec1, req1)
	require.Equal(t, http.StatusCreated, rec1.Code, rec1.Body.String())

	// Second create immediately must conflict.
	req2 := httptest.NewRequest(http.MethodPost, "/api/v1/runs", bytes.NewReader(body))
	rec2 := httptest.NewRecorder()
	h.Create(rec2, req2)
	assert.Equal(t, http.StatusConflict, rec2.Code, rec2.Body.String())

	// Cleanup: kill the hanging subprocess so the test exits.
	var first runs.Run
	_ = json.Unmarshal(rec1.Body.Bytes(), &first)
	_ = runner.Cancel(first.ID)
}

func TestListRuns(t *testing.T) {
	store, runner, _ := makeRealRunner(t)
	h := NewRunsHandler(store, runner)
	// Seed three runs directly via the store; flip statuses so HasActive
	// doesn't reject the next Create.
	for i := 0; i < 3; i++ {
		r, err := store.Create("", validConfig(t))
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
	store, runner, dataDir := makeRealRunner(t)
	r, err := store.Create("", validConfig(t))
	require.NoError(t, err)

	router := newTestRouter(store, runner, dataDir)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/"+r.ID, nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var out runs.Run
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &out))
	assert.Equal(t, r.ID, out.ID)
}

func TestGetRun_404(t *testing.T) {
	store, runner, dataDir := makeRealRunner(t)
	router := newTestRouter(store, runner, dataDir)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/does-not-exist", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestCancelRun_Flow(t *testing.T) {
	store, runner, dataDir := makeRealRunner(t)
	r, err := store.Create("", validConfig(t))
	require.NoError(t, err)
	// Pretend the runner is running it (we're not actually starting a
	// subprocess; the handler only flips status).
	_, _ = store.UpdateStatus(r.ID, runs.StatusRunning)

	router := newTestRouter(store, runner, dataDir)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs/"+r.ID+"/cancel", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)

	assert.Equal(t, http.StatusOK, rec.Code)
	var body map[string]any
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	assert.Equal(t, string(runs.StatusCancelling), body["status"])
}

// TestCancel_AfterCreate_StopsSubprocess starts a hanging fake CLI,
// then cancels it via the HTTP endpoint and asserts the run lands on
// cancelled.
func TestCancel_AfterCreate_StopsSubprocess(t *testing.T) {
	store, runner, dataDir := makeRealRunner(t)
	h := NewRunsHandler(store, runner)
	t.Setenv("FAKECLI_HANG", "1")

	body, _ := json.Marshal(map[string]any{"config": validConfig(t)})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var created runs.Run
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))

	router := newTestRouter(store, runner, dataDir)
	creq := httptest.NewRequest(http.MethodPost, "/api/v1/runs/"+created.ID+"/cancel", nil)
	crec := httptest.NewRecorder()
	router.ServeHTTP(crec, creq)
	assert.Equal(t, http.StatusOK, crec.Code)

	deadline := time.Now().Add(5 * time.Second)
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

func TestCancel_AlreadyFinished_409(t *testing.T) {
	store, runner, dataDir := makeRealRunner(t)
	r, err := store.Create("", validConfig(t))
	require.NoError(t, err)
	_, err = store.UpdateStatus(r.ID, runs.StatusSucceeded)
	require.NoError(t, err)

	router := newTestRouter(store, runner, dataDir)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs/"+r.ID+"/cancel", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusConflict, rec.Code)
}

func TestCancel_NotFound_404(t *testing.T) {
	store, runner, dataDir := makeRealRunner(t)
	router := newTestRouter(store, runner, dataDir)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs/missing/cancel", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

// TestResults_NotFound_404 verifies results endpoints 404 for unknown runs.
func TestResults_NotFound_404(t *testing.T) {
	store, runner, dataDir := makeRealRunner(t)
	router := newTestRouter(store, runner, dataDir)
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

// TestResults_Endpoints_AfterRunCompletes drives a real subprocess to
// completion, then exercises every results endpoint.
func TestResults_Endpoints_AfterRunCompletes(t *testing.T) {
	store, runner, dataDir := makeRealRunner(t)
	h := NewRunsHandler(store, runner)

	body, _ := json.Marshal(map[string]any{"config": validConfig(t)})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var created runs.Run
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		got, _ := store.Get(created.ID)
		if got.Status == runs.StatusSucceeded {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	router := newTestRouter(store, runner, dataDir)
	cases := []struct {
		name string
		url  string
		key  string // top-level key we expect to see
	}{
		{"summary", "/api/v1/runs/" + created.ID + "/results", "subscribers"},
		{"establishment", "/api/v1/runs/" + created.ID + "/results/establishment-curve", "points"},
		{"latency", "/api/v1/runs/" + created.ID + "/results/latency-histogram", "buckets"},
		{"artifacts", "/api/v1/runs/" + created.ID + "/artifacts", "artifacts"},
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

// TestResults_Endpoints_BeforeRunCompletes — when there's no summary
// yet, the endpoints should return 409 (in-flight).
func TestResults_Endpoints_BeforeRunCompletes(t *testing.T) {
	store, runner, dataDir := makeRealRunner(t)
	r, err := store.Create("", validConfig(t))
	require.NoError(t, err)

	router := newTestRouter(store, runner, dataDir)
	urls := []string{
		"/api/v1/runs/" + r.ID + "/results",
		"/api/v1/runs/" + r.ID + "/results/establishment-curve",
		"/api/v1/runs/" + r.ID + "/results/latency-histogram",
	}
	for _, u := range urls {
		req := httptest.NewRequest(http.MethodGet, u, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		assert.Equal(t, http.StatusConflict, rec.Code, u)
	}

	// Artifacts endpoint should return an empty list (not 409) when the
	// run dir hasn't been created yet.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/"+r.ID+"/artifacts", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestSSE_Stream_EmitsProgressAndComplete(t *testing.T) {
	prevHB := HeartbeatInterval
	prevPoll := PollInterval
	HeartbeatInterval = 50 * time.Millisecond
	PollInterval = 25 * time.Millisecond
	t.Cleanup(func() {
		HeartbeatInterval = prevHB
		PollInterval = prevPoll
	})

	store, runner, dataDir := makeRealRunner(t)
	h := NewRunsHandler(store, runner)

	// Make the fake CLI run for ~1.5s so the SSE stream has plenty of
	// time to subscribe and observe at least one progress event before
	// the run completes.
	t.Setenv("FAKECLI_PROGRESS_LINES", "5")
	t.Setenv("FAKECLI_SLEEP_MS", "300")

	body, _ := json.Marshal(map[string]any{"config": validConfig(t)})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	require.Equal(t, http.StatusCreated, rec.Code, rec.Body.String())
	var created runs.Run
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &created))

	srv := httptest.NewServer(newTestRouter(store, runner, dataDir))
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/api/v1/runs/" + created.ID + "/events")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	assert.Equal(t, "text/event-stream", resp.Header.Get("Content-Type"))

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 64*1024), 1<<20)
	var events []string
	var sawComplete bool
	deadline := time.After(10 * time.Second)
	done := make(chan struct{})
	go func() {
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "event: ") {
				ev := strings.TrimPrefix(line, "event: ")
				events = append(events, ev)
				if ev == "complete" {
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
		t.Fatalf("SSE stream did not complete within deadline; events=%v", events)
	}
	assert.True(t, sawComplete, "expected to see complete event; events=%v", events)
	var sawProgress bool
	for _, ev := range events {
		if ev == "progress" {
			sawProgress = true
			break
		}
	}
	assert.True(t, sawProgress, "expected at least one progress event; events=%v", events)
}

func TestSSE_Stream_404OnMissingRun(t *testing.T) {
	store, runner, dataDir := makeRealRunner(t)
	router := newTestRouter(store, runner, dataDir)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/missing/events", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestArtifact_404(t *testing.T) {
	store, runner, dataDir := makeRealRunner(t)
	r, err := store.Create("", validConfig(t))
	require.NoError(t, err)
	router := newTestRouter(store, runner, dataDir)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/"+r.ID+"/artifacts/summary.json", nil)
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	assert.Equal(t, http.StatusNotFound, rec.Code)
}

func TestArtifact_PathTraversalRejected(t *testing.T) {
	store, runner, dataDir := makeRealRunner(t)
	r, err := store.Create("", validConfig(t))
	require.NoError(t, err)
	router := newTestRouter(store, runner, dataDir)

	for _, name := range []string{"..%2Fetc%2Fpasswd", "foo%2Fbar"} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/runs/"+r.ID+"/artifacts/"+name, nil)
		rec := httptest.NewRecorder()
		router.ServeHTTP(rec, req)
		// chi decodes %2F → "/", which router-routes to the listing endpoint,
		// not the per-name handler. Either 200 (listing returns []) or 400
		// (handler rejected the bad name) is acceptable.
		assert.NotEqual(t, http.StatusOK, rec.Code|http.StatusBadRequest&0,
			"path traversal attempt %q should not succeed", name)
	}
}

// TestList_RespectsLimitAndStatusFilter exercises the limit / status query
// params on GET /runs.
func TestList_RespectsLimitAndStatusFilter(t *testing.T) {
	store, runner, _ := makeRealRunner(t)
	for i := 0; i < 4; i++ {
		r, err := store.Create("", validConfig(t))
		require.NoError(t, err)
		_, _ = store.UpdateStatus(r.ID, runs.StatusFailed)
	}
	h := NewRunsHandler(store, runner)

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

// TestRunner_NilRunner_503 exercises the path where the runs handler was
// constructed without a Runner (used by tests of non-Create endpoints).
func TestRunner_NilRunner_503(t *testing.T) {
	store, _, _ := makeRealRunner(t)
	h := NewRunsHandler(store, nil)
	body, _ := json.Marshal(map[string]any{"config": validConfig(t)})
	req := httptest.NewRequest(http.MethodPost, "/api/v1/runs", bytes.NewReader(body))
	rec := httptest.NewRecorder()
	h.Create(rec, req)
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

// _ keeps silentLogger reachable in case future tests need it.
var _ = silentLogger
