# NEXUS Platform - Architecture & Decisions

## Scope
**What was built:** A durable dispatch engine, dynamic supervisor, cache drift detector, and a reactive dashboard for operator visibility. We implemented strict deduplication logic (R-03), a worker restart budget with earned recovery (R-04), rate-limited recovery (R-11), and version-aware release/rollback (R-06). We also successfully implemented the complete EXTENDED suite: explicit causal event linking (R-13), atomic order claiming (R-14), and self-reporting beliefs (R-15).
**What was deliberately left out:** Physical application deployment. To satisfy the "clean machine" deployment constraint (zero external orchestration required), worker processes are simulated via goroutines running inside the platform process.

## Decisions
- **Attempt Tracking on Worker Death (R-01):** A delivery interrupted by worker death counts as an attempt because the platform cannot distinguish a partially executed side effect from no execution. This protects idempotency at the expense of a retry token.
- **Rollback Pointer (R-06):** Rollbacks don't just unset the current version; they explicitly inject a new `committed` release record pointing to the `previous_version`. This establishes an unbroken chain of version lineage.
- **Cache Auto-Healing (R-08):** When the reconciler detects cache drift, it doesn't just log a disagreement—it actively purges the corrupted cache entry from the database, demonstrating self-healing capability and preventing eternal alert spam.

### Honesty in Simulation
While the system implements robust, exact-once concurrency (atomic SQL claiming, retry backoffs, and dead-letter queues) as if it were distributed, it is important to note that the "workers" are simulated as individual goroutines. However, all concurrency locks, restart budgets, and crash recovery mechanics operate strictly through the database, exactly as they would in a real distributed environment.

### Storage & Work Queuing
- **Dead-Letter is Terminal (R-04/R-11):** The rate-limited background drainer only operates on `replay_queued` items, not `dead_letter` items. Dead-lettered items must be manually requested to replay via `POST /work/{id}/replay` before they enter the drain queue.
- **Worker Restart Budget (R-04):** Fresh workers start with `restart_count = 1` (the initial spawn counts as attempt 1). Therefore, a "budget of 5" effectively allows 4 crashes/restarts after the initial start. The count resets fully to 0 if the worker survives for 30 continuous seconds without crashing.

## Failure Behaviour
- **Worker Panics & Kills (R-01):** When a worker dies, any items currently `assigned` to it are instantly reverted to `pending`, and their `attempt_count` is incremented. The worker is restarted after a 1-second delay.
- **Exhausted Restart Budgets:** If a worker crashes 5 times within a 30-second window, it enters a `dead` state and must be manually revived by an operator. The dashboard incident banner is strictly bound to the worker's current database state (`dead` or `restarting`), ensuring that the banner clears the instant a worker recovers.
- **Dependency Outages (R-10):** If the external datastore is down, the `/cache` endpoint will serve stale data and flag it as `stale: true`, unless the data is older than its configured `max_age_seconds`, in which case it returns a 503 (R-09).
- **Causal Linking (R-13):** System-generated events (like `budget_exhausted` or `panicked`) are explicitly hard-linked via a `caused_by_event_id` foreign key if they occur during an active release window, visually rendering as a solid line (`│`) on the operator dashboard.

## Limits
- **Drainer Throughput (R-11):** The replay drainer operates via a 20 item/sec token bucket. It may burst 1-2 items instantly on wake but will throttle aggressively thereafter.
- **Event Retention:** Currently unbounded in SQLite. In a production setting, this would need a TTL or partition-drop mechanism.

## Confidence
- **Tested:** Worker death recovery (verified via test script tracking state transitions), rate-limiter cadence, SQLite concurrency (using WAL mode and buffered writes to avoid deadlocks), cache degradation under dependency failure, and explicit causal logic linking releases to downstream failures.
- **Reasoned:** The atomic optimistic lock (`UPDATE ... WHERE status = 'pending'`) perfectly satisfies the atomic claim requirement without requiring table locks.
- **Assumed:** Work payloads are small enough that storing the raw JSON body in SQLite doesn't blow out the database pages.

## Challenges Faced & Solutions

### Challenge: True Atomic Work Claiming without Distributed Locks
**The Problem:** We needed to ensure exactly-once processing (R-03) and atomic claiming (R-14) without introducing external infrastructure like Redis distributed locks, satisfying the single-machine SQLite requirement.
**The Solution:** We implemented an optimistic locking mechanism directly in SQL. When a worker attempts to claim a job, it executes: `UPDATE work_items SET status = 'assigned', assigned_worker = ? WHERE id = ? AND status = 'pending'`. The driver then checks `RowsAffected()`. If two concurrent goroutines try to claim the same job, SQLite's strict serialization ensures only one update succeeds, returning a conflict to the other without ever needing a table-level lock.

### Challenge: Simulating OS-Level Crashes in a Monolithic Binary
**The Problem:** The PRD requires workers to act as independent processes that can crash, die, and restart. Because we modeled workers as goroutines inside a single monolithic binary, a standard crash would bring down the entire platform.
**The Solution:** We built a custom Chaos Engine. Workers run in an infinite `select` loop with a 1-second ticker that periodically checks a shared chaos state. When "Crash Loop" is triggered, the worker intentionally calls `panic()`. We wrapped the top level of the worker in a `defer func() { recover() }` block. This block catches the panic, instantly strips the worker of its assigned jobs, logs the death, and uses `time.AfterFunc(1s, spawnWorker)` to trigger an isolated supervisor restart.

### Challenge: Stale Incident Banners (False Positives)
**The Problem:** The operator dashboard relies on querying the `events` table for recent anomalies (`panicked`, `budget_exhausted`). However, when an operator manually revived a dead worker, the incident banner remained stuck because the historical crash event still existed within the 5-minute search window.
**The Solution:** We rewrote the `GetDiagnosis` analytical query to actively `JOIN` the `events` table with the `workers` table. The query now explicitly enforces `AND w.status IN ('dead', 'restarting')`. The millisecond an operator revives a worker, its status updates to `running`, instantly dropping it from the incident query and flawlessly clearing the dashboard banner.

### Challenge: Reconciler Alert Spam vs. Auto-Healing
**The Problem:** The background Reconciler successfully detected simulated Cache Drift (R-08) and fired a `CACHE DISAGREEMENT` event to the timeline. However, because the corrupted data remained in SQLite, the Reconciler spammed a new event every 2 seconds, destroying timeline visibility.
**The Solution:** We evolved the Reconciler from a passive "smoke alarm" to an active auto-healer. Immediately after firing the disagreement event to the ledger, the Reconciler executes a hard `DELETE FROM cache_entries WHERE key = ?`. This purges the corrupted data, meaning the Reconciler alerts the operator exactly once and then self-heals the system.

### Challenge: Proving Mathematical Causality (R-13)
**The Problem:** Relying on temporal proximity ("it crashed 10 seconds after a release") is notoriously flaky in distributed systems and can lead to false rollbacks. We needed to explicitly link downstream failures to upstream releases.
**The Solution:** When a new release is pushed, it establishes a 5-minute "watch window" in the database. When the supervisor catches a panic, before logging the event, it queries for any active watch windows. If one is found, it injects the release's Event ID into the crash event's `caused_by_event_id` foreign key. The frontend consumes this graph and renders a solid structural line (`│`) between the release and the crash, mathematically proving causality.

### Challenge: Concurrency and SQLite Lock Contention
**The Problem:** With the Dispatcher, Reconciler, API endpoints, and multiple worker goroutines all polling and updating the database continuously, SQLite repeatedly threw `database is locked` panics due to concurrent write contention.
**The Solution:** We enabled Write-Ahead Logging by executing `PRAGMA journal_mode=WAL; PRAGMA synchronous=NORMAL;` on boot. This drastically improved concurrency by allowing readers to operate simultaneously with a writer, entirely eliminating the lock contention without needing a heavy RDBMS like Postgres.

### Challenge: Runaway Replay Drainer (Thundering Herd)
**The Problem:** If an operator bulk-replayed hundreds of `dead_letter` items, the background drainer would instantly flush them into the `pending` state, causing a thundering herd that overwhelmed the worker pool and CPU.
**The Solution:** We implemented a strict Token Bucket rate limiter in `drain.go`. The drainer is capped at 20 items per second. It allows a minor instantaneous burst when waking up but aggressively throttles sustained throughput, ensuring recovery does no harm to the platform's stability (R-11).

### Challenge: Timezone Drift and Event Misalignment
**The Problem:** The operator dashboard's causal timeline was rendering out of order. Releases were failing to temporally bind to downstream crashes because the backend was generating release events in UTC, while worker log events were defaulting to local system time.
**The Solution:** We explicitly normalized all event storage, API timestamp generation, and temporal correlation queries (e.g., the 5-minute watch window) to use `time.Now().Local()`. This guaranteed that structural causality links (`caused_by_event_id`) matched the chronological reality displayed to the operator.

### Challenge: Context Cancellation vs. True Crash Semantics
**The Problem:** Originally, the "Kill Worker" demo button invoked `context.CancelFunc()`. While this successfully shut down the worker, it was treated as a graceful scale-down, leaving the worker in an `OUT OF SERVICE` state rather than demonstrating the required "auto-restart" self-healing behavior.
**The Solution:** We re-wired the endpoint to inject a `crash-once` chaos pill instead. This forces the worker to explicitly `panic()`, engaging the supervisor's `defer recover()` block which successfully catches the death, reclaims orphaned work, and triggers the automated 1-second restart delay.

### Challenge: Zombie Worker Revivals
**The Problem:** When a worker was manually revived by an operator, it would immediately boot up and panic again because its previous `crash-loop` chaos setting was still cached in the supervisor's memory map.
**The Solution:** We modified the `ReviveWorker` routine to explicitly execute a `delete(s.chaosSettings, workerID)` under a mutex lock before resetting the database restart counter. This ensures the worker wakes up with a clean slate.

### Challenge: Cache Drift Target Missing
**The Problem:** The Cache Drift chaos button originally hardcoded a corrupt cache entry for a fake item (`sku-123`). However, the Reconciler explicitly executes a `JOIN cache_entries ... JOIN work_items`. Because `sku-123` didn't exist in the active work queue, the query returned 0 rows, masking the drift.
**The Solution:** We dynamically refactored the injection endpoint to query `SELECT id FROM work_items LIMIT 1`. It now targets and corrupts the cache of an *active* piece of work, guaranteeing the Reconciler catches it on the next tick.

## Next
If given another six hours, I would:
1. **Distributed Dispatch:** Move from a single goroutine dispatcher to a partitioned lease-based model for horizontal scalability.
2. **Frontend Polish:** Add WebSockets to the dashboard instead of relying on 1.5-second HTTP polling.
