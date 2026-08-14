# SOT Attendance Backend

Go backend containing Discord attendance bot and member-authenticated web API.

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
make local-up
make local-logs
```

Bot status shows `N playing CR Roleplay`. Counter polls cached Discord presences every `DISCORD_POLL_INTERVAL` milliseconds. It includes visible online members whose Discord activity name, details, or state matches `FIVEM_SERVER_NAME`, ignoring case, spaces, and punctuation. Offline, invisible, and bot accounts are excluded. Discord REST member responses do not contain activities; Presence Intent feeds this cache.

Set `APP_ENV=production` to restrict status counts and player transition logs to members holding `DISCORD_ROLE_ID`. `DISCORD_ROLE_ID` is required in production. Set `APP_ENV=local` to inspect all non-bot guild members during testing; role filtering is disabled even when a role ID is present.

Player transitions are sent as embeds to `DISCORD_PLAYER_LOG_CHANNEL_ID`: `Connecting..` when activity name is `FiveM` and details contain `Connecting`, `Connected` when activity name matches `FIVEM_SERVER_NAME`, and `Disconnected` when neither signature exists or member becomes offline/invisible. Server text inside FiveM details/state does not count as connected. Repeated polls in same state do not duplicate logs. Embeds use Discord activity start timestamps, show `SOT Players: N` without a capacity suffix, and include Discord embed timestamp.

A pending disconnect is delayed for at least 15 seconds (or two poll intervals when longer). If Discord replaces the `FiveM` activity with the server activity during that window, the pending disconnect is cancelled, preventing a false disconnect between `Connecting..` and `Connected`.

`BLACKLISTED_USERS` accepts comma-separated Discord user IDs. Blacklisted users produce no player transition logs, but remain included in bot status count and can still use commands.

`DISCORD_COMMAND_PREFIX` controls message commands. Run `<prefix>me` (default `!me`) to show display name, username, FiveM playtime, Discord-reported activity start time, avatar thumbnail, and timestamp. Non-playing members show `Not Played`. Playtime is calculated from Discord activity `timestamps.start`; when Discord omits or sends an invalid start timestamp, playtime and start time show `Unavailable`.

Run `<prefix>recap` (default `!recap`) to show ranked member playtime in an embed. Recap uses `start_attendance` from the `settings` table in `Asia/Jakarta`; session time before that boundary is excluded. Completed sessions and currently connected sessions are included. `playtime_threshold` uses Go duration format such as `90m`; playtime strictly above this threshold appears under Attended, otherwise under Not Attending.

Discord Administrators can announce attendance in the current channel with `<prefix>attendance-start` and `<prefix>attendance-end`. Start copy invites members to launch FiveM and join `FIVEM_SERVER_NAME`; end copy thanks players. Both embeds omit time fields, retain footer timestamps, and send an explicit `@here` mention. Discord `@here` alerts online members who can access the channel, not offline members.

Daily scheduler sends start broadcast to `DISCORD_PLAYER_CHAT_CHANNEL_ID` at `settings.start_attendance`. At `settings.end_attendance`, it sends closing broadcast to player chat and attendance recap embed to `DISCORD_PLAYER_RECAP_CHANNEL_ID`. Closing and recap delivery are attempted independently. Times use strict `HH:MM` 24-hour format in `Asia/Jakarta`. Example `21:00` start plus `01:00` end handles overnight attendance. Startup does not backfill missed announcements; next scheduled occurrence is used. Missing or invalid attendance settings stop bot startup.

Before sending automatic end recap, scheduler bulk upserts one `attendance_logs` row per participant. Each row snapshots capped playtime, required threshold, and attended result. Re-running same attendance window updates existing rows instead of creating duplicates.

Go source is bind-mounted into development container. Air rebuilds and replaces bot process after `.go` file changes; no manual Docker restart needed. Gateway reconnect takes a moment after each rebuild.

Local Compose also starts PostgreSQL. Use `make local-db-up` to start database without bot. Bot applies embedded SQL migrations at startup. When member reaches `Connected` FiveM state first time, user ID, username, display name, and Discord activity start time are inserted into `members`. Unique `user_id` prevents duplicate rows on later reconnects. Blacklisted users are not stored.

Stop with:

```sh
make local-down
```

## Quality checks

```sh
make check
```

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
internal/command/profile/    profile command presentation
internal/command/router/     prefix command routing
internal/config/  environment validation
internal/database/ PostgreSQL pool and embedded SQL migrations
internal/discord/embed/ reusable Discord embed builder
internal/member/  member persistence
```
