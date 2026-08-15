INSERT INTO settings (settings, value)
VALUES ('start_date_contract', '28')
ON CONFLICT (settings) DO NOTHING;
