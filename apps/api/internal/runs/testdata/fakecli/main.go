// Command fakecli — minimal stand-in for the real `radstorm` binary.
//
// Purpose:
//
//	Used by runs_test.go to drive the Runner end-to-end without needing
//	the actual CLI. Accepts the same `run-scenario --config <path> --out
//	<dir>` invocation the runner uses, then writes a small progress.jsonl
//	and a summary.json shaped like the real CLI's output.
//
//	Behaviour is controlled by env vars set on the subprocess by the test:
//	  - FAKECLI_PROGRESS_LINES (default "3"): number of progress lines to
//	    write before exiting.
//	  - FAKECLI_SLEEP_MS (default "20"): ms to sleep between progress
//	    lines (so tests can observe the running state and SSE tail).
//	  - FAKECLI_FAIL ("1" -> exit 1 after writing partial output).
//	  - FAKECLI_HANG ("1" -> sleep forever; used to test cancellation).
//
// Related files:
//   - apps/api/internal/runs/runner_test.go
//   - apps/api/internal/runs/runner.go
//   - .orchestration/contracts/results-schema.md
//
// Briefing: .orchestration/briefings/3b-api-full.md
//
// Contract: not a public binary; test-only.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

func main() {
	var (
		_       = flag.String("config", "", "path to config.toml")
		outDir  = flag.String("out", "", "output directory")
		sub     = flag.String("subcommand", "", "ignored, present so flag parser doesn't choke on positional")
	)
	_ = sub
	// The first positional arg is "run-scenario"; just skip it.
	if len(os.Args) >= 2 && os.Args[1] == "run-scenario" {
		os.Args = append([]string{os.Args[0]}, os.Args[2:]...)
	}
	flag.Parse()

	if *outDir == "" {
		fmt.Fprintln(os.Stderr, "fakecli: --out is required")
		os.Exit(2)
	}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		fmt.Fprintln(os.Stderr, "mkdir:", err)
		os.Exit(2)
	}

	if os.Getenv("FAKECLI_HANG") == "1" {
		// Block until killed; honour SIGTERM by writing a partial summary.
		ch := make(chan os.Signal, 1)
		signal.Notify(ch, syscall.SIGTERM, syscall.SIGINT, os.Interrupt)
		<-ch
		_ = writeSummary(*outDir, "cancelled")
		os.Exit(130)
	}

	lines := getEnvInt("FAKECLI_PROGRESS_LINES", 3)
	sleepMs := getEnvInt("FAKECLI_SLEEP_MS", 20)

	progPath := filepath.Join(*outDir, "progress.jsonl")
	pf, err := os.Create(progPath)
	if err != nil {
		fmt.Fprintln(os.Stderr, "create progress:", err)
		os.Exit(2)
	}

	for i := 0; i < lines; i++ {
		ev := map[string]any{
			"offset_ms":   (i + 1) * 1000,
			"activated":   (i + 1) * 100,
			"established": (i + 1) * 80,
			"failed":      0,
			"in_flight":   20,
			"retransmits": i,
		}
		b, _ := json.Marshal(ev)
		_, _ = pf.Write(append(b, '\n'))
		_ = pf.Sync()
		fmt.Fprintf(os.Stdout, "wrote progress %d/%d\n", i+1, lines)
		if sleepMs > 0 {
			time.Sleep(time.Duration(sleepMs) * time.Millisecond)
		}
	}
	_ = pf.Close()

	if os.Getenv("FAKECLI_FAIL") == "1" {
		_ = writeSummary(*outDir, "failed")
		os.Exit(1)
	}

	if err := writeSummary(*outDir, "succeeded"); err != nil {
		fmt.Fprintln(os.Stderr, "write summary:", err)
		os.Exit(2)
	}
	os.Exit(0)
}

func writeSummary(dir string, outcome string) error {
	summary := map[string]any{
		"run_id":        "fake",
		"started_at":    "2026-05-02T00:00:00.000Z",
		"finished_at":   "2026-05-02T00:00:01.000Z",
		"duration_ms":   1000,
		"scenario_type": "uniform",
		"outcome":       outcome,
		"subscribers": map[string]any{
			"total":                  100,
			"established":            80,
			"auth_failed":            0,
			"acct_failed":            0,
			"terminated":             0,
			"still_in_flight_at_end": 20,
		},
		"establishment": map[string]any{
			"time_to_first_ms": 50,
			"time_to_full_ms":  1000,
			"latency_ms": map[string]any{
				"min":  10,
				"p50":  100,
				"p95":  500,
				"p99":  900,
				"p999": 1500,
				"max":  2000,
			},
			"curve": []map[string]any{
				{"offset_ms": 0, "activated": 0, "established": 0, "in_flight": 0},
				{"offset_ms": 500, "activated": 50, "established": 40, "in_flight": 10},
				{"offset_ms": 1000, "activated": 100, "established": 80, "in_flight": 20},
			},
		},
		"retransmits": map[string]any{
			"total":                       0,
			"subscribers_with_retransmit": 0,
			"per_subscriber_distribution": map[string]any{"p50": 0, "p95": 0, "p99": 0, "max": 0},
		},
		"coa":           map[string]any{"received": 0, "acked": 0, "naked": 0, "dropped": 0, "response_latency_us": nil},
		"disconnect":    map[string]any{"received": 0, "acked": 0, "naked": 0, "response_latency_us": nil},
		"server_health": map[string]any{"unresponsive_periods": []any{}, "error_responses": 0},
		"thresholds":    map[string]any{"evaluated": []any{}, "overall": "pass"},
		"artifacts":     map[string]any{"events_parquet": "events.parquet", "subscribers_parquet": "subscribers.parquet", "run_log": "run.log"},
	}
	b, _ := json.MarshalIndent(summary, "", "  ")
	return os.WriteFile(filepath.Join(dir, "summary.json"), b, 0o644)
}

func getEnvInt(name string, def int) int {
	if v := os.Getenv(name); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			return n
		}
	}
	return def
}
