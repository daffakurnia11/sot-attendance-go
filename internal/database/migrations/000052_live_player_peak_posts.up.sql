ALTER TABLE player_peak_reports ADD COLUMN IF NOT EXISTS posted_count INTEGER NOT NULL DEFAULT -1;
UPDATE player_peak_reports SET posted_count=member_count WHERE delivered_message_id IS NOT NULL AND posted_count=-1;
