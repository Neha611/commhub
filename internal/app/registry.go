package app

import (
	"context"

	"github.com/Neha611/commhub/internal/adapter"
	"github.com/Neha611/commhub/internal/adapter/calendar"
	"github.com/Neha611/commhub/internal/adapter/fake"
	"github.com/Neha611/commhub/internal/adapter/gmail"
	"github.com/Neha611/commhub/internal/config"
	"github.com/Neha611/commhub/internal/provider/google"
	"github.com/Neha611/commhub/internal/secrets"
)

// Build constructs one adapter per (provider, service) pair that is actually
// configured and enabled. Nothing is instantiated for a service the user has
// not connected, which is why an unconfigured service cannot render a broken
// pane — it renders no pane at all.
//
// A provider whose credential cannot be loaded contributes no adapters and is
// reported in errs, rather than taking down the ones that work.
func Build(ctx context.Context, cfg config.Config, be secrets.Backend) (ads []adapter.Adapter, errs []error) {
	for _, p := range cfg.Providers {
		switch p.Kind {
		case "fake":
			ads = append(ads, fake.NewMail(), fake.NewCalendar())

		case "google":
			if be == nil {
				continue
			}
			prov, err := google.New(ctx, p.ID, p.Label, p.Scopes, be)
			if err != nil {
				errs = append(errs, err)
				continue
			}
			features := google.ParseFeatures(p.Features)
			if google.Has(features, adapter.FeatureCalendarRead) {
				ads = append(ads, calendar.New(prov))
			}
			if google.Has(features, adapter.FeatureMailTriage) {
				ads = append(ads, gmail.New(prov, features, p.Label))
			}
		}
	}
	return ads, errs
}

// Services returns the distinct service names with a live adapter, in a stable
// display order.
func Services(as []adapter.Adapter) []string {
	order := []string{"gmail", "calendar"}
	seen := map[string]bool{}
	for _, a := range as {
		seen[a.Descriptor().Service] = true
	}
	var out []string
	for _, s := range order {
		if seen[s] {
			out = append(out, s)
		}
	}
	return out
}
