// Command radstorm — CLI integration tests.
//
// Purpose:
//
//	Smoke-test each subcommand at the cobra level (via Execute). Tests
//	avoid real UDP unless explicitly opted into (Docker-only e2e lives
//	separately).
//
// Briefing: .orchestration/briefings/3a-scenario-cli.md
package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Apextech-sys/reflex-radstorm/pkg/collector"
)

func TestRoot_ShowsHelp(t *testing.T) {
	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"--help"})
	if err := cmd.Execute(); err != nil {
		t.Fatalf("--help: %v", err)
	}
	if !strings.Contains(buf.String(), "run-scenario") {
		t.Errorf("--help output missing run-scenario; got %q", buf.String())
	}
	if !strings.Contains(buf.String(), "analyze-results") {
		t.Errorf("--help output missing analyze-results")
	}
}

func TestValidateConfig_MissingArg(t *testing.T) {
	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"validate-config"})
	err := cmd.Execute()
	if err == nil {
		t.Fatalf("expected error when no path supplied")
	}
}

func TestValidateConfig_OK(t *testing.T) {
	dir := t.TempDir()
	credPath := filepath.Join(dir, "creds.csv")
	if err := os.WriteFile(credPath, []byte("username,password\nu1,p1\nu2,p2\n"), 0o644); err != nil {
		t.Fatalf("write creds: %v", err)
	}

	tomlBody := `
[target]
auth_address  = "127.0.0.1:11812"
acct_address  = "127.0.0.1:11813"
shared_secret = "x"

[coa_listener]
bind_address  = "127.0.0.1:13799"
shared_secret = "x"

[subscribers]
count = 2
credentials_file = "` + filepathEscape(credPath) + `"
auth_method_pap_pct = 100
type_pppoe_pct = 100
include_acct_start = false

[source]
ips = ["127.0.0.1"]
port_range = [20000, 20100]

[nas]
ip_address = "127.0.0.1"
identifier = "test-nas"

[scenario]
type = "uniform"
hard_timeout_sec = 30

[scenario.uniform]
duration_sec = 1.0

[output]
directory = "out"
`
	cfgPath := filepath.Join(dir, "scenario.toml")
	if err := os.WriteFile(cfgPath, []byte(tomlBody), 0o644); err != nil {
		t.Fatalf("write toml: %v", err)
	}

	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	cmd.SetArgs([]string{"validate-config", cfgPath})

	old := os.Stdout
	rPipe, wPipe, _ := os.Pipe()
	os.Stdout = wPipe
	err := cmd.Execute()
	_ = wPipe.Close()
	os.Stdout = old
	stdoutCaptured, _ := readAll(rPipe)

	if err != nil {
		t.Fatalf("validate-config: %v\nstderr: %s", err, buf.String())
	}
	if !strings.Contains(stdoutCaptured, "ok:") {
		t.Errorf("validate-config stdout missing ok: %q", stdoutCaptured)
	}
}

func TestAnalyzeResults_FromSummary(t *testing.T) {
	dir := t.TempDir()
	sum := collector.Summary{
		RunID:        "test-run",
		ScenarioType: "uniform",
		Outcome:      collector.OutcomeSucceeded,
		DurationMs:   1234,
		Subscribers: collector.SubscribersSection{
			Total:       3,
			Established: 3,
		},
		Thresholds:    collector.ThresholdsSection{Evaluated: []collector.Threshold{}, Overall: collector.ThresholdsOverallNone},
		Establishment: collector.EstablishmentSection{Curve: []collector.CurvePoint{}},
	}
	b, _ := json.Marshal(&sum)
	if err := os.WriteFile(filepath.Join(dir, "summary.json"), b, 0o644); err != nil {
		t.Fatalf("write summary: %v", err)
	}

	cmd := newRootCmd()
	var buf bytes.Buffer
	cmd.SetOut(&buf)
	cmd.SetErr(&buf)
	// Capture stdout in a tee — we use fmt.Print directly so route the
	// process's stdout temporarily.
	old := os.Stdout
	rPipe, wPipe, _ := os.Pipe()
	os.Stdout = wPipe
	cmd.SetArgs([]string{"analyze-results", dir})
	err := cmd.Execute()
	_ = wPipe.Close()
	os.Stdout = old
	out, _ := readAll(rPipe)

	if err != nil {
		t.Fatalf("analyze-results: %v", err)
	}
	if !strings.Contains(out, "test-run") {
		t.Errorf("output missing run_id: %q", out)
	}
}

func TestExitCode(t *testing.T) {
	if exitCode(nil) != 0 {
		t.Errorf("nil → 0")
	}
	if exitCode(&exitErr{code: 2, err: nil}) != 2 {
		t.Errorf("exitErr → 2")
	}
}

// readAll mirrors io.ReadAll without pulling in another import path.
func readAll(r interface{ Read([]byte) (int, error) }) (string, error) {
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 4096)
	for {
		n, err := r.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			break
		}
	}
	return string(buf), nil
}

// filepathEscape doubles backslashes in a Windows path so it can be
// embedded inside a TOML double-quoted string.
func filepathEscape(p string) string {
	return strings.ReplaceAll(p, `\`, `\\`)
}
