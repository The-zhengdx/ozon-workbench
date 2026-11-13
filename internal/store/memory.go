package store

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"ozon-workbench/internal/domain"
)

type treeRow struct {
	Tree     json.RawMessage
	SyncedAt time.Time
}

type attrRow struct {
	Attrs     json.RawMessage
	FetchedAt time.Time
	ExpiresAt *time.Time
}

type configRow struct {
	Value     json.RawMessage
	UpdatedAt time.Time
}

type idemRow struct {
	Response  json.RawMessage
	CreatedAt time.Time
}

type snap struct {
	NextShop, NextRule, NextTask, NextTurn, NextMsg        int64
	NextAsset, NextProduct, NextPrice, NextBatch, NextItem int64
	MaxActive                                              int
	SettingsAt                                             time.Time
	Shops                                                  map[int64]domain.Shop
	Config                                                 map[string]configRow
	Trees                                                  map[string]treeRow
	Rules                                                  map[string]domain.Rule
	Attrs                                                  map[string]attrRow
	Dicts                                                  map[string]domain.DictValue
	Syncs                                                  map[string]domain.DictSync
	Tasks                                                  map[int64]domain.Task
	Turns                                                  map[int64]domain.Turn
	Messages                                               map[int64]domain.Message
	Assets                                                 map[int64]domain.Asset
	Products                                               map[int64]domain.Product
	Deliveries                                             []domain.Delivery
	Prices                                                 map[int64]domain.PricingRule
	Batches                                                map[int64]domain.ImportBatch
	Items                                                  map[int64]domain.ImportBatchItem
	Idem                                                   map[string]idemRow
}

// Mem 是带事务回滚的内存库。FailNextCommit 让下一次成功的事务按失败回滚，用来覆盖 AT-17。
type Mem struct {
	mu         sync.Mutex
	data       snap
	failCommit bool
}

func NewMem() *Mem {
	return &Mem{data: newSnap(time.Now().UTC())}
}

func newSnap(now time.Time) snap {
	return snap{
		MaxActive:  3,
		SettingsAt: now,
		Shops:      map[int64]domain.Shop{},
		Config:     map[string]configRow{},
		Trees:      map[string]treeRow{},
		Rules:      map[string]domain.Rule{},
		Attrs:      map[string]attrRow{},
		Dicts:      map[string]domain.DictValue{},
		Syncs:      map[string]domain.DictSync{},
		Tasks:      map[int64]domain.Task{},
		Turns:      map[int64]domain.Turn{},
		Messages:   map[int64]domain.Message{},
		Assets:     map[int64]domain.Asset{},
		Products:   map[int64]domain.Product{},
		Prices:     map[int64]domain.PricingRule{},
		Batches:    map[int64]domain.ImportBatch{},
		Items:      map[int64]domain.ImportBatchItem{},
		Idem:       map[string]idemRow{},
	}
}

func (m *Mem) Close() {}

func (m *Mem) FailNextCommit() { m.failCommit = true }

func (m *Mem) InTx(ctx context.Context, fn func(Tx) error) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	raw, err := json.Marshal(m.data)
	if err != nil {
		return err
	}
	var backup snap
	if err := json.Unmarshal(raw, &backup); err != nil {
		return err
	}
	err = fn(&memTx{s: &m.data})
	if err != nil || m.failCommit {
		m.data = backup
		if err == nil && m.failCommit {
			m.failCommit = false
			return ErrForcedRollback
		}
		return err
	}
	return nil
}

type memTx struct{ s *snap }

func ruleKey(categoryID, typeID int64) string {
	return fmt.Sprintf("%d:%d", categoryID, typeID)
}

func attrKey(shopID, categoryID, typeID int64, lang string) string {
	return fmt.Sprintf("%d:%d:%d:%s", shopID, categoryID, typeID, lang)
}

func dictKey(row domain.DictValue) string {
	return fmt.Sprintf("%d:%d:%d:%d:%s:%d", row.ShopID, row.DescriptionCategoryID, row.TypeID, row.AttributeID, row.Language, row.ValueID)
}

func syncKey(shopID, categoryID, typeID, attributeID int64, lang string) string {
	return fmt.Sprintf("%d:%d:%d:%d:%s", shopID, categoryID, typeID, attributeID, lang)
}

func (t *memTx) CreateShop(_ context.Context, shop domain.Shop) (domain.Shop, error) {
	for _, existing := range t.s.Shops {
		if existing.ClientID == shop.ClientID {
			return domain.Shop{}, ErrConflict
		}
	}
	t.s.NextShop++
	shop.ID = t.s.NextShop
	t.s.Shops[shop.ID] = shop
	return shop, nil
}

func (t *memTx) UpdateShop(_ context.Context, shop domain.Shop) (domain.Shop, error) {
	if _, ok := t.s.Shops[shop.ID]; !ok {
		return domain.Shop{}, ErrNotFound
	}
	for _, existing := range t.s.Shops {
		if existing.ID != shop.ID && existing.ClientID == shop.ClientID {
			return domain.Shop{}, ErrConflict
		}
	}
	t.s.Shops[shop.ID] = shop
	return shop, nil
}

func (t *memTx) ListShops(context.Context) ([]domain.Shop, error) {
	out := make([]domain.Shop, 0, len(t.s.Shops))
	for _, shop := range t.s.Shops {
		out = append(out, shop)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (t *memTx) GetShop(_ context.Context, id int64) (domain.Shop, error) {
	shop, ok := t.s.Shops[id]
	if !ok {
		return domain.Shop{}, ErrNotFound
	}
	return shop, nil
}

func (t *memTx) GetMaxActive(context.Context) (int, error) { return t.s.MaxActive, nil }

func (t *memTx) SetMaxActive(_ context.Context, n int, now time.Time) error {
	if n <= 0 {
		return fmt.Errorf("max_active_tasks 必须是正整数")
	}
	t.s.MaxActive = n
	t.s.SettingsAt = now
	return nil
}

func (t *memTx) GetConfig(_ context.Context, key string) (json.RawMessage, bool, error) {
	row, ok := t.s.Config[key]
	if !ok {
		return nil, false, nil
	}
	return append(json.RawMessage(nil), row.Value...), true, nil
}

func (t *memTx) SetConfig(_ context.Context, key string, val json.RawMessage, now time.Time) error {
	t.s.Config[key] = configRow{Value: append(json.RawMessage(nil), val...), UpdatedAt: now}
	return nil
}

func (t *memTx) PutTree(_ context.Context, lang string, tree json.RawMessage, synced time.Time) error {
	t.s.Trees[lang] = treeRow{Tree: append(json.RawMessage(nil), tree...), SyncedAt: synced}
	return nil
}

func (t *memTx) GetTree(_ context.Context, lang string) (json.RawMessage, time.Time, bool, error) {
	row, ok := t.s.Trees[lang]
	if !ok {
		return nil, time.Time{}, false, nil
	}
	return append(json.RawMessage(nil), row.Tree...), row.SyncedAt, true, nil
}

func (t *memTx) GetRule(_ context.Context, categoryID, typeID int64) (domain.Rule, bool, error) {
	rule, ok := t.s.Rules[ruleKey(categoryID, typeID)]
	return rule, ok, nil
}

func (t *memTx) SaveRule(_ context.Context, rule domain.Rule, now time.Time) (domain.Rule, error) {
	key := ruleKey(rule.DescriptionCategoryID, rule.TypeID)
	if existing, ok := t.s.Rules[key]; ok {
		existing.Prompt = rule.Prompt
		existing.Enabled = rule.Enabled
		existing.Version++
		existing.UpdatedAt = now
		t.s.Rules[key] = existing
		return existing, nil
	}
	t.s.NextRule++
	rule.ID = t.s.NextRule
	rule.Version = 1
	rule.CreatedAt = now
	rule.UpdatedAt = now
	t.s.Rules[key] = rule
	return rule, nil
}

func (t *memTx) GetAttrCache(_ context.Context, shopID, categoryID, typeID int64, lang string) (json.RawMessage, time.Time, *time.Time, bool, error) {
	row, ok := t.s.Attrs[attrKey(shopID, categoryID, typeID, lang)]
	if !ok {
		return nil, time.Time{}, nil, false, nil
	}
	return append(json.RawMessage(nil), row.Attrs...), row.FetchedAt, row.ExpiresAt, true, nil
}

func (t *memTx) PutAttrCache(_ context.Context, shopID, categoryID, typeID int64, lang string, attrs json.RawMessage, fetched time.Time, expires *time.Time) error {
	t.s.Attrs[attrKey(shopID, categoryID, typeID, lang)] = attrRow{
		Attrs: append(json.RawMessage(nil), attrs...), FetchedAt: fetched, ExpiresAt: expires,
	}
	return nil
}

func (t *memTx) ListDict(_ context.Context, q domain.DictQuery) ([]domain.DictValue, error) {
	out := make([]domain.DictValue, 0)
	needle := strings.ToLower(strings.TrimSpace(q.Keyword))
	for _, row := range t.s.Dicts {
		if row.ShopID != q.ShopID || row.DescriptionCategoryID != q.CategoryID || row.TypeID != q.TypeID || row.AttributeID != q.AttributeID || row.Language != q.Language {
			continue
		}
		if needle != "" && !strings.Contains(strings.ToLower(row.ValueText), needle) && !strings.Contains(fmt.Sprint(row.ValueID), needle) {
			continue
		}
		out = append(out, row)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ValueID < out[j].ValueID })
	if q.Offset > 0 {
		if q.Offset >= len(out) {
			return []domain.DictValue{}, nil
		}
		out = out[q.Offset:]
	}
	if q.Limit > 0 && len(out) > q.Limit {
		out = out[:q.Limit]
	}
	return out, nil
}

func (t *memTx) UpsertDict(_ context.Context, rows []domain.DictValue) error {
	for _, row := range rows {
		t.s.Dicts[dictKey(row)] = row
	}
	return nil
}

func (t *memTx) GetDictSync(_ context.Context, shopID, categoryID, typeID, attributeID int64, lang string) (domain.DictSync, bool, error) {
	row, ok := t.s.Syncs[syncKey(shopID, categoryID, typeID, attributeID, lang)]
	return row, ok, nil
}

func (t *memTx) PutDictSync(_ context.Context, row domain.DictSync) error {
	t.s.Syncs[syncKey(row.ShopID, row.DescriptionCategoryID, row.TypeID, row.AttributeID, row.Language)] = row
	return nil
}

func (t *memTx) CreateTask(_ context.Context, task domain.Task) (domain.Task, error) {
	t.s.NextTask++
	task.ID = t.s.NextTask
	t.s.Tasks[task.ID] = task
	return task, nil
}

func (t *memTx) UpdateTask(_ context.Context, task domain.Task) error {
	if _, ok := t.s.Tasks[task.ID]; !ok {
		return ErrNotFound
	}
	t.s.Tasks[task.ID] = task
	return nil
}

func (t *memTx) GetTaskByToken(_ context.Context, token string) (domain.Task, error) {
	if token == "" {
		return domain.Task{}, ErrNotFound
	}
	for _, task := range t.s.Tasks {
		if task.CategoryToken == token {
			return task, nil
		}
	}
	return domain.Task{}, ErrNotFound
}

func (t *memTx) GetTask(_ context.Context, id int64) (domain.Task, error) {
	task, ok := t.s.Tasks[id]
	if !ok {
		return domain.Task{}, ErrNotFound
	}
	return task, nil
}

func (t *memTx) ListTasks(_ context.Context, f domain.TaskFilter) ([]domain.Task, error) {
	out := make([]domain.Task, 0)
	for _, task := range t.s.Tasks {
		if f.ShopID != 0 && task.ShopID != f.ShopID {
			continue
		}
		if f.Status != "" && task.Status != f.Status {
			continue
		}
		if f.BaseSKU != "" && !strings.Contains(strings.ToLower(task.BaseSKU), strings.ToLower(f.BaseSKU)) {
			continue
		}
		out = append(out, task)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].CreatedAt.Equal(out[j].CreatedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].CreatedAt.After(out[j].CreatedAt)
	})
	if f.Offset > 0 {
		if f.Offset >= len(out) {
			return []domain.Task{}, nil
		}
		out = out[f.Offset:]
	}
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, nil
}

func (t *memTx) CountTasks(_ context.Context, statuses []string) (int, error) {
	allow := map[string]bool{}
	for _, status := range statuses {
		allow[status] = true
	}
	n := 0
	for _, task := range t.s.Tasks {
		if len(allow) == 0 || allow[task.Status] {
			n++
		}
	}
	return n, nil
}

func (t *memTx) ClaimQueued(_ context.Context, now time.Time) ([]domain.Task, error) {
	active := 0
	for _, task := range t.s.Tasks {
		if domain.HoldsSlot(task.Status) {
			active++
		}
	}
	slots := t.s.MaxActive - active
	if slots <= 0 {
		return nil, nil
	}
	var queued []domain.Task
	for _, task := range t.s.Tasks {
		if task.Status == domain.StatusQueued {
			queued = append(queued, task)
		}
	}
	sort.Slice(queued, func(i, j int) bool {
		if queued[i].CreatedAt.Equal(queued[j].CreatedAt) {
			return queued[i].ID < queued[j].ID
		}
		return queued[i].CreatedAt.Before(queued[j].CreatedAt)
	})
	if len(queued) > slots {
		queued = queued[:slots]
	}
	for i := range queued {
		admitted := now
		queued[i].Status = domain.StatusDispatching
		queued[i].AdmittedAt = &admitted
		queued[i].UpdatedAt = now
		t.s.Tasks[queued[i].ID] = queued[i]
	}
	return queued, nil
}

func (t *memTx) CreateTurn(_ context.Context, turn domain.Turn) (domain.Turn, error) {
	for _, existing := range t.s.Turns {
		if existing.TaskID == turn.TaskID && (existing.RunStatus == domain.TurnStarting || existing.RunStatus == domain.TurnRunning) {
			return domain.Turn{}, ErrConflict
		}
	}
	t.s.NextTurn++
	turn.ID = t.s.NextTurn
	t.s.Turns[turn.ID] = turn
	return turn, nil
}

func (t *memTx) UpdateTurn(_ context.Context, turn domain.Turn) error {
	if _, ok := t.s.Turns[turn.ID]; !ok {
		return ErrNotFound
	}
	t.s.Turns[turn.ID] = turn
	return nil
}

func (t *memTx) ListTurns(_ context.Context, taskID int64) ([]domain.Turn, error) {
	out := make([]domain.Turn, 0)
	for _, turn := range t.s.Turns {
		if turn.TaskID == taskID {
			out = append(out, turn)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Seq < out[j].Seq })
	return out, nil
}

func (t *memTx) ActiveTurn(_ context.Context, taskID int64) (domain.Turn, bool, error) {
	for _, turn := range t.s.Turns {
		if turn.TaskID == taskID && (turn.RunStatus == domain.TurnStarting || turn.RunStatus == domain.TurnRunning) {
			return turn, true, nil
		}
	}
	return domain.Turn{}, false, nil
}

func (t *memTx) CreateMessage(_ context.Context, msg domain.Message) (domain.Message, error) {
	t.s.NextMsg++
	msg.ID = t.s.NextMsg
	t.s.Messages[msg.ID] = msg
	return msg, nil
}

func (t *memTx) UpdateMessage(_ context.Context, msg domain.Message) error {
	if _, ok := t.s.Messages[msg.ID]; !ok {
		return ErrNotFound
	}
	t.s.Messages[msg.ID] = msg
	return nil
}

func (t *memTx) ListMessages(_ context.Context, taskID int64) ([]domain.Message, error) {
	out := make([]domain.Message, 0)
	for _, msg := range t.s.Messages {
		if msg.TaskID == taskID {
			out = append(out, msg)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (t *memTx) PendingMessages(_ context.Context, taskID int64) ([]domain.Message, error) {
	out := make([]domain.Message, 0)
	for _, msg := range t.s.Messages {
		if msg.TaskID != taskID || msg.DeliveryStatus != domain.DeliveryPending {
			continue
		}
		if msg.Role != domain.RoleUser && msg.Role != domain.RoleSystem {
			continue
		}
		out = append(out, msg)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (t *memTx) UpsertAsset(_ context.Context, asset domain.Asset) (domain.Asset, error) {
	for id, existing := range t.s.Assets {
		if existing.TaskID == asset.TaskID && existing.AssetID == asset.AssetID {
			asset.ID = id
			if asset.CreatedAt.IsZero() {
				asset.CreatedAt = existing.CreatedAt
			}
			t.s.Assets[id] = asset
			return asset, nil
		}
	}
	t.s.NextAsset++
	asset.ID = t.s.NextAsset
	t.s.Assets[asset.ID] = asset
	return asset, nil
}

func (t *memTx) ListAssets(_ context.Context, taskID int64) ([]domain.Asset, error) {
	out := make([]domain.Asset, 0)
	for _, asset := range t.s.Assets {
		if asset.TaskID == taskID {
			out = append(out, asset)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (t *memTx) CreateProduct(_ context.Context, product domain.Product) (domain.Product, error) {
	if product.OfferID != nil && *product.OfferID != "" {
		for _, existing := range t.s.Products {
			if existing.ShopID == product.ShopID && existing.OfferID != nil && *existing.OfferID == *product.OfferID {
				return domain.Product{}, ErrConflict
			}
		}
	}
	t.s.NextProduct++
	product.ID = t.s.NextProduct
	t.s.Products[product.ID] = product
	return product, nil
}

func (t *memTx) UpdateProduct(_ context.Context, product domain.Product) error {
	current, ok := t.s.Products[product.ID]
	if !ok {
		return ErrNotFound
	}
	if product.OfferID != nil && *product.OfferID != "" {
		for _, existing := range t.s.Products {
			if existing.ID != product.ID && existing.ShopID == product.ShopID && existing.OfferID != nil && *existing.OfferID == *product.OfferID {
				return ErrConflict
			}
		}
	}
	if product.CreatedAt.IsZero() {
		product.CreatedAt = current.CreatedAt
	}
	t.s.Products[product.ID] = product
	return nil
}

func (t *memTx) GetProduct(_ context.Context, id int64) (domain.Product, error) {
	product, ok := t.s.Products[id]
	if !ok {
		return domain.Product{}, ErrNotFound
	}
	return product, nil
}

func (t *memTx) FindProductByOffer(_ context.Context, shopID int64, offerID string) (domain.Product, bool, error) {
	for _, product := range t.s.Products {
		if product.ShopID == shopID && product.OfferID != nil && *product.OfferID == offerID {
			return product, true, nil
		}
	}
	return domain.Product{}, false, nil
}

func (t *memTx) ListProducts(_ context.Context, f domain.ProductFilter) ([]domain.Product, error) {
	out := make([]domain.Product, 0)
	for _, product := range t.s.Products {
		if f.ShopID != 0 && product.ShopID != f.ShopID {
			continue
		}
		if f.OfferID != "" && (product.OfferID == nil || !strings.Contains(strings.ToLower(*product.OfferID), strings.ToLower(f.OfferID))) {
			continue
		}
		if f.BaseSKU != "" && !strings.Contains(strings.ToLower(product.BaseSKU), strings.ToLower(f.BaseSKU)) {
			continue
		}
		if f.PublishStatus != "" && product.PublishStatus != f.PublishStatus {
			continue
		}
		if f.CategoryID != 0 && product.DescriptionCategoryID != f.CategoryID {
			continue
		}
		if f.TypeID != 0 && product.TypeID != f.TypeID {
			continue
		}
		if f.ValidationStatus != "" && product.ValidationStatus != f.ValidationStatus {
			continue
		}
		if f.TextReviewStatus != "" && product.TextReviewStatus != f.TextReviewStatus {
			continue
		}
		out = append(out, product)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].UpdatedAt.Equal(out[j].UpdatedAt) {
			return out[i].ID > out[j].ID
		}
		return out[i].UpdatedAt.After(out[j].UpdatedAt)
	})
	if f.Offset > 0 {
		if f.Offset >= len(out) {
			return []domain.Product{}, nil
		}
		out = out[f.Offset:]
	}
	if f.Limit > 0 && len(out) > f.Limit {
		out = out[:f.Limit]
	}
	return out, nil
}

func (t *memTx) GetDelivery(_ context.Context, taskID int64, skuRef string) (domain.Delivery, bool, error) {
	for _, row := range t.s.Deliveries {
		if row.TaskID == taskID && row.SKURef == skuRef {
			return row, true, nil
		}
	}
	return domain.Delivery{}, false, nil
}

func (t *memTx) CreateDelivery(_ context.Context, row domain.Delivery) error {
	for _, existing := range t.s.Deliveries {
		if existing.TaskID == row.TaskID && existing.SKURef == row.SKURef {
			if existing.ProductID == row.ProductID {
				return nil
			}
			return ErrConflict
		}
		if existing.ProductID == row.ProductID {
			return ErrConflict
		}
	}
	t.s.Deliveries = append(t.s.Deliveries, row)
	return nil
}

func (t *memTx) ListDeliveries(_ context.Context, taskID int64) ([]domain.Delivery, error) {
	out := make([]domain.Delivery, 0)
	for _, row := range t.s.Deliveries {
		if row.TaskID == taskID {
			out = append(out, row)
		}
	}
	return out, nil
}

func (t *memTx) CreatePricingRule(_ context.Context, rule domain.PricingRule) (domain.PricingRule, error) {
	t.s.NextPrice++
	rule.ID = t.s.NextPrice
	t.s.Prices[rule.ID] = rule
	return rule, nil
}

func (t *memTx) UpdatePricingRule(_ context.Context, rule domain.PricingRule) error {
	if _, ok := t.s.Prices[rule.ID]; !ok {
		return ErrNotFound
	}
	t.s.Prices[rule.ID] = rule
	return nil
}

func (t *memTx) ListPricingRules(context.Context) ([]domain.PricingRule, error) {
	out := make([]domain.PricingRule, 0, len(t.s.Prices))
	for _, rule := range t.s.Prices {
		out = append(out, rule)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (t *memTx) GetPricingRule(_ context.Context, id int64) (domain.PricingRule, error) {
	rule, ok := t.s.Prices[id]
	if !ok {
		return domain.PricingRule{}, ErrNotFound
	}
	return rule, nil
}

func (t *memTx) CreateBatch(_ context.Context, batch domain.ImportBatch) (domain.ImportBatch, error) {
	t.s.NextBatch++
	batch.ID = t.s.NextBatch
	t.s.Batches[batch.ID] = batch
	return batch, nil
}

func (t *memTx) UpdateBatch(_ context.Context, batch domain.ImportBatch) error {
	if _, ok := t.s.Batches[batch.ID]; !ok {
		return ErrNotFound
	}
	t.s.Batches[batch.ID] = batch
	return nil
}

func (t *memTx) GetBatch(_ context.Context, id int64) (domain.ImportBatch, error) {
	batch, ok := t.s.Batches[id]
	if !ok {
		return domain.ImportBatch{}, ErrNotFound
	}
	return batch, nil
}

func (t *memTx) ListBatches(_ context.Context, shopID int64) ([]domain.ImportBatch, error) {
	out := make([]domain.ImportBatch, 0)
	for _, batch := range t.s.Batches {
		if shopID == 0 || batch.ShopID == shopID {
			out = append(out, batch)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

func (t *memTx) CreateBatchItem(_ context.Context, item domain.ImportBatchItem) (domain.ImportBatchItem, error) {
	for _, existing := range t.s.Items {
		if existing.BatchID == item.BatchID && existing.ProductID == item.ProductID {
			return domain.ImportBatchItem{}, ErrConflict
		}
	}
	t.s.NextItem++
	item.ID = t.s.NextItem
	t.s.Items[item.ID] = item
	return item, nil
}

func (t *memTx) UpdateBatchItem(_ context.Context, item domain.ImportBatchItem) error {
	if _, ok := t.s.Items[item.ID]; !ok {
		return ErrNotFound
	}
	t.s.Items[item.ID] = item
	return nil
}

func (t *memTx) ListBatchItems(_ context.Context, batchID int64) ([]domain.ImportBatchItem, error) {
	out := make([]domain.ImportBatchItem, 0)
	for _, item := range t.s.Items {
		if item.BatchID == batchID {
			out = append(out, item)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out, nil
}

func (t *memTx) FindBatchByOzonTask(_ context.Context, shopID, ozonTaskID int64) (domain.ImportBatch, bool, error) {
	for _, batch := range t.s.Batches {
		if batch.ShopID == shopID && batch.OzonTaskID != nil && *batch.OzonTaskID == ozonTaskID {
			return batch, true, nil
		}
	}
	return domain.ImportBatch{}, false, nil
}

func (t *memTx) GetIdem(_ context.Context, scope, key string) (json.RawMessage, bool, error) {
	row, ok := t.s.Idem[scope+"\x00"+key]
	if !ok {
		return nil, false, nil
	}
	return append(json.RawMessage(nil), row.Response...), true, nil
}

func (t *memTx) PutIdem(_ context.Context, scope, key string, resp json.RawMessage, now time.Time) error {
	t.s.Idem[scope+"\x00"+key] = idemRow{Response: append(json.RawMessage(nil), resp...), CreatedAt: now}
	return nil
}
