DO $$
BEGIN
    IF to_regclass('public.office_money_transactions') IS NULL
        AND to_regclass('public.money_transactions') IS NOT NULL THEN
        ALTER TABLE money_transactions RENAME TO office_money_transactions;
    END IF;
END $$;
