# NEXUS Reliability Platform

A highly durable, single-node workload management platform built in Go and SQLite for the NEXUS evaluation challenge. 

## 1. Start

This project is built using only the standard library and a pure-Go SQLite driver (`modernc.org/sqlite`), requiring zero external dependencies like Docker or CGO. 

To start the platform on a clean machine, simply run:

```bash
go run ./cmd/platform
```

**What success looks like:** The platform will compile and boot immediately. You will see log lines indicating that the supervisor has started, workers are starting up on port `8080`, and the background drainer loop is actively monitoring.

## 2. Use

Once started, open the operator console in your browser:
**[http://localhost:8080/](http://localhost:8080/)**

To push work and see it processed:
1. Scroll down to the **DEMO CONTROLS** box on the dashboard.
2. Click **Post New Work**.
3. Watch the **WORK** metrics box at the top. You will see a `waiting` item appear and immediately drop back to `0` as one of the active workers claims and processes it.

## 3. Break

The platform supports explicitly simulating catastrophic failures. Use the **DEMO CONTROLS** panel in the dashboard to trigger them:

- **Kill Worker 1 (R-04):** Simulates an immediate process crash. The worker is instantly replaced by the supervisor.
- **Crash Loop Worker 2 (R-04):** Injects a poison pill. Watch it panic, restart, and panic again. After 5 attempts, the supervisor strips its budget and marks it `OUT OF SERVICE`. (Click **Revive Worker 2** to manually intervene).
- **Post Duplicate Work (R-03):** Submits two identical payloads back-to-back using the same idempotency token. Only the first will be processed; the second is suppressed without side effects.
- **Fail Work on Worker 3 (R-11):** Forces the worker to abandon its current item. The item is safely parked in the **DEAD LETTER** box where you can manually replay it.
- **Toggle Dependency Down (R-10):** Severs the external simulated connection. The platform header gracefully degrades.
- **Cause Cache Drift (R-08):** Manually corrupts a cached entry in the database. Within seconds, the reconciler spots the drift and spawns a loud **CACHE DISAGREEMENT** alert.
- **Push new release (R-06):** Injects a new deployment. Watch the `COMPONENTS` list seamlessly reboot onto the new version without dropping any pending work.

## 4. Look

Everything you need to evaluate the system is located at **http://localhost:8080/**. 

**What to notice first:**
1. **The Diagnostic Banner:** If you trigger a crash loop, a red banner instantly tells you *why* it happened, exactly *when*, and provides a `[ROLLBACK]` button if it was caused by a recent release.
2. **The Causal Timeline:** Look at the event trace. A dotted line (`⋮`) indicates chronological adjacency. A solid line (`│`) proves mathematical causality (via the `caused_by_event_id` foreign key).
3. **The Components View:** Watch the exact restart budgets (`0/5`) for every worker. If a worker is healthy for 30 seconds, it will "earn" its budget back automatically.
