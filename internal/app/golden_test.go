package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/Neha611/commhub/internal/adapter/fake"
	"github.com/Neha611/commhub/internal/config"
	"github.com/Neha611/commhub/internal/store"
)

// updateGolden regenerates the snapshot: UPDATE_GOLDEN=1 go test ./internal/app
func updateGolden() bool { return os.Getenv("UPDATE_GOLDEN") == "1" }

// fixedNow keeps the snapshot stable: every relative time in the rendered
// frame derives from this instant.
var fixedNow = time.Date(2026, 9, 7, 9, 18, 0, 0, time.UTC)

func TestGoldenDashboard(t *testing.T) {
	// Times reach the pane through the store, and time.Unix returns them in the
	// local zone — correct for users, who want their own clock, but it makes a
	// rendered snapshot depend on where it was generated. Pin the zone so the
	// golden file means the same thing on a laptop in IST and on a CI runner in
	// UTC.
	origLocal := time.Local
	time.Local = time.UTC
	t.Cleanup(func() { time.Local = origLocal })

	st, err := store.Open(filepath.Join(t.TempDir(), "t.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	cfg := config.Defaults()
	cfg.Upsert(config.Provider{ID: fake.ProviderID, Kind: "fake", Label: "demo"})
	ctx := context.Background()
	if err := fake.NewMail().SyncAt(ctx, st, fixedNow); err != nil {
		t.Fatal(err)
	}
	if err := fake.NewCalendar().SyncAt(ctx, st, fixedNow); err != nil {
		t.Fatal(err)
	}

	t.Setenv("COMMHUB_DEV", "1")
	ads, _ := Build(ctx, cfg, nil)
	m := New(st, cfg, ads, nil)
	m.width, m.height = 132, 30
	m.now = fixedNow

	items := store.Rank(mustRecent(t, st), cfg.Priority, fixedNow)
	next := nextAt(items, fixedNow)
	counts, _ := st.Counts(ctx)
	byPane := map[paneID][]store.Ranked{}
	for _, p := range m.panes {
		if p.id == panePriority {
			continue
		}
		for _, it := range items {
			if it.Service == string(p.id) {
				byPane[p.id] = append(byPane[p.id], it)
			}
		}
	}
	u, _ := m.Update(loadedMsg{priority: items, byPane: byPane, next: next, counts: counts})
	got := stripANSI(u.(Model).View())

	golden := filepath.Join("testdata", "dashboard.golden")
	if updateGolden() {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Log("golden updated")
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("missing golden file (run: UPDATE_GOLDEN=1 go test ./internal/app): %v", err)
	}
	if got != string(want) {
		t.Errorf("dashboard changed.\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func mustRecent(t *testing.T, st *store.Store) []store.Item {
	t.Helper()
	items, err := st.Recent(context.Background(), 30*24*time.Hour, 2000)
	if err != nil {
		t.Fatal(err)
	}
	return items
}

func nextAt(items []store.Ranked, now time.Time) *store.Item {
	var best *store.Item
	for i := range items {
		it := items[i].Item
		if it.StartsAt == nil || it.StartsAt.Before(now) {
			continue
		}
		if best == nil || it.StartsAt.Before(*best.StartsAt) {
			cp := it
			best = &cp
		}
	}
	return best
}

// stripANSI removes styling so the golden file records layout and content, not
// colour codes, which differ by terminal.
func stripANSI(s string) string {
	var out []rune
	rs := []rune(s)
	for i := 0; i < len(rs); i++ {
		if rs[i] == 0x1B {
			j := i + 1
			if j < len(rs) && rs[j] == '[' {
				for ; j < len(rs); j++ {
					if rs[j] >= 0x40 && rs[j] <= 0x7E && rs[j] != '[' {
						break
					}
				}
			}
			i = j
			continue
		}
		out = append(out, rs[i])
	}
	return string(out)
}
