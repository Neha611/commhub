package secrets

import (
	"errors"
	"os"
	"strings"
	"testing"

	"github.com/Neha611/commhub/internal/safe"
)

func TestEnvBackendIsReadOnly(t *testing.T) {
	e := &envBackend{}
	if err := e.Set("commhub:google:personal:refresh", safe.NewSecret("x")); err == nil {
		t.Fatal("the environment backend must not claim to store secrets it cannot store")
	}
}

func TestEnvBackendNameMapping(t *testing.T) {
	got := envName(Key("google:personal", "refresh"))
	want := "COMMHUB_SECRET_GOOGLE_PERSONAL_REFRESH"
	if got != want {
		t.Fatalf("envName = %q, want %q", got, want)
	}
}

func TestEnvBackendReads(t *testing.T) {
	t.Setenv("COMMHUB_SECRET_GOOGLE_PERSONAL_REFRESH", "1//token")
	e := &envBackend{}
	if !e.Available() {
		t.Fatal("backend should be available when COMMHUB_SECRET_* is set")
	}
	s, err := e.Get(Key("google:personal", "refresh"))
	if err != nil {
		t.Fatal(err)
	}
	if s.Reveal() != "1//token" {
		t.Fatalf("got %q", s.Reveal())
	}
	if _, err := e.Get(Key("google:other", "refresh")); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing key should report ErrNotFound, got %v", err)
	}
}

func TestFileBackendRoundTripAndWrongPassphrase(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)

	pass := func(string, bool) (safe.Secret, error) { return safe.NewSecret("correct horse"), nil }
	b := newFileBackend(pass)
	if !b.Available() {
		t.Fatal("file backend should be available with a config dir and a prompt")
	}
	key := Key("google:personal", "refresh")
	if err := b.Set(key, safe.NewSecret("1//real-token")); err != nil {
		t.Fatal(err)
	}

	// A fresh instance must decrypt from disk, not from memory.
	b2 := newFileBackend(pass)
	got, err := b2.Get(key)
	if err != nil {
		t.Fatal(err)
	}
	if got.Reveal() != "1//real-token" {
		t.Fatalf("round trip returned %q", got.Reveal())
	}

	wrong := newFileBackend(func(string, bool) (safe.Secret, error) {
		return safe.NewSecret("battery staple"), nil
	})
	if _, err := wrong.Get(key); err == nil {
		t.Fatal("the wrong passphrase decrypted the store")
	}

	if err := b2.Delete(key); err != nil {
		t.Fatal(err)
	}
	if _, err := newFileBackend(pass).Get(key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("delete did not remove the secret: %v", err)
	}
}

func TestSealedFileIsNotPlaintext(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	pass := func(string, bool) (safe.Secret, error) { return safe.NewSecret("pw"), nil }
	b := newFileBackend(pass)
	if err := b.Set(Key("google:personal", "refresh"), safe.NewSecret("SUPER-SECRET-CANARY")); err != nil {
		t.Fatal(err)
	}
	raw, err := readFile(b.path)
	if err != nil {
		t.Fatal(err)
	}
	if contains(raw, "SUPER-SECRET-CANARY") {
		t.Fatalf("the secret store is plaintext on disk:\n%s", raw)
	}
}

func readFile(p string) (string, error) {
	b, err := osReadFile(p)
	return string(b), err
}

func osReadFile(p string) ([]byte, error) { return os.ReadFile(p) }

func contains(hay, needle string) bool { return strings.Contains(hay, needle) }
