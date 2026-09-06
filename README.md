# CommHub

One terminal view of your mail, calendar and meetings. A single static Go
binary, entirely local: there is no backend, no hosted OAuth app, and no
account of any kind to create with the maintainer.

The point is the **Priority pane** — everything that needs attention from every
connected account, in one ranked list, rather than a set of unread badges you
have to compare by eye.

```
╭─── Priority ───────────────╮╭─── Mail (10) ──────────────╮╭─── Calendar ───────────────╮
│ ! Platform standup  in 12m ││ @ Sam Okonkwo (3)      12m ││ ! Platform standup  in 12m │
│ @ Sam Okonkwo (3)      12m ││ @ Anna Delgado         34m ││   1:1 with Priya  in 1h14m │
│ @ Dana Whitfield        1h ││ @ Dana Whitfield        1h ││   Incident review  in 3h35m│
│ • Marcus Lee            3h ││ • Marcus Lee            3h ││   Design critique     in 8h│
╰────────────────────────────╯╰────────────────────────────╯╰────────────────────────────╯
 Next: Platform standup  in 12 min   [enter to join]
 gp priority  gm mail  gc calendar  j/k nav  enter open  r reply  / search  ? help  q quit
```

## Getting started

```bash
go build -o bin/commhub ./cmd/commhub
./bin/commhub connect google
./bin/commhub
```

`connect google` walks you through creating your own Google Cloud OAuth client
— a one-time, five-minute setup — then opens your browser to authorise. It
requests two read-only scopes and nothing else.

**Publish your consent screen.** The wizard says so too, but it is the step
people skip: a consent screen left in "Testing" has its refresh tokens revoked
by Google after seven days. You will see an "unverified app" warning when
authorising; that is expected, because the app is yours and you are its only
user.

Want to see the interface before setting any of that up?

```bash
./bin/commhub connect fake     # invented sample data, no credentials, no network
./bin/commhub disconnect fake:demo
```

Nothing in that mode is real — the footer says so while it is active.

### Second account

```bash
./bin/commhub connect google --label work
```

Each account is an independent provider with its own credential, and both feed
the same Priority pane.

## How ranking works

A score is computed **every time the pane is drawn**, never stored — because a
message becomes less urgent as it ages while a meeting becomes *more* urgent as
it approaches, so a number written at insert time is wrong within the hour.

```
score = base + recency − noise
```

| Signal | Weight |
|---|---|
| Meeting starts within 15 min | 120 |
| Unread, addressed directly to you | 100 |
| Meeting starts within 60 min | 90 |
| Unread with your address in To: | 80 |
| Starred | 70 |
| Reply in a thread you are in | 60 |
| Unread, Cc: only | 45 |
| Unread list or bulk mail | 20 |
| Recency | +40 × e^(−hours/6) |
| Bot sender · muted · mailing list | −30 · −25 · −10 |

Every weight lives under `[priority]` in `~/.config/commhub/config.toml`.
Threads collapse to a single row, so a six-message thread takes one line.

## Least privilege

A fresh install asks Google for **two read-only scopes** and nothing else:

| Feature | Scope | Default |
|---|---|---|
| Calendar, next meeting, Meet join | `calendar.events.readonly` | on |
| Mail triage — senders, subjects, labels | `gmail.metadata` | on |
| Reading bodies in the TUI | `gmail.readonly` | off |
| Mark read / archive | `gmail.modify` | off |
| Reply and compose | `gmail.send` | off |
| RSVP to invitations | `calendar.events` | off |

`commhub enable reply` asks Google for exactly one more scope at the moment you
turn the feature on. `commhub disable reply` drops it again and revokes the old
token. `commhub status` prints what is currently held.

Under the default ladder **no message bodies are ever cached** — the store holds
senders, subjects and labels only.

## Where things are kept

| | |
|---|---|
| Config | `~/.config/commhub/config.toml` — non-secret metadata only |
| Cache | `~/.local/share/commhub/commhub.db` |
| Logs | `~/.local/state/commhub/` |
| Secrets | OS keychain → sealed file → environment, in that order |

Files are created `0600` and directories `0700`, with the mode applied at
creation rather than afterwards, so there is no window in which they are
world-readable. `commhub status` shows which secrets backend is in use.

If there is no D-Bus Secret Service — over SSH, in WSL, on a headless box —
CommHub falls back to an argon2id-sealed file rather than refusing to run.

## Security

Everything CommHub renders is attacker-controlled: subjects, sender names,
snippets, and event titles, since anyone can send you an unsolicited invite. Two
boundaries are enforced at ingest, before anything reaches the store:

- **Terminal escapes are stripped**, including OSC 52, which would otherwise let
  a message silently write your system clipboard. The sanitiser is fuzzed in CI.
- **Links are allowlisted to http and https.** Any other scheme is dropped, so
  `vscode://`, `file://` and friends can never reach the URL opener. Opening
  asks for confirmation and names the destination host first.

Child processes — your `$EDITOR`, the browser, `xdg-open` — are spawned with a
scrubbed environment, so no credential can leak into an editor plugin. Files are
created with their final permissions rather than chmod-ed afterwards, so there is
no window in which they are world-readable.

## Development

```bash
make check     # go vet + go test
make fuzz      # fuzz the sanitiser for 60s
make golden    # regenerate the dashboard snapshot
```

The synthetic adapter means the entire interface is testable with no
credentials, no network, and no fixtures to maintain.

## License

Not yet chosen — see SPEC §17. Pick before the first release.
