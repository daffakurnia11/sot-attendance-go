package stock

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	ActionDeposit   = "deposit"
	ActionWithdraw  = "withdraw"
	MaxLineQuantity = 10_000
)

var (
	ErrInvalidTransaction  = errors.New("invalid safebox stock transaction")
	ErrItemNotFound        = errors.New("safebox stock item not found")
	ErrInsufficientStock   = errors.New("insufficient safebox stock")
	ErrQuantityOverflow    = errors.New("safebox stock quantity exceeds supported range")
	ErrIdempotencyConflict = errors.New("safebox stock idempotency key was reused with different input")
)

type Item struct {
	Safebox  string `json:"safebox"`
	ItemKey  string `json:"item_key"`
	Name     string `json:"name"`
	Group    string `json:"stock_group"`
	Quantity int32  `json:"quantity"`
}

type TransactionLine struct {
	ItemKey  string `json:"item_key"`
	Quantity int32  `json:"quantity"`
}

type Transaction struct {
	Safebox        string            `json:"safebox"`
	Action         string            `json:"action"`
	Reason         string            `json:"reason"`
	ActorMemberID  int64             `json:"-"`
	Items          []TransactionLine `json:"items"`
	IdempotencyKey string            `json:"idempotency_key"`
}

type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }

func (r *Repository) List(ctx context.Context) ([]Item, error) {
	const query = `
		SELECT b.safebox, i.item_key, i.name, i.stock_group, b.quantity
		FROM safebox_stock_balances b
		JOIN safebox_stock_items i ON i.id = b.item_id
		ORDER BY CASE b.safebox WHEN 'public' THEN 0 ELSE 1 END, i.stock_group, i.id`
	rows, err := r.pool.Query(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("list safebox stock: %w", err)
	}
	defer rows.Close()
	items := make([]Item, 0)
	for rows.Next() {
		var item Item
		if err := rows.Scan(&item.Safebox, &item.ItemKey, &item.Name, &item.Group, &item.Quantity); err != nil {
			return nil, fmt.Errorf("scan safebox stock: %w", err)
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("read safebox stock: %w", err)
	}
	return items, nil
}

func (r *Repository) Transact(ctx context.Context, transaction Transaction) error {
	transaction.Reason = strings.TrimSpace(transaction.Reason)
	transaction.IdempotencyKey = strings.TrimSpace(transaction.IdempotencyKey)
	if (transaction.Safebox != "public" && transaction.Safebox != "boss") ||
		(transaction.Action != ActionDeposit && transaction.Action != ActionWithdraw) ||
		transaction.ActorMemberID <= 0 || transaction.Reason == "" || len(transaction.Reason) > 500 ||
		transaction.IdempotencyKey == "" || len(transaction.IdempotencyKey) > 128 ||
		len(transaction.Items) == 0 || len(transaction.Items) > 100 {
		return ErrInvalidTransaction
	}
	lines := append([]TransactionLine(nil), transaction.Items...)
	seen := make(map[string]struct{}, len(lines))
	for index := range lines {
		lines[index].ItemKey = strings.TrimSpace(lines[index].ItemKey)
		if lines[index].ItemKey == "" || lines[index].Quantity <= 0 || lines[index].Quantity > MaxLineQuantity {
			return ErrInvalidTransaction
		}
		if _, exists := seen[lines[index].ItemKey]; exists {
			return ErrInvalidTransaction
		}
		seen[lines[index].ItemKey] = struct{}{}
	}
	sort.Slice(lines, func(left, right int) bool { return lines[left].ItemKey < lines[right].ItemKey })
	fingerprintInput, err := json.Marshal(struct {
		Safebox string            `json:"safebox"`
		Action  string            `json:"action"`
		Reason  string            `json:"reason"`
		Items   []TransactionLine `json:"items"`
	}{transaction.Safebox, transaction.Action, transaction.Reason, lines})
	if err != nil {
		return fmt.Errorf("encode safebox stock transaction fingerprint: %w", err)
	}
	fingerprintHash := sha256.Sum256(fingerprintInput)
	fingerprint := hex.EncodeToString(fingerprintHash[:])

	databaseTransaction, err := r.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin safebox stock transaction: %w", err)
	}
	defer func() { _ = databaseTransaction.Rollback(ctx) }()

	const recordRequest = `
		INSERT INTO safebox_stock_requests (idempotency_key, actor_member_id, request_fingerprint)
		VALUES ($1, $2, $3)
		ON CONFLICT (idempotency_key) DO NOTHING
		RETURNING idempotency_key`
	var recordedKey string
	if err := databaseTransaction.QueryRow(ctx, recordRequest, transaction.IdempotencyKey, transaction.ActorMemberID, fingerprint).Scan(&recordedKey); err != nil {
		if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("record safebox stock request: %w", err)
		}
		var existingActor int64
		var existingFingerprint string
		if err := databaseTransaction.QueryRow(ctx, `SELECT actor_member_id, request_fingerprint FROM safebox_stock_requests WHERE idempotency_key = $1`, transaction.IdempotencyKey).Scan(&existingActor, &existingFingerprint); err != nil {
			return fmt.Errorf("read safebox stock request: %w", err)
		}
		if existingActor != transaction.ActorMemberID || existingFingerprint != fingerprint {
			return ErrIdempotencyConflict
		}
		return nil
	}

	const lockBalance = `
		SELECT b.item_id, b.quantity
		FROM safebox_stock_balances b
		JOIN safebox_stock_items i ON i.id = b.item_id
		WHERE b.safebox = $1 AND i.item_key = $2
		FOR UPDATE`
	const updateBalance = `
		UPDATE safebox_stock_balances
		SET quantity = $1, updated_at = NOW()
		WHERE safebox = $2 AND item_id = $3`
	const insertAudit = `
		INSERT INTO safebox_stock_transactions
			(safebox, item_id, action, quantity_before, quantity_after, delta, reason, actor_member_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`

	for _, line := range lines {
		var itemID int64
		var before int32
		if err := databaseTransaction.QueryRow(ctx, lockBalance, transaction.Safebox, line.ItemKey).Scan(&itemID, &before); err != nil {
			if errors.Is(err, pgx.ErrNoRows) {
				return ErrItemNotFound
			}
			return fmt.Errorf("lock safebox stock balance: %w", err)
		}
		after := int64(before)
		if transaction.Action == ActionDeposit {
			after += int64(line.Quantity)
			if after > math.MaxInt32 {
				return ErrQuantityOverflow
			}
		} else {
			after -= int64(line.Quantity)
			if after < 0 {
				return ErrInsufficientStock
			}
		}
		if _, err := databaseTransaction.Exec(ctx, updateBalance, int32(after), transaction.Safebox, itemID); err != nil {
			return fmt.Errorf("update safebox stock balance: %w", err)
		}
		delta := int32(after) - before
		if _, err := databaseTransaction.Exec(ctx, insertAudit, transaction.Safebox, itemID, transaction.Action, before, int32(after), delta, transaction.Reason, transaction.ActorMemberID); err != nil {
			return fmt.Errorf("record safebox stock transaction: %w", err)
		}
	}
	if err := databaseTransaction.Commit(ctx); err != nil {
		return fmt.Errorf("commit safebox stock transaction: %w", err)
	}
	return nil
}
