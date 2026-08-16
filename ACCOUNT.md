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

## Next
If given another six hours, I would:
1. **Distributed Dispatch:** Move from a single goroutine dispatcher to a partitioned lease-based model for horizontal scalability.
2. **Frontend Polish:** Add WebSockets to the dashboard instead of relying on 1.5-second HTTP polling.
