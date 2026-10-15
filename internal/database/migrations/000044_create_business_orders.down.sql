-- Removing order linkage leaves existing money and stock ledger entries intact.
DROP INDEX IF EXISTS safebox_stock_transactions_order_interaction_idx;
DROP INDEX IF EXISTS safebox_stock_transactions_business_order_item_idx;
ALTER TABLE safebox_stock_transactions
    DROP COLUMN IF EXISTS business_order_item_id,
    DROP COLUMN IF EXISTS discord_interaction_id;
DROP INDEX IF EXISTS money_transactions_discord_interaction_idx;
DROP INDEX IF EXISTS money_transactions_business_order_idx;
ALTER TABLE money_transactions
    DROP COLUMN IF EXISTS business_order_id,
    DROP COLUMN IF EXISTS discord_interaction_id;
DROP TABLE IF EXISTS business_order_items;
DROP TABLE IF EXISTS business_orders;
