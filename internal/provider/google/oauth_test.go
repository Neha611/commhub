package google

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/Neha611/commhub/internal/adapter"
)

func TestAuthURLCarriesPKCEAndState(t *testing.T) {
	var captured string
	go func() {
		// Authorize blocks; we only need the URL it hands the browser.
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, _ = Authorize(ctx, ClientFile{ClientID: "id", ClientSecret: "sec"},
		[]string{"scope-a"}, false, func(u string) error { captured = u; return nil })

	if captured == "" {
		t.Fatal("no authorisation URL was produced")
	}
	u, err := url.Parse(captured)
	if err != nil {
		t.Fatal(err)
	}
	q := u.Query()
	if q.Get("code_challenge") == "" || q.Get("code_challenge_method") != "S256" {
		t.Fatalf("PKCE missing from the auth URL: %s", captured)
	}
	if q.Get("state") == "" {
		t.Fatal("no state parameter — the loopback callback would be unauthenticated")
	}
	if q.Get("access_type") != "offline" {
		t.Fatal("without access_type=offline Google issues no refresh token")
	}
	// The listener must be loopback-only: a callback reachable from the network
	// would let anyone on the LAN complete the flow.
	if !strings.HasPrefix(q.Get("redirect_uri"), "http://127.0.0.1:") {
		t.Fatalf("redirect_uri is not loopback: %q", q.Get("redirect_uri"))
	}
}

func TestCallbackRejectsMismatchedState(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		_, err := Authorize(ctx, ClientFile{ClientID: "id", ClientSecret: "sec"},
			[]string{"scope-a"}, false, func(raw string) error {
				u, _ := url.Parse(raw)
				redirect := u.Query().Get("redirect_uri")
				// Any local process can reach the callback. Forging a code with
				// the wrong state must not be accepted.
				go http.Get(redirect + "?code=forged&state=wrong-state")
				return nil
			})
		done <- err
	}()

	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "state mismatch") {
			t.Fatalf("forged callback was not rejected: %v", err)
		}
	case <-ctx.Done():
		t.Fatal("Authorize did not return")
	}
}

func TestScopeLadderIsLeastPrivilege(t *testing.T) {
	got := ScopesFor(adapter.DefaultFeatures)
	want := []string{
		"https://www.googleapis.com/auth/calendar.events.readonly",
		"https://www.googleapis.com/auth/gmail.metadata",
	}
	if len(got) != len(want) {
		t.Fatalf("default install requests %v, want exactly %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("default scopes = %v, want %v", got, want)
		}
	}
	// No write scope may be reachable without an explicitly enabled feature.
	for _, s := range got {
		if strings.Contains(s, "gmail.send") || strings.Contains(s, "gmail.modify") ||
			s == "https://www.googleapis.com/auth/calendar.events" {
			t.Fatalf("a write scope is on by default: %s", s)
		}
	}
}

func TestEnablingOneFeatureAddsOneScope(t *testing.T) {
	base := ScopesFor(adapter.DefaultFeatures)
	withReply := ScopesFor(append(append([]adapter.Feature{}, adapter.DefaultFeatures...), adapter.FeatureReply))
	if len(withReply) != len(base)+1 {
		t.Fatalf("enabling reply changed the scope set by %d, want 1", len(withReply)-len(base))
	}
}

func TestScopesAreStableAndDeduplicated(t *testing.T) {
	fs := []adapter.Feature{adapter.FeatureMailTriage, adapter.FeatureMailTriage, adapter.FeatureCalendarRead}
	a := ScopesFor(fs)
	b := ScopesFor([]adapter.Feature{adapter.FeatureCalendarRead, adapter.FeatureMailTriage})
	if len(a) != 2 || len(b) != 2 || a[0] != b[0] || a[1] != b[1] {
		t.Fatalf("scope set is not stable: %v vs %v", a, b)
	}
}
