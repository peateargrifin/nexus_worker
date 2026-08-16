# NEXUS checkpoint — CORE + EXPECTED complete

Repo target: `https://github.com/peateargrifin/nexus_worker.git`

## Status: CORE (R-01–R-06) — verified with real HTTP/DB evidence

| Req | What | Verified |
|---|---|---|
| R-01 | Accepted work survives a crash | Recovery scan reverts `assigned` → `pending` on restart |
| R-02 | Dead-lettered items visible | Status flips to `dead_letter`, reason recorded, queryable via `GET /events` |
| R-03 | Duplicate delivery harmless | Real redelivery test (not just resubmission) — conditional UPDATE suppresses second `done`, logs `duplicate_suppressed` |
| R-04 | Retries/restarts capped, earns recovery | Exhaustion path (climbs to budget, stops) AND positive recovery path (30s continuous health → resets to 0) both proven with real timestamps |
| R-05 | Past is queryable | `GET /events?since=&entity_type=&entity_id=` working |
| R-06 | One-action rollback | `POST /releases` refuses without `previous_version`; rollback flips version + logs event |
| 3.4 | Operator can revive a dead worker | `POST /workers/{id}/revive` — added specifically for this, verified |

## Status: EXPECTED (R-07–R-12) — verified with real HTTP/DB evidence

| Req | What | Verified |
|---|---|---|
| R-07 | Release linked to effects | `caused_by_event_id` auto-set during watch window |
| R-08 | Disagreements found, no false positives | Tick-by-tick log shows genuine two-consecutive-tick wait before firing |
| R-09 | Cache carries age | `age_seconds` in every `/cache/{key}` response |
| R-10 | Honest degradation | `dependency-down` → `stale:true` + real cached value, never a blank error |
| R-11 | Rate-limited recovery | Per-item drain timestamps show ~50ms pacing (20/sec), burst-then-steady is expected token-bucket behavior |
| R-12 | 90-second diagnosis | Plain-English sentence correlating crash → nearest preceding release |

## Notes to carry into ACCOUNT.md (small, not blockers)

- **R-04 budget semantics**: fresh worker starts at `restart_count=1` (initial spawn counts as attempt 1) — a "budget of 5" allows 4 restarts after the first start, not 5. Already added to ACCOUNT.md.
- **R-07 linking is temporal, not causal**: any event during a release's watch window gets linked, even if unrelated. Defensible at this scale (one release watched at a time) — say so explicitly rather than let a reviewer find it.
- **R-11 burst capacity**: first 1-2 drained items land faster than the steady ~50ms cadence before the token bucket settles. Name the burst size explicitly.
- **R-02 data consistency to double check**: one test run showed `attempt_count=0` on an item that had just exhausted `max_attempts=1` — confirm the counter increments before the dead-letter decision fires, or the DB will show a number that contradicts the event log.

## Not yet built

- Operator dashboard (`web/dashboard.html` + `GET /status` aggregation endpoint) — nothing exists yet as a screen, only as raw JSON/SQL
- EXTENDED (R-13–R-15) — deliberately last, per plan
- `.http` endpoint collection for manual testing — see `nexus-endpoints.http`

## Immediate next steps, in order

1. Push current state to `github.com/peateargrifin/nexus_worker.git`
2. Connect Beekeeper Studio to `nexus.db` for live inspection
3. Use `nexus-endpoints.http` (REST Client extension) to manually exercise every CORE+EXPECTED endpoint
4. Build `GET /status` + `dashboard.html` (spec + working file provided)
5. Then EXTENDED (R-13–R-15)
