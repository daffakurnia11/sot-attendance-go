# FiveM Player Log Webhook Implementation Plan

**Repository:** `sot-attendance-go`
**Contract:** `docs/fivem-player-log-webhook-contract-v1.md`
**Endpoint:** `POST /api/v1/webhooks/server-logs`
**Updated:** 3 September 2026
**Status:** built and running against production data; docs, metrics and tests noted in section 12 outstanding

## 1. Goal

Detect players active on the CR Roleplay FiveM server but not visible on Discord, and vice versa, so attendance reflects actual presence.

| Discord source | FiveM source | Result |
|---|---|---|
| Connected | Connected | Connected |
| Missing or invisible | Connected | Invisible |
| Connected | Missing or disconnected | Mismatched |
| No member match | Connected | Mismatched |

To support it: receive shared-secret-authenticated lifecycle events, store player identity in `server_members`, store one row per event in `server_logs`, and link server players to existing `members` rows.

Existing `player_logs` is untouched. It holds Discord-presence events and drives attendance and playtime today. Writing FiveM events there would mix two sources, double-count playtime, and destroy the second signal this feature depends on.

## 2. Design decisions and why

Everything below was chosen against one constraint: **the FiveM Lua resource must stay trivial.** It holds no state, generates no ids, and computes no digests.

| Decision | Reason |
|---|---|
| Identity keyed on `(license_id, cid)` | One Rockstar account holds several framework characters. Keyed on license alone they collapsed into one row and the earlier character was lost |
| Two tables, not three | An earlier revision added `server_sessions`. Removed: session state and playtime derive from `server_logs` at read time, which needs no extra table, no status update, and no view |
| Ingestion is append-only and order-independent | The sender delivers asynchronously with independent retries, so reordering is routine. Rejecting a reordered event returned a non-retryable 422 and lost it permanently |
| No locks beyond one advisory lock per body | Order-independence removed the need. The pool is capped at `MaxConns = 5`, so lock contention during a restart burst was a real risk |
| Session id derived server-side | The sender has no session concept and asked not to manage one. Correlated by license instead |
| Idempotency key is the payload | The sender generates no event id. `UNIQUE (payload)` on `jsonb` compares canonicalised content, so a retry dedupes even when the encoder reorders keys — which a hash of raw bytes could not do |
| Shared secret in a header, not an HMAC | Owner decision. FiveM has no built-in HMAC-SHA256, so signing meant bundling a pure-Lua SHA-2 implementation. Traded for TLS-only transport; see section 5 |
| Whole request body stored in `payload` | Debugging and logging, and it means dropping a column never loses data |
| All four identifiers required | Owner decision. Consequence in section 4 |
| Identity conflicts never reject | A non-retryable 422 the sender could not fix would lose the event. Conflicts log at warn and store anyway |

## 3. Data model

Migrations, in order:

```text
000015_create_server_members_and_logs
000016_relax_server_log_ping
000017_align_server_logs_with_derived_payload
000018_reduce_server_logs_to_six_columns
000019_store_payload_drop_event_id
000020_key_server_members_by_license_and_cid
```

`000016`-`000019` each narrow the schema as the payload settled. A fresh database gets the same end state by applying them in order.

### Migration runner constraints (verified)

`internal/database/database.go` embeds `migrations/*.up.sql` and `Migrate` executes **every** `.up.sql` file, in filename order, on every process start. There is no `schema_migrations` table and no version tracking. Two consequences:

1. **Every statement must be safely re-runnable.** `CREATE TABLE IF NOT EXISTS`, `CREATE INDEX IF NOT EXISTS`, `ALTER TABLE ... DROP COLUMN IF EXISTS`, `DROP CONSTRAINT IF EXISTS`. `ALTER TABLE ADD CONSTRAINT` has no `IF NOT EXISTS`, so `000017` guards it with a `pg_constraint` lookup inside a `DO $$` block.
2. **`.down.sql` is never executed.** Only `*.up.sql` is embedded. Down files are manual-rollback documentation.

Every migration in this feature has been applied to production and re-applied to prove idempotency.

`SKIP_MIGRATIONS` remains the escape hatch, and is set for both services in `compose.yaml`, so local runs never migrate.

### `server_members`

One row per stable player identity.

| Column | Type | Rule |
|---|---|---|
| `id` | `BIGINT IDENTITY` | Primary key |
| `member_id` | `BIGINT` | Nullable foreign key to `members.id` |
| `license_id` | `TEXT` | Required. Half of the identity key |
| `discord_user_id` | `TEXT` | The only matching key besides `license_id` |
| `fivem_id` | `TEXT` | Operator reference, never matched on |
| `steamhex` | `TEXT` | Operator reference, never matched on |
| `player_name` | `TEXT` | Latest FiveM display name |
| `username` | `TEXT` | Latest passport/character name |
| `cid` | `TEXT` | Required. The other half of the identity key. Never overwritten |
| `created_at` | `TIMESTAMPTZ` | Default `NOW()` |
| `updated_at` | `TIMESTAMPTZ` | Default `NOW()`. Doubles as "last seen" |

- `member_id REFERENCES members(id) ON DELETE SET NULL` preserves server history if a member is deleted.
- **`UNIQUE (license_id, cid)` is the identity key: one row per framework character.** One Rockstar account can hold several characters - same license, same Steam, different `cid`. Keyed on `license_id` alone they collapsed into one row whose `cid` and `username` were overwritten by whichever event arrived last, so the earlier character vanished. `cid` is `NOT NULL`, so the composite key cannot be defeated by NULLs failing to collide.
- Session correlation follows from this: `findOpenSession` keys on `server_member_id`, so a visit belongs to a character rather than an account. Two characters on one account get separate visits.
- No unique index on `member_id`, `discord_user_id`, `fivem_id`, or `steamhex`. **One member legitimately owns many rows** - several characters, and several licenses over time if they use more than one Steam or Rockstar account. Any per-member aggregation must sum across those rows.
- Index `(member_id)` for the join and `(discord_user_id)` for the relink sweep.

### `server_logs`

One row per event, append-only. Nothing is ever updated.

| Column | Type | Rule |
|---|---|---|
| `id` | `BIGINT IDENTITY` | Primary key, and the ordering tiebreaker |
| `server_member_id` | `BIGINT` | Required foreign key to `server_members.id` |
| `session_id` | `UUID` | Derived. Groups one visit |
| `status` | `TEXT` | `connecting`, `connected`, or `disconnected` |
| `occurred_at` | `TIMESTAMPTZ` | From `event.timestamp` |
| `payload` | `JSONB` | The exact request body. Also the idempotency key |

- `UNIQUE (payload)` provides idempotency. This is the only thing stopping a retry from inserting a second row.
- `payload` is nullable: rows written before `000019` have no body to backfill, and NULLs do not collide in a unique index. Every new row carries it.
- `server_member_id REFERENCES server_members(id) ON DELETE RESTRICT`.
- Check the `status` enum. A typo there would corrupt every read query, so the database enforces it.
- `id` is load-bearing, not decorative: the sender may send the same `event.timestamp` for several events of one visit, so `ORDER BY occurred_at DESC` alone is ambiguous.
- Indexes: `(session_id, occurred_at DESC, id DESC)`, `(server_member_id, occurred_at DESC)`, and `(session_id) WHERE status = 'disconnected'` for the session-correlation anti-join.

No length, range, or format checks. The Go validator enforces every contract limit; duplicating them in SQL only creates two places for a limit to drift.

### Derived reads

Latest state per visit:

```sql
SELECT DISTINCT ON (session_id)
       session_id, server_member_id, status, occurred_at
FROM server_logs
ORDER BY session_id, occurred_at DESC, id DESC;
```

Playtime per visit, tolerating a missing `connected` event:

```sql
SELECT session_id,
       MAX(occurred_at) FILTER (WHERE status = 'disconnected') - MIN(occurred_at) AS duration
FROM server_logs
GROUP BY session_id;
```

`duration` is null while the visit is open. Anything dropped from the schema is still readable from `payload`, for example `payload->'event'->>'reason'` and `payload->'player'->>'ping'`.

## 4. Ingestion semantics

**Append-only and order-independent.** Any authenticated, well-formed event is stored. No event is rejected for arrival order, a missing sibling event, a repeated status, or an identity disagreement. The only rejections are the ones the contract documents.

One transaction, four statements.

**Step 1 - serialise equal bodies.**

```sql
SELECT pg_advisory_xact_lock(hashtextextended($1, 0))
```

`$1` is the body text. The hash is computed by Postgres and never stored. Without it, two concurrent copies of one delivery could both pass the duplicate check below.

**Step 2 - duplicate check.**

```sql
SELECT sl.session_id, sl.server_member_id, sm.member_id
FROM server_logs sl
JOIN server_members sm ON sm.id = sl.server_member_id
WHERE sl.payload = $1::jsonb
```

A hit returns `duplicate: true` with the original visit's match state and touches nothing.

**Step 3 - upsert the identity and resolve the member.**

```sql
WITH previous AS (
    SELECT discord_user_id, fivem_id, steamhex
    FROM server_members WHERE license_id = $1 AND cid = $7
), upserted AS (
    INSERT INTO server_members (license_id, member_id, discord_user_id, fivem_id, steamhex,
                                player_name, username, cid)
    VALUES ($1, (SELECT m.id FROM members m WHERE m.user_id = $2), $2, $3, $4, $5, $6, $7)
    ON CONFLICT (license_id, cid) DO UPDATE SET
        discord_user_id = COALESCE(EXCLUDED.discord_user_id, server_members.discord_user_id),
        fivem_id        = COALESCE(EXCLUDED.fivem_id, server_members.fivem_id),
        steamhex        = COALESCE(EXCLUDED.steamhex, server_members.steamhex),
        player_name     = EXCLUDED.player_name,
        username        = EXCLUDED.username,
        member_id       = COALESCE(
            server_members.member_id,
            (SELECT m.id FROM members m
             WHERE m.user_id = COALESCE(EXCLUDED.discord_user_id, server_members.discord_user_id))
        ),
        updated_at      = NOW()
    RETURNING id, member_id
)
SELECT u.id, u.member_id,
       COALESCE(p.discord_user_id IS NOT NULL AND $2 IS NOT NULL AND p.discord_user_id <> $2, false),
       COALESCE(p.fivem_id        IS NOT NULL AND $3 IS NOT NULL AND p.fivem_id        <> $3, false),
       COALESCE(p.steamhex        IS NOT NULL AND $4 IS NOT NULL AND p.steamhex        <> $4, false)
FROM upserted u LEFT JOIN previous p ON true
```

Member matching is folded in, so there is no separate lookup. Three details carry weight:

- The `previous` CTE reads the pre-insert row, because a CTE sees the snapshot taken when the statement started. That yields the identifier disagreements without a second round trip.
- `COALESCE` on `member_id` means an existing link is never replaced by a different member.
- The `member_id` subquery matches on `COALESCE(EXCLUDED.discord_user_id, server_members.discord_user_id)`, not `EXCLUDED` alone. Otherwise an event that omitted `discord` would fail to link a row that already held a usable one.

Profile fields overwrite unconditionally. A retry arriving out of order can briefly write a display name a few seconds stale; the next event corrects it. Guarding that was not worth an extra column and three `CASE` expressions.

When a supplied identifier differs from a stored non-null value, log at warn with the field name and `server_members.id`. There is no flag column: a log line is enough to notice, and no operator surface exists to work a queue.

**Step 4 - resolve the visit, then insert.**

`connecting` always opens a new visit — it marks the start of an attempt, and a retry of it never reaches here because step 2 already deduped. The other two statuses attach to the player's open visit:

```sql
SELECT sl.session_id
FROM server_logs sl
WHERE sl.server_member_id = $1
    AND NOT EXISTS (
        SELECT 1 FROM server_logs d
        WHERE d.session_id = sl.session_id AND d.status = 'disconnected'
    )
ORDER BY sl.occurred_at DESC, sl.id DESC
LIMIT 1
```

No open visit means one is opened, so a missed `connecting` costs nothing. The session UUID is minted from `crypto/rand`.

```sql
INSERT INTO server_logs (payload, server_member_id, session_id, status, occurred_at)
VALUES ($1::jsonb, $2, $3, $4, $5)
ON CONFLICT (payload) DO NOTHING
RETURNING id
```

Zero rows means duplicate — defence in depth behind step 2, and a success rather than a 500, because the contract classes 5xx as retryable and this delivery could never succeed.

Never match a player by `player_name`, `username`, `cid`, `player.server_id`, or `members.cfx_name`. Those values change or collide.

### Known costs of deriving

- **A visit splits when `disconnected` overtakes `connecting`.** No open visit exists, so a second visit opens. Events are never lost; that visit's duration is wrong.
- **A visit stays open forever if its `disconnected` never arrives** (server crash, lost sender queue). The next `connected` for that player attaches to the stale visit. A sweep that force-closes visits idle for more than 24 hours is not built.
- **A `cid` change creates a new identity row.** `cid` is part of the key, so a framework rename or a reused character slot produces a second row rather than updating the first. Both link to the same member and both sets of visits still count, but the two read as different characters.
- **All four identifiers required means incomplete players are not stored at all.** No `discord:` identifier without the Discord app running, no `steam:` without Steam. Those players return `422` and appear in no state — not even "Mismatched", which is the state section 1 exists to surface. Requiring `license` + `discord` only, or storing the event with an incompleteness flag, are the two ways to close that blind spot.

## 5. Configuration and authentication

`FIVEM_WEBHOOK_SECRET` is in `internal/api.Config`, `.env.example`, and `.env.production.example`. Startup fails when it is blank, shorter than 32 characters, or the example placeholder, following the existing `APP_JWT_SECRET` pattern in `internal/api/config.go`. Never logged.

`internal/serverlog/auth.go` compares it with `subtle.ConstantTimeCompare`. A plain `==` would leak the secret one byte at a time to anyone able to measure response latency. `Bearer <secret>` and surrounding whitespace are tolerated. A clock is injected for deterministic tests.

`Fresh` bounds `event.timestamp` to +/- 300 seconds. With bearer-token authentication this is **not** a replay defence — anyone holding the secret can mint a fresh timestamp. It limits accidental redelivery and stale sender queues, and it is the reason a retry queued beyond five minutes is undeliverable.

> **Two operational requirements, not suggestions.**
>
> **Serve this route over HTTPS only.** The secret is now the entire credential and travels on every request. Over plain HTTP it is readable by anyone on the path.
>
> **Keep it out of logs.** The Go code never logs it, but anything in front of the API that records request headers — nginx, a proxy, an APM agent — would capture it.
>
> The secret that leaked in the first revision of the contract document is in git history and must be treated as compromised. Rotate before go-live: `openssl rand -hex 32`.

Rotation has no overlap window: SOT restarts with the new value and the sender must switch at the same moment. Agree a window with the CR Roleplay developer.

## 6. Backend package

```text
internal/serverlog/model.go            request and stored types, contract limits as constants
internal/serverlog/validator.go        every contract section 3 rule
internal/serverlog/auth.go             shared-secret compare and freshness
internal/serverlog/repository.go       the four-statement transaction
internal/serverlog/relink.go           the relink sweep
internal/serverlog/{validator,auth,repository}_test.go
```

The validator is lenient about shape where leniency costs nothing and strict where a wrong value would corrupt stored data:

- Absent, `null`, and `""` mean the same thing for every field, because Lua JSON encoders spell a missing value all three ways.
- `event.type` accepts bare `connecting` and the legacy `player.connecting`.
- Timestamps accept RFC3339, no-zone, and space-separated forms, all read as UTC.
- A `discord:` prefix on `identifiers.discord` is stripped.
- `player.ping`, `player.server_id`, and `event.reason` are declared on the request type so `DisallowUnknownFields` accepts them, but nothing stores them field-wise and nothing validates them. They survive inside `payload`.

Field limits live in one block of constants shared by the validator and its tests. **The contract document is the source of truth**: a validator stricter than the published contract rejects legitimate traffic with a non-retryable 422.

Validation errors name the field and never echo the rejected value, so they are safe to return to the sender and safe to log. A test asserts that.

## 7. HTTP endpoint

```text
internal/api/server_logs.go
internal/api/ratelimit.go
internal/api/{server_logs,ratelimit}_test.go
```

Registered in `internal/api/handler.go` outside the bearer-token handlers. The shared secret is the sole authentication; there is no member JWT and the route must not be mounted behind the existing auth middleware.

Handler order:

1. Generate a request ID and set `X-SOT-Request-Id`.
2. Require `application/json`.
3. **Authenticate**, then **rate limit** — both before the body is read. An unauthenticated request costs a constant-time compare instead of a 16 KiB read.
4. Read the body through `http.MaxBytesReader` at 16 KiB.
5. Decode exactly one JSON object with `DisallowUnknownFields`; reject trailing data.
6. Require `X-SOT-Contract-Version` to be a served version.
7. Validate.
8. Check `event.timestamp` freshness.
9. Store, and return `202` for new and duplicate events alike.

### Request ID

Generated per request from `crypto/rand`, set on `X-SOT-Request-Id` for **every** response including errors, included in the error envelope, and attached to every log line for that request. The contract tells the sender to log it, so it must always be present.

### Error mapping

| Condition | HTTP | `error.code` |
|---|---|---|
| Unreadable body, not one JSON object, or unknown field | 400 | `INVALID_JSON` |
| Version header missing or not served | 400 | `UNSUPPORTED_CONTRACT_VERSION` |
| Secret missing or wrong | 401 | `INVALID_SECRET` |
| `event.timestamp` outside the window | 401 | `EXPIRED_TIMESTAMP` |
| Body over 16 KiB | 413 | `PAYLOAD_TOO_LARGE` |
| Wrong content type | 415 | `UNSUPPORTED_MEDIA_TYPE` |
| Field validation failure | 422 | `INVALID_EVENT` |
| Rate limit exceeded | 429 | `RATE_LIMITED` |
| Database or internal failure | 500 | `INTERNAL_ERROR` |
| Store or authenticator not wired | 503 | `SERVER_LOGS_UNAVAILABLE` |

`error.message` must never contain the secret, a full license identifier, or raw body content. A test asserts a 500 leaks no connection string.

### Rate limiting

Token bucket for the route: 100 requests per second, burst 500, matching the published contract. One shared secret means one sender, so the bucket is per-endpoint rather than per-credential. Because it now runs before the body read, it also caps unauthenticated traffic.

### Latency budget

Target p99 under 500 ms against the sender's 5 second timeout. Watch the `MaxConns = 5` pool ceiling under load; raise it in `internal/database.Open` only with evidence.

## 8. Automatic relink

The contract promises an event stored with `matched_member: false` is linked later without a resend. `cmd/api` runs an immediate sweep at startup and repeats it every 15 minutes:

```sql
UPDATE server_members sm
SET member_id = m.id, updated_at = NOW()
FROM members m
WHERE sm.member_id IS NULL
  AND sm.discord_user_id IS NOT NULL
  AND m.user_id = sm.discord_user_id
```

There is deliberately no per-user hook. `members` rows are created in bulk by the Discord guild sync (`member.Repository.UpsertGuildMembers`) and by `RecordLog`, not at a single registration call site, so a per-user relink would have no clean home and would fire once per player inside a bulk sync. Worst-case wait is 15 minutes.

Log rows linked per sweep. A steadily rising unmatched count means players are connecting without a Discord identifier — a sender-side or onboarding problem, not an ingestion bug.

Provide an operator unlink path that clears `member_id`, so a mislinked identity can be corrected without editing rows by hand.

## 9. Existing read model integration

Keep `dashboard.Repository` CFX polling unchanged for the first delivery. Webhook ingestion must not silently alter attendance totals.

Read queries for future consumers:

- Latest FiveM status per player, from the `DISTINCT ON (session_id)` query in section 3 joined through `server_members`.
- Unmatched players from `server_members WHERE member_id IS NULL`, ordered by `updated_at DESC`.
- Discord latest status from existing `player_logs`, joined via `server_members.member_id = members.id`, producing the state table in section 1.

Do not replace the current dashboard, attendance, or payslip queries until webhook ingestion has production data and parity monitoring.

## 10. Observability

No metrics package exists in this repository: no Prometheus client, no `expvar`, no `/metrics`. **Not yet built.** The intent is `internal/metrics` backed by stdlib `expvar` on the existing bot health address, with counters for accepted, duplicate, unmatched, identity-flagged, invalid-secret, expired-timestamp, invalid-payload, rate-limited, database failure, and relinked.

Structured logging is in place. Per request: `request_id`, `session_id`, `status`, `duplicate`, `matched_member`, `member_id` when matched, HTTP status, duration. An identifier disagreement logs at warn with the field name and `server_members.id`.

Never log the secret, a full license identifier, a Steam identifier, or the raw body.

## 11. Retention

Keep everything. One RP server produces roughly three rows per visit — thousands a day, not millions.

`payload` roughly triples row width, so revisit sooner than the previous estimate: at roughly 10 million rows, either introduce a retention window or partition monthly by `occurred_at`. Neither needs a schema change now.

## 12. Tests and outstanding work

Passing today: build, `go vet`, `gofmt`, the full test suite, and `-race`.

Covered by unit tests:

- Secret accepted, plus wrong, empty, blank, truncated, extended, and case-changed variants; the authenticator copies the secret so a later mutation cannot widen what is accepted.
- Freshness at both boundaries and outside them.
- Every identifier required, for both absent and blank.
- `discord:` prefix stripping; all accepted timestamp layouts; `event.type` aliases.
- Every length, charset, and prefix limit at its boundary, checked against the contract tables.
- Control characters and invalid UTF-8.
- Fields accepted on the wire but not stored are not validated.
- Validation errors do not echo the rejected value.
- Every row of the section 7 error table, asserting status, `error.code`, and envelope shape.
- `X-SOT-Request-Id` on success and on every error.
- No member JWT required; route not behind auth middleware.
- Oversized body returns 413 without reading the whole body.
- Rate limiter returns 429 with `Retry-After`; per-key buckets; refill; burst cap after idle.
- A nil `Limiter` returns 503 rather than panicking.
- The exact request body reaches the store.
- Repository query shape: transaction-scoped advisory lock, duplicate lookup on `payload`, `ON CONFLICT (payload) DO NOTHING`, member resolved on initial insert.

Verified manually against production data: all three statuses, one derived session across them, `matched_member: true` resolving to `members.id = 3`, byte-identical and key-reordered retries both deduping, playtime derived correctly, `reason` and `ping` recoverable from `payload`, and every error code.

**Outstanding:**

1. **Per-member aggregation is not built.** One member now owns a row per character and per license. The read queries in section 9 are sketches; a naive join would double-count playtime or silently pick one row. Solve this before attendance reads go live.
2. **No repository tests against a real database.** `Store` and `Relink` have no DB-backed coverage; only query strings are asserted. The order-independence claims in section 4 — all six arrival orders converging, split visits, stale open visits — are untested.
3. **`internal/metrics` not built** (section 10).
4. **CI deploy preflight.** `.github/workflows/ci.yml` now checks `FIVEM_WEBHOOK_SECRET`, but `.env.production` on the droplet is hand-maintained; confirm the value is present before the next deploy or the api container crash-loops and `bot` never starts.
5. **Nothing committed.** Six migrations and the whole feature are uncommitted on `main`.
6. **Stale-visit sweep** not built (section 4).
7. **Operator unlink path** not built (section 8).

Regression checks:

```bash
GOCACHE=/tmp/sot-attendance-go-cache go test ./...
GOCACHE=/tmp/sot-attendance-go-cache go test -race ./...
GOCACHE=/tmp/sot-attendance-go-cache go vet ./...
GOCACHE=/tmp/sot-attendance-go-cache go build ./cmd/api ./cmd/bot
```

## 13. Rollout

1. Rotate the secret exposed in the first contract revision.
2. Confirm `FIVEM_WEBHOOK_SECRET` is set in `.env.production` on the droplet.
3. Deploy. Confirm startup fails when the secret is blank or too short.
4. Confirm the public endpoint is HTTPS and that no proxy logs request headers.
5. Deliver the endpoint and secret to the CR Roleplay developer out of band.
6. Verify new, duplicate, out-of-order, unmatched, flagged, wrong-secret, stale-timestamp, partial-identifier, and rate-limited behaviour against a non-production endpoint.
7. Enable the FiveM sender for a small test window.
8. Compare webhook events against current CFX polling and Discord presence.
9. Enable reporting integration only after parity is acceptable.

## 14. Completion criteria

- Every migration applies cleanly twice in a row under the existing startup runner.
- Every authenticated, well-formed event is stored exactly once.
- No event is lost to arrival order, a missing sibling event, a repeated status, or an identity disagreement.
- Every log row links to a `server_members` row; every matched row links to a real `members` row.
- Unmatched identities and their logs remain stored and queryable, and link automatically within 15 minutes of the member registering.
- Anything not stored as a column is recoverable from `payload`.
- Existing Discord `player_logs`, attendance, and payslip results are unchanged.
- Duplicate requests create no extra rows.
- Every response carries `X-SOT-Request-Id`; every error uses the published envelope and a published code.
- Logs and error messages contain no secret, license, or raw body.
- The endpoint is HTTPS only and the leaked secret is rotated.
- Full Go test, race, vet, and build checks pass.
