// Command radstorm — run-scenario subcommand.
//
// Purpose:
//
//	`radstorm run-scenario --config <path> --out <dir>` is the production
//	driver. It loads the TOML config, applies defaults, validates,
//	mirrors the file to <out>/config.toml, and hands off to scenario.Run.
//	SIGINT / SIGTERM cancel the context for an orderly drain.
//
//	Exit codes per the briefing:
//	  0 — completed successfully
//	  1 — internal error (config load, scenario run failure)
//	  2 — completed but threshold evaluation failed
//
// Briefing: .orchestration/briefings/3a-scenario-cli.md
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/Apextech-sys/radstorm/pkg/collector"
	"github.com/Apextech-sys/radstorm/pkg/config"
	"github.com/Apextech-sys/radstorm/pkg/scenario"
)

func newRunScenarioCmd() *cobra.Command {
	var (
		cfgPath string
		outDir  string
		seed    int64
		drainS  int
	)

	cmd := &cobra.Command{
		Use:   "run-scenario",
		Short: "Run a scenario against the target RADIUS server",
		RunE: func(cmd *cobra.Command, args []string) error {
			if cfgPath == "" {
				return errors.New("--config is required")
			}
			if outDir == "" {
				return errors.New("--out is required")
			}

			// Load + validate (the source bytes feed the config_hash).
			source, err := os.ReadFile(cfgPath)
			if err != nil {
				return &exitErr{code: 1, err: fmt.Errorf("read config: %w", err)}
			}
			cfg, creds, err := config.Load(cfgPath)
			if err != nil {
				return &exitErr{code: 1, err: fmt.Errorf("load config: %w", err)}
			}

			if err := os.MkdirAll(outDir, 0o755); err != nil {
				return &exitErr{code: 1, err: fmt.Errorf("mkdir out: %w", err)}
			}
			// Mirror the config into the output directory for traceability.
			if err := os.WriteFile(filepath.Join(outDir, "config.toml"), source, 0o644); err != nil {
				return &exitErr{code: 1, err: fmt.Errorf("write config copy: %w", err)}
			}

			r, err := scenario.New(scenario.Opts{
				Config:       cfg,
				Creds:        creds,
				OutputDir:    outDir,
				Logger:       slog.Default(),
				Seed:         seed,
				DrainTimeout: time.Duration(drainS) * time.Second,
				ConfigSource: source,
			})
			if err != nil {
				return &exitErr{code: 1, err: fmt.Errorf("scenario.New: %w", err)}
			}

			ctx, stop := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
			defer stop()

			if err := r.Run(ctx); err != nil {
				return &exitErr{code: 1, err: fmt.Errorf("scenario.Run: %w", err)}
			}

			// Read summary.json back to evaluate thresholds and print key
			// outcomes to stdout.
			sumPath := filepath.Join(outDir, "summary.json")
			b, err := os.ReadFile(sumPath)
			if err != nil {
				return &exitErr{code: 1, err: fmt.Errorf("read summary.json: %w", err)}
			}
			var sum collector.Summary
			if err := json.Unmarshal(b, &sum); err != nil {
				return &exitErr{code: 1, err: fmt.Errorf("parse summary.json: %w", err)}
			}

			fmt.Printf("run %s — outcome=%s subscribers=%d/%d duration=%dms\n",
				sum.RunID, sum.Outcome,
				sum.Subscribers.Established, sum.Subscribers.Total,
				sum.DurationMs)
			fmt.Printf("artifacts: %s\n", outDir)

			if sum.Thresholds.Overall == collector.ThresholdsOverallFail {
				return &exitErr{code: 2, err: fmt.Errorf("threshold evaluation failed")}
			}
			return nil
		},
	}

	cmd.Flags().StringVar(&cfgPath, "config", "", "path to scenario TOML")
	cmd.Flags().StringVar(&outDir, "out", "", "output directory for artifacts")
	cmd.Flags().Int64Var(&seed, "seed", 0, "RNG seed for cold_start (0 = time-based)")
	cmd.Flags().IntVar(&drainS, "drain-seconds", 30, "drain timeout in seconds")

	return cmd
}
