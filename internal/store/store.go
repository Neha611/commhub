// Package store owns the SQLite cache. It is the single source of truth the
// TUI reads from: adapters normalise remote state into rows and upsert them,
// and nothing on the render path ever touches the network.
package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/Neha611/commhub/internal/config"
	"github.com/Neha611/commhub/internal/safe"
	_ "modernc.org/sqlite" // pure Go, no cgo — required for a static binary
)

type Store struct {
	db *sql.DB
}

// Open creates or opens the cache. The file is created with 0600 before SQLite
// touches it, so there is no window where the message cache is world-readable.
func Open(path string) (*Store, error) {
	if path != ":memory:" {
		if err := safe.MkdirSecure(dirOf(path)); err != nil {
			return nil, err
		}
		if err := ensureMode(path); err != nil {
			return nil, err
		}
	}
	dsn := path + "?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)"
	if path == ":memory:" {
		dsn = "file::memory:?cache=shared"
	}
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(1) // modernc + WAL is happiest serialised for our volumes
	s := &Store{db: db}
	if err := s.migrate(context.Background()); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return s, nil
}

func OpenDefault() (*Store, error) {
	p, err := config.DBPath()
	if err != nil {
		return nil, err
	}
	return Open(p)
}

func (s *Store) Close() error { return s.db.Close() }
func (s *Store) DB() *sql.DB  { return s.db }

// Item is the normalised unit every adapter produces and every pane renders.
//
// It carries priority SIGNALS, never a score: urgency decays for messages and
// rises for meetings, so a value frozen at insert is wrong within the hour.
type Item struct {
	Service    string // "gmail" | "calendar" | "fake"
	ProviderID string
	ExternalID string
	ThreadID   string
	Title      string
	Sender     string
	Preview    string // empty under the gmail.metadata scope ladder
	Timestamp  time.Time
	StartsAt   *time.Time // calendar only; drives time-to-meeting urgency
	Unread     bool

	IsDirectToMe        bool
	IsToMe              bool
	IsThreadParticipant bool
	IsStarred           bool
	IsBot               bool
	IsMuted             bool
	IsListMail          bool

	ActionURL string // validated at ingest; https/http only
	UpdatedAt time.Time
}

// Key uniquely identifies an item across providers.
func (i Item) Key() string { return i.Service + "|" + i.ProviderID + "|" + i.ExternalID }

type Provider struct {
	ID          string
	Kind        string
	Label       string
	Scopes      []string
	Status      string // ok | needs_reauth | revoked
	ConnectedAt time.Time
}
