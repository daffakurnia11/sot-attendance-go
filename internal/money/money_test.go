package money

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5"
)

type fakeRow struct {
	values []any
	err    error
}

func (r fakeRow) Scan(destinations ...any) error {
	if r.err != nil {
		return r.err
	}
	for index, value := range r.values {
		switch destination := destinations[index].(type) {
		case *string:
			*destination = value.(string)
		case *int64:
			*destination = value.(int64)
		}
	}
	return nil
}

type fakeDatabase struct{ row pgx.Row }

func (database fakeDatabase) QueryRow(context.Context, string, ...any) pgx.Row { return database.row }

func TestBalance(t *testing.T) {
	t.Parallel()
	repository := NewRepository(fakeDatabase{row: fakeRow{values: []any{"1012500"}}})
	got, err := repository.Balance(context.Background(), AccountOffice)
	if err != nil || got != 1012500 {
		t.Fatalf("Balance() = %d, %v", got, err)
	}
	if _, err := repository.Balance(context.Background(), Account("invalid")); !errors.Is(err, ErrInvalidAccount) {
		t.Fatalf("invalid account error = %v", err)
	}
}

func TestTransactValidationAndResult(t *testing.T) {
	t.Parallel()
	repository := NewRepository(fakeDatabase{row: fakeRow{values: []any{int64(7), int64(1000), int64(1250)}}})
	transaction, err := repository.Transact(context.Background(), Transaction{Account: AccountDirty, Action: ActionDeposit, Amount: 250, Reason: " income ", ActorMemberID: 4})
	if err != nil || transaction.ID != 7 || transaction.BalanceBefore != 1000 || transaction.BalanceAfter != 1250 || transaction.Reason != "income" {
		t.Fatalf("Transact() = %#v, %v", transaction, err)
	}
	for _, invalid := range []Transaction{
		{Account: Account("invalid"), Action: ActionDeposit, Amount: 1, Reason: "x"},
		{Account: AccountOffice, Action: "invalid", Amount: 1, Reason: "x"},
		{Account: AccountOffice, Action: ActionDeposit, Amount: 0, Reason: "x"},
		{Account: AccountOffice, Action: ActionDeposit, Amount: 1, Reason: " "},
	} {
		if _, err := repository.Transact(context.Background(), invalid); err == nil {
			t.Errorf("Transact(%#v) unexpectedly succeeded", invalid)
		}
	}
}

func TestTransactNoRows(t *testing.T) {
	t.Parallel()
	repository := NewRepository(fakeDatabase{row: fakeRow{err: pgx.ErrNoRows}})
	_, err := repository.Transact(context.Background(), Transaction{Account: AccountOffice, Action: ActionWithdraw, Amount: 2, Reason: "x"})
	if !errors.Is(err, ErrInsufficientFunds) {
		t.Fatalf("withdraw error = %v", err)
	}
	_, err = repository.Transact(context.Background(), Transaction{Account: AccountOffice, Action: ActionDeposit, Amount: 2, Reason: "x"})
	if !errors.Is(err, ErrBalanceOverflow) {
		t.Fatalf("deposit error = %v", err)
	}
}
