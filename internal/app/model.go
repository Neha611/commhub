// Package app is the Bubble Tea root: state, keybinds, and navigation.
//
// It reads only from the store. Sync runs in commands (goroutines) that write
// to the store and then signal a reload, so no network call can ever block the
// render loop.
package app

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Neha611/commhub/internal/adapter"
	"github.com/Neha611/commhub/internal/config"
	"github.com/Neha611/commhub/internal/provider/google"
	"github.com/Neha611/commhub/internal/safe"
	"github.com/Neha611/commhub/internal/secrets"
	"github.com/Neha611/commhub/internal/store"
	tea "github.com/charmbracelet/bubbletea"
)

type mode int

const (
	modeNormal mode = iota
	modeSearch
	modeHelp
	modeConfirmOpen
	modeWelcome
)

type paneID string

const (
	panePriority paneID = "priority"
)

type pane struct {
	id     paneID
	title  string
	items  []store.Ranked
	cursor int
}

type Model struct {
	st  *store.Store
	cfg config.Config
	ads []adapter.Adapter

	panes  []pane
	active int

	mode        mode
	search      string
	next        *store.Item
	counts      map[string]int
	status      string
	statusUntil time.Time
	pendingG    bool
	confirmURL  string

	demo          bool
	needsReauth   bool
	optCursor     int
	be            secrets.Backend
	width, height int
	now           time.Time
	err           error
}

func New(st *store.Store, cfg config.Config, ads []adapter.Adapter, be secrets.Backend) Model {
	m := Model{st: st, cfg: cfg, ads: ads, be: be, now: time.Now(), counts: map[string]int{}}
	for _, a := range ads {
		if a.Descriptor().ProviderID == "fake:demo" {
			m.demo = true
		}
	}
	m.panes = append(m.panes, pane{id: panePriority, title: "Priority"})
	for _, s := range Services(ads) {
		m.panes = append(m.panes, pane{id: paneID(s), title: titleFor(s)})
	}
	if len(ads) == 0 {
		m.mode = modeWelcome
	}
	return m
}

func titleFor(s string) string {
	switch s {
	case "gmail":
		return "Mail"
	case "calendar":
		return "Calendar"
	default:
		return s
	}
}

func (m Model) Connected() bool { return len(m.ads) > 0 }

// Messages.

type loadedMsg struct {
	priority []store.Ranked
	byPane   map[paneID][]store.Ranked
	next     *store.Item
	counts   map[string]int
	err      error
}
type syncedMsg struct{ err error }
type tickMsg time.Time
type statusMsg string
type reloadMsg struct{}
type reloadedMsg struct {
	cfg  config.Config
	ads  []adapter.Adapter
	errs []error
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(m.load(), m.sync(), tick())
}

func tick() tea.Cmd {
	return tea.Tick(20*time.Second, func(t time.Time) tea.Msg { return tickMsg(t) })
}

// load reads the store off the render path and returns everything the panes
// need in one message.
func (m Model) load() tea.Cmd {
	st, cfg, search := m.st, m.cfg, m.search
	// Copy out just the pane identifiers, on this goroutine. Capturing m.panes
	// itself would have the background goroutine range over the same backing
	// array the update loop writes items into — a data race the tests cannot
	// reach, because they drive load() synchronously.
	ids := make([]paneID, 0, len(m.panes))
	for _, p := range m.panes {
		ids = append(ids, p.id)
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		out := loadedMsg{byPane: map[paneID][]store.Ranked{}}

		var items []store.Ranked
		var err error
		if search != "" {
			raw, e := st.Search(ctx, search, 500)
			err = e
			items = store.Rank(raw, cfg.Priority, time.Now())
		} else {
			items, err = st.Priority(ctx, cfg.Priority, 200)
		}
		if err != nil {
			out.err = err
			return out
		}
		out.priority = items
		for _, id := range ids {
			if id == panePriority {
				continue
			}
			var sub []store.Ranked
			for _, it := range items {
				if it.Service == string(id) {
					sub = append(sub, it)
				}
			}
			out.byPane[id] = sub
		}
		out.next, _ = st.NextMeeting(ctx)
		out.counts, _ = st.Counts(ctx)
		return out
	}
}

// sync fans out to every adapter in its own goroutine. Failures are surfaced,
// never fatal: one degraded account must not blank another pane.
func (m Model) sync() tea.Cmd {
	ads, st := m.ads, m.st
	if len(ads) == 0 {
		return nil
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		var firstErr error
		for _, a := range ads {
			if err := a.Sync(ctx, st); err != nil && firstErr == nil {
				firstErr = err
			}
		}
		return syncedMsg{err: firstErr}
	}
}

// reload re-reads config and rebuilds the adapter set, which is what makes a
// service connected from the welcome screen appear without a restart.
func (m Model) reload() tea.Cmd {
	be := m.be
	return func() tea.Msg {
		cfg, err := config.Load()
		if err != nil {
			return statusMsg("reload: " + err.Error())
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		ads, errs := Build(ctx, cfg, be)
		return reloadedMsg{cfg: cfg, ads: ads, errs: errs}
	}
}

func (m *Model) rebuildPanes() {
	m.panes = []pane{{id: panePriority, title: "Priority"}}
	for _, s := range Services(m.ads) {
		m.panes = append(m.panes, pane{id: paneID(s), title: titleFor(s)})
	}
	m.active = 0
	m.demo = false
	for _, a := range m.ads {
		if a.Descriptor().ProviderID == "fake:demo" {
			m.demo = true
		}
	}
}

func (m *Model) flash(s string) {
	m.status = s
	m.statusUntil = time.Now().Add(6 * time.Second)
}

func (m Model) current() *pane {
	if m.active < 0 || m.active >= len(m.panes) {
		return nil
	}
	return &m.panes[m.active]
}

func (m Model) selected() (store.Ranked, bool) {
	p := m.current()
	if p == nil || len(p.items) == 0 || p.cursor >= len(p.items) {
		return store.Ranked{}, false
	}
	return p.items[p.cursor], true
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
		return m, nil

	case tickMsg:
		m.now = time.Time(msg)
		return m, tea.Batch(tick(), m.load())

	case loadedMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.err = nil
		for i := range m.panes {
			if m.panes[i].id == panePriority {
				m.panes[i].items = msg.priority
			} else {
				m.panes[i].items = msg.byPane[m.panes[i].id]
			}
			if m.panes[i].cursor >= len(m.panes[i].items) {
				m.panes[i].cursor = max(0, len(m.panes[i].items)-1)
			}
		}
		m.next = msg.next
		m.counts = msg.counts
		return m, nil

	case syncedMsg:
		if msg.err != nil {
			// Google revokes refresh tokens after seven days while a project is
			// in "Testing", which is where most self-hosted setups live. That
			// is a one-command fix, so it must not read like a failure.
			if isExpiredCredential(msg.err) {
				m.needsReauth = true
			} else {
				m.flash("sync: " + msg.err.Error())
			}
		}
		return m, m.load()

	case statusMsg:
		m.flash(string(msg))
		return m, nil

	case reloadMsg:
		return m, m.reload()

	case reloadedMsg:
		m.cfg = msg.cfg
		m.ads = msg.ads
		m.needsReauth = false
		m.rebuildPanes()
		if len(msg.errs) > 0 {
			m.flash(msg.errs[0].Error())
		} else if m.Connected() {
			m.mode = modeNormal
			m.flash("connected — syncing")
		}
		return m, tea.Batch(m.sync(), m.load())

	case tea.KeyMsg:
		if m.mode == modeWelcome {
			return m.onWelcomeKey(msg)
		}
		return m.onKey(msg)
	}
	return m, nil
}

func (m Model) onKey(k tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Search capture takes precedence over every other binding.
	if m.mode == modeSearch {
		switch k.Type {
		case tea.KeyEnter:
			m.mode = modeNormal
			return m, m.load()
		case tea.KeyEsc:
			m.mode, m.search = modeNormal, ""
			return m, m.load()
		case tea.KeyBackspace:
			if m.search != "" {
				m.search = m.search[:len(m.search)-1]
			}
			return m, m.load()
		case tea.KeyRunes, tea.KeySpace:
			m.search += string(k.Runes)
			if k.Type == tea.KeySpace {
				m.search += " "
			}
			return m, m.load()
		}
		return m, nil
	}

	if m.mode == modeConfirmOpen {
		switch k.String() {
		case "y", "enter":
			url := m.confirmURL
			m.mode, m.confirmURL = modeNormal, ""
			return m, openCmd(url)
		default:
			m.mode, m.confirmURL = modeNormal, ""
			return m, nil
		}
	}

	// g-prefixed jumps: gp priority, gm mail, gc calendar.
	if m.pendingG {
		m.pendingG = false
		switch k.String() {
		case "p":
			m.jump(panePriority)
		case "m":
			m.jump(paneID("gmail"))
		case "c":
			m.jump(paneID("calendar"))
		case "g":
			if p := m.current(); p != nil {
				m.panes[m.active].cursor = 0
			}
		}
		return m, nil
	}

	switch k.String() {
	case "q", "ctrl+c":
		return m, tea.Quit
	case "A":
		m.mode = modeWelcome
		m.optCursor = 0
		return m, nil
	case "?":
		if m.mode == modeHelp {
			m.mode = modeNormal
		} else {
			m.mode = modeHelp
		}
		return m, nil
	case "esc":
		if m.mode == modeHelp {
			m.mode = modeNormal
			return m, nil
		}
		if m.search != "" {
			m.search = ""
			return m, m.load()
		}
		return m, nil
	case "g":
		m.pendingG = true
		return m, nil
	case "G":
		if p := m.current(); p != nil && len(p.items) > 0 {
			m.panes[m.active].cursor = len(p.items) - 1
		}
		return m, nil
	case "j", "down":
		if p := m.current(); p != nil && p.cursor < len(p.items)-1 {
			m.panes[m.active].cursor++
		}
		return m, nil
	case "k", "up":
		if p := m.current(); p != nil && p.cursor > 0 {
			m.panes[m.active].cursor--
		}
		return m, nil
	case "tab", "l", "right":
		if len(m.panes) > 0 {
			m.active = (m.active + 1) % len(m.panes)
		}
		return m, nil
	case "shift+tab", "h", "left":
		if len(m.panes) > 0 {
			m.active = (m.active - 1 + len(m.panes)) % len(m.panes)
		}
		return m, nil
	case "/":
		m.mode = modeSearch
		return m, nil
	case "R":
		m.flash("syncing…")
		return m, m.sync()
	case "J":
		// Join the NEXT meeting, which is what the footer advertises. It is a
		// separate key from "enter" because enter acts on the selected row,
		// and a footer that promises one thing while the key does another is
		// how people end up opening the wrong link.
		if m.next == nil || m.next.ActionURL == "" {
			m.flash("no joinable meeting coming up")
			return m, nil
		}
		// A conference link arrives inside an invite, and an invite can come
		// from anyone. Hard-pinning to meet.google.com would break legitimate
		// Zoom and Teams invites, so the protection is the scheme allowlist at
		// ingest plus naming the destination host before we leave the terminal.
		m.confirmURL = m.next.ActionURL
		m.mode = modeConfirmOpen
		return m, nil
	case "enter", "o":
		it, ok := m.selected()
		if !ok {
			return m, nil
		}
		if it.ActionURL == "" {
			m.flash("no link on this item")
			return m, nil
		}
		// Show where the keypress actually goes before it goes there (SEC-01).
		m.confirmURL = it.ActionURL
		m.mode = modeConfirmOpen
		return m, nil
	case "m":
		it, ok := m.selected()
		if !ok {
			return m, nil
		}
		return m, m.markRead(it)
	case "r":
		it, ok := m.selected()
		if !ok {
			return m, nil
		}
		return m, m.reply(it)
	}
	return m, nil
}

func (m *Model) jump(id paneID) {
	for i, p := range m.panes {
		if p.id == id {
			m.active = i
			return
		}
	}
	m.flash(string(id) + " is not connected — run `commhub connect google`")
}

func (m Model) markRead(it store.Ranked) tea.Cmd {
	st, ads := m.st, m.ads
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		for _, a := range ads {
			d := a.Descriptor()
			if d.Service == it.Service && d.ProviderID == it.ProviderID && d.Caps.CanMarkRead {
				if err := a.MarkRead(ctx, it.Item); err != nil {
					return statusMsg("mark read failed: " + err.Error())
				}
			}
		}
		if err := st.MarkRead(ctx, it.Item); err != nil {
			return statusMsg("mark read failed: " + err.Error())
		}
		return syncedMsg{}
	}
}

// reply hands composition to $EDITOR. The child process runs with a scrubbed
// environment, so the editor — and every plugin and language server it loads —
// never sees a credential (SEC-02).
func (m Model) reply(it store.Ranked) tea.Cmd {
	editor := os.Getenv("EDITOR")
	if editor == "" {
		editor = "vi"
	}
	dir, err := os.MkdirTemp("", "commhub-compose-")
	if err != nil {
		return func() tea.Msg { return statusMsg("compose: " + err.Error()) }
	}
	if err := os.Chmod(dir, safe.DirMode); err != nil {
		return func() tea.Msg { return statusMsg("compose: " + err.Error()) }
	}
	path := filepath.Join(dir, "reply.md")
	header := fmt.Sprintf("# Reply to %s\n# Subject: Re: %s\n\n", it.Sender, it.Title)
	if err := safe.WriteFileSecure(path, []byte(header)); err != nil {
		return func() tea.Msg { return statusMsg("compose: " + err.Error()) }
	}

	cmd := safe.Command(context.Background(), editor, path)
	ads := m.ads
	item := it.Item
	return tea.ExecProcess(cmd, func(err error) tea.Msg {
		defer os.RemoveAll(dir)
		if err != nil {
			return statusMsg("editor exited: " + err.Error())
		}
		body, rerr := os.ReadFile(path)
		if rerr != nil {
			return statusMsg("compose: " + rerr.Error())
		}
		draft := adapter.Draft{Body: stripComments(string(body)), Subject: "Re: " + item.Title}
		if draft.Body == "" {
			return statusMsg("empty draft — nothing sent")
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		for _, a := range ads {
			d := a.Descriptor()
			if d.Service == item.Service && d.ProviderID == item.ProviderID {
				if !d.Caps.CanReply {
					return statusMsg("reply is not enabled — run `commhub enable reply`")
				}
				if err := a.Reply(ctx, item, draft); err != nil {
					return statusMsg("send failed: " + err.Error())
				}
				return statusMsg("reply sent to " + item.Sender)
			}
		}
		return statusMsg("no adapter can reply to this item")
	})
}

func stripComments(s string) string {
	var out []byte
	for _, line := range splitLines(s) {
		if len(line) > 0 && line[0] == '#' {
			continue
		}
		out = append(out, line...)
		out = append(out, '\n')
	}
	return trimSpace(string(out))
}

func openCmd(url string) tea.Cmd {
	return func() tea.Msg {
		if err := safe.OpenURL(url); err != nil {
			return statusMsg("could not open: " + err.Error())
		}
		return statusMsg("opened " + safe.DisplayHost(url))
	}
}

// isExpiredCredential reports whether an error means "sign in again" rather
// than "something went wrong".
func isExpiredCredential(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	for _, s := range []string{"invalid_grant", "token expired", "no stored credential"} {
		if strings.Contains(msg, s) {
			return true
		}
	}
	var apiErr *google.APIError
	return errors.As(err, &apiErr) && apiErr.NeedsReauth()
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
