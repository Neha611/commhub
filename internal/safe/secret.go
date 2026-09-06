// Package safe holds the security primitives the spec makes acceptance
// criteria: redacted secrets (SEC-06), terminal-escape sanitising (SEC-09),
// URL allowlisting (SEC-01), environment scrubbing (SEC-02) and race-free
// file creation (SEC-04).
package safe

import (
	"errors"
	"fmt"
	"log/slog"
)

// Redacted is what a Secret renders as anywhere it is formatted, logged or
// marshalled. Reading the real value requires an explicit Reveal call, which
// greps cleanly in review.
const Redacted = "[redacted]"

// Secret wraps a credential so that it cannot be leaked by accident. Every
// stringification path is overridden; see SEC-06.
type Secret struct {
	v string
}

func NewSecret(v string) Secret { return Secret{v: v} }

// Reveal returns the underlying value. Every call site is a deliberate
// decision to handle plaintext credential material.
func (s Secret) Reveal() string { return s.v }

func (s Secret) IsZero() bool { return s.v == "" }

func (s Secret) String() string   { return Redacted }
func (s Secret) GoString() string { return Redacted }

// Format covers %v, %s, %q, %#v and every other verb.
func (s Secret) Format(f fmt.State, verb rune) {
	switch verb {
	case 'q':
		fmt.Fprintf(f, "%q", Redacted)
	default:
		fmt.Fprint(f, Redacted)
	}
}

func (s Secret) MarshalText() ([]byte, error) { return []byte(Redacted), nil }
func (s Secret) MarshalJSON() ([]byte, error) { return []byte(`"` + Redacted + `"`), nil }

// LogValue satisfies slog.LogValuer so structured logs redact too.
func (s Secret) LogValue() slog.Value { return slog.StringValue(Redacted) }

// UnmarshalText deliberately fails: secrets must never be loaded from config
// files or any other plaintext document.
func (s *Secret) UnmarshalText([]byte) error {
	return errors.New("safe: secrets cannot be decoded from plaintext documents")
}
