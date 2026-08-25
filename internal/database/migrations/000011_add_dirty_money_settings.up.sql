INSERT INTO settings (settings, value)
VALUES
    ('dirty_money_balance', '18000'),
    ('dirty_money_initialized', 'true')
ON CONFLICT (settings) DO NOTHING;
