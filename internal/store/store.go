// Package store 保存 V2 业务状态。内存实现给单元测试，PostgreSQL 实现给本地运行。
package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"ozon-workbench/internal/domain"
)

var (
	ErrNotFound       = errors.New("not found")
	ErrConflict       = errors.New("conflict")
	ErrForcedRollback = errors.New("forced rollback")
)

type DB interface {
	InTx(ctx context.Context, fn func(Tx) error) error
	Close()
}

// Tx 的方法都运行在同一个事务里。调用方负责业务判断。
type Tx interface {
	CreateShop(ctx context.Context, shop domain.Shop) (domain.Shop, error)
	UpdateShop(ctx context.Context, shop domain.Shop) (domain.Shop, error)
	ListShops(ctx context.Context) ([]domain.Shop, error)
	GetShop(ctx context.Context, id int64) (domain.Shop, error)

	GetMaxActive(ctx context.Context) (int, error)
	SetMaxActive(ctx context.Context, n int, now time.Time) error

	GetConfig(ctx context.Context, key string) (json.RawMessage, bool, error)
	SetConfig(ctx context.Context, key string, val json.RawMessage, now time.Time) error

	PutTree(ctx context.Context, lang string, tree json.RawMessage, synced time.Time) error
	GetTree(ctx context.Context, lang string) (json.RawMessage, time.Time, bool, error)

	GetRule(ctx context.Context, categoryID, typeID int64) (domain.Rule, bool, error)
	SaveRule(ctx context.Context, rule domain.Rule, now time.Time) (domain.Rule, error)

	GetAttrCache(ctx context.Context, shopID, categoryID, typeID int64, lang string) (json.RawMessage, time.Time, *time.Time, bool, error)
	PutAttrCache(ctx context.Context, shopID, categoryID, typeID int64, lang string, attrs json.RawMessage, fetched time.Time, expires *time.Time) error

	ListDict(ctx context.Context, q domain.DictQuery) ([]domain.DictValue, error)
	UpsertDict(ctx context.Context, rows []domain.DictValue) error
	GetDictSync(ctx context.Context, shopID, categoryID, typeID, attributeID int64, lang string) (domain.DictSync, bool, error)
	PutDictSync(ctx context.Context, row domain.DictSync) error

	CreateTask(ctx context.Context, task domain.Task) (domain.Task, error)
	UpdateTask(ctx context.Context, task domain.Task) error
	GetTask(ctx context.Context, id int64) (domain.Task, error)
	GetTaskByToken(ctx context.Context, token string) (domain.Task, error)
	ListTasks(ctx context.Context, f domain.TaskFilter) ([]domain.Task, error)
	CountTasks(ctx context.Context, statuses []string) (int, error)
	ClaimQueued(ctx context.Context, now time.Time) ([]domain.Task, error)

	CreateTurn(ctx context.Context, turn domain.Turn) (domain.Turn, error)
	UpdateTurn(ctx context.Context, turn domain.Turn) error
	ListTurns(ctx context.Context, taskID int64) ([]domain.Turn, error)
	ActiveTurn(ctx context.Context, taskID int64) (domain.Turn, bool, error)

	CreateMessage(ctx context.Context, msg domain.Message) (domain.Message, error)
	UpdateMessage(ctx context.Context, msg domain.Message) error
	ListMessages(ctx context.Context, taskID int64) ([]domain.Message, error)
	PendingMessages(ctx context.Context, taskID int64) ([]domain.Message, error)

	UpsertAsset(ctx context.Context, asset domain.Asset) (domain.Asset, error)
	ListAssets(ctx context.Context, taskID int64) ([]domain.Asset, error)

	CreateProduct(ctx context.Context, product domain.Product) (domain.Product, error)
	UpdateProduct(ctx context.Context, product domain.Product) error
	GetProduct(ctx context.Context, id int64) (domain.Product, error)
	FindProductByOffer(ctx context.Context, shopID int64, offerID string) (domain.Product, bool, error)
	ListProducts(ctx context.Context, f domain.ProductFilter) ([]domain.Product, error)
	GetDelivery(ctx context.Context, taskID int64, skuRef string) (domain.Delivery, bool, error)
	CreateDelivery(ctx context.Context, row domain.Delivery) error
	ListDeliveries(ctx context.Context, taskID int64) ([]domain.Delivery, error)

	CreatePricingRule(ctx context.Context, rule domain.PricingRule) (domain.PricingRule, error)
	UpdatePricingRule(ctx context.Context, rule domain.PricingRule) error
	ListPricingRules(ctx context.Context) ([]domain.PricingRule, error)
	GetPricingRule(ctx context.Context, id int64) (domain.PricingRule, error)

	CreateBatch(ctx context.Context, batch domain.ImportBatch) (domain.ImportBatch, error)
	UpdateBatch(ctx context.Context, batch domain.ImportBatch) error
	GetBatch(ctx context.Context, id int64) (domain.ImportBatch, error)
	ListBatches(ctx context.Context, shopID int64) ([]domain.ImportBatch, error)
	CreateBatchItem(ctx context.Context, item domain.ImportBatchItem) (domain.ImportBatchItem, error)
	UpdateBatchItem(ctx context.Context, item domain.ImportBatchItem) error
	ListBatchItems(ctx context.Context, batchID int64) ([]domain.ImportBatchItem, error)
	FindBatchByOzonTask(ctx context.Context, shopID, ozonTaskID int64) (domain.ImportBatch, bool, error)

	GetIdem(ctx context.Context, scope, key string) (json.RawMessage, bool, error)
	PutIdem(ctx context.Context, scope, key string, resp json.RawMessage, now time.Time) error
}
