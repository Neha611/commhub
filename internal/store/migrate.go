package store

import (
	"context"
	"os"
	"path/filepath"

	"github.com/Neha611/commhub/internal/safe"
)

// migrations are applied in order; user_version records how far we got.
// They are append-only: never edit a shipped migration, add a new one.
var migrations = []string{
	`
CREATE TABLE providers (
  id           TEXT PRIMARY KEY,
  kind         TEXT NOT NULL,
  label        TEXT NOT NULL,
  scopes       TEXT NOT NULL DEFAULT '[]',
  status       TEXT NOT NULL DEFAULT 'ok',
  connected_at INTEGER NOT NULL
);

CREATE TABLE items (
  service      TEXT NOT NULL,
  provider_id  TEXT NOT NULL,
  external_id  TEXT NOT NULL,
  thread_id    TEXT NOT NULL DEFAULT '',
  title        TEXT NOT NULL DEFAULT '',
  sender       TEXT NOT NULL DEFAULT '',
  preview      TEXT NOT NULL DEFAULT '',
  ts           INTEGER NOT NULL,
  starts_at    INTEGER,
  unread       INTEGER NOT NULL DEFAULT 0,
  is_direct_to_me       INTEGER NOT NULL DEFAULT 0,
  is_to_me              INTEGER NOT NULL DEFAULT 0,
  is_thread_participant INTEGER NOT NULL DEFAULT 0,
  is_starred            INTEGER NOT NULL DEFAULT 0,
  is_bot                INTEGER NOT NULL DEFAULT 0,
  is_muted              INTEGER NOT NULL DEFAULT 0,
  is_list_mail          INTEGER NOT NULL DEFAULT 0,
  action_url   TEXT NOT NULL DEFAULT '',
  updated_at   INTEGER NOT NULL,
  PRIMARY KEY (service, provider_id, external_id)
);

CREATE INDEX items_ts        ON items(ts DESC);
CREATE INDEX items_unread    ON items(unread, ts DESC);
CREATE INDEX items_starts_at ON items(starts_at) WHERE starts_at IS NOT NULL;
CREATE INDEX items_provider  ON items(provider_id);

CREATE VIRTUAL TABLE items_fts USING fts5(
  title, sender, preview,
  item_key UNINDEXED,
  tokenize = 'unicode61'
);

CREATE TABLE sync_state (
  service     TEXT NOT NULL,
  provider_id TEXT NOT NULL,
  key         TEXT NOT NULL,
  value       TEXT NOT NULL,
  PRIMARY KEY (service, provider_id, key)
);
`,
}

func (s *Store) migrate(ctx context.Context) error {
	var version int
	if err := s.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	for i := version; i < len(migrations); i++ {
		tx, err := s.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, migrations[i]); err != nil {
			tx.Rollback()
			return err
		}
		// PRAGMA cannot be parameterised.
		if _, err := tx.ExecContext(ctx, "PRAGMA user_version = "+itoa(i+1)); err != nil {
			tx.Rollback()
			return err
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	return nil
}

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	var b []byte
	for i > 0 {
		b = append([]byte{byte('0' + i%10)}, b...)
		i /= 10
	}
	return string(b)
}

func dirOf(p string) string { return filepath.Dir(p) }

// ensureMode creates the database file with 0600 before SQLite opens it, so
// the cache is never briefly world-readable (SEC-04).
func ensureMode(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, safe.FileMode)
	if err == nil {
		return f.Close()
	}
	if os.IsExist(err) {
		return os.Chmod(path, safe.FileMode)
	}
	return err
}
