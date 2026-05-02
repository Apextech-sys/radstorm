// Command radstorm — single-auth subcommand.
//
// Purpose:
//
//	`radstorm single-auth --config <path> --user <u> --pass <p>` is the
//	dev sanity-check tool: spin up one I/O engine, send one Access-Request
//	using the config's NAS attributes + shared secret, and report the
//	round-trip latency. Useful when bringing up a new test rig before
//	committing to a full scenario run.
//
// Briefing: .orchestration/briefings/3a-scenario-cli.md
package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"time"

	"github.com/spf13/cobra"

	"github.com/Apextech-sys/radstorm/pkg/config"
	"github.com/Apextech-sys/radstorm/pkg/io"
	"github.com/Apextech-sys/radstorm/pkg/radius"
)

func newSingleAuthCmd() *cobra.Command {
	var (
		cfgPath string
		user    string
		pass    string
		method  string
		timeout int
	)

	cmd := &cobra.Command{
		Use:   "single-auth",
		Short: "Send one Access-Request to verify connectivity to the target",
		RunE: func(cmd *cobra.Command, args []string) error {
			if cfgPath == "" {
				return errors.New("--config is required")
			}
			if user == "" || pass == "" {
				return errors.New("--user and --pass are required")
			}

			cfg, err := config.LoadConfigOnly(cfgPath)
			if err != nil {
				return &exitErr{code: 1, err: fmt.Errorf("load config: %w", err)}
			}

			ips := []net.IP{}
			for _, s := range cfg.Source.IPs {
				ip := net.ParseIP(s)
				if ip == nil {
					return &exitErr{code: 1, err: fmt.Errorf("invalid source IP %q", s)}
				}
				ips = append(ips, ip)
			}

			eng, err := io.NewEngine(io.Opts{
				SourceIPs: ips,
				// Use kernel-chosen ephemeral ports — single-auth needs
				// only one socket, and any port range we pick risks
				// colliding with a concurrent run.
				PortRangeLo: 0,
				PortRangeHi: 0,
				Secret:      []byte(cfg.Target.SharedSecret),
			})
			if err != nil {
				return &exitErr{code: 1, err: fmt.Errorf("io.NewEngine: %w", err)}
			}

			ctx, cancel := context.WithTimeout(context.Background(), time.Duration(timeout)*time.Second)
			defer cancel()

			if err := eng.Start(ctx); err != nil {
				return &exitErr{code: 1, err: fmt.Errorf("engine start: %w", err)}
			}
			defer func() { _ = eng.Stop(ctx) }()

			authDst, err := net.ResolveUDPAddr("udp", cfg.Target.AuthAddress)
			if err != nil {
				return &exitErr{code: 1, err: fmt.Errorf("resolve target.auth_address: %w", err)}
			}

			extras := []radius.Attribute{}
			if ip := net.ParseIP(cfg.NAS.IPAddress).To4(); ip != nil {
				extras = append(extras, radius.Attribute{Type: radius.AttrNASIPAddress, Value: ip})
			}
			extras = append(extras, radius.Attribute{Type: radius.AttrNASIdentifier, Value: []byte(cfg.NAS.Identifier)})

			build := func(id uint8) (*radius.Packet, error) {
				if method == "chap" {
					return radius.NewAccessRequestCHAP(id, []byte(cfg.Target.SharedSecret), user, pass, extras...)
				}
				return radius.NewAccessRequestPAP(id, []byte(cfg.Target.SharedSecret), user, pass, extras...)
			}

			policy := io.RetransmitPolicy{
				InitialTimeout: time.Duration(cfg.Retransmit.InitialTimeoutMs) * time.Millisecond,
				MaxRetries:     cfg.Retransmit.MaxRetries,
				Backoff:        io.BackoffExponential,
				BackoffBase:    time.Duration(cfg.Retransmit.BackoffBaseMs) * time.Millisecond,
			}
			if policy.InitialTimeout <= 0 {
				policy.InitialTimeout = 3 * time.Second
			}

			start := time.Now()
			res, err := eng.Send(ctx, authDst, build, policy, 0)
			elapsed := time.Since(start)
			if err != nil {
				fmt.Fprintf(os.Stderr, "send failed after %v: %v\n", elapsed, err)
				return &exitErr{code: 1, err: err}
			}

			fmt.Printf("user=%s method=%s reply=%s identifier=%d latency=%dµs retransmits=%d\n",
				user, method, res.Reply.Code, res.Reply.Identifier,
				res.LatencyUs, res.RetransmitN)

			switch res.Reply.Code {
			case radius.CodeAccessAccept:
				return nil
			case radius.CodeAccessReject:
				return &exitErr{code: 1, err: errors.New("Access-Reject")}
			default:
				return &exitErr{code: 1, err: fmt.Errorf("unexpected reply code %d", res.Reply.Code)}
			}
		},
	}

	cmd.Flags().StringVar(&cfgPath, "config", "", "scenario TOML to draw NAS attrs / shared secret from")
	cmd.Flags().StringVar(&user, "user", "", "User-Name attribute")
	cmd.Flags().StringVar(&pass, "pass", "", "User-Password (PAP) or CHAP-Password (CHAP)")
	cmd.Flags().StringVar(&method, "method", "pap", "auth method: pap | chap")
	cmd.Flags().IntVar(&timeout, "timeout", 10, "overall timeout in seconds")
	return cmd
}
