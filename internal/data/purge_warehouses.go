package data

import (
	"context"

	"ozon-workbench/internal/biz"
)

// PurgeSampleWarehouses deletes built-in sample warehouses from shops.
// Call it only when the process is not in mock mode. A mock-mode database keeps its samples.
func PurgeSampleWarehouses(ctx context.Context, d *Data) (int, error) {
	rows, err := d.Client.Shop.Query().All(ctx)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, row := range rows {
		kept, changed := biz.StripUntrustedWarehouses(bizWarehouses(row.Warehouses), row.WarehousesMock)
		if !changed {
			continue
		}
		update := d.Client.Shop.UpdateOneID(row.ID).
			SetWarehouses(schemaWarehouses(kept)).
			SetWarehousesMock(false)
		if len(kept) == 0 {
			update.ClearWarehousesSyncedAt()
		}
		if _, err := update.Save(ctx); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}
