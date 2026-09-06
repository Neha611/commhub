package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Neha611/commhub/internal/safe"
)

func TestSaveCreatesFilesWithFinalModeOnly(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfg := Defaults()
	cfg.Upsert(Provider{ID: "google:personal", Kind: "google", Label: "me@example.com"})
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "commhub", "config.toml")
	fi, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	// Write-then-chmod would leave a window where this is world-readable.
	if fi.Mode().Perm() != safe.FileMode {
		t.Fatalf("config mode = %v, want %v", fi.Mode().Perm(), safe.FileMode)
	}
	di, err := os.Stat(filepath.Join(dir, "commhub"))
	if err != nil {
		t.Fatal(err)
	}
	if di.Mode().Perm() != safe.DirMode {
		t.Fatalf("config dir mode = %v, want %v", di.Mode().Perm(), safe.DirMode)
	}
}

func TestRoundTripAndMissingConfigIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("a first run with no config must not error: %v", err)
	}
	if cfg.Priority.UnreadDirect == 0 {
		t.Fatal("defaults were not applied")
	}
	cfg.Upsert(Provider{ID: "google:work", Kind: "google", Label: "work", Features: []string{"triage"}})
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	back, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if p, ok := back.Find("google:work"); !ok || p.Label != "work" {
		t.Fatalf("provider did not survive a round trip: %+v", back.Providers)
	}
	if back.Version != Version {
		t.Fatalf("version = %d", back.Version)
	}
}

func TestConfigNeverStoresSecrets(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	cfg := Defaults()
	cfg.Upsert(Provider{ID: "google:personal", Kind: "google", Label: "me@example.com",
		Scopes: []string{"https://www.googleapis.com/auth/gmail.metadata"}})
	if err := Save(cfg); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "commhub", "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	// The Provider struct has no field that could carry a token; this test
	// fails loudly if one is ever added.
	for _, bad := range []string{"token", "refresh", "password", "secret", "cookie"} {
		if containsFold(string(raw), bad) {
			t.Fatalf("config contains a credential-shaped field %q:\n%s", bad, raw)
		}
	}
}

func containsFold(hay, needle string) bool {
	h, n := []rune(hay), []rune(needle)
	lower := func(r rune) rune {
		if r >= 'A' && r <= 'Z' {
			return r + 32
		}
		return r
	}
	for i := 0; i+len(n) <= len(h); i++ {
		ok := true
		for j := range n {
			if lower(h[i+j]) != lower(n[j]) {
				ok = false
				break
			}
		}
		if ok {
			return true
		}
	}
	return false
}
