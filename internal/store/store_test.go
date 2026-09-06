package store

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

func newTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestRoundTrip(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	in := []Item{{
		Service: "gmail", ProviderID: "p", ExternalID: "1", ThreadID: "t",
		Title: "Contract review", Sender: "Dana", Preview: "signature needed",
		Timestamp: time.Now(), Unread: true, IsDirectToMe: true,
		ActionURL: "https://mail.google.com/x",
	}}
	if err := s.UpsertItems(ctx, in); err != nil {
		t.Fatal(err)
	}
	got, err := s.Recent(ctx, time.Hour, 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Title != "Contract review" || !got[0].IsDirectToMe {
		t.Fatalf("round trip lost data: %+v", got)
	}
	// Upsert must update in place, not duplicate.
	in[0].Title = "Contract review (updated)"
	if err := s.UpsertItems(ctx, in); err != nil {
		t.Fatal(err)
	}
	got, _ = s.Recent(ctx, time.Hour, 10)
	if len(got) != 1 || got[0].Title != "Contract review (updated)" {
		t.Fatalf("upsert duplicated or failed to update: %+v", got)
	}
}

func TestIngestSanitisesAndDropsUnsafeURLs(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	// Hostile content must be neutralised at the store boundary, so it can
	// never reach the renderer or the URL opener.
	err := s.UpsertItems(ctx, []Item{{
		Service: "calendar", ProviderID: "p", ExternalID: "evil",
		Title:     "\x1b[31mStandup\x1b[0m‮gnp.exe",
		Sender:    "attacker\x00",
		Timestamp: time.Now(),
		ActionURL: "vscode://ms-vscode.remote/attach",
	}})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := s.Recent(ctx, time.Hour, 10)
	if len(got) != 1 {
		t.Fatal("item not stored")
	}
	if got[0].Title != "Standupgnp.exe" {
		t.Fatalf("title not sanitised: %q", got[0].Title)
	}
	if got[0].ActionURL != "" {
		t.Fatalf("unsafe URL survived ingest: %q", got[0].ActionURL)
	}
}

func TestSearchHandlesFTSMetacharacters(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	if err := s.UpsertItems(ctx, []Item{{
		Service: "gmail", ProviderID: "p", ExternalID: "1",
		Title: "deploy is wedged", Sender: "Sam", Timestamp: time.Now(), Unread: true,
	}}); err != nil {
		t.Fatal(err)
	}
	if got, err := s.Search(ctx, "deploy", 10); err != nil || len(got) != 1 {
		t.Fatalf("plain search failed: %v %d", err, len(got))
	}
	// FTS5 has its own grammar; unescaped input would error or match wrongly.
	for _, q := range []string{`"`, `OR`, `deploy AND`, `NEAR(a b`, `*`, `col:val`, `-x`} {
		if _, err := s.Search(ctx, q, 10); err != nil {
			t.Fatalf("search(%q) errored: %v", q, err)
		}
	}
}

func TestDeleteProviderClearsEverything(t *testing.T) {
	ctx := context.Background()
	s := newTestStore(t)
	_ = s.UpsertProvider(ctx, Provider{ID: "p", Kind: "google", Label: "me"})
	_ = s.UpsertItems(ctx, []Item{{
		Service: "gmail", ProviderID: "p", ExternalID: "1",
		Title: "x", Timestamp: time.Now(), Unread: true,
	}})
	_ = s.SetSyncState(ctx, "gmail", "p", "history_id", "42")

	if err := s.DeleteProvider(ctx, "p"); err != nil {
		t.Fatal(err)
	}
	if got, _ := s.Recent(ctx, time.Hour, 10); len(got) != 0 {
		t.Fatal("items survived disconnect")
	}
	if v, _ := s.GetSyncState(ctx, "gmail", "p", "history_id"); v != "" {
		t.Fatal("sync state survived disconnect")
	}
	if ps, _ := s.Providers(ctx); len(ps) != 0 {
		t.Fatal("provider row survived disconnect")
	}
	if got, _ := s.Search(ctx, "x", 10); len(got) != 0 {
		t.Fatal("search index survived disconnect")
	}
}

func TestDeleteProviderToleratesPartialState(t *testing.T) {
	// disconnect must succeed even when there is nothing left to remove.
	if err := newTestStore(t).DeleteProvider(context.Background(), "never-existed"); err != nil {
		t.Fatalf("disconnect on absent provider failed: %v", err)
	}
}
