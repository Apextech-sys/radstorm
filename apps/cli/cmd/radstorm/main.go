// Command radstorm — entrypoint for the radstorm CLI.
//
// Purpose:
//
//	Cobra-based root command that wires together every public subcommand:
//	  - run-scenario      drive an end-to-end scenario against a target rig
//	  - validate-config   parse + validate a scenario config (no I/O)
//	  - single-auth       send one Access-Request as a sanity check
//	  - analyze-results   render summary.txt from a results directory
//
//	Each subcommand lives in its own file in this package. main.go owns
//	root-level concerns: logger setup, version, exit-code aggregation.
//
// Related files:
//   - apps/cli/cmd/radstorm/run_scenario.go
//   - apps/cli/cmd/radstorm/validate_config.go
//   - apps/cli/cmd/radstorm/single_auth.go
//   - apps/cli/cmd/radstorm/analyze_results.go
//   - pkg/scenario/runner.go (driven by run-scenario)
//
// Briefing: .orchestration/briefings/3a-scenario-cli.md
package main

import (
	"log/slog"
	"os"

	"github.com/spf13/cobra"
)

// Version is overridden via -ldflags in release builds.
var Version = "dev"

func main() {
	if err := newRootCmd().Execute(); err != nil {
		// Cobra already prints the user-facing error; non-zero exit is
		// our additional signal to the shell.
		os.Exit(exitCode(err))
	}
}

// exitErr lets subcommands signal a specific exit code (e.g. 2 for
// threshold failure) without losing the underlying error message.
type exitErr struct {
	code int
	err  error
}

func (e *exitErr) Error() string { return e.err.Error() }
func (e *exitErr) Unwrap() error { return e.err }

// exitCode unpacks an *exitErr if present, else returns 1.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if ee, ok := err.(*exitErr); ok {
		return ee.code
	}
	return 1
}

// newRootCmd constructs the root cobra.Command and attaches every
// subcommand. Kept as a function so tests can spin up isolated trees.
func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:           "radstorm",
		Short:         "RADIUS load and chaos test harness",
		Long:          "radstorm drives synthetic RADIUS subscribers against a target server, captures every event with microsecond precision, and produces analyzable Parquet + summary artifacts.",
		Version:       Version,
		SilenceUsage:  true,
		SilenceErrors: false,
	}

	// Persistent flags shared by every subcommand.
	var verbose bool
	root.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose logging")
	root.PersistentPreRun = func(cmd *cobra.Command, args []string) {
		level := slog.LevelInfo
		if verbose {
			level = slog.LevelDebug
		}
		h := slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})
		slog.SetDefault(slog.New(h))
	}

	root.AddCommand(newRunScenarioCmd())
	root.AddCommand(newValidateConfigCmd())
	root.AddCommand(newSingleAuthCmd())
	root.AddCommand(newAnalyzeResultsCmd())

	return root
}
