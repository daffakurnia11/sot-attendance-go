package business

import (
	"context"
	"github.com/jackc/pgx/v5/pgxpool"
)

type PackageComponent struct {
	StockItemID int64  `json:"stock_item_id"`
	Name        string `json:"name"`
	Quantity    int64  `json:"quantity"`
}

type Entry struct {
	Components    []PackageComponent `json:"components,omitempty"`
	Version       int64              `json:"version"`
	StockItemID   int64              `json:"stock_item_id"`
	StockItemName string             `json:"stock_item_name"`
	ID            int64              `json:"id"`
	PartnerCode   string             `json:"partner_code"`
	PartnerName   string             `json:"partner_name"`
	Direction     string             `json:"direction"`
	Name          string             `json:"name"`
	Price         int64              `json:"price"`
	Unit          string             `json:"unit"`
	Notes         string             `json:"notes"`
	StockGroup    string             `json:"stock_group"`
}
type Repository struct{ pool *pgxpool.Pool }

func NewRepository(pool *pgxpool.Pool) *Repository { return &Repository{pool: pool} }
func (r *Repository) List(ctx context.Context) ([]Entry, error) {
	rows, err := r.pool.Query(ctx, `SELECT c.id,p.code,p.name,c.direction,c.name,c.price,c.unit,c.notes,i.stock_group,c.version,i.id,i.name,
 CASE WHEN c.direction='internal' AND c.unit='package' THEN
 COALESCE((SELECT jsonb_agg(jsonb_build_object('stock_item_id',pc.stock_item_id,'name',si.name,'quantity',pc.quantity) ORDER BY pc.stock_item_id)
 FROM business_internal_package_components pc JOIN safebox_stock_items si ON si.id=pc.stock_item_id WHERE pc.price_id=c.id),'[]'::jsonb)
 ELSE '[]'::jsonb END FROM (
 SELECT id,partner_id,'purchase' AS direction,name,price,unit,notes,stock_item_id,version FROM business_partner_products
 UNION ALL SELECT id,partner_id,'sale' AS direction,name,price,unit,notes,stock_item_id,version FROM business_partner_prices
 UNION ALL SELECT id,partner_id,'internal' AS direction,name,price,unit,notes,stock_item_id,version FROM business_internal_prices
 ) c JOIN business_partners p ON p.id=c.partner_id JOIN safebox_stock_items i ON i.id=c.stock_item_id ORDER BY p.code,c.direction,c.name,c.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	entries := make([]Entry, 0)
	for rows.Next() {
		var e Entry
		if err := rows.Scan(&e.ID, &e.PartnerCode, &e.PartnerName, &e.Direction, &e.Name, &e.Price, &e.Unit, &e.Notes, &e.StockGroup, &e.Version, &e.StockItemID, &e.StockItemName, &e.Components); err != nil {
			return nil, err
		}
		entries = append(entries, e)
	}
	return entries, rows.Err()
}
