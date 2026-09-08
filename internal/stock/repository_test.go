package stock

import (
	"context"
	"errors"
	"testing"
)

func TestTransactRejectsInvalidRequestsBeforeDatabaseAccess(t *testing.T) {
	repository := &Repository{}
	tests := []Transaction{
		{},
		{Safebox: "public", Action: ActionDeposit, Reason: "reason", ActorMemberID: 7, IdempotencyKey: "request"},
		{Safebox: "unknown", Action: ActionDeposit, Reason: "reason", ActorMemberID: 7, IdempotencyKey: "request", Items: []TransactionLine{{ItemKey: "iron", Quantity: 1}}},
		{Safebox: "public", Action: "reset", Reason: "reason", ActorMemberID: 7, IdempotencyKey: "request", Items: []TransactionLine{{ItemKey: "iron", Quantity: 1}}},
		{Safebox: "public", Action: ActionDeposit, Reason: " ", ActorMemberID: 7, IdempotencyKey: "request", Items: []TransactionLine{{ItemKey: "iron", Quantity: 1}}},
		{Safebox: "public", Action: ActionDeposit, Reason: "reason", ActorMemberID: 7, IdempotencyKey: "request", Items: []TransactionLine{{ItemKey: "iron", Quantity: 0}}},
		{Safebox: "public", Action: ActionDeposit, Reason: "reason", ActorMemberID: 7, IdempotencyKey: "request", Items: []TransactionLine{{ItemKey: "iron", Quantity: MaxLineQuantity + 1}}},
		{Safebox: "public", Action: ActionDeposit, Reason: "reason", ActorMemberID: 7, IdempotencyKey: "request", Items: []TransactionLine{{ItemKey: "iron", Quantity: 1}, {ItemKey: "iron", Quantity: 2}}},
	}
	for _, transaction := range tests {
		if err := repository.Transact(context.Background(), transaction); !errors.Is(err, ErrInvalidTransaction) {
			t.Fatalf("Transact(%+v) error = %v, want ErrInvalidTransaction", transaction, err)
		}
	}
}
