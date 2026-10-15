-- Order terms are frozen at creation. Totals and settlement limits are
-- validated by the application inside the same transaction as ledger writes.
CREATE TABLE IF NOT EXISTS business_orders (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    partner_id BIGINT NOT NULL REFERENCES business_partners(id) ON DELETE RESTRICT,
    direction TEXT NOT NULL CHECK (direction IN ('purchase', 'sale')),
    order_date DATE NOT NULL,
    pickup_date DATE NOT NULL,
    status TEXT NOT NULL DEFAULT 'open',
    additional_price BIGINT NOT NULL DEFAULT 0,
    total_price BIGINT NOT NULL CHECK (total_price >= 0),
    notes TEXT NOT NULL DEFAULT '',
    created_by_member_id BIGINT NOT NULL REFERENCES members(id) ON DELETE RESTRICT,
    discord_message_id TEXT UNIQUE CHECK (BTRIM(discord_message_id) <> ''),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT business_orders_additional_price_notes CHECK (
        additional_price = 0 OR BTRIM(notes) <> ''
    )
);

CREATE TABLE IF NOT EXISTS business_order_items (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    order_id BIGINT NOT NULL REFERENCES business_orders(id) ON DELETE RESTRICT,
    stock_item_id BIGINT NOT NULL REFERENCES safebox_stock_items(id) ON DELETE RESTRICT,
    product_name TEXT NOT NULL CHECK (BTRIM(product_name) <> ''),
    unit TEXT NOT NULL CHECK (BTRIM(unit) <> ''),
    unit_price BIGINT NOT NULL CHECK (unit_price >= 0),
    quantity BIGINT NOT NULL CHECK (quantity > 0),
    stock_units_per_unit BIGINT NOT NULL CHECK (stock_units_per_unit > 0)
);

ALTER TABLE money_transactions
    ADD COLUMN IF NOT EXISTS business_order_id BIGINT REFERENCES business_orders(id) ON DELETE RESTRICT,
    ADD COLUMN IF NOT EXISTS discord_interaction_id TEXT
        CHECK (BTRIM(discord_interaction_id) <> '');

ALTER TABLE safebox_stock_transactions
    ADD COLUMN IF NOT EXISTS business_order_item_id BIGINT REFERENCES business_order_items(id) ON DELETE RESTRICT,
    ADD COLUMN IF NOT EXISTS discord_interaction_id TEXT
        CHECK (BTRIM(discord_interaction_id) <> '');

CREATE INDEX IF NOT EXISTS business_orders_partner_date_idx
    ON business_orders(partner_id, order_date DESC, id DESC);
CREATE INDEX IF NOT EXISTS business_orders_direction_status_idx
    ON business_orders(direction, status, pickup_date, id);
CREATE INDEX IF NOT EXISTS business_order_items_order_idx
    ON business_order_items(order_id);
CREATE INDEX IF NOT EXISTS money_transactions_business_order_idx
    ON money_transactions(business_order_id) WHERE business_order_id IS NOT NULL;
CREATE UNIQUE INDEX IF NOT EXISTS money_transactions_discord_interaction_idx
    ON money_transactions(discord_interaction_id) WHERE discord_interaction_id IS NOT NULL;
CREATE INDEX IF NOT EXISTS safebox_stock_transactions_business_order_item_idx
    ON safebox_stock_transactions(business_order_item_id) WHERE business_order_item_id IS NOT NULL;
-- One Discord fulfillment action may write one stock entry per order item.
CREATE UNIQUE INDEX IF NOT EXISTS safebox_stock_transactions_order_interaction_idx
    ON safebox_stock_transactions(discord_interaction_id, business_order_item_id)
    WHERE discord_interaction_id IS NOT NULL AND business_order_item_id IS NOT NULL;
