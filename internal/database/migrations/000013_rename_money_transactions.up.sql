DO $$
BEGIN
    IF to_regclass('public.money_transactions') IS NULL
        AND to_regclass('public.office_money_transactions') IS NOT NULL THEN
        ALTER TABLE office_money_transactions RENAME TO money_transactions;
    END IF;
END $$;

DO $$
DECLARE
    rename_pair TEXT[];
BEGIN
    FOREACH rename_pair SLICE 1 IN ARRAY ARRAY[
		['office_money_transactions_pkey', 'money_transactions_pkey'],
		['office_money_transactions_id_not_null', 'money_transactions_id_not_null'],
		['office_money_transactions_actor_member_id_not_null', 'money_transactions_actor_member_id_not_null'],
		['office_money_transactions_transaction_type_not_null', 'money_transactions_transaction_type_not_null'],
		['office_money_transactions_direction_not_null', 'money_transactions_direction_not_null'],
		['office_money_transactions_amount_not_null', 'money_transactions_amount_not_null'],
		['office_money_transactions_balance_before_not_null', 'money_transactions_balance_before_not_null'],
		['office_money_transactions_balance_after_not_null', 'money_transactions_balance_after_not_null'],
		['office_money_transactions_reason_not_null', 'money_transactions_reason_not_null'],
		['office_money_transactions_created_at_not_null', 'money_transactions_created_at_not_null'],
		['office_money_transactions_account_type_not_null', 'money_transactions_account_type_not_null'],
        ['office_money_transactions_actor_foreign_key', 'money_transactions_actor_foreign_key'],
        ['office_money_transactions_reversed_foreign_key', 'money_transactions_reversed_foreign_key'],
        ['office_money_transactions_type_valid', 'money_transactions_type_valid'],
        ['office_money_transactions_direction_valid', 'money_transactions_direction_valid'],
        ['office_money_transactions_amount_positive', 'money_transactions_amount_positive'],
        ['office_money_transactions_balances_not_negative', 'money_transactions_balances_not_negative'],
        ['office_money_transactions_reason_not_blank', 'money_transactions_reason_not_blank'],
        ['office_money_transactions_balance_change_valid', 'money_transactions_balance_change_valid'],
        ['office_money_transactions_type_direction_valid', 'money_transactions_type_direction_valid'],
        ['office_money_transactions_opening_valid', 'money_transactions_opening_valid'],
        ['office_money_transactions_reversal_reference_valid', 'money_transactions_reversal_reference_valid'],
        ['office_money_transactions_reversed_once', 'money_transactions_reversed_once'],
        ['office_money_transactions_account_type_valid', 'money_transactions_account_type_valid']
    ] LOOP
        IF EXISTS (
            SELECT 1 FROM pg_constraint
            WHERE conrelid = 'money_transactions'::regclass AND conname = rename_pair[1]
        ) AND NOT EXISTS (
            SELECT 1 FROM pg_constraint
            WHERE conrelid = 'money_transactions'::regclass AND conname = rename_pair[2]
        ) THEN
            EXECUTE format('ALTER TABLE money_transactions RENAME CONSTRAINT %I TO %I', rename_pair[1], rename_pair[2]);
        END IF;
    END LOOP;
END $$;

DO $$
DECLARE
    rename_pair TEXT[];
BEGIN
    FOREACH rename_pair SLICE 1 IN ARRAY ARRAY[
        ['office_money_transactions_created_at_idx', 'money_transactions_created_at_idx'],
        ['office_money_transactions_one_opening_idx', 'money_transactions_one_opening_idx'],
        ['office_money_transactions_one_opening_per_account_idx', 'money_transactions_one_opening_per_account_idx'],
        ['office_money_transactions_actor_idx', 'money_transactions_actor_idx'],
        ['office_money_transactions_account_created_at_idx', 'money_transactions_account_created_at_idx']
    ] LOOP
        IF to_regclass('public.' || rename_pair[1]) IS NOT NULL
            AND to_regclass('public.' || rename_pair[2]) IS NULL THEN
            EXECUTE format('ALTER INDEX %I RENAME TO %I', rename_pair[1], rename_pair[2]);
        END IF;
    END LOOP;
END $$;
