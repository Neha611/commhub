// Package fake provides a synthetic adapter.
//
// It exists so the entire shell — panes, keybinds, the Priority pane, search,
// help, and every not-connected state — can be built and tested before a single
// credential exists, and so CI can exercise the UI with no network and no
// secrets. It is the reason the product's ranking can be validated before the
// Google integration is written.
package fake

import (
	"context"
	"fmt"
	"math/rand"
	"time"

	"github.com/Neha611/commhub/internal/adapter"
	"github.com/Neha611/commhub/internal/store"
)

const ProviderID = "fake:demo"

type Adapter struct {
	service string
	seed    int64
}

func NewMail() *Adapter     { return &Adapter{service: "gmail", seed: 7} }
func NewCalendar() *Adapter { return &Adapter{service: "calendar", seed: 11} }

func (a *Adapter) Descriptor() adapter.Descriptor {
	return adapter.Descriptor{
		Service:    a.service,
		ProviderID: ProviderID,
		Label:      "demo (synthetic data)",
		Caps:       adapter.Caps{CanReply: true, CanMarkRead: true, CanOpenURL: true},
	}
}

func (a *Adapter) RequiredScopes([]adapter.Feature) []string { return nil }
func (a *Adapter) Close() error                              { return nil }

func (a *Adapter) MarkRead(ctx context.Context, it store.Item) error { return nil }
func (a *Adapter) Reply(ctx context.Context, it store.Item, d adapter.Draft) error {
	return nil
}

func (a *Adapter) Interval() time.Duration { return 30 * time.Second }

func (a *Adapter) Sync(ctx context.Context, s *store.Store) error {
	return a.SyncAt(ctx, s, time.Now())
}

// SyncAt generates against an injected clock, so golden-file snapshots of the
// shell are deterministic.
func (a *Adapter) SyncAt(ctx context.Context, s *store.Store, now time.Time) error {
	return s.UpsertItems(ctx, a.generate(now))
}

type mailSeed struct {
	sender, title, preview                              string
	direct, to, thread, starred, bot, list, muted, read bool
	ageMin                                              int
	threadID                                            string
}

// generate produces a plausible morning inbox: a couple of things that genuinely
// need an answer, a thread in progress, and the usual sediment of newsletters
// and robots — so the Priority pane has something real to rank.
func (a *Adapter) generate(now time.Time) []store.Item {
	rng := rand.New(rand.NewSource(a.seed))
	if a.service == "calendar" {
		return a.events(now)
	}

	seeds := []mailSeed{
		{sender: "Sam Okonkwo", title: "Re: staging deploy is wedged", preview: "I rolled back to 4.2.1 but the migration lock is still held — can you look before standup?", direct: true, thread: true, threadID: "t-deploy", ageMin: 12},
		{sender: "Sam Okonkwo", title: "Re: staging deploy is wedged", preview: "Never mind the lock, it cleared. Still seeing 502s on the worker pool though.", direct: true, thread: true, threadID: "t-deploy", ageMin: 41},
		{sender: "Priya Raman", title: "Re: staging deploy is wedged", preview: "Worker pool is mine, I'll take it. Leaving the migration to you.", thread: true, threadID: "t-deploy", ageMin: 55},
		{sender: "Dana Whitfield", title: "Contract review — needs your signature today", preview: "Legal signed off this morning. Last one outstanding is yours; DocuSign link inside.", direct: true, starred: true, ageMin: 95},
		{sender: "Marcus Lee", title: "Q3 roadmap doc — comments open until Friday", preview: "Left you three questions in the reliability section, mostly about the error budget.", to: true, ageMin: 180, threadID: "t-roadmap"},
		{sender: "CI Runner", title: "[FAILED] main — build #2841", preview: "integration/store_test.go:214 timed out after 30s", bot: true, ageMin: 22},
		{sender: "CI Runner", title: "[PASSED] main — build #2842", preview: "All 148 tests passed in 71s", bot: true, read: true, ageMin: 8},
		{sender: "Golang Weekly", title: "Issue 541: sync.Map rewrites, and a faster JSON decoder", preview: "Plus: what actually changed in the new GC pacer.", list: true, ageMin: 300},
		{sender: "Anna Delgado", title: "lunch?", preview: "the thai place or the other thai place", direct: true, ageMin: 34},
		{sender: "Notion", title: "Weekly digest for Platform Team", preview: "6 pages updated, 2 comments mention you.", bot: true, list: true, muted: true, ageMin: 420},
		{sender: "Recruiting Ops", title: "Interview debrief due: candidate #4471", preview: "Scorecard is still open from Tuesday's loop.", to: true, ageMin: 1500},
		{sender: "AWS Billing", title: "Your invoice is available", preview: "Account 4471-2290-1183 — $1,842.19 for August.", bot: true, list: true, read: true, ageMin: 2600},
	}

	items := make([]store.Item, 0, len(seeds))
	for i, sd := range seeds {
		ts := now.Add(-time.Duration(sd.ageMin) * time.Minute)
		id := fmt.Sprintf("m-%d-%d", a.seed, i)
		th := sd.threadID
		if th == "" {
			th = id
		}
		items = append(items, store.Item{
			Service: "gmail", ProviderID: ProviderID, ExternalID: id, ThreadID: th,
			Title: sd.title, Sender: sd.sender, Preview: sd.preview,
			Timestamp: ts, Unread: !sd.read,
			IsDirectToMe: sd.direct, IsToMe: sd.to || sd.direct,
			IsThreadParticipant: sd.thread, IsStarred: sd.starred,
			IsBot: sd.bot, IsMuted: sd.muted, IsListMail: sd.list,
			ActionURL: "https://mail.google.com/mail/u/0/#inbox/" + id,
			UpdatedAt: now,
		})
		_ = rng
	}
	return items
}

type eventSeed struct {
	title  string
	inMin  int
	length int
	meet   bool
}

func (a *Adapter) events(now time.Time) []store.Item {
	seeds := []eventSeed{
		{title: "Platform standup", inMin: 12, length: 15, meet: true},
		{title: "1:1 with Priya", inMin: 74, length: 30, meet: true},
		{title: "Incident review — staging deploy", inMin: 215, length: 60, meet: true},
		{title: "Design critique: onboarding flow", inMin: 480, length: 45, meet: true},
		{title: "Focus block — no meetings", inMin: 1490, length: 120},
	}
	items := make([]store.Item, 0, len(seeds))
	for i, sd := range seeds {
		start := now.Add(time.Duration(sd.inMin) * time.Minute)
		id := fmt.Sprintf("e-%d", i)
		url := ""
		if sd.meet {
			url = fmt.Sprintf("https://meet.google.com/dem-ostr-%03d", i)
		}
		st := start
		items = append(items, store.Item{
			Service: "calendar", ProviderID: ProviderID, ExternalID: id, ThreadID: id,
			Title: sd.title, Sender: fmt.Sprintf("%d min", sd.length),
			Preview:   start.Format("Mon 15:04"),
			Timestamp: start, StartsAt: &st, Unread: false,
			ActionURL: url, UpdatedAt: now,
		})
	}
	return items
}
