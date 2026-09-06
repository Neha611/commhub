package store

import (
	"context"
	"math"
	"sort"
	"time"

	"github.com/Neha611/commhub/internal/config"
)

// Ranked is an item with its computed score and the thread it stands for.
type Ranked struct {
	Item
	Score       float64
	ThreadCount int // how many rows this one row collapses
}

// Rank implements SPEC §08.
//
//	score = base + recency + directness − noise
//
// The score is computed here, at read time, and never stored: message urgency
// decays with age while meeting urgency RISES as the start time approaches, so
// a value written at insert is wrong within the hour.
//
// SQL does the filtering and bounding; ranking and thread collapsing happen in
// Go. That keeps the weights exactly as configured, keeps the formula unit
// testable against the continuous function, and makes "one row per thread"
// straightforward. The architectural rule still holds: the TUI reads only the
// store, and adapters are not consulted.
func Rank(items []Item, w config.Priority, now time.Time) []Ranked {
	byThread := map[string][]Item{}
	var order []string
	for _, it := range items {
		k := threadKey(it)
		if _, seen := byThread[k]; !seen {
			order = append(order, k)
		}
		byThread[k] = append(byThread[k], it)
	}

	out := make([]Ranked, 0, len(order))
	for _, k := range order {
		group := byThread[k]
		// The most urgent member represents the thread, so a six-message thread
		// is one row in the pane rather than six.
		best := group[0]
		bestScore := Score(best, w, now)
		for _, it := range group[1:] {
			if sc := Score(it, w, now); sc > bestScore {
				best, bestScore = it, sc
			}
		}
		out = append(out, Ranked{Item: best, Score: bestScore, ThreadCount: len(group)})
	}

	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		return out[i].Timestamp.After(out[j].Timestamp)
	})
	return out
}

func threadKey(it Item) string {
	if it.ThreadID != "" {
		return it.Service + "|" + it.ProviderID + "|t:" + it.ThreadID
	}
	return it.Key()
}

// Score evaluates one item. Exported so the weights can be tested directly.
func Score(it Item, w config.Priority, now time.Time) float64 {
	return base(it, w, now) + recency(it, w, now) - noise(it, w)
}

func base(it Item, w config.Priority, now time.Time) float64 {
	// A meeting's urgency is a function of how soon it starts, and a meeting
	// already under way is no longer the thing you need warning about.
	if it.StartsAt != nil {
		d := it.StartsAt.Sub(now)
		switch {
		case d < -15*time.Minute:
			return 0
		case d <= 15*time.Minute:
			return w.MeetingImminent
		case d <= time.Hour:
			return w.MeetingSoon
		default:
			return w.UnreadBulk
		}
	}
	if !it.Unread {
		// Read items keep only their starred weight, so flagged things stay
		// visible without unread status.
		if it.IsStarred {
			return w.Starred
		}
		return 0
	}
	switch {
	case it.IsDirectToMe:
		return w.UnreadDirect
	case it.IsStarred:
		return w.Starred
	case it.IsToMe:
		return w.UnreadTo
	case it.IsThreadParticipant:
		return w.ThreadParticipant
	case it.IsListMail:
		return w.UnreadBulk
	default:
		return w.UnreadCC
	}
}

// recency is the spec's +RecencyWeight × exp(−age_hours / halfLife).
func recency(it Item, w config.Priority, now time.Time) float64 {
	ref := it.Timestamp
	if it.StartsAt != nil {
		// For meetings, "recent" means "soon": decay on time remaining.
		d := it.StartsAt.Sub(now)
		if d < 0 {
			return 0
		}
		return w.RecencyWeight * math.Exp(-d.Hours()/w.RecencyHalfLife)
	}
	age := now.Sub(ref).Hours()
	if age < 0 {
		age = 0
	}
	return w.RecencyWeight * math.Exp(-age/w.RecencyHalfLife)
}

func noise(it Item, w config.Priority) float64 {
	var n float64
	if it.IsBot {
		n += w.PenaltyBot
	}
	if it.IsMuted {
		n += w.PenaltyMuted
	}
	if it.IsListMail {
		n += w.PenaltyList
	}
	return n
}

// Priority loads, ranks and trims in one call — what the Priority pane renders.
func (s *Store) Priority(ctx context.Context, w config.Priority, limit int) ([]Ranked, error) {
	items, err := s.Recent(ctx, 14*24*time.Hour, 2000)
	if err != nil {
		return nil, err
	}
	ranked := Rank(items, w, time.Now())
	if len(ranked) > limit {
		ranked = ranked[:limit]
	}
	return ranked, nil
}

// NextMeeting returns the soonest upcoming event across all providers, skipping
// anything already finished. Declined and all-day events are filtered by the
// calendar adapter before they ever reach the store.
func (s *Store) NextMeeting(ctx context.Context) (*Item, error) {
	items, err := s.Recent(ctx, 24*time.Hour, 500)
	if err != nil {
		return nil, err
	}
	now := time.Now()
	var best *Item
	for i := range items {
		it := items[i]
		if it.StartsAt == nil || it.StartsAt.Before(now.Add(-15*time.Minute)) {
			continue
		}
		if best == nil || it.StartsAt.Before(*best.StartsAt) {
			cp := it
			best = &cp
		}
	}
	return best, nil
}
