package secrets

import (
	"errors"
	"os"
	"strings"

	"github.com/Neha611/commhub/internal/safe"
)

// envBackend reads credentials from COMMHUB_* variables. It is read-only and
// documented as the least-preferred option: environment variables are inherited
// by child processes, which is exactly why safe.Command scrubs them (SEC-02).
type envBackend struct{}

func (e *envBackend) Name() string { return "env" }

func (e *envBackend) Available() bool {
	for _, kv := range os.Environ() {
		if strings.HasPrefix(kv, "COMMHUB_") && strings.Contains(kv, "_SECRET_") {
			return true
		}
	}
	return false
}

// envName maps "commhub:google:personal:refresh" to
// "COMMHUB_SECRET_GOOGLE_PERSONAL_REFRESH".
func envName(key string) string {
	k := strings.TrimPrefix(key, "commhub:")
	var b strings.Builder
	b.WriteString("COMMHUB_SECRET_")
	for _, r := range k {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r - 32)
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return b.String()
}

func (e *envBackend) Get(key string) (safe.Secret, error) {
	v, ok := os.LookupEnv(envName(key))
	if !ok || v == "" {
		return safe.Secret{}, ErrNotFound
	}
	return safe.NewSecret(v), nil
}

func (e *envBackend) Set(string, safe.Secret) error {
	return errReadOnly
}

func (e *envBackend) Delete(string) error { return errReadOnly }

var errReadOnly = errors.New("secrets: the environment backend is read-only; unset the COMMHUB_SECRET_* variables or use a keychain")
