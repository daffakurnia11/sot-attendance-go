-- One row per visit a source recorded, with its edges snapped to the webhook.
--
-- server_logs holds three witnesses to the same visit: the CR Roleplay webhook
-- ('server'), Discord activity ('discord') and the Cfx.re roster ('cfx'). Each
-- keeps its own sessions, and readers union them, so a player seen by all
-- three is credited once. The weaker two are late or early by their own lag -
-- CFX by its directory cache, Discord by its absence streak - and a plain union
-- would add that lag to every visit the webhook reported exactly.
--
-- So the webhook wins where it spoke. A discord or cfx visit whose connect lies
-- within ten minutes of a webhook connect for the same character starts at the
-- webhook's time; the same for disconnects. Where the webhook said nothing, the
-- weaker witness stands on its own, which is the case it exists for.
--
-- Recreated on every boot: the runner replays migrations and a view has no
-- data to lose. DROP first because CREATE OR REPLACE cannot change columns.
DROP VIEW IF EXISTS server_visits;

CREATE VIEW server_visits AS
WITH raw AS (
    SELECT sl.session_id, sl.server_member_id, sl.source,
        MIN(sl.occurred_at) FILTER (WHERE sl.status = 'connected') AS connected_at,
        MAX(sl.occurred_at) FILTER (WHERE sl.status = 'disconnected') AS disconnected_at,
        MIN(sl.occurred_at) AS first_event_at,
        MAX(sl.occurred_at) AS last_event_at,
        -- The slot from the visit's own connected event. A connecting event
        -- carries a temporary deferral number instead, which names nothing.
        (ARRAY_AGG(sl.payload->'player'->>'server_id' ORDER BY sl.occurred_at)
            FILTER (WHERE sl.status = 'connected'))[1] AS server_id
    FROM server_logs sl
    GROUP BY sl.session_id, sl.server_member_id, sl.source
), start_snap AS (
    SELECT DISTINCT ON (w.session_id) w.session_id, s.connected_at
    FROM raw w
    JOIN raw s ON s.server_member_id = w.server_member_id AND s.source = 'server'
    WHERE w.source <> 'server'
        AND s.connected_at BETWEEN w.connected_at - INTERVAL '10 minutes'
            AND w.connected_at + INTERVAL '10 minutes'
    ORDER BY w.session_id, ABS(EXTRACT(EPOCH FROM (s.connected_at - w.connected_at)))
), end_snap AS (
    SELECT DISTINCT ON (w.session_id) w.session_id, s.disconnected_at
    FROM raw w
    JOIN raw s ON s.server_member_id = w.server_member_id AND s.source = 'server'
    WHERE w.source <> 'server'
        AND s.disconnected_at BETWEEN w.disconnected_at - INTERVAL '10 minutes'
            AND w.disconnected_at + INTERVAL '10 minutes'
    ORDER BY w.session_id, ABS(EXTRACT(EPOCH FROM (s.disconnected_at - w.disconnected_at)))
)
SELECT raw.session_id, raw.server_member_id, raw.source, raw.server_id,
    raw.first_event_at, raw.last_event_at,
    COALESCE(start_snap.connected_at, raw.connected_at) AS connected_at,
    COALESCE(end_snap.disconnected_at, raw.disconnected_at) AS disconnected_at
FROM raw
LEFT JOIN start_snap ON start_snap.session_id = raw.session_id
LEFT JOIN end_snap ON end_snap.session_id = raw.session_id;
