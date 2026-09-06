package safe

import "testing"

func TestCheckURLRejectsNonHTTP(t *testing.T) {
	// Every one of these is one keypress from a local handler if it reaches
	// xdg-open, which is why the allowlist runs at ingest.
	bad := []string{
		"file:///etc/passwd",
		"javascript:alert(1)",
		"vscode://ms-vscode.remote/attach",
		"ms-msdt:/id PCWDiagnostic",
		"smb://attacker.example/share",
		"data:text/html;base64,PHNjcmlwdD4=",
		"HTTP:/nohost",
		"https://exa\nmple.com",
		"https://example.com\x00",
		"",
		"   ",
		"not a url at all",
	}
	for _, b := range bad {
		if _, err := CheckURL(b); err == nil {
			t.Fatalf("CheckURL(%q) accepted a URL it should refuse", b)
		}
	}
}

func TestCheckURLAcceptsHTTP(t *testing.T) {
	good := []string{
		"https://meet.google.com/abc-defg-hij",
		"http://localhost:8080/callback",
		"https://mail.google.com/mail/u/0/#inbox/x",
	}
	for _, g := range good {
		if _, err := CheckURL(g); err != nil {
			t.Fatalf("CheckURL(%q) = %v, want accepted", g, err)
		}
	}
}

func TestCheckHostedURLPinsMeet(t *testing.T) {
	if _, err := CheckHostedURL("https://meet.google.com/abc-defg-hij", MeetHost); err != nil {
		t.Fatalf("genuine Meet link refused: %v", err)
	}
	// A conference link in an invite is attacker-controlled; pinning stops it
	// redirecting the join keypress somewhere else.
	for _, bad := range []string{
		"https://meet.google.com.evil.example/abc",
		"https://evil.example/meet.google.com",
		"https://meet.google.evil/abc",
	} {
		if _, err := CheckHostedURL(bad, MeetHost); err == nil {
			t.Fatalf("CheckHostedURL(%q) accepted a look-alike host", bad)
		}
	}
}
