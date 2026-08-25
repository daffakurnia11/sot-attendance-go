ALTER TABLE settings
    ADD COLUMN IF NOT EXISTS created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    ADD COLUMN IF NOT EXISTS updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

INSERT INTO settings (settings, value)
VALUES
    ('office_money_balance', '1012500'),
    ('office_money_initialized', 'true')
ON CONFLICT (settings) DO NOTHING;

CREATE TABLE IF NOT EXISTS money_transactions (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    actor_member_id BIGINT NOT NULL,
    transaction_type TEXT NOT NULL,
    direction TEXT NOT NULL,
    amount BIGINT NOT NULL,
    balance_before BIGINT NOT NULL,
    balance_after BIGINT NOT NULL,
    reason TEXT NOT NULL,
    reversed_transaction_id BIGINT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT money_transactions_actor_foreign_key
        FOREIGN KEY (actor_member_id)
        REFERENCES members (id) ON DELETE RESTRICT,
    CONSTRAINT money_transactions_reversed_foreign_key
        FOREIGN KEY (reversed_transaction_id)
        REFERENCES money_transactions (id) ON DELETE RESTRICT,
    CONSTRAINT money_transactions_type_valid CHECK (
        transaction_type IN ('opening', 'deposit', 'withdrawal', 'reversal')
    ),
    CONSTRAINT money_transactions_direction_valid CHECK (
        direction IN ('credit', 'debit')
    ),
    CONSTRAINT money_transactions_amount_positive CHECK (amount > 0),
    CONSTRAINT money_transactions_balances_not_negative CHECK (
        balance_before >= 0 AND balance_after >= 0
    ),
    CONSTRAINT money_transactions_reason_not_blank CHECK (
        BTRIM(reason) <> '' AND CHAR_LENGTH(reason) <= 500
    ),
    CONSTRAINT money_transactions_balance_change_valid CHECK (
        (direction = 'credit' AND balance_after = balance_before + amount)
        OR (direction = 'debit' AND balance_after = balance_before - amount)
    ),
    CONSTRAINT money_transactions_type_direction_valid CHECK (
        (transaction_type IN ('opening', 'deposit') AND direction = 'credit')
        OR (transaction_type = 'withdrawal' AND direction = 'debit')
        OR transaction_type = 'reversal'
    ),
    CONSTRAINT money_transactions_opening_valid CHECK (
        transaction_type <> 'opening' OR balance_before = 0
    ),
    CONSTRAINT money_transactions_reversal_reference_valid CHECK (
        (transaction_type = 'reversal' AND reversed_transaction_id IS NOT NULL)
        OR (transaction_type <> 'reversal' AND reversed_transaction_id IS NULL)
    ),
    CONSTRAINT money_transactions_reversed_once UNIQUE (
        reversed_transaction_id
    )
);

CREATE INDEX IF NOT EXISTS money_transactions_created_at_idx
    ON money_transactions (created_at DESC, id DESC);

CREATE UNIQUE INDEX IF NOT EXISTS money_transactions_one_opening_idx
    ON money_transactions (transaction_type)
    WHERE transaction_type = 'opening';

CREATE INDEX IF NOT EXISTS money_transactions_actor_idx
    ON money_transactions (actor_member_id, created_at DESC);
