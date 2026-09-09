package safe

import (
	"context"
	"os"
	"os/exec"
	"runtime"
)

// envAllow is the complete set of variables passed to a child process.
// Everything else — including any COMMHUB_* credential from the environment
// secrets backend — is dropped (SEC-02).
//
// Without this, pressing "reply" hands the user's tokens to their editor, to
// every plugin that editor loads, and to every LSP those plugins spawn.
var envAllow = []string{
	"PATH", "HOME", "TERM", "LANG", "LC_ALL", "LC_CTYPE",
	"DISPLAY", "WAYLAND_DISPLAY", "XDG_RUNTIME_DIR", "XDG_SESSION_TYPE",
	"TMPDIR", "USER", "SHELL",
	// Needed by xdg-open to reach the desktop's real handler rather than a
	// generic fallback, and by $EDITOR for terminal integration. None of these
	// is a credential — scrubbing exists to withhold secrets, not to break the
	// desktop.
	"DBUS_SESSION_BUS_ADDRESS", "XDG_CURRENT_DESKTOP", "XDG_DATA_DIRS",
	"XDG_CONFIG_DIRS", "BROWSER", "COLORTERM",
}

// MinimalEnv builds the scrubbed environment for child processes.
func MinimalEnv() []string {
	out := make([]string, 0, len(envAllow))
	for _, k := range envAllow {
		if v, ok := os.LookupEnv(k); ok {
			out = append(out, k+"="+v)
		}
	}
	return out
}

// Command builds an exec.Cmd whose environment is scrubbed. Every child process
// CommHub spawns must be created through this.
func Command(ctx context.Context, name string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = MinimalEnv()
	return cmd
}

// OpenURL validates a URL and hands it to the platform opener as a distinct
// argv element — never through a shell, so no quoting bug can become command
// injection.
//
// The opener deliberately takes no context. It forks, hands the URL to the
// desktop and exits; binding its lifetime to a context that the caller cancels
// on return kills it before the browser ever sees the URL. Start() has already
// returned nil by then, so the failure is silent and the user simply waits for
// a tab that never appears.
func OpenURL(raw string) error {
	clean, err := CheckURL(raw)
	if err != nil {
		return err
	}
	var name string
	var args []string
	switch runtime.GOOS {
	case "darwin":
		name = "open"
	case "windows":
		name, args = "rundll32", []string{"url.dll,FileProtocolHandler"}
	default:
		name = "xdg-open"
	}
	return startDetached(name, append(args, clean)...)
}

// startDetached runs a command with a scrubbed environment and does not wait
// for it, while still reaping it so it cannot become a zombie. Its lifetime is
// independent of the caller's.
func startDetached(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	cmd.Env = MinimalEnv()
	if err := cmd.Start(); err != nil {
		return err
	}
	go func() { _ = cmd.Wait() }()
	return nil
}
