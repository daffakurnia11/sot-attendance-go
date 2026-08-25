DROP INDEX IF EXISTS money_transactions_account_created_at_idx;
DROP INDEX IF EXISTS money_transactions_one_opening_per_account_idx;

CREATE UNIQUE INDEX IF NOT EXISTS money_transactions_one_opening_idx
    ON money_transactions (transaction_type)
    WHERE transaction_type = 'opening';

ALTER TABLE money_transactions
    DROP CONSTRAINT IF EXISTS money_transactions_account_type_valid,
    DROP COLUMN IF EXISTS account_type;
