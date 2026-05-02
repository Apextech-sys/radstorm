// Command radstorm — validate-config subcommand.
//
// Purpose:
//
//	`radstorm validate-config <path>` parses + validates a scenario
//	config without performing any I/O. With --preflight it additionally
//	resolves target.auth_address and reports whether the credentials
//	file is readable. Useful in CI before kicking off an actual run.
//
// Briefing: .orchestration/briefings/3a-scenario-cli.md
package main

import (
	"errors"
	"fmt"
	"net"

	"github.com/spf13/cobra"

	"github.com/Apextech-sys/reflex-radstorm/pkg/config"
	"github.com/Apextech-sys/reflex-radstorm/pkg/scenario"
)

func newValidateConfigCmd() *cobra.Command {
	var preflight bool

	cmd := &cobra.Command{
		Use:   "validate-config <path>",
		Short: "Parse and validate a scenario config",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			path := args[0]

			cfg, err := config.LoadConfigOnly(path)
			if err != nil {
				return &exitErr{code: 1, err: err}
			}
			fmt.Printf("ok: %s parses and validates\n", path)
			fmt.Printf("  scenario.type:     %s\n", cfg.Scenario.Type)
			fmt.Printf("  subscribers.count: %d\n", cfg.Subscribers.Count)
			fmt.Printf("  target.auth:       %s\n", cfg.Target.AuthAddress)
			fmt.Printf("  target.acct:       %s\n", cfg.Target.AcctAddress)
			fmt.Printf("  source.ips:        %v\n", cfg.Source.IPs)
			fmt.Printf("  output.dir:        %s\n", cfg.Output.Directory)

			// Verify the schedule generator accepts the config — catches
			// missing per-type sub-blocks the validator alone misses.
			sch, err := scenario.BuildSchedule(cfg, 1)
			if err != nil {
				return &exitErr{code: 1, err: fmt.Errorf("schedule: %w", err)}
			}
			fmt.Printf("  schedule:          %d activations (last @ %v)\n", sch.Total(), sch.Last())

			if preflight {
				if err := preflightChecks(cfg); err != nil {
					return &exitErr{code: 1, err: err}
				}
				fmt.Println("preflight: ok")
			}
			return nil
		},
	}

	cmd.Flags().BoolVar(&preflight, "preflight", false, "additionally resolve target addresses and read credentials")
	return cmd
}

// preflightChecks runs the lightweight network + filesystem checks that
// fail fast before a full scenario run.
func preflightChecks(cfg *config.Config) error {
	if _, err := net.ResolveUDPAddr("udp", cfg.Target.AuthAddress); err != nil {
		return fmt.Errorf("resolve target.auth_address: %w", err)
	}
	if cfg.Subscribers.IncludeAcctStart {
		if _, err := net.ResolveUDPAddr("udp", cfg.Target.AcctAddress); err != nil {
			return fmt.Errorf("resolve target.acct_address: %w", err)
		}
	}
	creds, err := config.LoadCredentials(cfg.Subscribers.CredentialsFile)
	if err != nil {
		return fmt.Errorf("read credentials: %w", err)
	}
	if len(creds) < cfg.Subscribers.Count {
		return errors.New("credentials file shorter than subscribers.count")
	}
	return nil
}
