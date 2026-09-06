// Package adapter defines the contract every service implements.
//
// Credentials belong to a Provider, not to a service: one Google provider holds
// one refresh token and serves both the Gmail and Calendar adapters. That is
// what lets a single command connect both, lets a user hold personal and work
// accounts at once, and lets a future service arrive as a new provider without
// touching the shell.
package adapter

import (
	"context"
	"time"

	"github.com/Neha611/commhub/internal/store"
)

// Feature is a user-facing capability. Each maps to the narrowest scope that
// delivers it; nothing is requested for a feature that is switched off.
type Feature string

const (
	FeatureCalendarRead Feature = "calendar"
	FeatureMailTriage   Feature = "triage"
	FeatureMailBodies   Feature = "bodies"
	FeatureMarkRead     Feature = "markread"
	FeatureReply        Feature = "reply"
	FeatureRSVP         Feature = "rsvp"
)

// DefaultFeatures is what a fresh install enables: read-only, both services.
var DefaultFeatures = []Feature{FeatureCalendarRead, FeatureMailTriage}

type Caps struct {
	CanReply    bool
	CanMarkRead bool
	CanOpenURL  bool
	CanRSVP     bool
}

type Descriptor struct {
	Service    string // "gmail" | "calendar" | "fake"
	ProviderID string
	Label      string
	Caps       Caps
}

// Draft is an outgoing message. Bodies are composed in $EDITOR, so this only
// carries the result.
type Draft struct {
	Body    string
	Subject string
}

// Adapter normalises one service for one provider into store rows. It never
// renders and never returns display data: the store is the only read path.
//
// There is deliberately no Listen method in v1. Neither Google adapter uses a
// long-lived socket — Gmail polls history.list, Calendar polls syncToken — so
// the reconnection supervisor, and its whole class of half-open-connection
// bugs, is deferred until a push-based service needs it.
type Adapter interface {
	Descriptor() Descriptor
	RequiredScopes(features []Feature) []string
	Sync(ctx context.Context, s *store.Store) error
	MarkRead(ctx context.Context, it store.Item) error
	Reply(ctx context.Context, it store.Item, d Draft) error
	Close() error
}

// Interval is how often an adapter wants to be polled. Implementations may
// override by satisfying Intervaller.
type Intervaller interface {
	Interval() time.Duration
}

func IntervalOf(a Adapter, fallback time.Duration) time.Duration {
	if iv, ok := a.(Intervaller); ok {
		if d := iv.Interval(); d > 0 {
			return d
		}
	}
	return fallback
}
