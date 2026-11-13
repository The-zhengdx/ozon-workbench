package app

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"ozon-workbench/internal/domain"
	"ozon-workbench/internal/ozonapi"
	"ozon-workbench/internal/store"
)

type TreeView struct {
	Language string          `json:"language"`
	Tree     json.RawMessage `json:"tree"`
	SyncedAt time.Time       `json:"synced_at"`
	Source   string          `json:"source"`
	Cached   bool            `json:"cached"`
}

func (e *Engine) SyncTree(ctx context.Context, shopID int64, language string) (TreeView, error) {
	language = normLang(language)
	shop, err := e.shopEnabled(ctx, shopID)
	if err != nil {
		return TreeView{}, err
	}
	raw, err := e.ozon(shop).Tree(ctx, language)
	if err != nil {
		return TreeView{}, fail(502, "OZON_UPSTREAM_UNAVAILABLE", "类目树同步失败", err.Error())
	}
	if len(raw) == 0 || string(raw) == "null" {
		return TreeView{}, fail(502, "OZON_UPSTREAM_UNAVAILABLE", "类目树响应为空", nil)
	}
	now := e.now()
	if err := e.DB.InTx(ctx, func(tx store.Tx) error {
		return tx.PutTree(ctx, language, raw, now)
	}); err != nil {
		return TreeView{}, err
	}
	return TreeView{Language: language, Tree: raw, SyncedAt: now, Source: e.ozon(shop).Source(), Cached: false}, nil
}

func (e *Engine) GetTree(ctx context.Context, language string) (TreeView, error) {
	language = normLang(language)
	var raw json.RawMessage
	var synced time.Time
	var ok bool
	err := e.DB.InTx(ctx, func(tx store.Tx) error {
		var err error
		raw, synced, ok, err = tx.GetTree(ctx, language)
		return err
	})
	if err != nil {
		return TreeView{}, err
	}
	if !ok {
		return TreeView{}, fail(404, "TREE_EMPTY", "还没有类目树缓存，请先同步", nil)
	}
	return TreeView{Language: language, Tree: raw, SyncedAt: synced, Source: "cache", Cached: true}, nil
}

type TreeHit struct {
	DescriptionCategoryID int64  `json:"description_category_id"`
	CategoryName          string `json:"category_name"`
	TypeID                int64  `json:"type_id,omitempty"`
	TypeName              string `json:"type_name,omitempty"`
}

func (e *Engine) SearchTree(ctx context.Context, language, query string) ([]TreeHit, error) {
	view, err := e.GetTree(ctx, language)
	if err != nil {
		return nil, err
	}
	var node any
	if err := json.Unmarshal(view.Tree, &node); err != nil {
		return nil, err
	}
	query = strings.ToLower(strings.TrimSpace(query))
	hits := make([]TreeHit, 0)
	var walk func(any, TreeHit)
	walk = func(v any, parent TreeHit) {
		switch n := v.(type) {
		case []any:
			for _, child := range n {
				walk(child, parent)
			}
		case map[string]any:
			hit := parent
			if id := int64From(n["description_category_id"]); id > 0 {
				hit.DescriptionCategoryID = id
			}
			if name, _ := n["category_name"].(string); name != "" {
				hit.CategoryName = name
			}
			if id := int64From(n["type_id"]); id > 0 {
				hit.TypeID = id
			}
			if name, _ := n["type_name"].(string); name != "" {
				hit.TypeName = name
			}
			if hit.TypeID > 0 || hit.DescriptionCategoryID > 0 {
				blob := strings.ToLower(hit.CategoryName + " " + hit.TypeName + " " + fmt.Sprint(hit.DescriptionCategoryID) + " " + fmt.Sprint(hit.TypeID))
				if query == "" || strings.Contains(blob, query) {
					if hit.TypeID > 0 {
						hits = append(hits, hit)
					}
				}
			}
			if children, ok := n["children"]; ok {
				walk(children, hit)
			}
		}
	}
	walk(node, TreeHit{})
	return hits, nil
}

func (e *Engine) Attributes(ctx context.Context, shopID, categoryID, typeID int64, language, token string) (json.RawMessage, error) {
	language = normLang(language)
	shop, err := e.shopForCategory(ctx, shopID, categoryID, typeID, token)
	if err != nil {
		return nil, err
	}
	key := fmt.Sprintf("attr:%d:%d:%d:%s", shop.ID, categoryID, typeID, language)
	var cached json.RawMessage
	var fetched time.Time
	var expires *time.Time
	var found bool
	_ = e.DB.InTx(ctx, func(tx store.Tx) error {
		cached, fetched, expires, found, _ = tx.GetAttrCache(ctx, shop.ID, categoryID, typeID, language)
		return nil
	})
	if found && !expired(e.now(), expires) && len(cached) > 0 {
		return cached, nil
	}
	if err := e.once(key, func() error {
		_ = e.DB.InTx(ctx, func(tx store.Tx) error {
			cached, fetched, expires, found, _ = tx.GetAttrCache(ctx, shop.ID, categoryID, typeID, language)
			return nil
		})
		if found && !expired(e.now(), expires) && len(cached) > 0 {
			return nil
		}
		attrs, err := e.ozon(shop).Attributes(ctx, categoryID, typeID, language)
		if err != nil {
			return fail(502, "OZON_UPSTREAM_UNAVAILABLE", "属性查询失败", err.Error())
		}
		if attrs == nil {
			attrs = []ozonapi.Attribute{}
		}
		raw, err := json.Marshal(attrs)
		if err != nil {
			return err
		}
		now := e.now()
		exp := e.cacheExpiry(ctx, now)
		return e.DB.InTx(ctx, func(tx store.Tx) error {
			return tx.PutAttrCache(ctx, shop.ID, categoryID, typeID, language, raw, now, exp)
		})
	}); err != nil {
		return nil, err
	}
	err = e.DB.InTx(ctx, func(tx store.Tx) error {
		cached, fetched, expires, found, err = tx.GetAttrCache(ctx, shop.ID, categoryID, typeID, language)
		return err
	})
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, fail(502, "OZON_UPSTREAM_UNAVAILABLE", "属性缓存没有写入", nil)
	}
	_ = fetched
	return cached, nil
}

func (e *Engine) Dictionary(ctx context.Context, shopID, categoryID, typeID, attributeID int64, language, keyword string, page, pageSize int, token string) (any, error) {
	language = normLang(language)
	shop, err := e.shopForCategory(ctx, shopID, categoryID, typeID, token)
	if err != nil {
		return nil, err
	}
	if pageSize <= 0 {
		pageSize = 100
	}
	if page <= 0 {
		page = 1
	}
	if strings.TrimSpace(keyword) != "" {
		return e.searchDict(ctx, shop, categoryID, typeID, attributeID, language, strings.TrimSpace(keyword), pageSize)
	}
	return e.pageDict(ctx, shop, categoryID, typeID, attributeID, language, page, pageSize)
}

func (e *Engine) searchDict(ctx context.Context, shop domain.Shop, categoryID, typeID, attributeID int64, language, keyword string, limit int) (any, error) {
	var sync domain.DictSync
	var hasSync bool
	var local []domain.DictValue
	err := e.DB.InTx(ctx, func(tx store.Tx) error {
		var err error
		sync, hasSync, err = tx.GetDictSync(ctx, shop.ID, categoryID, typeID, attributeID, language)
		if err != nil {
			return err
		}
		local, err = tx.ListDict(ctx, domain.DictQuery{
			ShopID: shop.ID, CategoryID: categoryID, TypeID: typeID, AttributeID: attributeID, Language: language, Keyword: keyword, Limit: limit,
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	freshComplete := hasSync && sync.ListStatus == "complete" && !expired(e.now(), sync.ExpiresAt)
	if freshComplete || len(local) > 0 {
		return map[string]any{"values": dictOut(local), "source": "cache", "list_status": statusOf(sync, hasSync)}, nil
	}
	values, err := e.ozon(shop).SearchValues(ctx, categoryID, typeID, attributeID, language, keyword, limit)
	if err != nil {
		return nil, fail(502, "OZON_UPSTREAM_UNAVAILABLE", "字典搜索失败", err.Error())
	}
	now := e.now()
	rows := toDict(shop.ID, categoryID, typeID, attributeID, language, "search", now, values)
	if err := e.DB.InTx(ctx, func(tx store.Tx) error {
		return tx.UpsertDict(ctx, rows)
	}); err != nil {
		return nil, err
	}
	return map[string]any{"values": values, "source": e.ozon(shop).Source(), "list_status": statusOf(sync, hasSync)}, nil
}

func (e *Engine) pageDict(ctx context.Context, shop domain.Shop, categoryID, typeID, attributeID int64, language string, page, pageSize int) (any, error) {
	var sync domain.DictSync
	var hasSync bool
	err := e.DB.InTx(ctx, func(tx store.Tx) error {
		var err error
		sync, hasSync, err = tx.GetDictSync(ctx, shop.ID, categoryID, typeID, attributeID, language)
		return err
	})
	if err != nil {
		return nil, err
	}
	if hasSync && sync.ListStatus == "complete" && !expired(e.now(), sync.ExpiresAt) {
		local, err := e.listDict(ctx, shop.ID, categoryID, typeID, attributeID, language, "", (page-1)*pageSize, pageSize)
		if err != nil {
			return nil, err
		}
		return map[string]any{"values": dictOut(local), "source": "cache", "list_status": "complete", "page": page, "page_size": pageSize}, nil
	}
	last := int64(0)
	if hasSync {
		last = sync.LastValueID
	}
	values, newLast, complete, err := e.ozon(shop).ListValues(ctx, categoryID, typeID, attributeID, language, last, pageSize)
	if err != nil {
		return nil, fail(502, "OZON_UPSTREAM_UNAVAILABLE", "字典分页失败", err.Error())
	}
	now := e.now()
	rows := toDict(shop.ID, categoryID, typeID, attributeID, language, "list", now, values)
	status := "partial"
	if complete {
		status = "complete"
	}
	sync = domain.DictSync{
		ShopID: shop.ID, DescriptionCategoryID: categoryID, TypeID: typeID, AttributeID: attributeID, Language: language,
		ListStatus: status, LastValueID: newLast, SyncedAt: &now, ExpiresAt: e.cacheExpiry(ctx, now),
	}
	if err := e.DB.InTx(ctx, func(tx store.Tx) error {
		if err := tx.UpsertDict(ctx, rows); err != nil {
			return err
		}
		return tx.PutDictSync(ctx, sync)
	}); err != nil {
		return nil, err
	}
	return map[string]any{"values": values, "source": e.ozon(shop).Source(), "list_status": status, "page": page, "page_size": pageSize}, nil
}

func (e *Engine) GetRule(ctx context.Context, categoryID, typeID int64) (domain.Rule, error) {
	var rule domain.Rule
	var ok bool
	err := e.DB.InTx(ctx, func(tx store.Tx) error {
		var err error
		rule, ok, err = tx.GetRule(ctx, categoryID, typeID)
		return err
	})
	if err != nil {
		return domain.Rule{}, err
	}
	if !ok {
		return domain.Rule{}, fail(404, "RULE_NOT_FOUND", "这个类目还没有提示词", nil)
	}
	return rule, nil
}

func (e *Engine) SaveRule(ctx context.Context, categoryID, typeID int64, prompt string, enabled bool) (domain.Rule, error) {
	prompt = strings.TrimSpace(prompt)
	if prompt == "" || categoryID <= 0 || typeID <= 0 {
		return domain.Rule{}, fail(400, "RULE_EMPTY", "类目、类型和提示词都不能为空", nil)
	}
	var saved domain.Rule
	err := e.DB.InTx(ctx, func(tx store.Tx) error {
		var err error
		saved, err = tx.SaveRule(ctx, domain.Rule{DescriptionCategoryID: categoryID, TypeID: typeID, Prompt: prompt, Enabled: enabled}, e.now())
		return err
	})
	return saved, err
}

func (e *Engine) shopEnabled(ctx context.Context, id int64) (domain.Shop, error) {
	shop, err := e.GetShop(ctx, id)
	if err != nil {
		return domain.Shop{}, err
	}
	if !shop.Enable {
		return domain.Shop{}, fail(409, "SHOP_DISABLED", "店铺已停用，不能发起新的加工或刊登", nil)
	}
	return shop, nil
}

func (e *Engine) shopForCategory(ctx context.Context, shopID, categoryID, typeID int64, token string) (domain.Shop, error) {
	if token != "" {
		var task domain.Task
		err := e.DB.InTx(ctx, func(tx store.Tx) error {
			var err error
			task, err = tx.GetTaskByToken(ctx, token)
			return err
		})
		if err != nil {
			return domain.Shop{}, fail(403, "CATEGORY_TOKEN", "类目查询令牌无效", nil)
		}
		if task.DescriptionCategoryID != categoryID || task.TypeID != typeID {
			return domain.Shop{}, fail(403, "CATEGORY_TOKEN", "令牌不能查询其他类目", nil)
		}
		return e.GetShop(ctx, task.ShopID)
	}
	if shopID == 0 {
		return domain.Shop{}, fail(400, "SHOP_REQUIRED", "需要店铺或任务令牌", nil)
	}
	return e.GetShop(ctx, shopID)
}

func (e *Engine) listDict(ctx context.Context, shopID, categoryID, typeID, attributeID int64, language, keyword string, offset, limit int) ([]domain.DictValue, error) {
	var rows []domain.DictValue
	err := e.DB.InTx(ctx, func(tx store.Tx) error {
		var err error
		rows, err = tx.ListDict(ctx, domain.DictQuery{
			ShopID: shopID, CategoryID: categoryID, TypeID: typeID, AttributeID: attributeID, Language: language, Keyword: keyword, Offset: offset, Limit: limit,
		})
		return err
	})
	return rows, err
}

func (e *Engine) cacheExpiry(ctx context.Context, now time.Time) *time.Time {
	var raw []byte
	_ = e.DB.InTx(ctx, func(tx store.Tx) error {
		val, ok, err := tx.GetConfig(ctx, "category_cache_ttl_seconds")
		if err != nil || !ok {
			return err
		}
		raw = val
		return nil
	})
	if len(raw) == 0 {
		return nil
	}
	var seconds int
	if err := json.Unmarshal(raw, &seconds); err != nil || seconds <= 0 {
		return nil
	}
	exp := now.Add(time.Duration(seconds) * time.Second)
	return &exp
}

func normLang(language string) string {
	language = strings.TrimSpace(language)
	if language == "" {
		return "RU"
	}
	return language
}

func expired(now time.Time, at *time.Time) bool {
	return at != nil && !at.After(now)
}

func int64From(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	default:
		return 0
	}
}

func toDict(shopID, categoryID, typeID, attributeID int64, language, source string, now time.Time, values []ozonapi.Value) []domain.DictValue {
	rows := make([]domain.DictValue, 0, len(values))
	for _, value := range values {
		rows = append(rows, domain.DictValue{
			ShopID: shopID, DescriptionCategoryID: categoryID, TypeID: typeID, AttributeID: attributeID, Language: language,
			ValueID: value.ID, ValueText: value.Value, Info: value.Info, Picture: value.Picture, Source: source, FetchedAt: now,
		})
	}
	return rows
}

func dictOut(rows []domain.DictValue) []ozonapi.Value {
	out := make([]ozonapi.Value, 0, len(rows))
	for _, row := range rows {
		out = append(out, ozonapi.Value{ID: row.ValueID, Value: row.ValueText, Info: row.Info, Picture: row.Picture})
	}
	return out
}

func statusOf(sync domain.DictSync, ok bool) string {
	if !ok || sync.ListStatus == "" {
		return "partial"
	}
	return sync.ListStatus
}
