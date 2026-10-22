package business

import (
	"context"
	"time"
)

type OrderLogItem struct {
	Name     string `json:"name"`
	Unit     string `json:"unit"`
	Price    int64  `json:"price"`
	Quantity int64  `json:"quantity"`
}
type OrderLogEntry struct {
	ID         int64          `json:"id"`
	Kind       string         `json:"kind"`
	Partner    string         `json:"partner"`
	Buyer      string         `json:"buyer"`
	Status     string         `json:"status"`
	OrderDate  string         `json:"order_date"`
	PickupDate string         `json:"pickup_date"`
	Additional int64          `json:"additional"`
	Total      int64          `json:"total"`
	Notes      string         `json:"notes"`
	CreatedAt  time.Time      `json:"created_at"`
	UpdatedAt  time.Time      `json:"updated_at"`
	CreatedBy  string         `json:"created_by"`
	Items      []OrderLogItem `json:"items"`
}
type OrderLog struct {
	Orders     []OrderLogEntry `json:"orders"`
	NextBefore int64           `json:"next_before"`
}

func (r *Repository) ListOrders(ctx context.Context, before int64, kind string) (OrderLog, error) {
	result := OrderLog{Orders: []OrderLogEntry{}}
	rows, err := r.pool.Query(ctx, `SELECT o.id,CASE WHEN p.code='SOT' AND o.direction='sale' THEN 'internal' ELSE o.direction END,
 p.name,COALESCE(NULLIF(b.display_name,''),b.username,''),o.status,to_char(o.order_date,'YYYY-MM-DD'),to_char(o.pickup_date,'YYYY-MM-DD'),o.additional_price,o.total_price,o.notes,o.created_at,o.updated_at,COALESCE(NULLIF(m.display_name,''),m.username,'Unknown'),
 COALESCE((SELECT jsonb_agg(jsonb_build_object('name',i.product_name,'unit',i.unit,'price',i.unit_price,'quantity',i.quantity) ORDER BY i.id) FROM business_order_items i WHERE i.order_id=o.id),'[]'::jsonb)
 FROM business_orders o JOIN business_partners p ON p.id=o.partner_id LEFT JOIN members b ON b.id=o.buyer_member_id LEFT JOIN members m ON m.id=o.created_by_member_id
 WHERE ($1::bigint=0 OR o.id<$1) AND ($2::text='' OR CASE WHEN p.code='SOT' AND o.direction='sale' THEN 'internal' ELSE o.direction END=$2) ORDER BY o.id DESC LIMIT 26`, before, kind)
	if err != nil {
		return result, err
	}
	defer rows.Close()
	for rows.Next() {
		var e OrderLogEntry
		if err = rows.Scan(&e.ID, &e.Kind, &e.Partner, &e.Buyer, &e.Status, &e.OrderDate, &e.PickupDate, &e.Additional, &e.Total, &e.Notes, &e.CreatedAt, &e.UpdatedAt, &e.CreatedBy, &e.Items); err != nil {
			return result, err
		}
		result.Orders = append(result.Orders, e)
	}
	if err = rows.Err(); err != nil {
		return result, err
	}
	if len(result.Orders) > 25 {
		result.Orders = result.Orders[:25]
		result.NextBefore = result.Orders[24].ID
	}
	return result, nil
}
