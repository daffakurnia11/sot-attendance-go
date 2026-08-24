# SOT Attendance Backend

Go backend containing Discord attendance bot and member-authenticated web API.

Dashboard CFX players come from the public Cfx.re directory, `https://frontend.cfx-services.net/api/servers/single/<FIVEM_SERVER_CFX_ID>`, where the ID is the short server code in a join link. Reading the directory rather than the game server's own `players.json` means the host only needs outbound HTTPS, not a route to the game server. `FIVEM_PLAYER_ID` filters player names case-insensitively as a substring (for example, `SOT` also matches `EM - SOTxHT BHND`). CFX failure is logged and returned as unavailable without hiding database-backed dashboard statistics.

## Web API authentication

Web API runs as separate `cmd/api` process on `WEB_API_ADDRESS` (`:8080` by default). Local Compose exposes it only on `127.0.0.1:8080`.

Auth.js must call API from its server-side Discord callback. Browser must never send Discord access token directly.

```http
POST /api/v1/auth/discord
Authorization: Bearer <discord-oauth-access-token>
```

API verifies token against Discord `/users/@me`, then looks up verified Discord ID in `members.user_id`. Existing member receives short-lived HS256 app JWT. Missing member receives `403 MEMBER_NOT_REGISTERED`. Username and display name are response metadata only, never identity keys.

```json
{
  "access_token": "<app-jwt>",
  "token_type": "Bearer",
  "expires_at": "2026-08-14T10:15:00Z",
  "member": {
    "id": 1,
    "discord_user_id": "123456789",
    "username": "member",
    "display_name": "Member",
    "character_name": ""
  }
}
```

Generate `APP_JWT_SECRET` with a cryptographically secure generator, for example `openssl rand -base64 48`. Do not reuse `AUTH_SECRET` or Discord client secret. `APP_JWT_TTL` defaults to `15m` and cannot exceed `24h`.

Login lifetime is adjustable through `APP_JWT_TTL`. Keep it equivalent to frontend `AUTH_SESSION_MAX_AGE_SECONDS` (for example `1h` and `3600`). Effective lifetime is whichever expires first.

Health endpoint: `GET /healthz`.

## Discord setup

1. Create an application and bot in [Discord Developer Portal](https://discord.com/developers/applications).
2. Under **Bot > Privileged Gateway Intents**, enable:
   - Presence Intent (required for current activity)
   - Server Members Intent (required for member profile data)
   - Message Content Intent (required for prefix commands)
3. Invite bot to configured guild.
4. Copy local environment file and set token, guild ID, and FiveM server name:

   ```sh
   cp .env.example .env
   ```

Never commit or paste bot token into source or logs. Rotate it in Developer Portal if exposed.

## Run locally

```sh
make local-up      # API and bot
make local-api     # API only
make local-logs
make local-down
```

Both run targets use `DATABASE_URL` from `.env` exactly as written, whether that
names the compose database or a remote one. There is no mode and no check.

Set `SKIP_MIGRATIONS=true` in `.env` to leave the schema untouched at startup.
The migrations are idempotent and destroy nothing, but a branch carrying a new
one would otherwise apply it to whichever database the build points at. Both
processes log `startup migrations skipped` when it is set.

Reaching a remote database from these containers needs the tunnel bound to every
interface, not just loopback: `host.docker.internal` resolves to Docker's
gateway rather than to `127.0.0.1`, so a loopback-only forward is invisible to
them. That also exposes the forwarded port to whatever network the machine is
on, so close it when finished.

Bot status rotates between `N CR players on Discord` and `N CR players on CFX` every `DISCORD_POLL_STATUS` milliseconds. Discord count polls cached presences every `DISCORD_POLL_INTERVAL` milliseconds and includes visible online members whose activity name matches `FIVEM_SERVER_NAME`, ignoring case, spaces, and punctuation. Offline, invisible, and bot accounts are excluded. Discord REST member responses do not contain activities; Presence Intent feeds this cache.

CFX count polls public directory every `FIVEM_SERVER_CFX_POLL_INTERVAL` milliseconds and applies existing `FIVEM_PLAYER_ID` name filter. Failed CFX requests keep last successful count; CFX status remains hidden until first successful poll.

Set `APP_ENV=production` to restrict status counts and player transition logs to members holding `DISCORD_ROLE_ID`. `DISCORD_ROLE_ID` is required in production. Set `APP_ENV=local` to inspect all non-bot guild members during testing; role filtering is disabled even when a role ID is present.

On gateway startup, bot fetches every guild member. Members holding `DISCORD_ROLE_ID` are bulk-upserted into `members` without creating player logs or replacing existing `first_connected_at`. `DISCORD_ADMIN_IDS` accepts comma-separated Discord role IDs and synchronizes `members.is_admin`; members without any configured admin role are cleared.

Player transitions are sent as embeds to `DISCORD_PLAYER_LOG_CHANNEL_ID`: `Connecting..` when activity name is `FiveM` and details contain `Connecting`, `Connected` when activity name matches `FIVEM_SERVER_NAME`, and `Disconnected` when neither signature exists or member becomes offline/invisible. Server text inside FiveM details/state does not count as connected. Repeated polls in same state do not duplicate logs. Embed titles use the saved character name, falling back to the Discord display name when no character name is saved. Embeds omit the server-name description, use Discord activity start timestamps, show `SOT Players: N` without a capacity suffix, and include Discord embed timestamp.

A pending disconnect is delayed for at least 15 seconds (or two poll intervals when longer). If Discord replaces the `FiveM` activity with the server activity during that window, the pending disconnect is cancelled, preventing a false disconnect between `Connecting..` and `Connected`.

`BLACKLISTED_USERS` accepts comma-separated Discord user IDs. Blacklisted users produce no player transition logs, but remain included in bot status count and can still use commands.

`DISCORD_COMMAND_PREFIX` controls message commands.

The bot also registers guild-scoped `/craft`, `/check`, and `/recap` application commands at gateway startup. These slash commands mirror their prefix-command equivalents and appear in Discord's command picker.

Run `/craft` to open an ephemeral multi-product crafting builder. Select a weapon, enter its quantity, and repeat for up to 20 products. The bot combines matching materials into one total, shows the total crafting time, and lets the caller explicitly post the completed result to the channel. Drafts expire after 10 minutes and quantities must be between 1 and 10,000.

For a faster public calculation, use the prefix command with one or more `<weapon-code>:<quantity>` pairs. Example: `!craft vector:30 mp9:20 crx_mk2:5`. Weapon codes are case-insensitive; hyphens are normalized to underscores. Both command forms read the saved `crafting_recipes` data and use the same calculator as the web API.

Run `<prefix>recap` (default `!recap`) to show ranked member playtime with clickable Discord member mentions in an embed. Recap uses `start_attendance` from the `settings` table in `Asia/Jakarta`; session time before that boundary is excluded. Completed sessions and currently connected sessions are included. `playtime_threshold` uses Go duration format such as `90m`; playtime strictly above this threshold appears under Attended, otherwise under Not Attended.

Run `<prefix>check` (default `!check`) to show the caller's character name followed by a clickable Discord member mention in one Name field, plus playtime and attendance status for the same attendance window. Mention one member after the command, such as `!check @member`, to check that member instead. `/check` exposes the same optional member selector. The footer summarizes attended, not-attending, and participating members. A member without playtime in the window appears with `0m` and `Not Attended`.

Daily scheduler sends start broadcast to `DISCORD_PLAYER_CHAT_CHANNEL_ID` at `settings.start_attendance`. At `settings.end_attendance`, it sends closing broadcast to player chat and attendance recap embed to `DISCORD_PLAYER_RECAP_CHANNEL_ID`. Closing and recap delivery are attempted independently. Times use strict `HH:MM` 24-hour format in `Asia/Jakarta`. Example `21:00` start plus `01:00` end handles overnight attendance. Startup does not backfill missed announcements; next scheduled occurrence is used. Missing or invalid attendance settings stop bot startup. Bot reloads schedule every 30 seconds and reloads window plus playtime threshold before end recap, so dashboard edits apply without restart.

Before sending automatic end recap, scheduler bulk upserts one `attendance_logs` row per participant. Each row snapshots capped playtime, required threshold, and attended result. Re-running same attendance window updates existing rows instead of creating duplicates.

Go source is bind-mounted into development container. Air rebuilds and replaces bot process after `.go` file changes; no manual Docker restart needed. Gateway reconnect takes a moment after each rebuild.

Local Compose also starts PostgreSQL. Bot applies embedded SQL migrations at startup. When member reaches `Connected` FiveM state first time, user ID, username, display name, and Discord activity start time are inserted into `members`. Unique `user_id` prevents duplicate rows on later reconnects. Blacklisted users are not stored.

Stop with:

```sh
make down
```

## Quality checks

```sh
make check
```

Runs `lint` (gofmt), `vet`, `test`, `race`, and `build`. CI runs `lint`, `vet`, `test`, and `race` on every pull request.

## Deployment

`.github/workflows/ci.yml` builds both binaries on every push to `main` and publishes two packages, then deploys over SSH:

- `ghcr.io/<owner>/<repo>/api` from the `production-api` Dockerfile target
- `ghcr.io/<owner>/<repo>/bot` from the `production` target

Both descend from the same `build` stage, so the second image is a cache hit on everything but its final `COPY`.

### Droplet prerequisites

Copy `compose.production.yaml` into the deploy directory and create `.env.production` alongside it from `.env.production.example`. The deploy job refuses to restart anything when that file is missing or when `DATABASE_URL`, `APP_JWT_SECRET`, or `DISCORD_BOT_TOKEN` is blank.

`compose.production.yaml` is separate from `compose.yaml` on purpose: the latter is the local development stack with bind mounts and Air, and has nothing a released host should inherit.

`bot` waits for a healthy `api` before starting. Both binaries apply the embedded migrations at startup, and those migrations are bare `CREATE ... IF NOT EXISTS` with no advisory lock, so starting them together can abort on `pg_type_typname_nsp_index`. The dependency serialises the two migrators.

Bot exposes internal `BOT_HEALTH_ADDRESS` readiness endpoint. It returns 200 only after Discord `READY`/`RESUMED` and returns 503 after gateway disconnect. Production Compose blocks deployment until both API and bot are ready.

### Host PostgreSQL

The production stack ships no database. PostgreSQL runs on the host and the containers reach it through the `host.docker.internal:host-gateway` alias.

That alias resolves to the Docker daemon's host-gateway address — the **default bridge** gateway, `172.17.0.1` — regardless of which network the container sits on. So `listen_addresses` needs no entry for this stack's own subnet, and no PostgreSQL restart is required.

The container's **source** address is a different matter: traffic to the host is not NAT-ed, so packets arrive from this stack's pinned subnet. Both the host firewall and `pg_hba.conf` filter on that source, and both must allow it:

```sh
# host firewall — the containers' source subnet reaching the gateway
ufw allow from 172.20.0.0/16 to 172.17.0.1 port 5432 proto tcp
```

```conf
# pg_hba.conf — authorise the same subnet
host    all    all    172.20.0.0/16    scram-sha-256
```

Then `systemctl reload postgresql`; `pg_hba.conf` is reload-only, so there is no downtime for anything else using the database.

This is why the compose network subnet is pinned to `172.20.0.0/16`: both rules above name it literally, and an allocator-chosen block would silently stop matching.

Symptoms when one is missing: the firewall rule shows as a **connection timeout**, the `pg_hba.conf` rule as `no pg_hba.conf entry for host`.

### Reverse proxy

The API publishes to loopback only and is fronted by Caddy on the host:

```caddyfile
sot-api.dafkur.com {
    reverse_proxy 127.0.0.1:8081
}
```

Port `8081` is `API_PORT` from `.env.production`. Pick a port nothing else on the host has claimed.

### Repository configuration

Create a `production` environment and set these secrets:

| Secret               | Purpose                                                                  |
| -------------------- | ------------------------------------------------------------------------ |
| `DO_HOST`            | Droplet hostname or IP                                                   |
| `DO_USER`            | SSH user                                                                 |
| `DO_SSH_PRIVATE_KEY` | SSH private key for that user                                            |
| `DO_APP_DIR`         | Deploy directory holding `compose.production.yaml` and `.env.production` |
| `GHCR_USERNAME`      | GHCR account used by the droplet to pull                                 |
| `GHCR_TOKEN`         | Personal access token with `read:packages`                               |

Pushing to GHCR uses the built-in `GITHUB_TOKEN`; `GHCR_USERNAME` and `GHCR_TOKEN` are only for the pull side on the droplet.

The deploy pins exact image digests and runs `docker compose up -d --wait`, which blocks until the API passes `/healthz`. A crash-looping container fails the deploy instead of reporting success. Roll back by re-running compose with `API_IMAGE` and `BOT_IMAGE` set to previous digests.

## Layout

```text
cmd/api/          web API process entrypoint
cmd/bot/          process entrypoint
internal/app/     Discord gateway lifecycle, handlers, dependency wiring
internal/api/     HTTP routes, Discord identity verification, API configuration
internal/auth/    signed application access tokens
internal/presence/ FiveM matching, status counter, transition logging
internal/attendance/ daily Asia/Jakarta scheduler
internal/command/attendance/ attendance announcement presentation
internal/command/crafting/ crafting command parsing and Discord presentation
internal/command/router/     prefix command routing
internal/config/  environment validation
internal/crafting/ crafting recipe persistence and shared calculator
internal/database/ PostgreSQL pool and embedded SQL migrations
internal/discord/embed/ reusable Discord embed builder
internal/member/  member persistence
```
