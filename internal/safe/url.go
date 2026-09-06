package safe

import (
	"errors"
	"net/url"
	"strings"
)

// ErrUnsafeURL is returned for any URL that is not plain http(s).
var ErrUnsafeURL = errors.New("safe: url scheme not allowed")

// MeetHost is the only host a "join meeting" action may open. Pinning it means
// a crafted conference link in an invite cannot redirect the keypress
// elsewhere.
const MeetHost = "meet.google.com"

// CheckURL validates an attacker-supplied URL against the allowlist. It runs at
// ingest, so a hostile URL never reaches the store (SEC-01).
//
// Only http and https are permitted. Every other scheme — file:, javascript:,
// vscode:, smb:, ms-msdt: — dispatches to a locally registered handler when
// passed to xdg-open, which turns one keypress on an unsolicited calendar
// invite into local code execution.
func CheckURL(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", ErrUnsafeURL
	}
	// Control characters and newlines can split a command or hide the real
	// target from the confirmation line.
	for _, r := range raw {
		if r < 0x20 || r == 0x7F {
			return "", ErrUnsafeURL
		}
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "", ErrUnsafeURL
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https":
	default:
		return "", ErrUnsafeURL
	}
	if u.Host == "" {
		return "", ErrUnsafeURL
	}
	return u.String(), nil
}

// CheckHostedURL additionally pins the host, for links whose destination is
// known in advance — Meet join links above all.
func CheckHostedURL(raw, host string) (string, error) {
	clean, err := CheckURL(raw)
	if err != nil {
		return "", err
	}
	u, err := url.Parse(clean)
	if err != nil {
		return "", ErrUnsafeURL
	}
	if !strings.EqualFold(u.Hostname(), host) {
		return "", ErrUnsafeURL
	}
	return clean, nil
}

// DisplayHost returns the hostname for the confirmation line shown before
// opening a URL, so the user sees where the keypress actually goes.
func DisplayHost(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "?"
	}
	return u.Hostname()
}
