# NEXUS — Go + SQLite implementation plan

## 0. Install Go (Windows — first time setup)

1. `winget install GoLang.Go` (or the MSI from go.dev/dl — either works)
2. Close and reopen your terminal / VS Code so PATH picks up the install
3. Verify: `go version` → should print something like `go1.23.x windows/amd64`
4. Install the VS Code Go extension (`golang.go`, published by the Go team) — gives gofmt-on-save, inline errors, go.mod awareness. Same editor you already use for Java.
5. `go mod init nexus` inside your project folder — this replaces Maven's pom.xml. No separate build tool: `go build` / `go run` do everything.
6. SQLite driver: `go get modernc.org/sqlite` — pure Go, no cgo, so no MSVC toolchain headache on Windows.
7. Beekeeper Studio (already installed) opens the `.db` file directly — no setup needed to inspect state live during dev.

That's the whole toolchain. No Docker, no JDK juggling, no separate DB server to start or stop.

## 1. Project layout

```
nexus-go/
  go.mod
  cmd/
    platform/main.go      — starts HTTP server + background loops + all workers (goroutines)
    sender/main.go        — demo script, feeds work in
  internal/
    store/                — all SQLite access, single source of truth
      schema.go
      workitems.go
      events.go
      releases.go
      cache.go
    dispatch/              — accept, assign, retry work
      dispatch.go
    supervisor/             — spawn/cancel/restart workers as goroutines (see architecture note below)
      supervisor.go
      worker.go
    release/                 — push + rollback releases
      release.go
    reconcile/                — cache-vs-truth drift checking
      reconcile.go
    chaos/                      — failure-injection HTTP handlers
      chaos.go
    api/                          — HTTP routing, dashboard status endpoint
      router.go
      dashboard.go
  web/
    dashboard.html                 — operator screen, static + JS polling
  nexus.db                          — created at runtime, gitignored
  start.ps1                          — one-command startup
  README.md
  ACCOUNT.md
```

## 1a. Architecture note — workers are goroutines, not OS processes

The obvious plan is spawning workers via `os/exec` as real separate OS processes — realistic, but risky in a 6-hour window on a first Go project: if `cmd/platform` dies or gets Ctrl+C'd, spawned children don't automatically die with it (no default parent-death signal on Windows, and correct cross-platform SIGINT/SIGTERM propagation is a real time sink). A reviewer restarting your platform could hit zombie workers still holding old jobs.

**Fix: workers run as goroutines inside `cmd/platform`**, each with its own cancellable `context.Context`, tracked by the supervisor in a `map[string]context.CancelFunc`. Zero zombies guaranteed — a goroutine cannot outlive its process — and no cross-platform signal handling needed. This still fully satisfies "stand-in services": start, stop, and fail them on purpose, just via context cancellation and a deliberate panic instead of `kill -9`. The restart/backoff/budget logic in R-04 doesn't care whether the thing that died was a goroutine or a process — same code path, same guarantee.

## 2. Data model — build this first, everything else sits on it

```sql
PRAGMA journal_mode = WAL;   -- crash-safe, concurrent-read-friendly
PRAGMA foreign_keys = ON;

CREATE TABLE work_items (
  id                 TEXT PRIMARY KEY,
  type               TEXT NOT NULL,
  body               TEXT NOT NULL,
  status             TEXT NOT NULL DEFAULT 'pending',   -- pending | assigned | done | dead_letter
  attempt_count      INTEGER NOT NULL DEFAULT 0,
  max_attempts       INTEGER NOT NULL DEFAULT 5,
  next_retry_at      DATETIME,
  assigned_worker    TEXT,
  idempotency_token  TEXT NOT NULL,
  dead_letter_reason TEXT,
  created_at         DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at         DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE TABLE events (
  id                  INTEGER PRIMARY KEY AUTOINCREMENT,
  ts                  DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  entity_type         TEXT NOT NULL,   -- work_item | worker | release | cache
  entity_id           TEXT NOT NULL,
  action              TEXT NOT NULL,
  reason              TEXT,
  caused_by_event_id  INTEGER REFERENCES events(id)   -- NULL = no known causal link (R-13)
);

CREATE TABLE releases (
  id                TEXT PRIMARY KEY,
  version           TEXT NOT NULL,
  previous_version  TEXT NOT NULL,     -- required at creation — R-06 "know how to undo before doing"
  status            TEXT NOT NULL DEFAULT 'watching',   -- watching | committed | rolled_back
  started_at        DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
  watch_until       DATETIME NOT NULL
);

CREATE TABLE workers (
  id                     TEXT PRIMARY KEY,
  pid                    INTEGER,
  status                 TEXT NOT NULL DEFAULT 'starting', -- starting | running | restarting | dead
  restart_count          INTEGER NOT NULL DEFAULT 0,
  restart_window_start   DATETIME,
  last_healthy_at        DATETIME
);

CREATE TABLE cache_entries (
  key             TEXT PRIMARY KEY,
  value           TEXT NOT NULL,
  cached_at       DATETIME NOT NULL,
  max_age_seconds INTEGER NOT NULL
);
```

WAL mode does a lot of quiet work here: a committed transaction is durable even if the process dies on the next line of code — that's most of what makes R-01 close to free instead of something you hand-build.

**Connection pool gotcha — handle this in your DB init, before writing anything else.** `database/sql` opens a connection pool by default; SQLite allows only one writer at a time even in WAL mode, so concurrent writes from the pool throw `database is locked`. Fix:

```go
db.SetMaxOpenConns(1)
db.Exec("PRAGMA busy_timeout = 5000;")
```

Capping the pool at one connection serializes access in memory instead of failing at the SQLite layer — at this project's scale that costs nothing. `busy_timeout` is a second safety net, and it also covers the moment you pop open Beekeeper Studio mid-demo to inspect `nexus.db` while the platform is still running.

## 3. CORE — R-01 to R-06, built bulletproof

*(Rounding "R1–5" up to R-06 since it's the sixth CORE requirement in the doc — release rollback is the one INC-2291 lost 20 minutes to.)*

### R-01 — accepted work survives a crash
- `POST /work` handler: one transaction — INSERT into work_items, COMMIT, only then return 200 to the sender. If the process dies before commit, the sender gets no 200 and knows to retry — never a false "accepted."
- Startup recovery scan: every row with `status='assigned'` gets reverted to `pending` — since workers are goroutines, none survive a platform restart, so there's no ambiguity to check against a "live worker set." Nothing stays silently stuck mid-flight.
- Directly defeats the doc's callout — *carrying on delivering but forgetting how many attempts were already made* — `attempt_count` lives in the row, untouched by process restarts.

### R-02 — every item ends somewhere visible
- Four-state enum only: `pending → assigned → done | dead_letter`. No fifth silent state.
- No DELETE statement exists anywhere for work_items — rows are append/update-only, so nothing can vanish.
- Dashboard queries `WHERE status='dead_letter'` directly — dead-lettered items are one query away, always visible.

### R-03 — duplicate delivery is harmless
- Every work item gets an `idempotency_token` at accept time.
- Completion is a conditional update: `UPDATE work_items SET status='done' WHERE id=? AND status!='done'`. First "done" wins; SQLite reports 0 rows affected on the second — the handler logs `duplicate_suppressed` as an event and returns 200 without reprocessing.
- Defeats: *accepting a late "done" message as final for work now running twice, leaving numbers an operator can't make sense of* — the conditional UPDATE makes the second message a documented no-op.

### R-04 — retries and restarts have a hard limit
- Two independent budgets, deliberately not shared: `work_items.attempt_count` (item-level retry) vs `workers.restart_count` (worker-level restart). A worker restarting doesn't reset any item's retry budget, and vice versa.
- Backoff: `next_retry_at = now + base * 2^attempt_count`, capped at a max (e.g. 5 min), checked by a `time.Ticker`-driven dispatch loop.
- "Earning recovered": a worker's `restart_count` only resets to 0 after `last_healthy_at` has held for a settling period (e.g. 30s) — not the instant it starts. This is the exact INC-2291 bug: resetting budget on start so it never runs out.

### R-05 — the past is queryable
- `GET /events?since=<ts>&entity_type=&entity_id=` — every decision the platform makes writes an event first.
- Retention is a stated constant (e.g. 72h), enforced by a prune job. Querying before the retention window returns an explicit `{"before_retention": true}` marker — never an empty array indistinguishable from "nothing happened."

### R-06 — releases undo in one action
- `POST /releases` requires `previous_version` up front. If it can't be resolved, the endpoint returns 400 and refuses to create the release — if it can't say how to undo, it refuses.
- `POST /releases/{id}/rollback` — one call: flips the active-version pointer, tells the supervisor to restart workers on `previous_version`, records one event. No hand-reconstructing old state, ever.

## 4. EXPECTED — R-07 to R-12, built in full

**R-07 — release linked to effects.** Every event created while a release's `watch_until` hasn't passed gets `caused_by_event_id` auto-set to that release's start event. Dashboard timeline overlays release markers directly on the work-item event stream.

**R-08 — disagreements are found.** A `reconcile` goroutine runs every N seconds, comparing `cache_entries` against work_items truth for overlapping keys. A mismatch only becomes a logged disagreement after it persists across two consecutive checks — this defeats reporting a disagreement for what's just a normal update in progress.

**R-09 — cached values carry age.** `cache_entries.cached_at` is returned in every API response as a computed `age_seconds` field.

**R-10 — honest degradation.** When a simulated dependency is toggled down (via chaos), reads fall back to the last cache_entries row and respond with `{"stale": true, "age_seconds": N}` instead of a blank error.

**R-11 — recovery is rate-limited.** Backlog drain (moving dead_letter/queued items back to active dispatch after a worker recovers) is capped via a token bucket — e.g. 20 items/sec — so clearing 10,000 queued items can't hammer a worker that just came back healthy. This is the second half of INC-2291.

**R-12 — 90-second diagnosis.** `computeDiagnosis()` in `api/dashboard.go`: finds the most recent anomaly event, walks backward for the nearest preceding release or restart event within a lookback window, renders one plain sentence — "Worker w-3 has been crash-looping since 14:02, 8 seconds after release r-88 went out." That sentence is what a reviewer sees first.

## 5. EXTENDED — R-13 to R-15, touched and functional

**R-13 — order only claimed when known.** Already modeled in the schema: `events.caused_by_event_id` is either a real link (from R-07's auto-linking) or NULL. Dashboard shows linked events with a solid connector, merely-adjacent events with a dotted one.

**R-14 — platform can be asked about itself.** One endpoint: `GET /beliefs` → `{"believes_healthy": bool, "based_on_data_age_seconds": N, "would_change_if": "..."}`. Thin, single handler, genuinely present.

**R-15 — failures are triggerable.** This is the `chaos` package — see section 6. Every claimed failure mode gets a real endpoint, not a description.

## 6. Chaos endpoints — one per failure mode

| Endpoint | Triggers |
|---|---|
| `POST /chaos/kill-worker/{id}` | Cancels that worker's goroutine context mid-processing — tests R-01/R-04 |
| `POST /chaos/crash-loop/{id}` | Flags that worker to panic immediately on every restart — tests R-04's restart budget |
| `POST /chaos/duplicate/{id}` | Re-sends a completed item's delivery — tests R-03 |
| `POST /chaos/bad-release` | Pushes a release with a broken version flag, then you call the real rollback endpoint — tests R-06 |
| `POST /chaos/drift/{key}` | Directly writes a mismatched value into cache_entries, bypassing the normal write path — tests R-08 |
| `POST /chaos/dependency-down` | Toggles a flag the reconcile loop checks — tests R-10 |

## 7. Operator dashboard

Static `dashboard.html`, vanilla JS, polling `GET /status` every 1–2s. No build step, no npm — one file, served by the Go `net/http` server via `http.FileServer`. Sections: diagnosis banner (R-12), backlog size + oldest item age, per-worker state + restart count, recent events timeline with release markers overlaid (R-07), dead-letter list.

## 8. Six-hour build order

| Hour | Focus |
|---|---|
| 1 | Go install + go mod init + schema.go + empty HTTP router skeleton — confirm `go run ./cmd/platform` starts |
| 2 | R-01, R-02 — accept/persist/status, plus cmd/sender |
| 3 | R-03, R-04 — idempotency + backoff + supervisor launching worker goroutines with cancellable contexts |
| 4 | R-05, R-06 — events query + release/rollback, plus chaos endpoints for everything built so far |
| 5 | R-07–R-12 — release linking, reconcile/cache layer, rate-limited drain, diagnosis banner, remaining chaos endpoints |
| 6 | Dashboard HTML, R-13/14 polish, README.md + ACCOUNT.md, full run-through against the doc's "what finished means" checklist |

## 9. Before you submit

- [ ] Starts from cold with one documented command
- [ ] Survives stop/restart with zero cleanup needed
- [ ] Every claimed failure mode has a working chaos endpoint, demoable in under a minute
- [ ] A stranger reaches a diagnosis within 90 seconds of opening the dashboard
- [ ] ACCOUNT.md written against what you actually built, not what you planned — reread it against the code last