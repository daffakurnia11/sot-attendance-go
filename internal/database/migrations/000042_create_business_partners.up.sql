-- Partner purchase and sale catalogs. Safe to replay at startup.
CREATE TABLE IF NOT EXISTS business_partners (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    code TEXT NOT NULL,
    name TEXT NOT NULL,
    description TEXT NOT NULL DEFAULT '',
    CONSTRAINT business_partners_code_unique UNIQUE (code),
    CONSTRAINT business_partners_code_not_blank CHECK (BTRIM(code) <> ''),
    CONSTRAINT business_partners_name_not_blank CHECK (BTRIM(name) <> '')
);

-- Products the partner sells to SOT.
CREATE TABLE IF NOT EXISTS business_partner_products (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    partner_id BIGINT NOT NULL,
    name TEXT NOT NULL,
    stock_item_id BIGINT NOT NULL,
    price BIGINT NOT NULL,
    unit TEXT NOT NULL,
    notes TEXT NOT NULL DEFAULT '',
    CONSTRAINT business_partner_products_partner_fkey FOREIGN KEY (partner_id)
        REFERENCES business_partners (id) ON DELETE RESTRICT,
    CONSTRAINT business_partner_products_stock_item_fkey FOREIGN KEY (stock_item_id)
        REFERENCES safebox_stock_items (id) ON DELETE RESTRICT,
    CONSTRAINT business_partner_products_name_not_blank CHECK (BTRIM(name) <> ''),
    CONSTRAINT business_partner_products_unit_not_blank CHECK (BTRIM(unit) <> ''),
    CONSTRAINT business_partner_products_price_nonnegative CHECK (price >= 0)
);

CREATE INDEX IF NOT EXISTS business_partner_products_partner_idx ON business_partner_products (partner_id);
CREATE INDEX IF NOT EXISTS business_partner_products_stock_item_idx ON business_partner_products (stock_item_id);

-- Products SOT sells to the partner.
CREATE TABLE IF NOT EXISTS business_partner_prices (
    id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    partner_id BIGINT NOT NULL,
    name TEXT NOT NULL,
    stock_item_id BIGINT NOT NULL,
    price BIGINT NOT NULL,
    unit TEXT NOT NULL,
    notes TEXT NOT NULL DEFAULT '',
    CONSTRAINT business_partner_prices_partner_fkey FOREIGN KEY (partner_id)
        REFERENCES business_partners (id) ON DELETE RESTRICT,
    CONSTRAINT business_partner_prices_stock_item_fkey FOREIGN KEY (stock_item_id)
        REFERENCES safebox_stock_items (id) ON DELETE RESTRICT,
    CONSTRAINT business_partner_prices_name_not_blank CHECK (BTRIM(name) <> ''),
    CONSTRAINT business_partner_prices_unit_not_blank CHECK (BTRIM(unit) <> ''),
    CONSTRAINT business_partner_prices_price_nonnegative CHECK (price >= 0)
);

CREATE INDEX IF NOT EXISTS business_partner_prices_partner_idx ON business_partner_prices (partner_id);
CREATE INDEX IF NOT EXISTS business_partner_prices_stock_item_idx ON business_partner_prices (stock_item_id);
