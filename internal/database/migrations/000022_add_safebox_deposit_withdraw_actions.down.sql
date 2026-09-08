ALTER TABLE safebox_stock_transactions
    DROP CONSTRAINT IF EXISTS safebox_stock_transactions_action_valid;

ALTER TABLE safebox_stock_transactions
    ADD CONSTRAINT safebox_stock_transactions_action_valid CHECK (
        action IN ('adjustment', 'correction', 'reset')
    );
