// Package secrets stores credentials outside of any plaintext document.
//
// The spec's "no secrets in plaintext, ever" rule is only workable if a backend
// is always available. go-keyring needs a D-Bus Secret Service, which does not
// exist over SSH, in WSL, or on a headless box — so a keychain-only design
// makes CommHub impossible to configure for a large part of its audience.
// Hence three backends, auto-detected in order.
package secrets

import (
	"errors"
	"fmt"

	"github.com/Neha611/commhub/internal/safe"
)

var ErrNotFound = errors.New("secrets: not found")

// Backend is one storage mechanism. Implementations must never write a secret
// anywhere but their own store.
type Backend interface {
	Name() string
	Available() bool
	Get(key string) (safe.Secret, error)
	Set(key string, v safe.Secret) error
	Delete(key string) error
}

// Key builds the canonical storage key for a provider credential.
func Key(providerID, field string) string {
	return fmt.Sprintf("commhub:%s:%s", providerID, field)
}

// Open selects the first available backend. Order is deliberate: the OS
// keychain when there is one, then a passphrase-sealed file, then the
// environment as a documented last resort for CI and headless automation.
func Open(prompt PassphraseFunc) (Backend, error) {
	candidates := []Backend{
		&keyringBackend{},
		newFileBackend(prompt),
		&envBackend{},
	}
	for _, b := range candidates {
		if b.Available() {
			return b, nil
		}
	}
	return nil, errors.New("secrets: no usable backend (no keychain, no writable config dir, no COMMHUB_* variables)")
}

// OpenNamed forces a specific backend, for tests and for `--secrets-backend`.
func OpenNamed(name string, prompt PassphraseFunc) (Backend, error) {
	var b Backend
	switch name {
	case "keyring":
		b = &keyringBackend{}
	case "file":
		b = newFileBackend(prompt)
	case "env":
		b = &envBackend{}
	default:
		return nil, fmt.Errorf("secrets: unknown backend %q", name)
	}
	if !b.Available() {
		return nil, fmt.Errorf("secrets: backend %q is not available here", name)
	}
	return b, nil
}
