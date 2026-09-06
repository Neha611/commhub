package safe

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"
)

const canary = "xoxc-super-secret-value"

func TestSecretNeverRendersItsValue(t *testing.T) {
	s := NewSecret(canary)
	rendered := []string{
		s.String(),
		s.GoString(),
		fmt.Sprint(s),
		fmt.Sprintf("%v", s),
		fmt.Sprintf("%s", s),
		fmt.Sprintf("%q", s),
		fmt.Sprintf("%#v", s),
		fmt.Sprintf("%+v", struct{ Token Secret }{s}),
	}
	for _, r := range rendered {
		if strings.Contains(r, canary) {
			t.Fatalf("secret leaked through formatting: %q", r)
		}
	}
	b, err := json.Marshal(map[string]Secret{"token": s})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(b), canary) {
		t.Fatalf("secret leaked through JSON: %s", b)
	}
}

func TestSecretRedactedInStructuredLogs(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	log.Info("connecting", "token", NewSecret(canary))
	if strings.Contains(buf.String(), canary) {
		t.Fatalf("secret leaked into logs: %s", buf.String())
	}
}

func TestRevealIsTheOnlyWayOut(t *testing.T) {
	if got := NewSecret(canary).Reveal(); got != canary {
		t.Fatalf("Reveal() = %q", got)
	}
}
