# Backend Engineering — The Distilled Skill

The judgment layer. Not what the tools are — how a senior/staff engineer decides, what they refuse to do, and what separates a demo from something that survives production.

---

## I. The Five Mental Models That Govern Everything

Every specific rule below is downstream of one of these. Learn these five and you can derive the rest.

**1. Boundaries.**
Every vulnerability and most bugs happen where data crosses a boundary — between systems, languages, trust levels, or privilege levels. At each crossing ask: *what am I assuming about this data, and what breaks if that assumption is wrong?*
> SQL injection = user input crossing into query-language. XSS = user input crossing into HTML/JS. Broken authz = a request crossing a privilege boundary. Same failure, different costume.

**2. Code vs. Data confusion.**
The single root cause of the entire injection family. The fix is never "sanitize harder" or "blacklist bad characters" — it's using an API that structurally separates the two (parameterized queries, argument arrays), so ambiguity cannot exist.

**3. Fail fast, fail loud, fail early.**
The cost of a defect scales with how late it surfaces. Reject bad input at the entry point (400), not at the DB (500). Crash on missing config at startup, not on a user's request three hours later. A loud failure at deploy time is a non-event; a quiet one in production is an incident.

**4. Defense in layers.**
No single control is sufficient. Layers compound *multiplicatively* — an attacker must beat all of them simultaneously. Validation → parameterization → authz at point-of-access → security headers → monitoring. One strong layer is weaker than four moderate independent ones.

**5. I/O-bound vs. CPU-bound.**
~95% of backend work is waiting, not computing. Concurrency (structural) fixes waiting. Parallelism (hardware) fixes computing. Misdiagnosing which one you have is how people "optimize" the wrong thing for a week.

---

## II. Non-Negotiables

Things that are not judgment calls. If these are missing, the system is not production-ready — no exceptions, no "we'll add it later."

**Data & queries**
- Parameterized queries everywhere. Zero string-concatenated SQL. No exceptions.
- App DB user has DML permissions only, never DDL.
- `NOT NULL` is the default; nullability requires a reason.
- Every list query has an explicit `ORDER BY`. Return order is never guaranteed.
- Foreign keys used in frequent joins/filters are explicitly indexed (PKs are automatic, FKs are not).
- Every schema change goes through a migration in version control. Never hand-edit production schema.

**Input**
- Validate everything crossing the entry point: body, query params, path params, headers.
- Frontend validation is UX. Backend validation is security. Never one instead of the other.
- Query params are strings until you cast them. Every time.

**Auth**
- Passwords: Argon2id (or bcrypt) + per-user salt + tuned cost factor. Never SHA-256/MD5. Never plaintext.
- Session IDs / tokens: cryptographically random, `HttpOnly` + `Secure` + `SameSite`.
- Never store sensitive data in JWT claims — the payload is encoded, not encrypted.
- Never `localStorage` for tokens.
- Rate limit auth endpoints harder than everything else, in layers (per-IP → per-account → global).

**Authorization**
- Authz checks belong at the **point of data access**, not just the routing layer. `WHERE id = ? AND user_id = ?` — always.
- Return `404`, not `403`, for "exists but not yours." `403` leaks existence.
- Admin functions need an explicit role check. An unguessable URL is not a security control.

**Errors & config**
- One centralized global error handler that every error passes through.
- Generic message on the default/unclassified path. Never leak table names, constraints, or stack traces.
- One identical generic message for all auth failures — plus equalized timing (constant-time compare or artificial delay).
- Validate all config at startup and refuse to boot if something required is missing.
- Zero secrets in version control. A committed-then-deleted secret is a compromised secret — rotate it.
- Production log level is `info`, never `debug`.

**Operations**
- Graceful shutdown handling `SIGTERM` and `SIGINT` identically: stop accepting → drain in-flight → clean up in reverse acquisition order → exit, with a hard timeout.
- Health checks that verify the system *works*, not just that the process is alive.
- Structured (JSON) logs in production.

---

## III. Staff Engineer Thinking — The Judgment Calls

These have no universally correct answer. Knowing *which axis you're trading on* is the actual skill.

**Stateful sessions vs. stateless JWT**
Default to stateful. JWT buys statelessness and pays for it with revocation — you cannot invalidate an issued token before expiry without rotating the secret and logging out *every* user. Every workaround (blacklists) reintroduces the server-side state you went stateless to avoid.
> **The real insight:** a distributed cache (Redis) solves the horizontal-scaling argument for JWT without giving up revocation. Most "we need JWT to scale" reasoning is solving a problem you don't have. Choose per-client, not per-platform — sessions for the web app, JWT for mobile/third-party, is a legitimate hybrid.

**Build vs. buy (auth specifically)**
Implement it once to understand it. Ship a provider in production. A "complete" auth flow — multi-device revocation, social login, identity linking, RBAC — is weeks of work with severe, often invisible failure modes. Providers have a security team thinking about this full-time. Migrate to your own when the bill justifies the engineering cost, not before.

**Cache: what and how**
The real question isn't "is this slow" — it's **read/write ratio and staleness tolerance**. Product details, user profiles, trending topics, weather: all safe to cache because writes are rare relative to reads, not because of anything intrinsic to the data.
- Lazy/cache-aside → read-heavy, staleness-tolerant. The default.
- Write-through → staleness unacceptable, and you can absorb the write-path cost. Not a free upgrade.
- Eviction policy is structurally mandatory, not optional. "Subset of the data" is the definition of a cache.

**Index or don't**
Index if the field is in a frequent JOIN / WHERE / ORDER BY. But every index taxes every write on that table forever. Indexing is a cost/benefit call driven by actual query frequency — and an index that isn't earning its keep should be dropped, not kept "just in case."

**PATCH vs. PUT**
PATCH is the default in modern JSON APIs; partial update is the norm. PUT is full replacement and is rarer than its usage suggests. Interchangeable use "works" internally and breaks assumptions the moment your API is public.

**Timeouts and thresholds**
30-second shutdown timeout, 10 items per page, 60-second request timeout — these are *conventions, not answers*. Size them against your system's actual operation-duration profile. A copy-pasted default is a decision you didn't make.

**Open-source vs. managed observability**
Grafana/Prometheus/Loki/Jaeger vs. Datadog/New Relic is a **team-capacity decision**, not a technical-superiority one. A four-person team running a self-hosted observability stack badly is worse off than one paying for a managed tool.

**Staging fidelity vs. cost**
Staging's purpose is production-fidelity; true production-scale staging costs what production costs. A deliberately under-provisioned staging environment is a *conscious tradeoff*, not neglect — as long as the team knows which fidelity they gave up.

---

## IV. Mistakes That Look Correct

The dangerous class. These pass code review, ship, and fail later.

| Looks fine | Actually |
|---|---|
| Authz checked in routing middleware | Nothing stops `/books/5` from returning someone else's book. Check at the query. |
| `403 Forbidden` for a resource you don't own | Confirms the resource exists. Free enumeration for an attacker. Return `404`. |
| "User not found" vs. "Wrong password" | Two-stage attack: harvest valid emails, then brute-force only those. One generic message. |
| Same error message, different response times | Timing leaks which check failed. Equalize it. |
| Sequential integer IDs in URLs | `/invoices/102` implies 101 and 103 exist. Use UUIDs where enumeration matters. |
| `403` handled, `500` "handled" by a generic catch | You never classified the error. The 500 path should mean *"every known type was ruled out."* |
| `VARCHAR(255)` | A MySQL habit with no meaning in Postgres. Signals false intent, creates migration risk. Use `TEXT`. |
| `INNER JOIN` by default | Silently drops valid primary rows when an optional related row is missing. |
| Frontend form validation | Postman exists. Mobile clients exist. Attackers exist. None of them run your form. |
| `async/await` so no race conditions | Check-then-`await`-then-mutate races even single-threaded. The gap at `await` is real. |
| Global error handler placed mid-chain | It can only catch what runs *before* it. Must be last. |
| Cached with an ETag | Forget to bump it once and clients serve stale data indefinitely, silently. |
| Debug logs "only internal" | Logs go to third-party vendors and are a documented breach vector. Same hygiene as API responses. |
| `POST` returns `201` | Custom actions (`/archive`) return `200`. Response code reflects the *effect*, not the method. |
| List endpoint returns `404` when empty | Empty result is a successful list. `200` + `[]`. `404` is for a specific missing resource. |
| Heavy work inside the request | Third-party outage now breaks your signup. Or worse — you tell the user "email sent" when it wasn't. |
| CPU-heavy task in an event-loop server | The *entire server* freezes, not just that request. |
| Secrets deleted from the repo | Still in git history. Rotate, don't delete. |
| One big background task | Any failure re-runs the *whole* thing on retry. Small, chained, independently retryable. |

---

## V. Production-Ready Checklist

If someone asks "is this ready to ship," this is the list.

**Correctness**
- [ ] Every input validated at the entry point, all three kinds (type, format/syntactic, semantic)
- [ ] Cross-field and conditional validation where the domain requires it
- [ ] Consistent API conventions: plural resources, camelCase payloads, no abbreviations, server-managed fields rejected from client input
- [ ] Pagination + sorting + filtering on every list endpoint, with sane defaults for all three
- [ ] Status codes reflect actual effects (`201` vs `200` vs `204`; `401` vs `403`; `404` vs `409`)

**Resilience**
- [ ] Centralized global error handler, positioned last
- [ ] Retries with exponential backoff and a bounded max for anything calling an external service
- [ ] Graceful degradation / fallbacks for non-recoverable failures
- [ ] Background jobs for anything slow, externally dependent, or non-critical
- [ ] Idempotent background tasks (transactional, safely re-runnable — retries mean re-execution)
- [ ] Graceful shutdown across *every* resource-holding subsystem, not just the HTTP listener
- [ ] Timeouts everywhere: requests, DB queries, external calls, shutdown

**Security**
- [ ] Parameterized queries; argument arrays for shell commands
- [ ] Auth: proper hashing, secure cookie flags, layered rate limiting
- [ ] Authz enforced at point-of-access; centralized logic; default-deny
- [ ] **Automated** tests for authz specifically: user A ↛ user B's data; member ↛ admin functions; anonymous ↛ protected resources. Run in CI. Manual testing regresses.
- [ ] User content sanitized; CSP as a *secondary* layer
- [ ] Security headers via framework middleware
- [ ] No secrets in VCS, no debug logs in prod, no sensitive fields in logs

**Operability**
- [ ] Config validated at startup; fail-fast on missing required values
- [ ] Structured JSON logs with deliberate levels
- [ ] Metrics + traces, not just logs — the alert → metrics → logs → traces chain must actually work end-to-end
- [ ] Request/trace ID propagated through request context and downstream headers
- [ ] Deep health checks (DB connectivity *and* query latency, external service reachability, config/cache state)
- [ ] Monitoring on performance and business metrics, not only error rates — degradation precedes failure
- [ ] Audit logs on sensitive access, and every authz *failure* logged as a probing signal

---

## VI. Priorities — Where the Leverage Actually Is

Not everything deserves equal investment.

**Master these. They're ~99% of the job.**
Databases (schema design, indexing, query optimization, transactions), API design, error handling, auth/authz. These touch every line of a backend codebase and compound over an entire career.

**Know how to use these. Docs-level competence is enough.**
Elasticsearch/full-text search, message queues, specific observability vendors. Recognize the use case, follow the documentation, move on. Deep internals matter only if you're building the tooling itself.

**Deliberately skip until it's a real problem.**
Every serialization format, the full OSI stack, exotic databases, premature scaling architecture. Pick the industry-standard option in each category (HTTP, JSON, Postgres, Redis) and go deep on those first.

---

## VII. The Three Questions

Before merging anything that touches external data:

1. **Where is data crossing a boundary here?**
2. **What am I assuming about it?**
3. **What happens if that assumption is wrong?**

Ask these consistently and most of the vulnerability classes in this document never reach your codebase in the first place.

---

## VIII. The One-Line Summary

> Validate at the edge, parameterize at every interpreter, authorize at the point of access, fail fast and loud, offload anything slow, centralize your error handling, instrument everything, and never confuse "it works on the happy path" with "it's production-ready."
