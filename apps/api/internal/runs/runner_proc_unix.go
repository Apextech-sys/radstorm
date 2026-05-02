// Package runs — Unix-specific subprocess attributes.
//
// Purpose:
//
//	On Unix we want the radstorm subprocess in its own process group so
//	that ctx-cancel-driven SIGKILL doesn't accidentally hit our own
//	parent shell, and so we can send SIGTERM/SIGKILL to the whole group
//	cleanly. exec.CommandContext on Unix sends SIGKILL on context
//	cancellation by default; setting Setpgid + Pdeathsig makes that
//	predictable.
//
// Related files:
//   - apps/api/internal/runs/runner.go
//   - apps/api/internal/runs/runner_proc_windows.go
//
// Briefing: .orchestration/briefings/3b-api-full.md
//
// Contract: internal.

//go:build !windows

package runs

import (
	"os/exec"
	"syscall"
)

// configurePlatformProc puts the subprocess in its own process group so that
// signal delivery is well-defined and our parent shell isn't affected.
func configurePlatformProc(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
}
