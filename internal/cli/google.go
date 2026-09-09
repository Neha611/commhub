package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Neha611/commhub/internal/adapter"
	"github.com/Neha611/commhub/internal/adapter/gmail"
	"github.com/Neha611/commhub/internal/app"
	"github.com/Neha611/commhub/internal/config"
	"github.com/Neha611/commhub/internal/provider/google"
	"github.com/Neha611/commhub/internal/safe"
	"github.com/Neha611/commhub/internal/secrets"
	"github.com/Neha611/commhub/internal/store"
)

const setupGuide = `
CommHub uses an OAuth client that you own. There is no shared app, so nothing
routes through infrastructure the maintainer runs, your mail access is not
pooled with anyone else's quota, and you can revoke it at any time.

It is a five-minute setup, once.

  1. Create a project
     https://console.cloud.google.com/projectcreate

  2. Enable both APIs in that project
     https://console.cloud.google.com/apis/library/gmail.googleapis.com
     https://console.cloud.google.com/apis/library/calendar-json.googleapis.com

  3. Configure the consent screen
     https://console.cloud.google.com/auth/branding
       · App name, and your own address for both support and developer contact
       · Do not upload a logo — that forces app verification

  4. Add yourself as a test user
     https://console.cloud.google.com/auth/audience
       · Test users → Add users → the address you will sign in with

     Google revokes refresh tokens after seven days while an app is in
     "Testing", so you will reconnect about weekly. Switching to production
     avoids that, but Google requires a homepage URL and a privacy policy URL
     on a domain you can verify — which most people setting this up for
     themselves will not have. If you do, publish on the Audience page and
     the weekly reconnect goes away.

  5. Create credentials → OAuth client ID → Application type: "Desktop app"
     https://console.cloud.google.com/apis/credentials

  6. Download the JSON and save it as
     %s

CommHub will then ask Google for exactly two read-only scopes:

     calendar.events.readonly    your events, and the Meet link on them
     gmail.metadata              senders, subjects and labels — never bodies

Reading bodies, marking read and replying are separate opt-ins. Each asks
Google for one more scope at the moment you turn it on.
`

func connectGoogle(args []string) int {
	label := ""
	for i := 0; i < len(args); i++ {
		if args[i] == "--label" && i+1 < len(args) {
			label = args[i+1]
		}
	}

	clientPath, err := google.ClientPath()
	if err != nil {
		return fail(err)
	}

	if !google.ClientExists() {
		fmt.Printf(setupGuide, clientPath)
		fmt.Println()
		if !Confirm("Saved the JSON and ready to continue?") {
			fmt.Println("\nNothing was changed. Run `commhub connect google` again when you are ready.")
			return 0
		}
		if !google.ClientExists() {
			fmt.Fprintf(os.Stderr, "\nStill no file at %s\n", clientPath)
			return 1
		}
	}

	client, err := google.LoadClient()
	if err != nil {
		return fail(err)
	}

	features := adapter.DefaultFeatures
	scopes := google.ScopesFor(features)

	fmt.Println("\nOpening your browser to authorise CommHub.")
	fmt.Println("Requesting:")
	for _, s := range scopes {
		fmt.Println("  " + s)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()

	token, err := google.Authorize(ctx, client, scopes, false, openBrowser)
	if err != nil {
		return fail(err)
	}
	fmt.Println("\nAuthorised. Checking which account that was…")

	// The account's own address is learned from the credential we just got,
	// rather than by requesting an extra identity scope purely to ask.
	tmp, err := google.NewWithToken(ctx, "google:pending", "", scopes, token)
	if err != nil {
		return fail(err)
	}
	if label == "" {
		email, _, err := gmail.Profile(ctx, tmp)
		if err != nil {
			fmt.Fprintf(os.Stderr, "could not read the account address (%v)\n", err)
			fmt.Fprintln(os.Stderr, "pass --label yourself, e.g. `commhub connect google --label personal`")
			return 1
		}
		label = email
	}
	id := google.ID(label)

	be, err := secrets.Open(Passphrase)
	if err != nil {
		return fail(err)
	}
	if err := be.Set(secrets.Key(id, "refresh"), token); err != nil {
		return fail(fmt.Errorf("storing the credential: %w", err))
	}

	cfg, err := config.Load()
	if err != nil {
		return fail(err)
	}
	cfg.Upsert(config.Provider{
		ID: id, Kind: "google", Label: label,
		Scopes: scopes, Features: featureStrings(features),
	})
	if err := config.Save(cfg); err != nil {
		return fail(err)
	}

	st, err := store.OpenDefault()
	if err != nil {
		return fail(err)
	}
	defer st.Close()
	if err := st.UpsertProvider(ctx, store.Provider{
		ID: id, Kind: "google", Label: label, Scopes: scopes, Status: "ok",
	}); err != nil {
		return fail(err)
	}

	fmt.Printf("Connected %s.\nStored the credential in the %s backend.\n\nSyncing…\n", label, be.Name())

	ads, errs := app.Build(ctx, cfg, be)
	for _, e := range errs {
		fmt.Fprintln(os.Stderr, "  warning:", e)
	}
	for _, a := range ads {
		d := a.Descriptor()
		if err := a.Sync(ctx, st); err != nil {
			fmt.Fprintf(os.Stderr, "  %s: %v\n", d.Service, err)
			continue
		}
		fmt.Printf("  %s ok\n", d.Service)
	}

	counts, _ := st.Counts(ctx)
	next, _ := st.NextMeeting(ctx)
	fmt.Printf("\n  %d unread in the inbox\n", counts["gmail"])
	if next != nil && next.StartsAt != nil {
		fmt.Printf("  next meeting: %s at %s\n",
			safe.Text(next.Title), next.StartsAt.Local().Format("Mon 15:04"))
	} else {
		fmt.Printf("  no meetings in the next 3 weeks\n")
	}
	fmt.Printf("\nRun `commhub` to open the dashboard.\n")
	return 0
}

// reauthorize re-runs consent after the feature set changes. Enabling uses
// incremental authorisation, so the user approves only the new scope and
// everything already granted is preserved. Disabling cannot shrink a token, so
// the old one is revoked and a smaller one issued in its place.
func reauthorize(ctx context.Context, cfg *config.Config, p *config.Provider, adding bool) error {
	client, err := google.LoadClient()
	if err != nil {
		return err
	}
	scopes := google.ScopesFor(google.ParseFeatures(p.Features))

	be, err := secrets.Open(Passphrase)
	if err != nil {
		return err
	}
	key := secrets.Key(p.ID, "refresh")

	if !adding {
		if old, err := be.Get(key); err == nil {
			if err := google.Revoke(ctx, old); err != nil {
				fmt.Fprintf(os.Stderr, "warning: could not revoke the previous token: %v\n", err)
			} else {
				fmt.Println("Previous token revoked at Google.")
			}
		}
	}

	fmt.Println("Opening your browser. CommHub will hold:")
	for _, s := range scopes {
		fmt.Println("  " + s)
	}
	token, err := google.Authorize(ctx, client, scopes, adding, openBrowser)
	if err != nil {
		return err
	}
	if err := be.Set(key, token); err != nil {
		return err
	}
	p.Scopes = scopes
	return config.Save(*cfg)
}

// openBrowser prints the authorisation URL and then tries to open it.
//
// Printing is not a fallback for failure: xdg-open reports success as soon as
// it hands off, so a tab that opens behind the terminal, or in a browser the
// user is not watching, looks identical to one that never opened. The URL is
// always on screen so the flow can be completed either way.
func openBrowser(u string) error {
	fmt.Printf("\nIf a browser tab does not appear, open this URL:\n\n  %s\n\n", u)
	fmt.Println("Waiting for you to approve… (ctrl-c to cancel)")
	return safe.OpenURL(u)
}

func fail(err error) int {
	var denied *google.DeniedError
	if errors.As(err, &denied) {
		fmt.Fprintf(os.Stderr, "\ncommhub: %v\n\n", denied)
		fmt.Fprintln(os.Stderr, "This almost always means the consent screen is still in \"Testing\", which")
		fmt.Fprintln(os.Stderr, "only lets accounts you have listed as test users sign in.")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "  Fix it at https://console.cloud.google.com/auth/audience")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "    Publishing status → Publish app        (preferred: tokens do not expire)")
		fmt.Fprintln(os.Stderr, "    or Test users → + Add users → your own address")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Publishing shows a \"Google hasn't verified this app\" warning when you")
		fmt.Fprintln(os.Stderr, "authorise. That is expected: the app is yours and you are its only user.")
		fmt.Fprintln(os.Stderr, "Choose Advanced, then continue.")
		fmt.Fprintln(os.Stderr, "")
		fmt.Fprintln(os.Stderr, "Adding yourself as a test user works immediately, but Google revokes")
		fmt.Fprintln(os.Stderr, "refresh tokens after seven days in that state, so you would reconnect")
		fmt.Fprintln(os.Stderr, "every week.")
		return 1
	}
	var apiErr *google.APIError
	if errors.As(err, &apiErr) && apiErr.NeedsReauth() {
		fmt.Fprintln(os.Stderr, "commhub: Google refused the credential.")
		fmt.Fprintln(os.Stderr, "  If the consent screen is still in \"Testing\", publish it — refresh")
		fmt.Fprintln(os.Stderr, "  tokens expire after seven days in that state. Then connect again.")
		return 1
	}
	msg := err.Error()
	if strings.Contains(msg, "invalid_grant") {
		fmt.Fprintln(os.Stderr, "commhub: the stored credential is no longer valid.")
		fmt.Fprintln(os.Stderr, "  Run `commhub connect google` to sign in again.")
		return 1
	}
	fmt.Fprintln(os.Stderr, "commhub:", msg)
	return 1
}
