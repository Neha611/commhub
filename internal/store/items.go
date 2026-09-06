package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"
	"time"

	"github.com/Neha611/commhub/internal/safe"
)

// UpsertItems writes a batch atomically and keeps the search index in step.
//
// Every text field is sanitised and every URL is allowlisted HERE, at ingest,
// so hostile content never reaches the store and therefore can never reach the
// renderer or the URL opener (SEC-01, SEC-09).
func (s *Store) UpsertItems(ctx context.Context, items []Item) error {
	if len(items) == 0 {
		return nil
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	const upsert = `
INSERT INTO items (service, provider_id, external_id, thread_id, title, sender, preview,
                   ts, starts_at, unread, is_direct_to_me, is_to_me, is_thread_participant,
                   is_starred, is_bot, is_muted, is_list_mail, action_url, updated_at)
VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
ON CONFLICT(service, provider_id, external_id) DO UPDATE SET
  thread_id=excluded.thread_id, title=excluded.title, sender=excluded.sender,
  preview=excluded.preview, ts=excluded.ts, starts_at=excluded.starts_at,
  unread=excluded.unread, is_direct_to_me=excluded.is_direct_to_me,
  is_to_me=excluded.is_to_me, is_thread_participant=excluded.is_thread_participant,
  is_starred=excluded.is_starred, is_bot=excluded.is_bot, is_muted=excluded.is_muted,
  is_list_mail=excluded.is_list_mail, action_url=excluded.action_url,
  updated_at=excluded.updated_at`

	stmt, err := tx.PrepareContext(ctx, upsert)
	if err != nil {
		return err
	}
	defer stmt.Close()

	ftsDel, err := tx.PrepareContext(ctx, `DELETE FROM items_fts WHERE item_key = ?`)
	if err != nil {
		return err
	}
	defer ftsDel.Close()
	ftsIns, err := tx.PrepareContext(ctx,
		`INSERT INTO items_fts (title, sender, preview, item_key) VALUES (?,?,?,?)`)
	if err != nil {
		return err
	}
	defer ftsIns.Close()

	for _, it := range items {
		it = Clean(it)
		var starts any
		if it.StartsAt != nil {
			starts = it.StartsAt.Unix()
		}
		if it.UpdatedAt.IsZero() {
			it.UpdatedAt = time.Now()
		}
		_, err := stmt.ExecContext(ctx,
			it.Service, it.ProviderID, it.ExternalID, it.ThreadID, it.Title, it.Sender, it.Preview,
			it.Timestamp.Unix(), starts, b2i(it.Unread), b2i(it.IsDirectToMe), b2i(it.IsToMe),
			b2i(it.IsThreadParticipant), b2i(it.IsStarred), b2i(it.IsBot), b2i(it.IsMuted),
			b2i(it.IsListMail), it.ActionURL, it.UpdatedAt.Unix())
		if err != nil {
			return err
		}
		if _, err := ftsDel.ExecContext(ctx, it.Key()); err != nil {
			return err
		}
		if _, err := ftsIns.ExecContext(ctx, it.Title, it.Sender, it.Preview, it.Key()); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Clean is the ingest boundary. It is exported so adapters and their tests can
// assert that nothing reaches the store unsanitised.
func Clean(it Item) Item {
	it.Title = safe.Text(it.Title)
	it.Sender = safe.Text(it.Sender)
	it.Preview = safe.Text(it.Preview)
	it.ThreadID = safe.Text(it.ThreadID)
	if it.ActionURL != "" {
		clean, err := safe.CheckURL(it.ActionURL)
		if err != nil {
			it.ActionURL = "" // drop it rather than store something unopenable
		} else {
			it.ActionURL = clean
		}
	}
	return it
}

// Recent returns items for ranking: everything unread or touched lately, plus
// every upcoming meeting regardless of age.
func (s *Store) Recent(ctx context.Context, since time.Duration, limit int) ([]Item, error) {
	cutoff := time.Now().Add(-since).Unix()
	rows, err := s.db.QueryContext(ctx, `
SELECT service, provider_id, external_id, thread_id, title, sender, preview, ts, starts_at,
       unread, is_direct_to_me, is_to_me, is_thread_participant, is_starred, is_bot,
       is_muted, is_list_mail, action_url, updated_at
FROM items
WHERE ts >= ? OR unread = 1 OR (starts_at IS NOT NULL AND starts_at >= ?)
ORDER BY ts DESC
LIMIT ?`, cutoff, time.Now().Add(-1*time.Hour).Unix(), limit)
	if err != nil {
		return nil, err
	}
	return scanItems(rows)
}

// Search runs the local FTS index. User input is passed as a bound parameter
// and quoted as an FTS5 string literal, so neither SQL nor the FTS grammar can
// be injected (SEC-11).
func (s *Store) Search(ctx context.Context, q string, limit int) ([]Item, error) {
	q = strings.TrimSpace(q)
	if q == "" {
		return nil, nil
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT i.service, i.provider_id, i.external_id, i.thread_id, i.title, i.sender, i.preview,
       i.ts, i.starts_at, i.unread, i.is_direct_to_me, i.is_to_me, i.is_thread_participant,
       i.is_starred, i.is_bot, i.is_muted, i.is_list_mail, i.action_url, i.updated_at
FROM items_fts f
JOIN items i ON i.service || '|' || i.provider_id || '|' || i.external_id = f.item_key
WHERE items_fts MATCH ?
ORDER BY i.ts DESC
LIMIT ?`, ftsQuote(q), limit)
	if err != nil {
		return nil, err
	}
	return scanItems(rows)
}

// ftsQuote turns arbitrary user input into a safe FTS5 prefix query. Wrapping
// each term in double quotes makes operators, hyphens and parentheses literal.
func ftsQuote(q string) string {
	fields := strings.Fields(q)
	parts := make([]string, 0, len(fields))
	for _, f := range fields {
		f = strings.ReplaceAll(f, `"`, `""`)
		parts = append(parts, `"`+f+`"*`)
	}
	return strings.Join(parts, " ")
}

func (s *Store) MarkRead(ctx context.Context, it Item) error {
	_, err := s.db.ExecContext(ctx,
		`UPDATE items SET unread = 0, updated_at = ? WHERE service=? AND provider_id=? AND external_id=?`,
		time.Now().Unix(), it.Service, it.ProviderID, it.ExternalID)
	return err
}

// DeleteProvider removes an account's cached rows. Called by disconnect, and
// must work even when the config section has already gone.
func (s *Store) DeleteProvider(ctx context.Context, providerID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM items_fts WHERE item_key IN (
		   SELECT service || '|' || provider_id || '|' || external_id FROM items WHERE provider_id = ?)`,
		providerID); err != nil {
		return err
	}
	for _, q := range []string{
		`DELETE FROM items WHERE provider_id = ?`,
		`DELETE FROM sync_state WHERE provider_id = ?`,
		`DELETE FROM providers WHERE id = ?`,
	} {
		if _, err := tx.ExecContext(ctx, q, providerID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Purge drops cached bodies older than the retention window (SEC-05) and can
// wipe the cache entirely without disconnecting.
func (s *Store) Purge(ctx context.Context, olderThan time.Duration, all bool) (int64, error) {
	if all {
		res, err := s.db.ExecContext(ctx, `DELETE FROM items`)
		if err != nil {
			return 0, err
		}
		if _, err := s.db.ExecContext(ctx, `DELETE FROM items_fts`); err != nil {
			return 0, err
		}
		return res.RowsAffected()
	}
	cutoff := time.Now().Add(-olderThan).Unix()
	res, err := s.db.ExecContext(ctx,
		`UPDATE items SET preview = '' WHERE ts < ? AND preview != ''`, cutoff)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

func (s *Store) UpsertProvider(ctx context.Context, p Provider) error {
	scopes, _ := json.Marshal(p.Scopes)
	if p.ConnectedAt.IsZero() {
		p.ConnectedAt = time.Now()
	}
	if p.Status == "" {
		p.Status = "ok"
	}
	_, err := s.db.ExecContext(ctx, `
INSERT INTO providers (id, kind, label, scopes, status, connected_at) VALUES (?,?,?,?,?,?)
ON CONFLICT(id) DO UPDATE SET kind=excluded.kind, label=excluded.label,
  scopes=excluded.scopes, status=excluded.status`,
		p.ID, p.Kind, safe.Text(p.Label), string(scopes), p.Status, p.ConnectedAt.Unix())
	return err
}

func (s *Store) Providers(ctx context.Context) ([]Provider, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT id, kind, label, scopes, status, connected_at FROM providers ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Provider
	for rows.Next() {
		var p Provider
		var scopes string
		var ts int64
		if err := rows.Scan(&p.ID, &p.Kind, &p.Label, &scopes, &p.Status, &ts); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(scopes), &p.Scopes)
		p.ConnectedAt = time.Unix(ts, 0)
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) GetSyncState(ctx context.Context, service, providerID, key string) (string, error) {
	var v string
	err := s.db.QueryRowContext(ctx,
		`SELECT value FROM sync_state WHERE service=? AND provider_id=? AND key=?`,
		service, providerID, key).Scan(&v)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return v, err
}

func (s *Store) SetSyncState(ctx context.Context, service, providerID, key, value string) error {
	_, err := s.db.ExecContext(ctx, `
INSERT INTO sync_state (service, provider_id, key, value) VALUES (?,?,?,?)
ON CONFLICT(service, provider_id, key) DO UPDATE SET value=excluded.value`,
		service, providerID, key, value)
	return err
}

func (s *Store) Counts(ctx context.Context) (map[string]int, error) {
	rows, err := s.db.QueryContext(ctx,
		`SELECT service, COUNT(*) FROM items WHERE unread = 1 GROUP BY service`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var svc string
		var n int
		if err := rows.Scan(&svc, &n); err != nil {
			return nil, err
		}
		out[svc] = n
	}
	return out, rows.Err()
}

func scanItems(rows *sql.Rows) ([]Item, error) {
	defer rows.Close()
	var out []Item
	for rows.Next() {
		var it Item
		var ts, updated int64
		var starts sql.NullInt64
		var unread, dm, tome, thread, star, bot, muted, list int
		if err := rows.Scan(&it.Service, &it.ProviderID, &it.ExternalID, &it.ThreadID,
			&it.Title, &it.Sender, &it.Preview, &ts, &starts, &unread, &dm, &tome,
			&thread, &star, &bot, &muted, &list, &it.ActionURL, &updated); err != nil {
			return nil, err
		}
		it.Timestamp = time.Unix(ts, 0)
		it.UpdatedAt = time.Unix(updated, 0)
		if starts.Valid {
			t := time.Unix(starts.Int64, 0)
			it.StartsAt = &t
		}
		it.Unread = unread == 1
		it.IsDirectToMe = dm == 1
		it.IsToMe = tome == 1
		it.IsThreadParticipant = thread == 1
		it.IsStarred = star == 1
		it.IsBot = bot == 1
		it.IsMuted = muted == 1
		it.IsListMail = list == 1
		out = append(out, it)
	}
	return out, rows.Err()
}

func b2i(b bool) int {
	if b {
		return 1
	}
	return 0
}

// DeleteItem removes a single row and its search entry. Calendar uses it when
// an event is cancelled upstream.
func (s *Store) DeleteItem(ctx context.Context, service, providerID, externalID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	key := service + "|" + providerID + "|" + externalID
	if _, err := tx.ExecContext(ctx, `DELETE FROM items_fts WHERE item_key = ?`, key); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`DELETE FROM items WHERE service=? AND provider_id=? AND external_id=?`,
		service, providerID, externalID); err != nil {
		return err
	}
	return tx.Commit()
}
