// Command radstorm — analyze-results subcommand.
//
// Purpose:
//
//	`radstorm analyze-results <dir>` reads <dir>/summary.json and prints
//	the human-readable summary to stdout. Used both interactively and in
//	CI ("show me the last run") so the output is the same as
//	<dir>/summary.txt.
//
// Briefing: .orchestration/briefings/3a-scenario-cli.md
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/spf13/cobra"

	"github.com/Apextech-sys/radstorm/pkg/collector"
)

func newAnalyzeResultsCmd() *cobra.Command {
	var asJSON bool

	cmd := &cobra.Command{
		Use:   "analyze-results <dir>",
		Short: "Print the summary of a completed run",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := args[0]
			sumPath := filepath.Join(dir, "summary.json")
			b, err := os.ReadFile(sumPath)
			if err != nil {
				return &exitErr{code: 1, err: fmt.Errorf("read %s: %w", sumPath, err)}
			}

			if asJSON {
				// Re-emit the JSON pretty-printed so callers piping to jq
				// get a clean document regardless of how it was written.
				var raw map[string]interface{}
				if err := json.Unmarshal(b, &raw); err != nil {
					return &exitErr{code: 1, err: fmt.Errorf("parse summary.json: %w", err)}
				}
				out, err := json.MarshalIndent(raw, "", "  ")
				if err != nil {
					return &exitErr{code: 1, err: fmt.Errorf("marshal: %w", err)}
				}
				fmt.Println(string(out))
				return nil
			}

			// Default: parse and emit the human-readable rendering.
			var sum collector.Summary
			if err := json.Unmarshal(b, &sum); err != nil {
				return &exitErr{code: 1, err: fmt.Errorf("parse summary.json: %w", err)}
			}
			// Prefer summary.txt if it exists (matches the renderer the
			// collector wrote at finalization). Falls back to a minimal
			// inline rendering.
			txtPath := filepath.Join(dir, "summary.txt")
			if txt, err := os.ReadFile(txtPath); err == nil {
				fmt.Print(string(txt))
				return nil
			}
			fmt.Printf("run_id:        %s\n", sum.RunID)
			fmt.Printf("scenario:      %s\n", sum.ScenarioType)
			fmt.Printf("outcome:       %s\n", sum.Outcome)
			fmt.Printf("duration:      %d ms\n", sum.DurationMs)
			fmt.Printf("subscribers:   %d total / %d established / %d auth_failed / %d acct_failed\n",
				sum.Subscribers.Total, sum.Subscribers.Established,
				sum.Subscribers.AuthFailed, sum.Subscribers.AcctFailed)
			fmt.Printf("retransmits:   %d total\n", sum.Retransmits.Total)
			fmt.Printf("coa:           %d received / %d acked / %d naked\n",
				sum.CoA.Received, sum.CoA.Acked, sum.CoA.Naked)
			fmt.Printf("disconnect:    %d received / %d acked / %d naked\n",
				sum.Disconnect.Received, sum.Disconnect.Acked, sum.Disconnect.Naked)
			fmt.Printf("thresholds:    %s\n", sum.Thresholds.Overall)
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit summary.json pretty-printed instead of summary.txt")
	return cmd
}
