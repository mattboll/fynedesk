package wm

import (
	"context"
	"os/exec"
	"time"
)

// DefaultExecTimeout caps short-lived helper commands (wpctl, nmcli, brightnessctl,
// rfkill, …) so an unresponsive system daemon cannot block the Fyne UI thread
// forever. Suspend/resume on flaky hardware leaves these tools wedged until the
// daemon is restarted; without a timeout the panel becomes uninteractable and
// only a reboot recovers it.
const DefaultExecTimeout = 2 * time.Second

// ExecOutput runs name with args under the default timeout and returns stdout.
// Prefer this over exec.Command(...).Output() for any subprocess invoked from
// UI code paths (status modules, sidebar, settings, …).
func ExecOutput(name string, args ...string) ([]byte, error) {
	return ExecOutputCtx(DefaultExecTimeout, name, args...)
}

// ExecRun runs name with args under the default timeout and discards output.
// Prefer this over exec.Command(...).Run() for UI code paths.
func ExecRun(name string, args ...string) error {
	return ExecRunCtx(DefaultExecTimeout, name, args...)
}

// ExecOutputCtx is like ExecOutput but accepts a custom timeout.
func ExecOutputCtx(timeout time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).Output()
}

// ExecRunCtx is like ExecRun but accepts a custom timeout.
func ExecRunCtx(timeout time.Duration, name string, args ...string) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	return exec.CommandContext(ctx, name, args...).Run()
}
