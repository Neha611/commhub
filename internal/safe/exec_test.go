package safe

import (
	"context"
	"os"
	"strings"
	"testing"
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
	if err := OpenURL(context.Background(), "file:///etc/passwd"); err == nil {
		t.Fatal("OpenURL executed a file:// URL")
	}
}
