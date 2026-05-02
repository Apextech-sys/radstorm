// Package runs — Windows-specific subprocess attributes.
//
// Purpose:
//
//	exec.CommandContext on Windows cannot send SIGTERM; the context's
//	cancel function calls TerminateProcess which is the closest equivalent.
//	No SysProcAttr customisation is needed on Windows for our use case;
//	this file exists so the build doesn't pull in Unix syscall constants.
//
// Related files:
//   - apps/api/internal/runs/runner.go
//   - apps/api/internal/runs/runner_proc_unix.go
//
// Briefing: .orchestration/briefings/3b-api-full.md
//
// Contract: internal.

//go:build windows

package runs

import "os/exec"

// configurePlatformProc is a no-op on Windows.
func configurePlatformProc(_ *exec.Cmd) {}
