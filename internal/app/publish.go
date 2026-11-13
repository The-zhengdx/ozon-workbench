package app

import (
	"context"
	"encoding/json"
	"strings"

	"ozon-workbench/internal/domain"
	"ozon-workbench/internal/store"
)

type ProductPatch struct {
	Name         *string
	Cost         *string
	Price        *string
	OldPrice     *string
	PrimaryImage *string
	Images       []string
	HasImages    bool
	APIItem      *json.RawMessage
}

func (e *Engine) ListProducts(ctx context.Context, filter domain.ProductFilter) ([]domain.Product, error) {
	var rows []domain.Product
	err := e.DB.InTx(ctx, func(tx store.Tx) error {
		var err error
		rows, err = tx.ListProducts(ctx, filter)
		return err
	})
	return rows, err
}

func (e *Engine) GetProduct(ctx context.Context, id int64) (domain.Product, error) {
	var row domain.Product
	err := e.DB.InTx(ctx, func(tx store.Tx) error {
		var err error
		row, err = tx.GetProduct(ctx, id)
		return err
	})
	if err == store.ErrNotFound {
		return domain.Product{}, fail(404, "PRODUCT_NOT_FOUND", "商品不存在", nil)
	}
	return row, err
}

func (e *Engine) UpdateProduct(ctx context.Context, id int64, patch ProductPatch) (domain.Product, error) {
	var updated domain.Product
	err := e.DB.InTx(ctx, func(tx store.Tx) error {
		row, err := tx.GetProduct(store.ForUpdate(ctx), id)
		if err != nil {
			return err
		}
		if patch.Name != nil {
			row.Name = patch.Name
		}
		if patch.Cost != nil {
			row.Cost = patch.Cost
		}
		if patch.Price != nil {
			row.Price = patch.Price
		}
		if patch.OldPrice != nil {
			row.OldPrice = patch.OldPrice
		}
		if patch.PrimaryImage != nil {
			row.PrimaryImage = patch.PrimaryImage
		}
		if patch.HasImages {
			raw, _ := json.Marshal(patch.Images)
			row.Images = raw
		}
		row.APIJSONRevision++
		row.UpdatedAt = e.now()
		if patch.APIItem != nil {
			if err := mergeAPIItem(&row, *patch.APIItem); err != nil {
				return err
			}
		} else {
			rebuildAPIItem(&row)
		}
		row.ValidationStatus = "unchecked"
		row.ValidationErrors = []byte("[]")
		if err := validateProduct(&row, e.AllowTestMedia); err != nil {
			return err
		}
		updated = row
		return tx.UpdateProduct(ctx, row)
	})
	if err == store.ErrNotFound {
		return domain.Product{}, fail(404, "PRODUCT_NOT_FOUND", "商品不存在", nil)
	}
	return updated, err
}

func (e *Engine) ValidateProducts(ctx context.Context, ids []int64) ([]domain.Product, error) {
	out := make([]domain.Product, 0)
	err := e.DB.InTx(ctx, func(tx store.Tx) error {
		for _, id := range ids {
			row, err := tx.GetProduct(store.ForUpdate(ctx), id)
			if err != nil {
				return err
			}
			applyValidation(&row, e.AllowTestMedia)
			row.UpdatedAt = e.now()
			if err := tx.UpdateProduct(ctx, row); err != nil {
				return err
			}
			out = append(out, row)
		}
		return nil
	})
	return out, err
}

func (e *Engine) Publish(ctx context.Context, shopID int64, ids []int64) ([]domain.ImportBatch, error) {
	if len(ids) == 0 {
		return nil, fail(400, "PRODUCTS_REQUIRED", "请选择要刊登的 SKU", nil)
	}
	shop, err := e.shopEnabled(ctx, shopID)
	if err != nil {
		return nil, err
	}
	currency, err := e.ozon(shop).VerifyAccount(ctx)
	if err != nil {
		return nil, fail(409, "OZON_ACCOUNT_UNVERIFIED", "刊登前无法核验 Ozon 账户币种", err.Error())
	}
	if currency != shop.CurrencyCode {
		return nil, fail(409, "CURRENCY_MISMATCH", "店铺币种和 Ozon 账户核验结果不一致", map[string]string{"shop": shop.CurrencyCode, "account": currency})
	}
	var products []domain.Product
	if err := e.DB.InTx(ctx, func(tx store.Tx) error {
		for _, id := range ids {
			row, err := tx.GetProduct(store.ForUpdate(ctx), id)
			if err != nil {
				return err
			}
			if row.ShopID != shop.ID {
				return fail(409, "SHOP_MISMATCH", "只能提交同一店铺的商品", nil)
			}
			applyValidation(&row, e.AllowTestMedia)
			if row.ValidationStatus != "valid" {
				return fail(409, "PRODUCT_INVALID", "存在未通过预检的 SKU", map[string]any{"product_id": row.ID, "errors": json.RawMessage(row.ValidationErrors)})
			}
			products = append(products, row)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	limit := e.batchLimit()
	var batches []domain.ImportBatch
	for start := 0; start < len(products); start += limit {
		end := start + limit
		if end > len(products) {
			end = len(products)
		}
		batch, err := e.submitBatch(ctx, shop, products[start:end])
		if err != nil {
			return batches, err
		}
		batches = append(batches, batch)
	}
	return batches, nil
}

func (e *Engine) submitBatch(ctx context.Context, shop domain.Shop, products []domain.Product) (domain.ImportBatch, error) {
	items := make([]json.RawMessage, 0, len(products))
	for _, row := range products {
		items = append(items, row.APIItemJSON)
	}
	reqBody, _ := json.Marshal(map[string]any{"items": items})
	now := e.now()
	var batch domain.ImportBatch
	err := e.DB.InTx(ctx, func(tx store.Tx) error {
		var err error
		batch, err = tx.CreateBatch(ctx, domain.ImportBatch{
			ShopID: shop.ID, Status: "submitting", RequestJSON: reqBody, CreatedAt: now,
		})
		if err != nil {
			return err
		}
		for _, row := range products {
			if _, err := tx.CreateBatchItem(ctx, domain.ImportBatchItem{
				BatchID: batch.ID, ProductID: row.ID, OfferID: deref(row.OfferID), SubmittedRevision: row.APIJSONRevision,
				RequestItemJSON: row.APIItemJSON, Status: "created", CreatedAt: now, UpdatedAt: now,
			}); err != nil {
				return err
			}
			row.PublishStatus = "submitting"
			row.LastSubmittedAt = &now
			row.UpdatedAt = now
			if err := tx.UpdateProduct(ctx, row); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return domain.ImportBatch{}, err
	}
	taskID, raw, callErr := e.ozon(shop).Import(ctx, items)
	err = e.DB.InTx(ctx, func(tx store.Tx) error {
		batch, err = tx.GetBatch(ctx, batch.ID)
		if err != nil {
			return err
		}
		batch.ResponseJSON = raw
		if callErr != nil {
			batch.Status = "failed"
			batch.LastError, _ = json.Marshal(map[string]string{"message": callErr.Error()})
			finished := e.now()
			batch.FinishedAt = &finished
			return tx.UpdateBatch(ctx, batch)
		}
		batch.OzonTaskID = &taskID
		batch.Status = "processing"
		submitted := e.now()
		batch.SubmittedAt = &submitted
		if err := tx.UpdateBatch(ctx, batch); err != nil {
			return err
		}
		rows, err := tx.ListBatchItems(ctx, batch.ID)
		if err != nil {
			return err
		}
		for _, item := range rows {
			item.Status = "processing"
			item.UpdatedAt = e.now()
			if err := tx.UpdateBatchItem(ctx, item); err != nil {
				return err
			}
			product, err := tx.GetProduct(ctx, item.ProductID)
			if err != nil {
				return err
			}
			product.PublishStatus = "processing"
			product.UpdatedAt = e.now()
			if err := tx.UpdateProduct(ctx, product); err != nil {
				return err
			}
		}
		return nil
	})
	return batch, err
}

func (e *Engine) RefreshBatch(ctx context.Context, id int64) (domain.ImportBatch, error) {
	var batch domain.ImportBatch
	err := e.DB.InTx(ctx, func(tx store.Tx) error {
		var err error
		batch, err = tx.GetBatch(ctx, id)
		return err
	})
	if err != nil {
		return domain.ImportBatch{}, err
	}
	if batch.OzonTaskID == nil {
		return batch, fail(409, "IMPORT_NOT_SUBMITTED", "批次还没有 Ozon task_id", nil)
	}
	shop, err := e.GetShop(ctx, batch.ShopID)
	if err != nil {
		return domain.ImportBatch{}, err
	}
	rows, raw, err := e.ozon(shop).ImportInfo(ctx, *batch.OzonTaskID)
	if err != nil {
		return domain.ImportBatch{}, fail(502, "OZON_UPSTREAM_UNAVAILABLE", "导入结果查询失败", err.Error())
	}
	byOffer := map[string]struct {
		status    string
		productID int64
		errors    json.RawMessage
	}{}
	for _, row := range rows {
		byOffer[row.OfferID] = struct {
			status    string
			productID int64
			errors    json.RawMessage
		}{row.Status, row.ProductID, row.Errors}
	}
	now := e.now()
	err = e.DB.InTx(ctx, func(tx store.Tx) error {
		items, err := tx.ListBatchItems(ctx, id)
		if err != nil {
			return err
		}
		okN, badN := 0, 0
		for _, item := range items {
			info, found := byOffer[item.OfferID]
			if !found {
				continue
			}
			item.UpdatedAt = now
			product, err := tx.GetProduct(store.ForUpdate(ctx), item.ProductID)
			if err != nil {
				return err
			}
			if strings.EqualFold(info.status, "imported") || strings.EqualFold(info.status, "success") {
				item.Status = "imported"
				if info.productID > 0 {
					item.OzonProductID = &info.productID
					product.OzonProductID = &info.productID
				}
				rev := item.SubmittedRevision
				product.PublishStatus = "imported"
				product.LastImportedRevision = &rev
				product.LastImportedAt = &now
				product.LastErrorJSON = []byte("{}")
				okN++
			} else if strings.EqualFold(info.status, "failed") || len(info.errors) > 0 && string(info.errors) != "null" && string(info.errors) != "[]" {
				item.Status = "failed"
				item.ErrorJSON = info.errors
				product.PublishStatus = "failed"
				if len(info.errors) > 0 {
					product.LastErrorJSON = info.errors
				}
				badN++
			} else {
				item.Status = "processing"
				product.PublishStatus = "processing"
			}
			product.UpdatedAt = now
			if err := tx.UpdateBatchItem(ctx, item); err != nil {
				return err
			}
			if err := tx.UpdateProduct(ctx, product); err != nil {
				return err
			}
		}
		batch, err = tx.GetBatch(ctx, id)
		if err != nil {
			return err
		}
		batch.ResponseJSON = raw
		switch {
		case badN > 0 && okN > 0:
			batch.Status = "partial_failed"
			batch.FinishedAt = &now
		case badN > 0 && okN == 0:
			batch.Status = "failed"
			batch.FinishedAt = &now
		case okN == len(items) && len(items) > 0:
			batch.Status = "completed"
			batch.FinishedAt = &now
		default:
			batch.Status = "processing"
		}
		return tx.UpdateBatch(ctx, batch)
	})
	if err != nil {
		return domain.ImportBatch{}, err
	}
	_ = e.SyncPlatform(ctx, id)
	return e.batch(ctx, id)
}

func (e *Engine) SyncPlatform(ctx context.Context, batchID int64) error {
	batch, err := e.batch(ctx, batchID)
	if err != nil {
		return err
	}
	var offers []string
	var items []domain.ImportBatchItem
	if err := e.DB.InTx(ctx, func(tx store.Tx) error {
		var err error
		items, err = tx.ListBatchItems(ctx, batchID)
		return err
	}); err != nil {
		return err
	}
	for _, item := range items {
		if item.Status == "imported" {
			offers = append(offers, item.OfferID)
		}
	}
	if len(offers) == 0 {
		return nil
	}
	shop, err := e.GetShop(ctx, batch.ShopID)
	if err != nil {
		return err
	}
	rows, err := e.ozon(shop).Platform(ctx, offers)
	if err != nil {
		return fail(502, "OZON_UPSTREAM_UNAVAILABLE", "平台状态查询失败", err.Error())
	}
	byOffer := map[string]struct {
		moderation string
		sale       string
		productID  int64
		sku        int64
		raw        json.RawMessage
	}{}
	for _, row := range rows {
		byOffer[row.OfferID] = struct {
			moderation string
			sale       string
			productID  int64
			sku        int64
			raw        json.RawMessage
		}{row.Moderation, row.Sale, row.ProductID, row.SKU, row.Raw}
	}
	now := e.now()
	return e.DB.InTx(ctx, func(tx store.Tx) error {
		for _, item := range items {
			info, ok := byOffer[item.OfferID]
			if !ok {
				continue
			}
			product, err := tx.GetProduct(ctx, item.ProductID)
			if err != nil {
				return err
			}
			if info.moderation != "" {
				product.OzonModerationStatus = &info.moderation
			}
			if info.sale != "" {
				product.OzonSaleStatus = &info.sale
			}
			if info.productID > 0 {
				product.OzonProductID = &info.productID
			}
			if info.sku > 0 {
				product.OzonSKU = &info.sku
			}
			if len(info.raw) > 0 {
				product.OzonStatusRaw = info.raw
			}
			product.LastPlatformSyncedAt = &now
			product.UpdatedAt = now
			if err := tx.UpdateProduct(ctx, product); err != nil {
				return err
			}
		}
		return nil
	})
}

func (e *Engine) ListBatches(ctx context.Context, shopID int64) ([]domain.ImportBatch, error) {
	var rows []domain.ImportBatch
	err := e.DB.InTx(ctx, func(tx store.Tx) error {
		var err error
		rows, err = tx.ListBatches(ctx, shopID)
		return err
	})
	return rows, err
}

func (e *Engine) ListBatchItems(ctx context.Context, batchID int64) ([]domain.ImportBatchItem, error) {
	var rows []domain.ImportBatchItem
	err := e.DB.InTx(ctx, func(tx store.Tx) error {
		var err error
		rows, err = tx.ListBatchItems(ctx, batchID)
		return err
	})
	return rows, err
}

func (e *Engine) batch(ctx context.Context, id int64) (domain.ImportBatch, error) {
	var batch domain.ImportBatch
	err := e.DB.InTx(ctx, func(tx store.Tx) error {
		var err error
		batch, err = tx.GetBatch(ctx, id)
		return err
	})
	return batch, err
}

func deref(v *string) string {
	if v == nil {
		return ""
	}
	return *v
}

func applyValidation(row *domain.Product, allowTest bool) {
	errs := validateErrors(row, allowTest)
	raw, _ := json.Marshal(errs)
	row.ValidationErrors = raw
	if len(errs) == 0 {
		row.ValidationStatus = "valid"
	} else {
		row.ValidationStatus = "invalid"
	}
}

func validateProduct(row *domain.Product, allowTest bool) error {
	applyValidation(row, allowTest)
	return nil
}

type fieldErr struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

func validateErrors(row *domain.Product, allowTest bool) []fieldErr {
	var errs []fieldErr
	if row.OfferID == nil || strings.TrimSpace(*row.OfferID) == "" {
		errs = append(errs, fieldErr{"offer_id", "OFFER_REQUIRED", "缺少 offer_id"})
	}
	if row.Name == nil || strings.TrimSpace(*row.Name) == "" {
		errs = append(errs, fieldErr{"name", "NAME_REQUIRED", "缺少标题"})
	}
	if row.Price == nil || *row.Price == "" {
		errs = append(errs, fieldErr{"price", "PRICE_REQUIRED", "缺少售价"})
	}
	if row.PrimaryImage == nil || !strings.HasPrefix(*row.PrimaryImage, "https://") {
		errs = append(errs, fieldErr{"primary_image", "MEDIA_URL", "主图必须是 HTTPS"})
	} else if !allowTest && strings.Contains(*row.PrimaryImage, "mock-storage.local") {
		errs = append(errs, fieldErr{"primary_image", "NON_DURABLE_URL", "当前地址不能当作永久商品图"})
	}
	if len(row.APIItemJSON) == 0 {
		errs = append(errs, fieldErr{"api_item_json", "API_JSON", "还没有导入 JSON"})
	} else {
		var obj map[string]any
		if err := json.Unmarshal(row.APIItemJSON, &obj); err != nil {
			errs = append(errs, fieldErr{"api_item_json", "API_JSON", "导入 JSON 无法解析"})
		} else {
			for _, key := range []string{"cost", "origin_link", "base_sku", "generation_task_id", "task_id", "items", "video"} {
				if _, ok := obj[key]; ok {
					errs = append(errs, fieldErr{key, "FORBIDDEN_FIELD", "系统字段不能出现在导入 JSON"})
				}
			}
			if row.OfferID != nil && obj["offer_id"] != *row.OfferID {
				errs = append(errs, fieldErr{"offer_id", "JSON_MISMATCH", "导入 JSON 的 offer_id 和商品不一致"})
			}
		}
	}
	return errs
}

func rebuildAPIItem(row *domain.Product) {
	obj := map[string]any{
		"description_category_id": row.DescriptionCategoryID,
		"type_id":                 row.TypeID,
		"dimension_unit":          row.DimensionUnit,
		"weight_unit":             row.WeightUnit,
	}
	if row.OfferID != nil {
		obj["offer_id"] = *row.OfferID
	}
	if row.Name != nil {
		obj["name"] = *row.Name
	}
	if row.Price != nil {
		obj["price"] = *row.Price
	}
	if row.OldPrice != nil {
		obj["old_price"] = *row.OldPrice
	}
	if row.CurrencyCode != nil {
		obj["currency_code"] = *row.CurrencyCode
	}
	if row.VAT != nil {
		obj["vat"] = *row.VAT
	}
	if row.PrimaryImage != nil {
		obj["primary_image"] = *row.PrimaryImage
	}
	if len(row.Images) > 0 {
		var images []string
		_ = json.Unmarshal(row.Images, &images)
		obj["images"] = images
	}
	if len(row.Attributes) > 0 {
		var attrs any
		_ = json.Unmarshal(row.Attributes, &attrs)
		obj["attributes"] = attrs
	}
	if len(row.ComplexAttributes) > 0 {
		var attrs any
		_ = json.Unmarshal(row.ComplexAttributes, &attrs)
		obj["complex_attributes"] = attrs
	}
	raw, _ := json.Marshal(obj)
	row.APIItemJSON = raw
}

func mergeAPIItem(row *domain.Product, raw json.RawMessage) error {
	var obj map[string]any
	if err := json.Unmarshal(raw, &obj); err != nil {
		return fail(400, "API_JSON", "导入 JSON 无法解析", nil)
	}
	for _, key := range []string{"cost", "origin_link", "base_sku", "generation_task_id", "task_id", "items", "video"} {
		if _, ok := obj[key]; ok {
			return fail(400, "FORBIDDEN_FIELD", "导入 JSON 含有系统字段 "+key, nil)
		}
	}
	offer, _ := obj["offer_id"].(string)
	if row.OfferID != nil && offer != *row.OfferID {
		return fail(400, "JSON_MISMATCH", "导入 JSON 的 offer_id 必须和商品一致", nil)
	}
	if name, ok := obj["name"].(string); ok {
		row.Name = &name
	}
	if price, ok := obj["price"].(string); ok {
		row.Price = &price
	}
	row.APIItemJSON = raw
	return nil
}

// OnSale 只在平台可售状态明确等于 selling 时为真。审核中不能当成已在售。
func OnSale(row domain.Product) bool {
	return row.OzonSaleStatus != nil && *row.OzonSaleStatus == "selling"
}
