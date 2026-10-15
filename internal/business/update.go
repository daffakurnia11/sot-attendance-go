package business

import (
	"context"
	"encoding/json"
	"errors"
	"github.com/jackc/pgx/v5"
	"strings"
	"unicode/utf8"
)

var ErrInvalid = errors.New("invalid price update")
var ErrConflict = errors.New("price changed; refresh before editing")
var ErrDuplicate = errors.New("product already exists for this partner and unit")
var ErrNotFound = errors.New("price not found")

type Values struct {
	Price int64  `json:"price"`
	Unit  string `json:"unit"`
	Notes string `json:"notes"`
}
type Update struct {
	ID        int64  `json:"id"`
	Direction string `json:"direction"`
	Version   int64  `json:"version"`
	Values
}

func (u Update) Valid() bool {
	return u.ID > 0 && u.Version > 0 && (u.Direction == "sale" || u.Direction == "purchase") && u.Price >= 0 && u.Price <= 9007199254740991 && strings.TrimSpace(u.Unit) != "" && utf8.RuneCountInString(u.Unit) <= 50 && utf8.RuneCountInString(u.Notes) <= 2000
}
func (r *Repository) Update(ctx context.Context, u Update, actor int64) (int64, error) {
	if !u.Valid() || actor <= 0 {
		return 0, ErrInvalid
	}
	table := "business_partner_products"
	if u.Direction == "sale" {
		table = "business_partner_prices"
	}
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	// Serialize catalog edits so concurrent unit changes cannot create duplicates.
	if _, err = tx.Exec(ctx, "LOCK TABLE "+table+" IN SHARE ROW EXCLUSIVE MODE"); err != nil {
		return 0, err
	}
	var old Values
	var version, partner, item int64
	err = tx.QueryRow(ctx, "SELECT price,unit,notes,version,partner_id,stock_item_id FROM "+table+" WHERE id=$1 FOR UPDATE", u.ID).Scan(&old.Price, &old.Unit, &old.Notes, &version, &partner, &item)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	if err != nil {
		return 0, err
	}
	if version != u.Version {
		return 0, ErrConflict
	}
	u.Unit = strings.TrimSpace(u.Unit)
	var duplicate bool
	err = tx.QueryRow(ctx, "SELECT EXISTS(SELECT 1 FROM "+table+" WHERE partner_id=$1 AND stock_item_id=$2 AND LOWER(BTRIM(unit))=LOWER($3) AND id<>$4)", partner, item, u.Unit, u.ID).Scan(&duplicate)
	if err != nil {
		return 0, err
	}
	if duplicate {
		return 0, ErrDuplicate
	}
	if old == u.Values {
		return version, tx.Commit(ctx)
	}
	if _, err = tx.Exec(ctx, "UPDATE "+table+" SET price=$1,unit=$2,notes=$3,version=version+1 WHERE id=$4", u.Price, u.Unit, u.Notes, u.ID); err != nil {
		return 0, err
	}
	before, err := json.Marshal(old)
	if err != nil {
		return 0, err
	}
	after, err := json.Marshal(u.Values)
	if err != nil {
		return 0, err
	}
	if _, err = tx.Exec(ctx, "INSERT INTO business_price_audit(direction,entry_id,actor_member_id,before_value,after_value) VALUES($1,$2,$3,$4::jsonb,$5::jsonb)", u.Direction, u.ID, actor, string(before), string(after)); err != nil {
		return 0, err
	}
	return version + 1, tx.Commit(ctx)
}
