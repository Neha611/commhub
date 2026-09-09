// Package cli routes commands and runs the interactive wizards. Setup lives
// here rather than on the Adapter interface, so adapters stay unit-testable
// with no terminal attached.
package cli

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Neha611/commhub/internal/adapter"
	"github.com/Neha611/commhub/internal/app"
	"github.com/Neha611/commhub/internal/config"
	"github.com/Neha611/commhub/internal/provider/google"
	"github.com/Neha611/commhub/internal/secrets"
	"github.com/Neha611/commhub/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

var Version = "0.1.0-dev"

func Run(args []string) int {
	if len(args) == 0 {
		return runTUI()
	}
	switch args[0] {
	case "connect":
		return connect(args[1:])
	case "disconnect":
		return disconnect(args[1:])
	case "status":
		return status()
	case "enable":
		return setFeature(args[1:], true)
	case "disable":
		return setFeature(args[1:], false)
	case "purge":
		return purge(args[1:])
	case "version", "--version", "-v":
		fmt.Println("commhub " + Version)
		return 0
	case "help", "--help", "-h":
		usage()
		return 0
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n\n", args[0])
		usage()
		return 2
	}
}

func usage() {
	fmt.Print(`commhub — one terminal view of your mail, calendar and meetings

  commhub                     open the dashboard
  commhub connect google      connect Gmail, Calendar and Meet in one step
  commhub connect google --label work
                              a second, fully independent account
  commhub status              what is connected, and exactly which scopes are held
  commhub enable <feature>    grant one more capability (bodies, markread, reply, rsvp)
  commhub disable <feature>   drop it again, and revoke the scope
  commhub disconnect <id>     revoke at the provider, clear the secret, drop cached rows
  commhub purge [--all]       clear cached message bodies, keeping accounts connected
  commhub version

A fresh install holds two read-only scopes. Nothing else is requested until you
ask for it.
`)
}

func openStore() (*store.Store, config.Config, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, cfg, err
	}
	st, err := store.OpenDefault()
	if err != nil {
		return nil, cfg, err
	}
	return st, cfg, nil
}

func runTUI() int {
	st, cfg, err := openStore()
	if err != nil {
		fmt.Fprintln(os.Stderr, "commhub:", err)
		return 1
	}
	defer st.Close()

	be, _ := secrets.Open(Passphrase)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	ads, buildErrs := app.Build(ctx, cfg, be)
	cancel()
	for _, e := range buildErrs {
		fmt.Fprintln(os.Stderr, "commhub:", e)
	}
	p := tea.NewProgram(app.New(st, cfg, ads, be), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "commhub:", err)
		return 1
	}
	return 0
}

func connect(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: commhub connect <google|fake>")
		return 2
	}
	switch args[0] {
	case "fake":
		if !app.DevMode() {
			fmt.Fprintln(os.Stderr, "commhub: `fake` loads synthetic development data, not real mail.")
			fmt.Fprintln(os.Stderr, "  It is not part of normal use. Set COMMHUB_DEV=1 if you are working on CommHub.")
			return 2
		}
		return connectFake()
	case "google":
		return connectGoogle(args[1:])
	default:
		fmt.Fprintf(os.Stderr, "unknown service %q — try google\n", args[0])
		return 2
	}
}

func connectFake() int {
	st, cfg, err := openStore()
	if err != nil {
		fmt.Fprintln(os.Stderr, "commhub:", err)
		return 1
	}
	defer st.Close()

	cfg.Upsert(config.Provider{
		ID: "fake:demo", Kind: "fake", Label: "demo (synthetic data)",
		Features: featureStrings(adapter.DefaultFeatures),
	})
	if err := config.Save(cfg); err != nil {
		fmt.Fprintln(os.Stderr, "commhub:", err)
		return 1
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err := st.UpsertProvider(ctx, store.Provider{
		ID: "fake:demo", Kind: "fake", Label: "demo (synthetic data)", Status: "ok",
	}); err != nil {
		fmt.Fprintln(os.Stderr, "commhub:", err)
		return 1
	}
	ads, _ := app.Build(ctx, cfg, nil)
	for _, a := range ads {
		if err := a.Sync(ctx, st); err != nil {
			fmt.Fprintln(os.Stderr, "sync:", err)
			return 1
		}
	}
	counts, _ := st.Counts(ctx)
	items, _ := st.Recent(ctx, 30*24*time.Hour, 2000)
	events := 0
	for _, it := range items {
		if it.Service == "calendar" {
			events++
		}
	}
	fmt.Printf("Connected demo provider with synthetic data.\n")
	fmt.Printf("  %d unread mail, %d calendar events\n", counts["gmail"], events)
	fmt.Printf("\nRun `commhub` to open the dashboard.\n")
	return 0
}

func disconnect(args []string) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: commhub disconnect <provider-id>   (see `commhub status`)")
		return 2
	}
	id := args[0]
	st, cfg, err := openStore()
	if err != nil {
		fmt.Fprintln(os.Stderr, "commhub:", err)
		return 1
	}
	defer st.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	// Order matters, and partial state must still clean up: the secret is
	// cleared even when the config section has already gone.
	if be, err := secrets.Open(Passphrase); err == nil {
		// Revoke before deleting. Clearing the local copy without revoking
		// leaves a live credential in Google's records that the user believes
		// is gone.
		if p, ok := cfg.Find(id); ok && p.Kind == "google" {
			if tok, err := be.Get(secrets.Key(id, "refresh")); err == nil {
				if err := google.Revoke(ctx, tok); err != nil {
					fmt.Fprintf(os.Stderr, "warning: could not revoke at Google: %v\n", err)
					fmt.Fprintln(os.Stderr, "  revoke it manually at https://myaccount.google.com/permissions")
				} else {
					fmt.Println("Revoked at Google.")
				}
			}
		}
		for _, field := range []string{"refresh", "access", "client"} {
			_ = be.Delete(secrets.Key(id, field))
		}
	}
	removed := cfg.Remove(id)
	if err := config.Save(cfg); err != nil {
		fmt.Fprintln(os.Stderr, "commhub:", err)
		return 1
	}
	if err := st.DeleteProvider(ctx, id); err != nil {
		fmt.Fprintln(os.Stderr, "commhub:", err)
		return 1
	}
	if !removed {
		fmt.Printf("No provider %q in the config; cleared any leftover secret and cached rows anyway.\n", id)
		return 0
	}
	fmt.Printf("Disconnected %s — secret cleared, cached rows deleted.\n", id)
	return 0
}

func status() int {
	st, cfg, err := openStore()
	if err != nil {
		fmt.Fprintln(os.Stderr, "commhub:", err)
		return 1
	}
	defer st.Close()

	backend := "none available"
	if be, err := secrets.Open(nil); err == nil {
		backend = be.Name()
	} else if be, err := secrets.Open(Passphrase); err == nil {
		backend = be.Name()
	}
	fmt.Printf("secrets backend   %s\n", backend)

	dbPath, _ := config.DBPath()
	fmt.Printf("cache             %s\n", dbPath)

	if len(cfg.Providers) == 0 {
		fmt.Printf("\nNo accounts connected. Run `commhub connect google`.\n")
		return 0
	}

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	counts, _ := st.Counts(ctx)
	provs, _ := st.Providers(ctx)
	statusByID := map[string]string{}
	for _, p := range provs {
		statusByID[p.ID] = p.Status
	}

	fmt.Printf("\n%-18s %-10s %-24s %s\n", "PROVIDER", "STATUS", "FEATURES", "SCOPES HELD")
	for _, p := range cfg.Providers {
		s := statusByID[p.ID]
		if s == "" {
			s = "unknown"
		}
		scopes := "none"
		if len(p.Scopes) > 0 {
			scopes = strings.Join(p.Scopes, " ")
		}
		fmt.Printf("%-18s %-10s %-24s %s\n", p.ID, s, strings.Join(p.Features, ","), scopes)
	}
	fmt.Printf("\nunread: ")
	if len(counts) == 0 {
		fmt.Print("nothing cached")
	}
	for svc, n := range counts {
		fmt.Printf("%s=%d  ", svc, n)
	}
	fmt.Println()
	return 0
}

func setFeature(args []string, on bool) int {
	if len(args) == 0 {
		fmt.Fprintln(os.Stderr, "usage: commhub enable|disable <bodies|markread|reply|rsvp|calendar|triage>")
		return 2
	}
	f := adapter.Feature(args[0])
	scopes, ok := google.FeatureScopes[f]
	if !ok {
		fmt.Fprintf(os.Stderr, "unknown feature %q\n", args[0])
		return 2
	}
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, "commhub:", err)
		return 1
	}
	if len(cfg.Providers) == 0 {
		fmt.Fprintln(os.Stderr, "no accounts connected yet")
		return 1
	}
	for i := range cfg.Providers {
		p := &cfg.Providers[i]
		if on {
			if !contains(p.Features, string(f)) {
				p.Features = append(p.Features, string(f))
			}
		} else {
			p.Features = remove(p.Features, string(f))
		}
	}
	if err := config.Save(cfg); err != nil {
		fmt.Fprintln(os.Stderr, "commhub:", err)
		return 1
	}
	if on {
		fmt.Printf("Enabling %s needs one additional scope:\n  %s\n\n", f, strings.Join(scopes, "\n  "))
	} else {
		fmt.Printf("Disabling %s drops:\n  %s\n\n", f, strings.Join(scopes, "\n  "))
	}
	ctx, cancel := context.WithTimeout(context.Background(), 6*time.Minute)
	defer cancel()
	for i := range cfg.Providers {
		p := &cfg.Providers[i]
		if p.Kind != "google" {
			continue
		}
		if err := reauthorize(ctx, &cfg, p, on); err != nil {
			return fail(err)
		}
	}
	fmt.Printf("\n%s is now %s.\n", f, map[bool]string{true: "enabled", false: "disabled"}[on])
	return 0
}

func purge(args []string) int {
	all := len(args) > 0 && args[0] == "--all"
	st, cfg, err := openStore()
	if err != nil {
		fmt.Fprintln(os.Stderr, "commhub:", err)
		return 1
	}
	defer st.Close()
	if all && !Confirm("Delete every cached item? Accounts stay connected.") {
		fmt.Println("cancelled")
		return 0
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	n, err := st.Purge(ctx, time.Duration(cfg.UI.RetentionDays)*24*time.Hour, all)
	if err != nil {
		fmt.Fprintln(os.Stderr, "commhub:", err)
		return 1
	}
	if all {
		fmt.Printf("Deleted %d cached items.\n", n)
	} else {
		fmt.Printf("Cleared cached previews on %d items older than %d days.\n", n, cfg.UI.RetentionDays)
	}
	return 0
}

func featureStrings(fs []adapter.Feature) []string {
	out := make([]string, len(fs))
	for i, f := range fs {
		out[i] = string(f)
	}
	return out
}

func contains(ss []string, s string) bool {
	for _, v := range ss {
		if v == s {
			return true
		}
	}
	return false
}

func remove(ss []string, s string) []string {
	out := ss[:0]
	for _, v := range ss {
		if v != s {
			out = append(out, v)
		}
	}
	return out
}
