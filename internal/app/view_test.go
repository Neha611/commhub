package app

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Neha611/commhub/internal/adapter"
	"github.com/Neha611/commhub/internal/adapter/fake"
	"github.com/Neha611/commhub/internal/config"
	"github.com/Neha611/commhub/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

// seeded builds a model backed by the fake adapter — the whole point of which
// is that the shell is testable with no credentials and no network.
func seeded(t *testing.T) Model {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })

	cfg := config.Defaults()
	cfg.Upsert(config.Provider{ID: fake.ProviderID, Kind: "fake", Label: "demo"})
	t.Setenv("COMMHUB_DEV", "1")
	ads, _ := Build(context.Background(), cfg, nil)
	ctx := context.Background()
	for _, a := range ads {
		if err := a.Sync(ctx, st); err != nil {
			t.Fatal(err)
		}
	}
	m := New(st, cfg, ads, nil)
	m.width, m.height = 130, 30
	m.now = time.Now()

	msg := m.load()()
	updated, _ := m.Update(msg)
	return updated.(Model)
}

func TestViewRendersAllPanes(t *testing.T) {
	out := seeded(t).View()
	for _, want := range []string{"Priority", "Mail", "Calendar", "Next:", "quit"} {
		if !strings.Contains(out, want) {
			t.Fatalf("rendered view is missing %q:\n%s", want, out)
		}
	}
}

func TestViewNeverEmitsRawEscapesFromContent(t *testing.T) {
	m := seeded(t)
	// lipgloss emits SGR sequences of its own; what must never appear is a
	// sequence that came from item content. Assert on the dangerous ones.
	out := m.View()
	for _, bad := range []string{"\x1b]52;", "\x1b]8;", "\x1b[2J", "\x1b]0;"} {
		if strings.Contains(out, bad) {
			t.Fatalf("view emitted a dangerous escape %q", bad)
		}
	}
}

func TestPriorityPanePutsTheImminentMeetingFirst(t *testing.T) {
	m := seeded(t)
	p := m.panes[0]
	if p.id != panePriority {
		t.Fatal("first pane is not Priority")
	}
	if len(p.items) == 0 {
		t.Fatal("priority pane is empty")
	}
	if p.items[0].StartsAt == nil {
		t.Fatalf("expected the imminent standup to lead, got %q", p.items[0].Title)
	}
}

func TestThreadIsOneRowNotSix(t *testing.T) {
	m := seeded(t)
	n := 0
	for _, it := range m.panes[0].items {
		if it.ThreadID == "t-deploy" {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("the deploy thread occupies %d rows, want 1", n)
	}
}

func TestWelcomeScreenWhenNothingConnected(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	m := New(st, config.Defaults(), nil, nil)
	m.width, m.height = 100, 24
	if m.mode != modeWelcome {
		t.Fatal("a fresh install should open on the welcome screen")
	}
	out := m.View()
	for _, want := range []string{"CommHub", "Connect a service", "Google"} {
		if !strings.Contains(out, want) {
			t.Fatalf("welcome screen is missing %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "Priority") {
		t.Fatal("an unconfigured install rendered panes")
	}
	// Synthetic data must not be offered anywhere a user could reach it.
	if strings.Contains(out, "Sample data") || strings.Contains(out, "Invented") {
		t.Fatalf("the welcome screen offers synthetic data:\n%s", out)
	}
}

func TestFakeProviderIsIgnoredOutsideDevMode(t *testing.T) {
	// A config left over from an older build must not resurrect fake data.
	cfg := config.Defaults()
	cfg.Upsert(config.Provider{ID: fake.ProviderID, Kind: "fake", Label: "demo"})

	t.Setenv("COMMHUB_DEV", "")
	ads, errs := Build(context.Background(), cfg, nil)
	if len(ads) != 0 {
		t.Fatalf("fake provider produced %d adapters in a normal run", len(ads))
	}
	if len(errs) == 0 {
		t.Fatal("ignoring a configured provider must be reported, not silent")
	}

	t.Setenv("COMMHUB_DEV", "1")
	ads, _ = Build(context.Background(), cfg, nil)
	if len(ads) == 0 {
		t.Fatal("COMMHUB_DEV=1 should still load fixtures for development")
	}
}

func TestWelcomeChooserMovesAndRefusesUnavailable(t *testing.T) {
	st, _ := store.Open(filepath.Join(t.TempDir(), "t.db"))
	defer st.Close()
	m := New(st, config.Defaults(), nil, nil)
	m.width, m.height = 100, 30

	// Slack is listed but not built yet; selecting it must say so rather than
	// appear to do nothing.
	for range integrations() {
		u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("j")})
		m = u.(Model)
	}
	if got := integrations()[m.optCursor]; got.available {
		t.Skip("last option is available; nothing to assert")
	}
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !strings.Contains(u.(Model).status, "not available yet") {
		t.Fatalf("unavailable option gave no feedback, status = %q", u.(Model).status)
	}
}

func TestCannotEscapeWelcomeWithNothingConnected(t *testing.T) {
	st, _ := store.Open(filepath.Join(t.TempDir(), "t.db"))
	defer st.Close()
	m := New(st, config.Defaults(), nil, nil)
	m.width, m.height = 100, 30
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if u.(Model).mode != modeWelcome {
		t.Fatal("esc left the welcome screen with nothing behind it")
	}
}

func TestJumpToUnconnectedServiceIsGraceful(t *testing.T) {
	// Calendar connected, mail not: pressing gm must say so rather than crash
	// or silently do nothing. This is the "runs correctly with one, two or all
	// services" constraint, exercised at the keybind level.
	st, _ := store.Open(filepath.Join(t.TempDir(), "t.db"))
	defer st.Close()
	cfg := config.Defaults()
	m := New(st, cfg, []adapter.Adapter{fake.NewCalendar()}, nil)
	m.width, m.height = 100, 24

	if m.mode == modeWelcome {
		t.Fatal("a connected install should not open on the welcome screen")
	}
	for _, p := range m.panes {
		if p.id == paneID("gmail") {
			t.Fatal("a mail pane rendered without a mail adapter")
		}
	}
	m.pendingG = true
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("m")})
	if !strings.Contains(updated.(Model).status, "not connected") {
		t.Fatalf("expected a graceful message, got %q", updated.(Model).status)
	}
}

func TestOpenAsksBeforeLeavingTheTerminal(t *testing.T) {
	m := seeded(t)
	updated, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := updated.(Model)
	if got.mode != modeConfirmOpen {
		t.Fatal("enter opened a URL without confirming the destination")
	}
	if !strings.Contains(got.View(), "in your browser?") {
		t.Fatal("confirmation does not name the destination")
	}
}

func TestSearchFiltersTheCache(t *testing.T) {
	m := seeded(t)
	m.mode = modeSearch
	for _, r := range "wedged" {
		u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = u.(Model)
	}
	u, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = u.(Model)
	if cmd == nil {
		t.Fatal("applying a search did not reload")
	}
	u2, _ := m.Update(cmd())
	m = u2.(Model)
	if len(m.panes[0].items) == 0 {
		t.Fatal("search returned nothing for a term that is present")
	}
	for _, it := range m.panes[0].items {
		if !strings.Contains(strings.ToLower(it.Title+it.Preview+it.Sender), "wedged") {
			t.Fatalf("search returned an unrelated item: %q", it.Title)
		}
	}
}

func TestHelpOverlayToggles(t *testing.T) {
	m := seeded(t)
	u, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("?")})
	if !strings.Contains(u.(Model).View(), "jump to Priority") {
		t.Fatal("help overlay did not open")
	}
}

func TestFeatureScopesStayMinimal(t *testing.T) {
	// A default install must hold read-only scopes and nothing else.
	if len(adapter.DefaultFeatures) != 2 {
		t.Fatalf("default features changed: %v", adapter.DefaultFeatures)
	}
	for _, f := range adapter.DefaultFeatures {
		if f == adapter.FeatureReply || f == adapter.FeatureMarkRead || f == adapter.FeatureMailBodies {
			t.Fatalf("%s must not be on by default", f)
		}
	}
}
