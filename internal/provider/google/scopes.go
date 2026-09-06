package google

import (
	"sort"

	"github.com/Neha611/commhub/internal/adapter"
)

// FeatureScopes is the ladder from SPEC §04. Nothing outside this map is ever
// requested, and a feature the user has not enabled contributes nothing.
var FeatureScopes = map[adapter.Feature][]string{
	adapter.FeatureCalendarRead: {"https://www.googleapis.com/auth/calendar.events.readonly"},
	adapter.FeatureMailTriage:   {"https://www.googleapis.com/auth/gmail.metadata"},
	adapter.FeatureMailBodies:   {"https://www.googleapis.com/auth/gmail.readonly"},
	adapter.FeatureMarkRead:     {"https://www.googleapis.com/auth/gmail.modify"},
	adapter.FeatureReply:        {"https://www.googleapis.com/auth/gmail.send"},
	adapter.FeatureRSVP:         {"https://www.googleapis.com/auth/calendar.events"},
}

// ScopesFor returns the exact scope set for a feature list, deduplicated and
// ordered so the value stored in config is stable.
func ScopesFor(features []adapter.Feature) []string {
	seen := map[string]bool{}
	for _, f := range features {
		for _, s := range FeatureScopes[f] {
			seen[s] = true
		}
	}
	out := make([]string, 0, len(seen))
	for s := range seen {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

func ParseFeatures(ss []string) []adapter.Feature {
	out := make([]adapter.Feature, 0, len(ss))
	for _, s := range ss {
		f := adapter.Feature(s)
		if _, ok := FeatureScopes[f]; ok {
			out = append(out, f)
		}
	}
	return out
}

func Has(features []adapter.Feature, want adapter.Feature) bool {
	for _, f := range features {
		if f == want {
			return true
		}
	}
	return false
}
