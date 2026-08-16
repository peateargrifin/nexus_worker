# NEXUS Platform - Architecture & Decisions

## Decisions

- **Worker Restart Budget (R-04):** Fresh workers start with `restart_count = 1` (the initial spawn counts as attempt 1). Therefore, a "budget of 5" effectively allows 4 crashes/restarts after the initial start. The count resets fully to 0 if the worker survives for 30 continuous seconds without crashing.
- **Release Auto-Linking (R-07):** R-07 links by temporal window, not verified causation — an unrelated event during an active watch window would be misattributed.
- **R-02 Data Consistency**: Ensured that when a work item exhausts its attempts and is moved to `dead_letter`, the final incremented `attempt_count` is explicitly written to the database along with the status change. This prevents the DB state from falsely reporting `attempt_count=0` while the event log shows exhaustion.
- **R-07 Temporal Auto-linking**: The auto-linking of events to an active release is purely temporal. Any event created during a release's `watch_until` window is linked to that release, even if it might be causally unrelated. This is a defensible simplification at this scale (assuming one release is watched at a time).
- **R-11 Token Bucket Burst Capacity**: When the token bucket drainer initially wakes up or finds a new backlog, the first 1-2 items may be drained almost instantly before the strict ~50ms cadence (20 items/sec) settles. This burst-then-steady behavior is intentional and expected for a token bucket.
