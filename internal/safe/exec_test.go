package safe

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestMinimalEnvDropsSecrets(t *testing.T) {
	// The environment secrets backend puts credentials here, and $EDITOR would
	// otherwise inherit them along with every plugin it loads (SEC-02).
	t.Setenv("COMMHUB_SECRET_GOOGLE_PERSONAL_REFRESH", "1//leaked-refresh-token")
	t.Setenv("AWS_SECRET_ACCESS_KEY", "also-not-for-children")
	t.Setenv("PATH", os.Getenv("PATH"))

	env := MinimalEnv()
	joined := strings.Join(env, "\n")
	for _, forbidden := range []string{"COMMHUB_SECRET", "leaked-refresh-token", "AWS_SECRET"} {
		if strings.Contains(joined, forbidden) {
			t.Fatalf("MinimalEnv leaked %q:\n%s", forbidden, joined)
		}
	}
	if !strings.Contains(joined, "PATH=") {
		t.Fatal("MinimalEnv dropped PATH, children would not run")
	}
}

func TestMinimalEnvKeepsDesktopIntegration(t *testing.T) {
	// Scrubbing must not break the browser opener: without these, xdg-open
	// falls back to a generic handler and the OAuth flow can silently fail to
	// surface a tab.
	t.Setenv("DBUS_SESSION_BUS_ADDRESS", "unix:path=/run/user/1000/bus")
	t.Setenv("XDG_CURRENT_DESKTOP", "GNOME")
	joined := strings.Join(MinimalEnv(), "\n")
	for _, want := range []string{"DBUS_SESSION_BUS_ADDRESS=", "XDG_CURRENT_DESKTOP="} {
		if !strings.Contains(joined, want) {
			t.Fatalf("MinimalEnv dropped %s, breaking the desktop handler", want)
		}
	}
}

func TestCommandScrubsEnvironment(t *testing.T) {
	t.Setenv("COMMHUB_SECRET_X", "nope")
	cmd := Command(context.Background(), "true")
	for _, kv := range cmd.Env {
		if strings.HasPrefix(kv, "COMMHUB_SECRET_") {
			t.Fatalf("Command passed a secret to the child: %q", kv)
		}
	}
}

func TestOpenURLRefusesUnsafeSchemes(t *testing.T) {
	if err := OpenURL("file:///etc/passwd"); err == nil {
		t.Fatal("OpenURL executed a file:// URL")
	}
}

func TestStartDetachedOutlivesItsCaller(t *testing.T) {
	// Regression: the opener used to be built with exec.CommandContext, and the
	// caller cancelled that context on return. xdg-open was killed a moment
	// after Start(), before it could hand the URL to a browser — and Start()
	// had already returned nil, so nothing reported a failure and the user
	// waited for a tab that never came.
	marker := filepath.Join(t.TempDir(), "opened")

	func() {
		// A caller whose scope ends immediately, as openBrowser's did.
		if err := startDetached("sh", "-c", "sleep 0.4; touch "+marker); err != nil {
			t.Fatal(err)
		}
	}()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(marker); err == nil {
			return // survived
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("the detached process was killed before it finished its work")
}
