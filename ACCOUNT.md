# NEXUS Platform - Architecture & Decisions

## Scope
**What was built:** A durable dispatch engine, dynamic supervisor, cache drift detector, and a reactive dashboard for operator visibility. We implemented strict deduplication logic (R-03), a worker restart budget with earned recovery (R-04), rate-limited recovery (R-11), and version-aware release/rollback (R-06). 
**What was deliberately left out:** Full causal event linking. Event attribution (R-07) uses a temporal approximation (any event during a release's watch window is tied to that release). We also left out physical application deployment—worker processes are simulated via goroutines.

## Decisions
- **Attempt Tracking on Worker Death (R-01):** A delivery interrupted by worker death counts as an attempt because the platform cannot distinguish a partially executed side effect from no execution. This protects idempotency at the expense of a retry token.
- **Rollback Pointer (R-06):** Rollbacks don't just unset the current version; they explicitly inject a new `committed` release record pointing to the `previous_version`. This establishes an unbroken chain of version lineage.
- **Dead-Letter is Terminal (R-04/R-11):** The rate-limited background drainer only operates on `replay_queued` items, not `dead_letter` items. Dead-lettered items must be manually requested to replay via `POST /work/{id}/replay` before they enter the drain queue.
- **Worker Restart Budget (R-04):** Fresh workers start with `restart_count = 1` (the initial spawn counts as attempt 1). Therefore, a "budget of 5" effectively allows 4 crashes/restarts after the initial start. The count resets fully to 0 if the worker survives for 30 continuous seconds without crashing.

## Failure Behaviour
- **Worker Panics & Kills (R-01):** When a worker dies, any items currently `assigned` to it are instantly reverted to `pending`, and their `attempt_count` is incremented. The worker is restarted after a 1-second delay.
- **Exhausted Restart Budgets:** If a worker crashes 5 times within a 30-second window, it enters a `dead` state and must be manually revived by an operator via `/chaos/revive/{id}`.
- **Dependency Outages (R-10):** If the external datastore is down, the `/cache` endpoint will serve stale data and flag it as `stale: true`, unless the data is older than its configured `max_age_seconds`, in which case it returns a 503 (R-09).
- **Cache Drift (R-08):** If the cache disagrees with the database, or if the cache is older than the DB's `updated_at` by more than its configured `max_age_seconds`, a `disagreement` event is logged (after two consecutive failing ticks).

## Limits
- **Drainer Throughput (R-11):** The replay drainer operates via a 20 item/sec token bucket. It may burst 1-2 items instantly on wake but will throttle aggressively thereafter.
- **Event Retention:** Currently unbounded in SQLite. In a production setting, this would need a TTL or partition-drop mechanism.

## Confidence
- **Tested:** Worker death recovery (verified via test script tracking state transitions), rate-limiter cadence, SQLite concurrency (using WAL mode and buffered writes to avoid deadlocks), cache degradation under dependency failure.
- **Reasoned:** The temporal linking of events (R-07) is reliable assuming only one release is "watching" at a time.
- **Assumed:** Work payloads are small enough that storing the raw JSON body in SQLite doesn't blow out the database pages.

## Next
If given another six hours, I would:
1. **EXTENDED Suite (R-13 to R-15):** Implement explicit causal links, `GET /beliefs` self-reporting, and order claiming.
2. **Distributed Dispatch:** Move from a single goroutine dispatcher to a partitioned lease-based model for horizontal scalability.
3. **Frontend Polish:** Add WebSockets to the dashboard instead of relying on 2-second HTTP polling.
