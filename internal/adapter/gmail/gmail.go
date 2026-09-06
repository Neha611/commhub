// Package gmail reads Gmail through the REST API.
//
// The default scope is gmail.metadata: senders, subjects and labels, but never
// message bodies. That is why the store holds no body text on a fresh install —
// it is a consequence of the scope ladder, not a separate mechanism.
package gmail

import (
	"context"
	"encoding/base64"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Neha611/commhub/internal/adapter"
	"github.com/Neha611/commhub/internal/provider/google"
	"github.com/Neha611/commhub/internal/store"
)

const (
	service = "gmail"
	apiBase = "https://gmail.googleapis.com/gmail/v1/users/me"

	initialMessages = 60 // bounded first sync; a full archive would never finish
	fetchWorkers    = 4
)

type Adapter struct {
	p        *google.Provider
	features []adapter.Feature
	me       string // the account's own address, for To:/Cc: reasoning
}

func New(p *google.Provider, features []adapter.Feature, me string) *Adapter {
	return &Adapter{p: p, features: features, me: strings.ToLower(me)}
}

func (a *Adapter) Descriptor() adapter.Descriptor {
	return adapter.Descriptor{
		Service: service, ProviderID: a.p.ID(), Label: a.p.Label(),
		Caps: adapter.Caps{
			CanOpenURL:  true,
			CanMarkRead: google.Has(a.features, adapter.FeatureMarkRead),
			CanReply:    google.Has(a.features, adapter.FeatureReply),
		},
	}
}

func (a *Adapter) RequiredScopes(fs []adapter.Feature) []string { return google.ScopesFor(fs) }
func (a *Adapter) Close() error                                 { return nil }
func (a *Adapter) Interval() time.Duration                      { return 45 * time.Second }

// Profile returns the account's own email address. It doubles as the
// connection check in the wizard, and avoids requesting an openid/email scope
// purely to learn who we are.
func Profile(ctx context.Context, p *google.Provider) (string, string, error) {
	var out struct {
		EmailAddress string `json:"emailAddress"`
		HistoryID    uint64 `json:"historyId"`
	}
	if err := p.Get(ctx, apiBase+"/profile", nil, &out); err != nil {
		return "", "", err
	}
	return out.EmailAddress, strconv.FormatUint(out.HistoryID, 10), nil
}

type messageRef struct {
	ID       string `json:"id"`
	ThreadID string `json:"threadId"`
}

type message struct {
	ID           string   `json:"id"`
	ThreadID     string   `json:"threadId"`
	LabelIDs     []string `json:"labelIds"`
	Snippet      string   `json:"snippet"`
	InternalDate string   `json:"internalDate"`
	Payload      struct {
		Headers []struct {
			Name  string `json:"name"`
			Value string `json:"value"`
		} `json:"headers"`
	} `json:"payload"`
}

func (m message) header(name string) string {
	for _, h := range m.Payload.Headers {
		if strings.EqualFold(h.Name, name) {
			return h.Value
		}
	}
	return ""
}

func (m message) hasLabel(l string) bool {
	for _, x := range m.LabelIDs {
		if x == l {
			return true
		}
	}
	return false
}

// Sync advances from the stored historyId when there is one, and takes a
// bounded first page when there is not. A 404 means the cursor aged out of
// Google's history window, which is a full resync rather than an error.
func (a *Adapter) Sync(ctx context.Context, st *store.Store) error {
	histID, _ := st.GetSyncState(ctx, service, a.p.ID(), "history_id")

	var ids []string
	var err error
	if histID != "" {
		ids, err = a.changedSince(ctx, histID)
		var apiErr *google.APIError
		if errorsAs(err, &apiErr) && apiErr.CursorStale() {
			histID, err = "", nil
		}
		if err != nil {
			return err
		}
	}
	if histID == "" {
		ids, err = a.inboxPage(ctx, initialMessages)
		if err != nil {
			return err
		}
	}
	if len(ids) > 0 {
		items, err := a.fetchAll(ctx, ids)
		if err != nil {
			return err
		}
		if err := st.UpsertItems(ctx, items); err != nil {
			return err
		}
	}

	// Record where to resume. The profile's historyId is the newest point, so
	// taking it after the fetch means nothing between is missed.
	_, newest, err := Profile(ctx, a.p)
	if err != nil {
		return err
	}
	return st.SetSyncState(ctx, service, a.p.ID(), "history_id", newest)
}

func (a *Adapter) inboxPage(ctx context.Context, max int) ([]string, error) {
	params := url.Values{}
	params.Set("labelIds", "INBOX")
	params.Set("maxResults", strconv.Itoa(max))
	var resp struct {
		Messages []messageRef `json:"messages"`
	}
	if err := a.p.Get(ctx, apiBase+"/messages", params, &resp); err != nil {
		return nil, err
	}
	ids := make([]string, 0, len(resp.Messages))
	for _, m := range resp.Messages {
		ids = append(ids, m.ID)
	}
	return ids, nil
}

func (a *Adapter) changedSince(ctx context.Context, historyID string) ([]string, error) {
	params := url.Values{}
	params.Set("startHistoryId", historyID)
	params.Set("labelId", "INBOX")
	for _, t := range []string{"messageAdded", "labelAdded", "labelRemoved"} {
		params.Add("historyTypes", t)
	}
	var resp struct {
		History []struct {
			Messages      []messageRef `json:"messages"`
			MessagesAdded []struct {
				Message messageRef `json:"message"`
			} `json:"messagesAdded"`
			LabelsAdded []struct {
				Message messageRef `json:"message"`
			} `json:"labelsAdded"`
			LabelsRemoved []struct {
				Message messageRef `json:"message"`
			} `json:"labelsRemoved"`
		} `json:"history"`
	}
	if err := a.p.Get(ctx, apiBase+"/history", params, &resp); err != nil {
		return nil, err
	}
	seen := map[string]bool{}
	var ids []string
	add := func(id string) {
		if id != "" && !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	for _, h := range resp.History {
		for _, m := range h.Messages {
			add(m.ID)
		}
		for _, m := range h.MessagesAdded {
			add(m.Message.ID)
		}
		for _, m := range h.LabelsAdded {
			add(m.Message.ID)
		}
		for _, m := range h.LabelsRemoved {
			add(m.Message.ID)
		}
	}
	return ids, nil
}

// fetchAll retrieves message metadata concurrently, with a small worker pool:
// enough to make a first sync quick, few enough to stay well inside a personal
// project's quota.
func (a *Adapter) fetchAll(ctx context.Context, ids []string) ([]store.Item, error) {
	type result struct {
		item store.Item
		err  error
	}
	jobs := make(chan string)
	results := make(chan result, len(ids))

	var wg sync.WaitGroup
	for range fetchWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for id := range jobs {
				m, err := a.get(ctx, id)
				if err != nil {
					results <- result{err: err}
					continue
				}
				results <- result{item: a.toItem(m)}
			}
		}()
	}
	go func() {
		defer close(jobs)
		for _, id := range ids {
			select {
			case <-ctx.Done():
				return
			case jobs <- id:
			}
		}
	}()
	wg.Wait()
	close(results)

	var items []store.Item
	var firstErr error
	for r := range results {
		if r.err != nil {
			if firstErr == nil {
				firstErr = r.err
			}
			continue
		}
		items = append(items, r.item)
	}
	// One message failing should not lose the rest of the sync.
	if len(items) == 0 && firstErr != nil {
		return nil, firstErr
	}
	return items, nil
}

func (a *Adapter) get(ctx context.Context, id string) (message, error) {
	params := url.Values{}
	// format=metadata is all the default scope permits, and all triage needs.
	params.Set("format", "metadata")
	for _, h := range []string{"From", "To", "Cc", "Subject", "Date", "List-Unsubscribe", "List-Id", "Message-ID"} {
		params.Add("metadataHeaders", h)
	}
	var m message
	err := a.p.Get(ctx, apiBase+"/messages/"+url.PathEscape(id), params, &m)
	return m, err
}

func (a *Adapter) toItem(m message) store.Item {
	ts := time.Now()
	if ms, err := strconv.ParseInt(m.InternalDate, 10, 64); err == nil {
		ts = time.UnixMilli(ms)
	}
	from := m.header("From")
	to := strings.ToLower(m.header("To"))
	cc := strings.ToLower(m.header("Cc"))

	toMe := strings.Contains(to, a.me)
	// "Direct" means the message is aimed at you specifically, not at a group
	// you happen to be in — that distinction is what makes the ranking useful.
	direct := toMe && countAddresses(to) == 1 && cc == ""

	isList := m.header("List-Unsubscribe") != "" || m.header("List-Id") != "" ||
		m.hasLabel("CATEGORY_PROMOTIONS") || m.hasLabel("CATEGORY_FORUMS")

	subject := m.header("Subject")
	if subject == "" {
		subject = "(no subject)"
	}

	return store.Item{
		Service: service, ProviderID: a.p.ID(), ExternalID: m.ID, ThreadID: m.ThreadID,
		Title:  subject,
		Sender: displayName(from),
		// Empty under gmail.metadata; populated only when the user has opted
		// into gmail.readonly.
		Preview:      m.Snippet,
		Timestamp:    ts,
		Unread:       m.hasLabel("UNREAD"),
		IsStarred:    m.hasLabel("STARRED") || m.hasLabel("IMPORTANT"),
		IsToMe:       toMe,
		IsDirectToMe: direct,
		IsBot:        looksAutomated(from) || m.hasLabel("CATEGORY_UPDATES"),
		IsListMail:   isList,
		ActionURL:    "https://mail.google.com/mail/u/0/#inbox/" + m.ThreadID,
		UpdatedAt:    time.Now(),
	}
}

func (a *Adapter) MarkRead(ctx context.Context, it store.Item) error {
	if !google.Has(a.features, adapter.FeatureMarkRead) {
		return fmt.Errorf("marking read is not enabled — run `commhub enable markread`")
	}
	body := map[string][]string{"removeLabelIds": {"UNREAD"}}
	return a.p.PostJSON(ctx, apiBase+"/messages/"+url.PathEscape(it.ExternalID)+"/modify", body, nil)
}

// Reply sends through the Gmail API rather than SMTP: no second credential, no
// host and port to collect, and threading handled by passing threadId with the
// In-Reply-To and References headers Gmail expects.
func (a *Adapter) Reply(ctx context.Context, it store.Item, d adapter.Draft) error {
	if !google.Has(a.features, adapter.FeatureReply) {
		return fmt.Errorf("replying is not enabled — run `commhub enable reply`")
	}
	orig, err := a.get(ctx, it.ExternalID)
	if err != nil {
		return err
	}
	to := orig.header("Reply-To")
	if to == "" {
		to = orig.header("From")
	}
	msgID := orig.header("Message-ID")
	refs := strings.TrimSpace(orig.header("References") + " " + msgID)

	subject := d.Subject
	if subject == "" {
		subject = "Re: " + orig.header("Subject")
	}

	var b strings.Builder
	fmt.Fprintf(&b, "To: %s\r\n", to)
	fmt.Fprintf(&b, "Subject: %s\r\n", subject)
	if msgID != "" {
		// Without these two headers the reply starts a new thread in every
		// other mail client, even though Gmail would group it.
		fmt.Fprintf(&b, "In-Reply-To: %s\r\n", msgID)
		fmt.Fprintf(&b, "References: %s\r\n", refs)
	}
	b.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=UTF-8\r\n\r\n")
	b.WriteString(d.Body)
	b.WriteString("\r\n")

	payload := map[string]string{
		"raw":      base64.URLEncoding.EncodeToString([]byte(b.String())),
		"threadId": it.ThreadID,
	}
	return a.p.PostJSON(ctx, apiBase+"/messages/send", payload, nil)
}

func displayName(from string) string {
	from = strings.TrimSpace(from)
	if i := strings.LastIndex(from, "<"); i > 0 {
		name := strings.TrimSpace(strings.Trim(from[:i], `" `))
		if name != "" {
			return name
		}
		return strings.Trim(from[i+1:], "<> ")
	}
	return strings.Trim(from, "<> ")
}

func countAddresses(header string) int {
	if strings.TrimSpace(header) == "" {
		return 0
	}
	return strings.Count(header, ",") + 1
}

func looksAutomated(from string) bool {
	f := strings.ToLower(from)
	for _, s := range []string{"noreply", "no-reply", "donotreply", "do-not-reply",
		"mailer-daemon", "notifications@", "automated", "bounce"} {
		if strings.Contains(f, s) {
			return true
		}
	}
	return false
}

func errorsAs(err error, target **google.APIError) bool {
	for err != nil {
		if e, ok := err.(*google.APIError); ok {
			*target = e
			return true
		}
		u, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = u.Unwrap()
	}
	return false
}
