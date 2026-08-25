ALTER TABLE money_transactions
    ADD COLUMN IF NOT EXISTS account_type TEXT NOT NULL DEFAULT 'office';

ALTER TABLE money_transactions
    DROP CONSTRAINT IF EXISTS money_transactions_account_type_valid;

ALTER TABLE money_transactions
    ADD CONSTRAINT money_transactions_account_type_valid
        CHECK (account_type IN ('office', 'dirty'));

DROP INDEX IF EXISTS money_transactions_one_opening_idx;

CREATE UNIQUE INDEX IF NOT EXISTS money_transactions_one_opening_per_account_idx
    ON money_transactions (account_type, transaction_type)
    WHERE transaction_type = 'opening';

CREATE INDEX IF NOT EXISTS money_transactions_account_created_at_idx
    ON money_transactions (account_type, created_at DESC, id DESC);
