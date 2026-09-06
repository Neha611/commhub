package store

import (
	"math"
	"testing"
	"time"

	"github.com/Neha611/commhub/internal/config"
)

func w() config.Priority { return config.Defaults().Priority }

func mail(id string, unread bool, ageMin int, mod func(*Item)) Item {
	now := time.Now()
	it := Item{
		Service: "gmail", ProviderID: "p", ExternalID: id, ThreadID: id,
		Title: "subject " + id, Sender: "someone",
		Timestamp: now.Add(-time.Duration(ageMin) * time.Minute), Unread: unread,
	}
	if mod != nil {
		mod(&it)
	}
	return it
}

func meeting(id string, inMin int) Item {
	now := time.Now()
	start := now.Add(time.Duration(inMin) * time.Minute)
	return Item{
		Service: "calendar", ProviderID: "p", ExternalID: id, ThreadID: id,
		Title: "meeting " + id, Timestamp: start, StartsAt: &start,
	}
}

func TestMeetingUrgencyRisesAsItApproaches(t *testing.T) {
	// This is why the score cannot be stored: the same row's rank must change
	// with the clock.
	now := time.Now()
	far := meeting("m", 240)
	soon := meeting("m", 10)
	if Score(soon, w(), now) <= Score(far, w(), now) {
		t.Fatal("an imminent meeting must outrank a distant one")
	}
}

func TestFinishedMeetingsFallAway(t *testing.T) {
	now := time.Now()
	past := meeting("m", -60)
	if got := Score(past, w(), now); got > 10 {
		t.Fatalf("a meeting an hour gone still scores %.1f", got)
	}
}

func TestMessageUrgencyDecaysWithAge(t *testing.T) {
	now := time.Now()
	fresh := mail("a", true, 5, func(i *Item) { i.IsDirectToMe = true })
	stale := mail("b", true, 60*24*3, func(i *Item) { i.IsDirectToMe = true })
	if Score(fresh, w(), now) <= Score(stale, w(), now) {
		t.Fatal("a fresh direct message must outrank a three-day-old one")
	}
}

func TestOrderingMatchesTheSpec(t *testing.T) {
	now := time.Now()
	items := []Item{
		mail("newsletter", true, 30, func(i *Item) { i.IsListMail = true }),
		mail("bot", true, 20, func(i *Item) { i.IsBot = true }),
		mail("direct", true, 30, func(i *Item) { i.IsDirectToMe = true }),
		meeting("standup", 12),
		mail("cc", true, 25, nil),
	}
	got := Rank(items, w(), now)
	if got[0].ExternalID != "standup" {
		t.Fatalf("imminent meeting should lead, got %q", got[0].ExternalID)
	}
	if got[1].ExternalID != "direct" {
		t.Fatalf("direct message should be second, got %q", got[1].ExternalID)
	}
	last := got[len(got)-1].ExternalID
	if last != "newsletter" && last != "bot" {
		t.Fatalf("noise should sink to the bottom, got %q", last)
	}
}

func TestThreadsCollapseToOneRow(t *testing.T) {
	now := time.Now()
	var items []Item
	for i := range 6 {
		it := mail("msg", true, i*10, func(x *Item) { x.IsDirectToMe = true })
		it.ExternalID = string(rune('a' + i))
		it.ThreadID = "t-deploy"
		items = append(items, it)
	}
	got := Rank(items, w(), now)
	if len(got) != 1 {
		t.Fatalf("a six-message thread produced %d rows, want 1", len(got))
	}
	if got[0].ThreadCount != 6 {
		t.Fatalf("ThreadCount = %d, want 6", got[0].ThreadCount)
	}
}

func TestRecencyMatchesTheContinuousFormula(t *testing.T) {
	// SPEC §08: +RecencyWeight × exp(−age_hours / halfLife)
	p := w()
	now := time.Now()
	for _, h := range []float64{0, 1, 3, 6, 12, 24} {
		it := Item{Timestamp: now.Add(-time.Duration(h * float64(time.Hour)))}
		want := p.RecencyWeight * math.Exp(-h/p.RecencyHalfLife)
		if got := recency(it, p, now); math.Abs(got-want) > 0.001 {
			t.Fatalf("recency at %.0fh = %.4f, want %.4f", h, got, want)
		}
	}
}

func TestNoisePenaltiesApply(t *testing.T) {
	now := time.Now()
	clean := mail("a", true, 10, nil)
	noisy := mail("b", true, 10, func(i *Item) { i.IsBot = true; i.IsMuted = true })
	diff := Score(clean, w(), now) - Score(noisy, w(), now)
	if math.Abs(diff-(w().PenaltyBot+w().PenaltyMuted)) > 0.001 {
		t.Fatalf("penalties did not apply cleanly, diff = %.2f", diff)
	}
}
