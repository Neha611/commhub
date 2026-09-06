// Package calendar reads Google Calendar, including the Meet link on an event.
//
// Meet needs no API and no scope of its own: a join link is a field on the
// event, so "join the meeting" is "open that URL".
package calendar

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"github.com/Neha611/commhub/internal/adapter"
	"github.com/Neha611/commhub/internal/provider/google"
	"github.com/Neha611/commhub/internal/store"
)

const (
	service  = "calendar"
	endpoint = "https://www.googleapis.com/calendar/v3/calendars/primary/events"

	// Bound the window so a decade of history never enters the cache.
	windowPast   = 24 * time.Hour
	windowFuture = 21 * 24 * time.Hour
)

type Adapter struct {
	p *google.Provider
}

func New(p *google.Provider) *Adapter { return &Adapter{p: p} }

func (a *Adapter) Descriptor() adapter.Descriptor {
	return adapter.Descriptor{
		Service: service, ProviderID: a.p.ID(), Label: a.p.Label(),
		Caps: adapter.Caps{CanOpenURL: true},
	}
}

func (a *Adapter) RequiredScopes(fs []adapter.Feature) []string { return google.ScopesFor(fs) }
func (a *Adapter) Close() error                                 { return nil }
func (a *Adapter) Interval() time.Duration                      { return 60 * time.Second }

func (a *Adapter) MarkRead(context.Context, store.Item) error { return nil }
func (a *Adapter) Reply(context.Context, store.Item, adapter.Draft) error {
	return fmt.Errorf("calendar events cannot be replied to")
}

type eventTime struct {
	DateTime string `json:"dateTime"`
	Date     string `json:"date"`
	TimeZone string `json:"timeZone"`
}

type attendee struct {
	Self           bool   `json:"self"`
	ResponseStatus string `json:"responseStatus"`
}

type event struct {
	ID        string    `json:"id"`
	Status    string    `json:"status"`
	Summary   string    `json:"summary"`
	HTMLLink  string    `json:"htmlLink"`
	Start     eventTime `json:"start"`
	End       eventTime `json:"end"`
	Recurring string    `json:"recurringEventId"`

	HangoutLink    string `json:"hangoutLink"`
	ConferenceData struct {
		EntryPoints []struct {
			Type string `json:"entryPointType"`
			URI  string `json:"uri"`
		} `json:"entryPoints"`
	} `json:"conferenceData"`

	Attendees []attendee `json:"attendees"`
	Organizer struct {
		DisplayName string `json:"displayName"`
		Email       string `json:"email"`
	} `json:"organizer"`
}

type listResponse struct {
	Items         []event `json:"items"`
	NextPageToken string  `json:"nextPageToken"`
	NextSyncToken string  `json:"nextSyncToken"`
}

// Sync pulls events incrementally when Google gives us a sync token, and falls
// back to a bounded window when it does not — or when the token has expired,
// which Google signals with 410 Gone.
func (a *Adapter) Sync(ctx context.Context, st *store.Store) error {
	token, _ := st.GetSyncState(ctx, service, a.p.ID(), "sync_token")

	items, deleted, next, err := a.fetch(ctx, token)
	if err != nil {
		var apiErr *google.APIError
		if errorsAs(err, &apiErr) && apiErr.CursorStale() && token != "" {
			// The token expired. Drop it and take the full window once.
			_ = st.SetSyncState(ctx, service, a.p.ID(), "sync_token", "")
			items, deleted, next, err = a.fetch(ctx, "")
		}
		if err != nil {
			return err
		}
	}

	for _, id := range deleted {
		if err := st.DeleteItem(ctx, service, a.p.ID(), id); err != nil {
			return err
		}
	}
	if err := st.UpsertItems(ctx, items); err != nil {
		return err
	}
	if next != "" {
		return st.SetSyncState(ctx, service, a.p.ID(), "sync_token", next)
	}
	return nil
}

func (a *Adapter) fetch(ctx context.Context, syncToken string) (items []store.Item, deleted []string, next string, err error) {
	page := ""
	for {
		params := url.Values{}
		params.Set("singleEvents", "true") // expand recurrences into occurrences
		params.Set("maxResults", "250")
		if syncToken != "" {
			// Sync tokens cannot be combined with timeMin/timeMax/orderBy; the
			// token already remembers the window from the initial request.
			params.Set("syncToken", syncToken)
			params.Set("showDeleted", "true")
		} else {
			now := time.Now()
			params.Set("timeMin", now.Add(-windowPast).Format(time.RFC3339))
			params.Set("timeMax", now.Add(windowFuture).Format(time.RFC3339))
			params.Set("orderBy", "startTime")
		}
		if page != "" {
			params.Set("pageToken", page)
		}

		var resp listResponse
		if err = a.p.Get(ctx, endpoint, params, &resp); err != nil {
			return nil, nil, "", err
		}
		for _, e := range resp.Items {
			if e.Status == "cancelled" {
				deleted = append(deleted, e.ID)
				continue
			}
			it, ok := a.toItem(e)
			if !ok {
				continue
			}
			items = append(items, it)
		}
		if resp.NextSyncToken != "" {
			next = resp.NextSyncToken
		}
		if resp.NextPageToken == "" {
			return items, deleted, next, nil
		}
		page = resp.NextPageToken
	}
}

// toItem converts an event, dropping the ones that would make the countdown
// wrong: all-day entries, and invitations the user has declined.
func (a *Adapter) toItem(e event) (store.Item, bool) {
	if e.Start.DateTime == "" {
		return store.Item{}, false // all-day or date-only
	}
	for _, at := range e.Attendees {
		if at.Self && at.ResponseStatus == "declined" {
			return store.Item{}, false
		}
	}
	start, err := time.Parse(time.RFC3339, e.Start.DateTime)
	if err != nil {
		return store.Item{}, false
	}

	join := e.HangoutLink
	for _, ep := range e.ConferenceData.EntryPoints {
		if ep.Type == "video" && ep.URI != "" {
			join = ep.URI
			break
		}
	}
	if join == "" {
		join = e.HTMLLink
	}

	dur := ""
	if e.End.DateTime != "" {
		if end, err := time.Parse(time.RFC3339, e.End.DateTime); err == nil {
			dur = strconv.Itoa(int(end.Sub(start).Minutes())) + " min"
		}
	}

	thread := e.ID
	if e.Recurring != "" {
		thread = e.Recurring // occurrences of one series collapse to a row
	}
	title := e.Summary
	if title == "" {
		title = "(no title)"
	}

	return store.Item{
		Service: service, ProviderID: a.p.ID(), ExternalID: e.ID, ThreadID: thread,
		Title: title, Sender: dur, Preview: start.Local().Format("Mon 15:04"),
		Timestamp: start, StartsAt: &start,
		ActionURL: join, UpdatedAt: time.Now(),
	}, true
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
