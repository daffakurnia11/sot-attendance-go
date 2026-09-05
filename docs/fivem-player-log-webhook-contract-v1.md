# CR Roleplay Player Log Webhook Contract

**Version:** 1.0
**Updated:** 3 September 2026
**For:** CR Roleplay developer

Send player lifecycle events from FiveM server-side code to SOT Attendance. Never call this webhook from a client resource.

## 1. Endpoint

```text
POST https://sot-api.dafkur.com/api/v1/webhooks/server-logs
Content-Type: application/json
Maximum body: 16 KiB
```

Note the host: **`sot-api.dafkur.com`**, not `sot.dafkur.com`. The latter serves the member web app, which answers `307` and redirects to its login page for anything it does not recognise — including this path. A `307` means you have the wrong host.

Use a 5 second client-side timeout. SOT targets a p99 response under 500 ms, so a request still running at 5 seconds is a failure, not a slow success.

## 2. Authentication

One shared secret, sent in a header on every request:

| Header | Value |
|---|---|
| `Content-Type` | `application/json` |
| `X-SOT-Contract-Version` | `1.0` |
| `X-SOT-Secret` | The shared secret |

The secret is delivered out of band and is never written in this document, in the repository, or in any chat transcript. Ask the SOT owner for it.

`Bearer <secret>` is also accepted, and surrounding whitespace is trimmed.

> **This is bearer-token authentication, not a signature.** The secret travels on every request, so:
>
> - **Use HTTPS only.** Over plain HTTP anyone on the network path reads the secret.
> - Keep it out of logs. Do not print request headers in the FiveM console or in any log shipper.
> - Nothing proves the body was not altered in transit; TLS is what protects it.
>
> Replacing the secret is a coordinated cutover: SOT restarts with the new value and the sender must switch at the same moment, so agree a window with the SOT owner first. Requests carrying a retired secret return `401 INVALID_SECRET`.

## 3. Event payload

```json
{
  "player": {
    "server_id": 142,
    "name": "SOT - Ayvix",
    "username": "Kenji Nakamura",
    "cid": "CID-1024",
    "identifiers": {
      "license": "license:abc123",
      "discord": "406954574998536202",
      "fivem": "fivem:123123",
      "steamhex": "steam:110000112345678"
    },
    "ping": 34
  },
  "event": {
    "type": "connecting",
    "timestamp": "2026-09-03T02:29:46Z",
    "reason": null
  }
}
```

Unknown fields are rejected. There is no `contract_version` in the body — the version is the header alone.

### `player`

| Field | Type | Required | Rule |
|---|---|---:|---|
| `server_id` | integer | yes | FiveM source/server ID. Accepted as sent |
| `name` | string | yes | 1-128 characters. Current FiveM display name |
| `username` | string | yes | 1-128 characters. Passport or character name |
| `cid` | string | yes | 1-64 characters. Character ID. **Must be stable for the life of a character** - half the identity key |
| `identifiers` | object | yes | See below. Three of the four fields are required |
| `ping` | integer | yes | Accepted as sent. Send it on every status, including `disconnected` |

`cid` and `steamhex` together are the identity SOT stores: a player is one **character on one Steam account**, so an account with several framework characters is tracked as several players, each with its own visits and playtime.

Both must be stable. Send the framework's real character identifier for `cid`, never a per-session or per-slot value that changes between logins. A change to either reads as a different player and starts a fresh history.

`license` is deliberately *not* part of the identity - it changes on a reinstall - so it is stored as data and follows the latest event. A license that differs from the one on file for the same character is accepted and flagged for review on our side; you need do nothing.

### `player.identifiers`

`license`, `discord` and `steamhex` are **required**: a missing key, a JSON `null`, and `""` are rejected identically. `fivem` is **optional**.

| Field | Required | Rule |
|---|---:|---|
| `license` | yes | 9-128 characters, must start with `license:` |
| `discord` | yes | 17-20 digits. A `discord:` prefix is stripped if you send it |
| `steamhex` | yes | 7-64 characters, must start with `steam:`. **The other half of the identity key**, so it must be stable |
| `fivem` | no | Stored as sent, up to 64 characters. No format rule. Absent, `null` and `""` are all treated as "not supplied" |

> The resource must **abort the call** when `GetPlayerIdentifiers` does not yield `license`, `discord` and `steamhex`, rather than send a partial payload. A partial payload returns `422` and the event is not stored at all — the player will not appear in attendance in any form.
>
> `fivem` is exempt: send it when you have it and omit it otherwise. It is absent whenever the player has no CFX account attached, and it is decorative — nothing matches on it — so refusing the event over it would lose the player for no gain.
>
> In practice `discord:` requires the Discord desktop app to be running, and `steam:` requires Steam. Players on the Rockstar or Epic launcher, or with Discord closed, produce no such identifier and cannot be reported.

### `event`

| Field | Required | Rule |
|---|---:|---|
| `type` | yes | `connecting`, `connected`, or `disconnected`. The older `player.connecting` spellings are also accepted |
| `timestamp` | yes | UTC, within **300 seconds** of SOT server time. RFC3339 (`2026-09-03T02:29:46Z`) preferred; `2026-09-03T02:29:46` and `2026-09-03 02:29:46` are also accepted and read as UTC |
| `reason` | no | Disconnect reason. `null` or absent is fine. Meaningful only on `disconnected` |

In Lua the timestamp is `os.date("!%Y-%m-%dT%H:%M:%SZ")`. The leading `!` is what makes it UTC — omitting it yields server-local time and returns `401 EXPIRED_TIMESTAMP` for any server not on UTC.

### What SOT stores

The whole request body is stored verbatim as JSON, so nothing you send is lost. `player.server_id`, `player.ping`, and `event.reason` are kept only inside that stored body; no field-level rule is applied to them.

## 4. Lifecycle

1. Send `player.connecting` when the player starts connecting.
2. Send `player.connected` once they are accepted.
3. Send `player.disconnected` from `playerDropped`.
4. Send asynchronously. Webhook failure must never reject, delay, or kick a player.

**There is no session id to manage.** SOT groups the three events into one visit itself, by license: a `connecting` event opens a visit, and `connected` and `disconnected` attach to that player's open visit. The response tells you which `session_id` was used, for logging only.

**Arrival order does not matter and no event is rejected for being out of order.** A `disconnected` that overtakes its own `connecting` is still stored; it starts a new visit rather than joining the earlier one, so that visit's duration will be wrong. Events are never lost to ordering.

A visit missing an event is stored and remains queryable.

## 5. Idempotency

**The request body is the idempotency key.** Re-send the same event and SOT returns `202` with `"duplicate": true`, writing nothing new.

The comparison is on JSON *content*, not bytes, so a retry still dedupes when your JSON encoder reorders keys or changes whitespace. You do not need to preserve the exact original string — though keeping it is cheaper than rebuilding it.

What does **not** dedupe: a retry with a refreshed `event.timestamp`. That is a different event as far as SOT is concerned, and it will be stored twice. **Retry the original body, with its original timestamp.**

Because `event.timestamp` must stay within 300 seconds, a retry queued longer than that can never be delivered. Drop events older than five minutes rather than retrying them forever.

## 6. Rate limits

| Limit | Value |
|---|---|
| Sustained | 100 requests per second |
| Burst | 500 requests |

A restart reconnecting 250 players produces roughly 750 events, which the burst plus one second of sustained allowance absorbs.

Exceeding the limit returns `429 RATE_LIMITED` with a `Retry-After` header in seconds. Honour `Retry-After` instead of the default backoff when it is present.

## 7. Responses

Every response carries an `X-SOT-Request-Id` header. Log it; quote it when reporting a problem.

A new or duplicate valid event returns `202 Accepted`:

```json
{
  "session_id": "4470af26-0b4a-4c1f-9a2e-6d5f1c3b7e88",
  "accepted": true,
  "duplicate": false,
  "matched_member": true
}
```

`matched_member: false` is a **success**. It means no registered SOT member is currently linked to this player. SOT stores the event and links it automatically once a matching Discord identity registers, within 15 minutes. **No resend is required, ever.** Never treat `false` as an error or a retry trigger.

### Error responses

Every non-2xx response uses this envelope:

```json
{
  "error": {
    "code": "INVALID_SECRET",
    "message": "X-SOT-Secret is missing or does not match",
    "request_id": "641a0aca7aa19eb335bb11a4d4c8ef37"
  }
}
```

Branch on `error.code`, never on `error.message`. Message text may change without a version bump.

| Status | `error.code` | Meaning |
|---|---|---|
| 400 | `INVALID_JSON` | Body unreadable, not a single JSON object, or carries an unknown field |
| 400 | `UNSUPPORTED_CONTRACT_VERSION` | `X-SOT-Contract-Version` missing or not served |
| 401 | `INVALID_SECRET` | `X-SOT-Secret` missing or does not match |
| 401 | `EXPIRED_TIMESTAMP` | `event.timestamp` more than 300 seconds from SOT server time |
| 413 | `PAYLOAD_TOO_LARGE` | Body over 16 KiB |
| 415 | `UNSUPPORTED_MEDIA_TYPE` | Content type is not `application/json` |
| 422 | `INVALID_EVENT` | A field fails a rule in section 3. `error.message` names the field |
| 429 | `RATE_LIMITED` | Limit from section 6 exceeded |
| 500 | `INTERNAL_ERROR` | SOT-side failure |
| 503 | `SERVER_LOGS_UNAVAILABLE` | Ingestion not configured on the SOT side |

There is no error code for conflicting player identity. If reported identifiers disagree with what SOT already holds for a license, the event is still accepted with `202`; SOT flags it for operator review. Identity questions never block ingestion and never require sender action.

## 8. Retries

| Result | Action |
|---|---|
| Network failure or timeout | Retry with backoff |
| `429 RATE_LIMITED` | Retry, honouring `Retry-After` |
| HTTP 5xx | Retry with backoff |
| `401 INVALID_SECRET` | Do not retry. Fix configuration and alert an operator |
| `401 EXPIRED_TIMESTAMP` | Do not retry. The body cannot be re-timestamped without becoming a new event. Drop it |
| Other HTTP 4xx | Do not retry. Log and alert; this indicates a sender bug |
| HTTP 2xx | Stop retrying |

Suggested delays: 1, 2, 4, 8, 16, then 30 seconds with jitter. Maximum 10 attempts, which finishes inside the 300 second timestamp window. Reuse the original body for every retry.

Log only `event.type`, HTTP status, `error.code`, attempt count, and `X-SOT-Request-Id`. Never log the secret or a full license identifier.

## 9. Versioning

`X-SOT-Contract-Version` must be `1.0`.

A minor version increase adds optional fields only and never removes or retypes an existing field. SOT continues to accept the previous minor version for at least 90 days after announcing a new one. A breaking change increases the major version and is announced separately.

## 10. cURL examples

Replace player values before testing. Export the secret rather than pasting it into a file:

```bash
read -rs -p 'shared secret: ' SOT_SECRET; echo
BASE='https://sot-api.dafkur.com/api/v1/webhooks/server-logs'

send_event() {
  curl --request POST "${BASE}" \
    --max-time 5 \
    --header 'Content-Type: application/json' \
    --header 'X-SOT-Contract-Version: 1.0' \
    --header "X-SOT-Secret: ${SOT_SECRET}" \
    --data-binary "$1"
}

IDS='"license":"license:abc123","discord":"406954574998536202","fivem":"fivem:123123","steamhex":"steam:110000112345678"'
PLAYER="\"server_id\":142,\"name\":\"SOT - Ayvix\",\"username\":\"Kenji Nakamura\",\"cid\":\"CID-1024\",\"identifiers\":{${IDS}},\"ping\":34"
NOW="$(date -u +%Y-%m-%dT%H:%M:%SZ)"
```

### Connecting

```bash
send_event "{\"player\":{${PLAYER}},\"event\":{\"type\":\"connecting\",\"timestamp\":\"${NOW}\",\"reason\":null}}"
```

### Connected

```bash
send_event "{\"player\":{${PLAYER}},\"event\":{\"type\":\"connected\",\"timestamp\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",\"reason\":null}}"
```

### Disconnected

```bash
send_event "{\"player\":{${PLAYER}},\"event\":{\"type\":\"disconnected\",\"timestamp\":\"$(date -u +%Y-%m-%dT%H:%M:%SZ)\",\"reason\":\"Connection Timed Out\"}}"
```

### Verify idempotency

Send any of the above twice. The second returns `202` with `"duplicate": true` and stores nothing new.

## 11. Lua sketch

```lua
local ENDPOINT = 'https://sot-api.dafkur.com/api/v1/webhooks/server-logs'
local SECRET   = GetConvar('sot_webhook_secret', '')   -- server.cfg, never a client file

local function identifiers(src)
    local out = {}
    for _, id in ipairs(GetPlayerIdentifiers(src)) do
        if     id:sub(1, 8) == 'license:' then out.license  = id
        elseif id:sub(1, 8) == 'discord:' then out.discord  = id:sub(9)
        elseif id:sub(1, 6) == 'fivem:'   then out.fivem    = id
        elseif id:sub(1, 6) == 'steam:'   then out.steamhex = id
        end
    end
    -- license, discord and steamhex are required; a partial payload is refused
    -- with 422. fivem is optional and simply omitted when absent.
    if out.license and out.discord and out.steamhex then return out end
    return nil
end

local function send(src, eventType, reason)
    local ids = identifiers(src)
    if not ids then return end

    local body = json.encode({
        player = {
            server_id   = src,
            name        = GetPlayerName(src),
            username    = getPassportName(src),   -- your framework
            cid         = getCharacterId(src),    -- your framework
            identifiers = ids,
            ping        = GetPlayerPing(src),
        },
        event = {
            type      = eventType,
            timestamp = os.date('!%Y-%m-%dT%H:%M:%SZ'),   -- the ! means UTC
            reason    = reason,
        },
    })

    PerformHttpRequest(ENDPOINT, function(status)
        if status ~= 202 then
            -- Retry 5xx/429/timeout with the SAME body. Never rebuild it with a
            -- fresh timestamp: that is a new event and will be stored twice.
        end
    end, 'POST', body, {
        ['Content-Type']           = 'application/json',
        ['X-SOT-Contract-Version'] = '1.0',
        ['X-SOT-Secret']           = SECRET,
    })
end

AddEventHandler('playerConnecting', function() send(source, 'connecting') end)
AddEventHandler('playerJoining',    function() send(source, 'connected') end)
AddEventHandler('playerDropped',    function(reason) send(source, 'disconnected', reason) end)
```

No HMAC library, no UUID generation, no session table. The resource holds no state between events.

## 12. Delivery checklist

- [ ] All requests originate from a server-side resource.
- [ ] Endpoint is HTTPS.
- [ ] Secret is read from server-only configuration, never from a file in version control or a client resource.
- [ ] `license`, `discord` and `steamhex` are present, or the call is skipped. `fivem` is sent when available.
- [ ] `event.timestamp` is UTC (`os.date` with the leading `!`).
- [ ] A new event is sent for each of the three statuses.
- [ ] Retries resend the original body unchanged, including its timestamp.
- [ ] Events older than 5 minutes are dropped, not retried.
- [ ] `Retry-After` is honoured on 429.
- [ ] `error.code` drives retry decisions, not `error.message`.
- [ ] `X-SOT-Request-Id` is logged for every response.
- [ ] The queue and retries never block the game thread.
- [ ] The secret never enters a log line or a client resource.
- [ ] Tested: all three statuses, duplicate body, out-of-order delivery, wrong secret, stale timestamp, partial identifiers, oversized body.
