package data

import (
	"context"

	"ozon-workbench/internal/biz"
	"ozon-workbench/internal/data/ent"
	"ozon-workbench/internal/data/ent/shop"
	"ozon-workbench/internal/data/schema"
)

type shopRepo struct {
	db *ent.Client
}

func NewShopRepo(data *Data) biz.ShopRepo {
	return &shopRepo{db: data.Client}
}

func (r *shopRepo) List(ctx context.Context) ([]*biz.Shop, error) {
	rows, err := r.db.Shop.Query().Order(shop.ByID()).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*biz.Shop, 0, len(rows))
	for _, row := range rows {
		out = append(out, toBizShop(row))
	}
	return out, nil
}

func (r *shopRepo) Get(ctx context.Context, id int) (*biz.Shop, error) {
	row, err := r.db.Shop.Get(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, &biz.Error{Status: 404, Reason: "NOT_FOUND", Message: "店铺不存在。"}
		}
		return nil, err
	}
	return toBizShop(row), nil
}

func (r *shopRepo) GetByClientID(ctx context.Context, clientID string) (*biz.Shop, error) {
	row, err := r.db.Shop.Query().Where(shop.ClientIDEQ(clientID)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return toBizShop(row), nil
}

func (r *shopRepo) Create(ctx context.Context, shop *biz.Shop) (*biz.Shop, error) {
	create := r.db.Shop.Create().
		SetName(shop.Name).
		SetClientID(shop.ClientID).
		SetAPIKeyCiphertext(shop.APIKeyCiphertext).
		SetWarehouses(schemaWarehouses(shop.Warehouses)).
		SetWarehousesMock(shop.WarehousesMock).
		SetIsDefault(shop.IsDefault).
		SetArchived(shop.Archived)
	if shop.WarehousesSyncedAt != nil {
		create.SetWarehousesSyncedAt(*shop.WarehousesSyncedAt)
	}
	row, err := create.Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, &biz.Error{Status: 409, Reason: "CLIENT_ID_EXISTS", Message: "这个 Client ID 已有店铺记录。", Fields: map[string]string{"client_id": "这个 Client ID 已有店铺记录。"}}
		}
		return nil, err
	}
	return toBizShop(row), nil
}

func (r *shopRepo) Update(ctx context.Context, shop *biz.Shop) (*biz.Shop, error) {
	update := r.db.Shop.UpdateOneID(shop.ID).
		SetName(shop.Name).
		SetClientID(shop.ClientID).
		SetAPIKeyCiphertext(shop.APIKeyCiphertext).
		SetWarehouses(schemaWarehouses(shop.Warehouses)).
		SetWarehousesMock(shop.WarehousesMock).
		SetIsDefault(shop.IsDefault).
		SetArchived(shop.Archived).
		SetNillableLastTestedAt(shop.LastTestedAt).
		SetNillableLastTestOk(shop.LastTestOK).
		SetNillableLastTestMessage(shop.LastTestMessage)
	if shop.WarehousesSyncedAt != nil {
		update.SetWarehousesSyncedAt(*shop.WarehousesSyncedAt)
	} else {
		update.ClearWarehousesSyncedAt()
	}
	row, err := update.Save(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, &biz.Error{Status: 404, Reason: "NOT_FOUND", Message: "店铺不存在。"}
		}
		if ent.IsConstraintError(err) {
			return nil, &biz.Error{Status: 409, Reason: "CLIENT_ID_EXISTS", Message: "这个 Client ID 已有店铺记录。", Fields: map[string]string{"client_id": "这个 Client ID 已有店铺记录。"}}
		}
		return nil, err
	}
	return toBizShop(row), nil
}

func (r *shopRepo) SetDefault(ctx context.Context, id int) error {
	tx, err := r.db.Tx(ctx)
	if err != nil {
		return err
	}
	if _, err = tx.Shop.Update().SetIsDefault(false).Save(ctx); err != nil {
		_ = tx.Rollback()
		return err
	}
	if _, err = tx.Shop.UpdateOneID(id).SetIsDefault(true).Save(ctx); err != nil {
		_ = tx.Rollback()
		if ent.IsNotFound(err) {
			return &biz.Error{Status: 404, Reason: "NOT_FOUND", Message: "店铺不存在。"}
		}
		return err
	}
	return tx.Commit()
}

func toBizShop(row *ent.Shop) *biz.Shop {
	return &biz.Shop{
		ID:                 row.ID,
		Name:               row.Name,
		ClientID:           row.ClientID,
		CurrencyCode:       row.CurrencyCode,
		APIKeyCiphertext:   append([]byte(nil), row.APIKeyCiphertext...),
		Warehouses:         bizWarehouses(row.Warehouses),
		WarehousesSyncedAt: row.WarehousesSyncedAt,
		WarehousesMock:     row.WarehousesMock,
		IsDefault:          row.IsDefault,
		Archived:           row.Archived,
		LastTestedAt:       row.LastTestedAt,
		LastTestOK:         row.LastTestOk,
		LastTestMessage:    row.LastTestMessage,
		CreatedAt:          row.CreatedAt,
		UpdatedAt:          row.UpdatedAt,
	}
}

func schemaWarehouses(in []biz.Warehouse) []schema.Warehouse {
	out := make([]schema.Warehouse, 0, len(in))
	for _, item := range in {
		out = append(out, schema.Warehouse{
			WarehouseID:   item.WarehouseID,
			Name:          item.Name,
			Status:        item.Status,
			IsRFBS:        item.IsRFBS,
			WarehouseType: item.WarehouseType,
		})
	}
	return out
}

func bizWarehouses(in []schema.Warehouse) []biz.Warehouse {
	out := make([]biz.Warehouse, 0, len(in))
	for _, item := range in {
		out = append(out, biz.Warehouse{
			WarehouseID:   item.WarehouseID,
			Name:          item.Name,
			Status:        item.Status,
			IsRFBS:        item.IsRFBS,
			WarehouseType: item.WarehouseType,
		})
	}
	return out
}
