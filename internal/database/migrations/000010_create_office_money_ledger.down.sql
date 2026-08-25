DROP TABLE IF EXISTS money_transactions;

DELETE FROM settings
WHERE settings IN ('office_money_balance', 'office_money_initialized');

ALTER TABLE settings
    DROP COLUMN IF EXISTS updated_at,
    DROP COLUMN IF EXISTS created_at;
