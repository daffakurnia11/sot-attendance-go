package money

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
)

type Account string

const (
	AccountOffice Account = "office"
	AccountDirty  Account = "dirty"

	ActionDeposit  = "deposit"
	ActionWithdraw = "withdraw"
)

var (
	ErrInvalidAccount    = errors.New("invalid money account")
	ErrInvalidAction     = errors.New("invalid money action")
	ErrInvalidAmount     = errors.New("invalid money amount")
	ErrInvalidReason     = errors.New("invalid money transaction reason")
	ErrInsufficientFunds = errors.New("insufficient money balance")
	ErrBalanceOverflow   = errors.New("money balance exceeds supported range")
)

type Transaction struct {
	ID            int64
	Account       Account
	Action        string
	Amount        int64
	BalanceBefore int64
	BalanceAfter  int64
	Reason        string
	ActorMemberID int64
}

type queryRower interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type Repository struct{ database queryRower }

func NewRepository(database queryRower) *Repository { return &Repository{database: database} }

func (r *Repository) Balance(ctx context.Context, account Account) (int64, error) {
	setting, err := settingFor(account)
	if err != nil {
		return 0, err
	}
	var raw string
	if err := r.database.QueryRow(ctx, `SELECT value FROM settings WHERE settings = $1`, setting).Scan(&raw); err != nil {
		return 0, fmt.Errorf("load %s money balance: %w", account, err)
	}
	balance, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || balance < 0 {
		return 0, fmt.Errorf("load %s money balance: stored balance is invalid", account)
	}
	return balance, nil
}

func (r *Repository) Transact(ctx context.Context, transaction Transaction) (Transaction, error) {
	setting, err := settingFor(transaction.Account)
	if err != nil {
		return Transaction{}, err
	}
	if transaction.Action != ActionDeposit && transaction.Action != ActionWithdraw {
		return Transaction{}, ErrInvalidAction
	}
	if transaction.Amount <= 0 {
		return Transaction{}, ErrInvalidAmount
	}
	transaction.Reason = strings.TrimSpace(transaction.Reason)
	if transaction.Reason == "" || len(transaction.Reason) > 500 {
		return Transaction{}, ErrInvalidReason
	}

	const query = `
		WITH current_balance AS (
			SELECT value::bigint AS balance
			FROM settings
			WHERE settings = $1
			FOR UPDATE
		), updated AS (
			UPDATE settings AS target
			SET value = CASE
				WHEN $2 = 'deposit' THEN (current_balance.balance + $3)::text
				ELSE (current_balance.balance - $3)::text
			END,
			updated_at = NOW()
			FROM current_balance
			WHERE target.settings = $1
				AND (($2 = 'deposit' AND current_balance.balance <= 9223372036854775807 - $3)
					OR ($2 = 'withdraw' AND current_balance.balance >= $3))
			RETURNING current_balance.balance AS balance_before, target.value::bigint AS balance_after
		), inserted AS (
			INSERT INTO money_transactions (
				account_type, actor_member_id, transaction_type, direction,
				amount, balance_before, balance_after, reason
			)
			SELECT $4, $5,
				CASE WHEN $2 = 'deposit' THEN 'deposit' ELSE 'withdrawal' END,
				CASE WHEN $2 = 'deposit' THEN 'credit' ELSE 'debit' END,
				$3, balance_before, balance_after, $6
			FROM updated
			RETURNING id, balance_before, balance_after
		)
		SELECT id, balance_before, balance_after FROM inserted`

	err = r.database.QueryRow(ctx, query, setting, transaction.Action, transaction.Amount, transaction.Account, transaction.ActorMemberID, transaction.Reason).Scan(
		&transaction.ID, &transaction.BalanceBefore, &transaction.BalanceAfter,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		if transaction.Action == ActionWithdraw {
			return Transaction{}, ErrInsufficientFunds
		}
		return Transaction{}, ErrBalanceOverflow
	}
	if err != nil {
		return Transaction{}, fmt.Errorf("apply %s money transaction: %w", transaction.Account, err)
	}
	return transaction, nil
}

func settingFor(account Account) (string, error) {
	switch account {
	case AccountOffice:
		return "office_money_balance", nil
	case AccountDirty:
		return "dirty_money_balance", nil
	default:
		return "", ErrInvalidAccount
	}
}
