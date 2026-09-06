package google

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/Neha611/commhub/internal/safe"
	"github.com/Neha611/commhub/internal/secrets"
	"golang.org/x/oauth2"
)

// Provider holds one account's credentials and hands out an authenticated HTTP
// client. Adapters never see the token.
type Provider struct {
	id     string
	label  string
	scopes []string
	client *http.Client
}

func ID(label string) string { return "google:" + label }

// New builds a provider from stored state. The refresh token is read once and
// handed to oauth2, which exchanges it for access tokens as needed.
func New(ctx context.Context, id, label string, scopes []string, be secrets.Backend) (*Provider, error) {
	cf, err := LoadClient()
	if err != nil {
		return nil, err
	}
	refresh, err := be.Get(secrets.Key(id, "refresh"))
	if err != nil {
		return nil, fmt.Errorf("no stored credential for %s: %w", id, err)
	}
	cfg := &oauth2.Config{
		ClientID:     cf.ClientID,
		ClientSecret: cf.ClientSecret,
		Endpoint:     Endpoint,
		Scopes:       scopes,
	}
	ts := cfg.TokenSource(ctx, &oauth2.Token{RefreshToken: refresh.Reveal()})
	return &Provider{
		id: id, label: label, scopes: scopes,
		client: &http.Client{
			Transport: &oauth2.Transport{Source: oauth2.ReuseTokenSource(nil, ts)},
			Timeout:   30 * time.Second,
		},
	}, nil
}

func (p *Provider) ID() string       { return p.id }
func (p *Provider) Label() string    { return p.label }
func (p *Provider) Scopes() []string { return p.scopes }

// APIError carries the HTTP status so adapters can act on the ones that matter:
// 401 means the credential is dead, 404 and 410 mean a sync cursor is stale.
type APIError struct {
	Status int
	Body   string
	URL    string
}

func (e *APIError) Error() string {
	return fmt.Sprintf("google api %s: %d %s", e.URL, e.Status, safe.Text(firstLine(e.Body)))
}

func (e *APIError) NeedsReauth() bool { return e.Status == 401 || e.Status == 403 }
func (e *APIError) CursorStale() bool { return e.Status == 404 || e.Status == 410 }

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	if len(s) > 300 {
		return s[:300]
	}
	return s
}

// Get performs an authenticated GET and decodes JSON into out.
//
// The response body is capped: a compromised or misbehaving endpoint must not
// be able to exhaust memory on a background goroutine, which in v1 would take
// the whole TUI down with it (SEC-08).
func (p *Provider) Get(ctx context.Context, endpoint string, params url.Values, out any) error {
	u := endpoint
	if len(params) > 0 {
		u += "?" + params.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	return p.do(req, u, out)
}

func (p *Provider) PostJSON(ctx context.Context, endpoint string, body any, out any) error {
	buf, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(string(buf)))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	return p.do(req, endpoint, out)
}

const maxResponseBytes = 8 << 20 // 8 MiB

func (p *Provider) do(req *http.Request, u string, out any) error {
	resp, err := p.client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxResponseBytes))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		// Never log or wrap the request headers here: they carry the bearer
		// token (SEC-06).
		return &APIError{Status: resp.StatusCode, Body: string(body), URL: redactURL(u)}
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(body, out)
}

// redactURL keeps query values out of error strings; sync tokens and page
// cursors are not secrets, but they are noise and can be long.
func redactURL(u string) string {
	if i := strings.IndexByte(u, '?'); i >= 0 {
		return u[:i]
	}
	return u
}

// NewWithToken builds a provider from a token held in memory. The connect
// wizard needs this: it must call the API to learn which account it just
// authorised before it knows the key to store the credential under.
func NewWithToken(ctx context.Context, id, label string, scopes []string, refresh safe.Secret) (*Provider, error) {
	cf, err := LoadClient()
	if err != nil {
		return nil, err
	}
	cfg := &oauth2.Config{
		ClientID:     cf.ClientID,
		ClientSecret: cf.ClientSecret,
		Endpoint:     Endpoint,
		Scopes:       scopes,
	}
	ts := cfg.TokenSource(ctx, &oauth2.Token{RefreshToken: refresh.Reveal()})
	return &Provider{
		id: id, label: label, scopes: scopes,
		client: &http.Client{
			Transport: &oauth2.Transport{Source: oauth2.ReuseTokenSource(nil, ts)},
			Timeout:   30 * time.Second,
		},
	}, nil
}
