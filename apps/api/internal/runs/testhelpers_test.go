// Package runs — shared test helpers.
//
// Purpose:
//
//	Builds the fake CLI binary at testdata/fakecli/ once per test process
//	and exposes its absolute path to runner / SSE / handlers tests. This
//	avoids re-compiling on every test invocation.
//
// Related files:
//   - apps/api/internal/runs/testdata/fakecli/main.go
//   - apps/api/internal/runs/runner.go
//
// Briefing: .orchestration/briefings/3b-api-full.md
//
// Contract: test-only.
package runs

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"testing"

	"github.com/Apextech-sys/radstorm/pkg/config"
)

var (
	fakeCLIOnce sync.Once
	fakeCLIPath string
	fakeCLIErr  error
)

// buildFakeCLI compiles testdata/fakecli/main.go to a temp binary and
// returns its absolute path. Idempotent across the entire test process.
func buildFakeCLI(t *testing.T) string {
	t.Helper()
	fakeCLIOnce.Do(func() {
		dir := t.TempDir() // safe: t.TempDir is process-scoped enough for our use
		bin := filepath.Join(dir, "fakecli")
		if runtime.GOOS == "windows" {
			bin += ".exe"
		}
		// Persist beyond t.TempDir cleanup by copying to a stable temp.
		// Actually t.TempDir is removed at the end of the *test*, not
		// the process. Use os.MkdirTemp directly instead.
		stableDir, err := os.MkdirTemp("", "radstorm-fakecli-*")
		if err != nil {
			fakeCLIErr = err
			return
		}
		bin = filepath.Join(stableDir, "fakecli")
		if runtime.GOOS == "windows" {
			bin += ".exe"
		}
		// Find the package source. testhelpers_test.go is colocated with
		// the testdata directory.
		// The runtime package's Caller gives us the file; find the dir.
		_, thisFile, _, _ := runtime.Caller(0)
		srcDir := filepath.Join(filepath.Dir(thisFile), "testdata", "fakecli")
		cmd := exec.Command("go", "build", "-o", bin, ".")
		cmd.Dir = srcDir
		cmd.Stdout = os.Stderr
		cmd.Stderr = os.Stderr
		if err := cmd.Run(); err != nil {
			fakeCLIErr = err
			return
		}
		fakeCLIPath = bin
	})
	if fakeCLIErr != nil {
		t.Fatalf("build fake CLI: %v", fakeCLIErr)
	}
	return fakeCLIPath
}

// minimalConfigForRunner returns a Config that won't fail Validate when
// fed to the runner (the runner doesn't validate, but writeConfigTOML
// needs a non-nil Config).
func minimalConfigForRunner() *config.Config {
	return &config.Config{
		Subscribers: config.Subscribers{Count: 100},
	}
}
