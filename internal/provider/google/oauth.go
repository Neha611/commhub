package google

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Neha611/commhub/internal/safe"
	"golang.org/x/oauth2"
)

// Endpoint is Google's OAuth 2.0 endpoint, declared here rather than imported
// from x/oauth2/google so the binary does not pull in that package's GCE
// metadata dependency.
var Endpoint = oauth2.Endpoint{
	AuthURL:   "https://accounts.google.com/o/oauth2/v2/auth",
	TokenURL:  "https://oauth2.googleapis.com/token",
	AuthStyle: oauth2.AuthStyleInParams,
}

// Authorize runs the loopback OAuth flow and returns the refresh token.
//
// Security requirements from SPEC §11.2 are all load-bearing here: PKCE, a
// validated state parameter, a listener bound to 127.0.0.1 on a random port
// that shuts down the moment the callback lands, and a browser launched through
// safe.OpenURL with a scrubbed environment.
func Authorize(ctx context.Context, c ClientFile, scopes []string, incremental bool,
	openBrowser func(string) error) (safe.Secret, error) {

	// Bind first, so the redirect URI names the port we actually own. Binding
	// 127.0.0.1 rather than 0.0.0.0 keeps the callback off the network.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return safe.Secret{}, fmt.Errorf("could not open a local callback port: %w", err)
	}
	defer ln.Close()
	redirect := fmt.Sprintf("http://127.0.0.1:%d/callback", ln.Addr().(*net.TCPAddr).Port)

	verifier := randomString(64)
	challenge := sha256.Sum256([]byte(verifier))
	state := randomString(32)

	cfg := &oauth2.Config{
		ClientID:     c.ClientID,
		ClientSecret: c.ClientSecret,
		Endpoint:     Endpoint,
		RedirectURL:  redirect,
		Scopes:       scopes,
	}
	opts := []oauth2.AuthCodeOption{
		oauth2.AccessTypeOffline,
		oauth2.SetAuthURLParam("code_challenge", base64.RawURLEncoding.EncodeToString(challenge[:])),
		oauth2.SetAuthURLParam("code_challenge_method", "S256"),
	}
	if incremental {
		// Preserve scopes already granted, so enabling one feature asks for one
		// scope rather than re-consenting to everything.
		opts = append(opts, oauth2.SetAuthURLParam("include_granted_scopes", "true"))
	} else {
		// Force the consent screen so Google reliably issues a refresh token.
		opts = append(opts, oauth2.ApprovalForce)
	}
	authURL := cfg.AuthCodeURL(state, opts...)

	type result struct {
		code string
		err  error
	}
	done := make(chan result, 1)
	srv := &http.Server{
		ReadHeaderTimeout: 10 * time.Second,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			q := r.URL.Query()
			// A loopback callback is reachable by any local process, so the
			// state parameter is the only thing tying this response to our
			// request. Reject anything that does not match.
			if q.Get("state") != state {
				http.Error(w, "state mismatch", http.StatusBadRequest)
				done <- result{err: errors.New("OAuth state mismatch — the callback did not come from the request CommHub started")}
				return
			}
			if e := q.Get("error"); e != "" {
				writePage(w, "Authorisation refused", "You can close this tab and try again.")
				done <- result{err: fmt.Errorf("Google returned %q", e)}
				return
			}
			code := q.Get("code")
			if code == "" {
				http.Error(w, "no code", http.StatusBadRequest)
				done <- result{err: errors.New("no authorisation code in the callback")}
				return
			}
			writePage(w, "CommHub is connected", "You can close this tab and return to the terminal.")
			done <- result{code: code}
		}),
	}
	go srv.Serve(ln)
	defer srv.Close()

	if err := openBrowser(authURL); err != nil {
		fmt.Printf("\nCould not open a browser automatically. Open this URL:\n\n%s\n\n", authURL)
	}

	select {
	case <-ctx.Done():
		return safe.Secret{}, ctx.Err()
	case res := <-done:
		if res.err != nil {
			return safe.Secret{}, res.err
		}
		tok, err := cfg.Exchange(ctx, res.code,
			oauth2.SetAuthURLParam("code_verifier", verifier))
		if err != nil {
			return safe.Secret{}, fmt.Errorf("token exchange failed: %w", err)
		}
		if tok.RefreshToken == "" {
			return safe.Secret{}, errors.New(
				"Google returned no refresh token. Remove CommHub at myaccount.google.com/permissions and connect again")
		}
		return safe.NewSecret(tok.RefreshToken), nil
	case <-time.After(5 * time.Minute):
		return safe.Secret{}, errors.New("timed out waiting for the browser")
	}
}

// Revoke tells Google to invalidate the token. disconnect calls this before
// clearing local state: deleting a token locally without revoking leaves a live
// credential in Google's records that the user believes is gone.
func Revoke(ctx context.Context, tok safe.Secret) error {
	form := url.Values{"token": {tok.Reveal()}}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://oauth2.googleapis.com/revoke", strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("revoke returned %s", resp.Status)
	}
	return nil
}

func randomString(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return base64.RawURLEncoding.EncodeToString(b)[:n]
}

func writePage(w http.ResponseWriter, title, body string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	fmt.Fprintf(w, `<!doctype html><meta charset=utf-8>
<title>%s</title>
<style>body{font:16px/1.6 system-ui,sans-serif;margin:18vh auto;max-width:30em;padding:0 1.5em;color:#13171c}
h1{font-size:1.4em;margin:0 0 .4em}p{color:#4b5561}</style>
<h1>%s</h1><p>%s</p>`, title, title, body)
}
