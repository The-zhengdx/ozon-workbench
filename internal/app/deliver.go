package app

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"time"

	"ozon-workbench/internal/codexcli"
	"ozon-workbench/internal/domain"
	"ozon-workbench/internal/ozonapi"
	"ozon-workbench/internal/pricing"
	"ozon-workbench/internal/product"
	"ozon-workbench/internal/store"
)

func (e *Engine) finalize(ctx context.Context, id int64) (bool, error) {
	err := e.DB.InTx(ctx, func(tx store.Tx) error {
		task, err := tx.GetTask(store.ForUpdate(ctx), id)
		if err != nil || task.Status != domain.StatusFinalizing {
			return err
		}
		pending, err := tx.PendingMessages(ctx, id)
		if err != nil {
			return err
		}
		now := e.now()
		if len(pending) > 0 {
			task.Status = domain.StatusRunning
			task.UpdatedAt = now
			return tx.UpdateTask(ctx, task)
		}
		reply, err := codexcli.ParseReply(task.LastReplyJSON)
		if err != nil || reply.Status != "completed" {
			task.Status = domain.StatusFailed
			task.LastError = "没有可交付的 completed 回复"
			task.FinishedAt = &now
			return tx.UpdateTask(ctx, task)
		}
		shop, err := tx.GetShop(ctx, task.ShopID)
		if err != nil {
			return err
		}
		attrs, dicts, hasCache := e.attrSnapshot(ctx, tx, shop.ID, task.DescriptionCategoryID, task.TypeID)
		assets, err := tx.ListAssets(ctx, task.ID)
		if err != nil {
			return err
		}
		allowed := map[string]string{}
		for _, asset := range assets {
			if asset.UploadStatus == domain.UploadUploaded && asset.PublicURL != "" {
				allowed[asset.PublicURL] = asset.PublicURL
			}
		}
		params, ruleID := pickPricing(ctx, tx)
		for _, sku := range reply.Data.Products {
			if _, ok, err := tx.GetDelivery(ctx, task.ID, sku.SKURef); err != nil {
				return err
			} else if ok {
				continue
			}
			offer := ""
			if sku.OfferIDSuggestion != nil {
				offer = strings.TrimSpace(*sku.OfferIDSuggestion)
			}
			if offer != "" {
				existing, found, err := tx.FindProductByOffer(ctx, shop.ID, offer)
				if err != nil {
					return err
				}
				if found && (existing.GenerationTaskID == nil || *existing.GenerationTaskID != task.ID) {
					return store.ErrConflict
				}
			}
			price, old := "", ""
			snap := json.RawMessage(`{}`)
			var usedRule *int64
			if sku.Cost.Total != nil && len(params) > 0 {
				quote, qErr := pricing.Evaluate(strconv.FormatFloat(*sku.Cost.Total, 'f', -1, 64), params)
				if qErr == nil {
					price, old, snap, usedRule = quote.Price, quote.OldPrice, quote.Snapshot, ruleID
				}
			}
			built, err := product.Build(product.Input{
				Shop: shop, Task: task, SKU: sku, Price: price, OldPrice: old,
				Attributes: attrs, DictIDs: dicts, AssetURLs: allowed, HasAttrCache: hasCache,
			})
			if err != nil {
				return err
			}
			if built.Blocking {
				return store.ErrConflict
			}
			row := productFrom(task, shop, sku, offer, price, old, snap, usedRule, built, now)
			created, err := tx.CreateProduct(ctx, row)
			if err != nil {
				return err
			}
			if err := tx.CreateDelivery(ctx, domain.Delivery{TaskID: task.ID, SKURef: sku.SKURef, ProductID: created.ID, DeliveredAt: now}); err != nil {
				return err
			}
		}
		task.Status = domain.StatusCompleted
		task.Stage = "delivery"
		task.FinishedAt = &now
		task.UpdatedAt = now
		task.LastError = ""
		return tx.UpdateTask(ctx, task)
	})
	if err == store.ErrConflict || err == store.ErrForcedRollback {
		_ = e.DB.InTx(ctx, func(tx store.Tx) error {
			task, getErr := tx.GetTask(ctx, id)
			if getErr != nil {
				return getErr
			}
			if err == store.ErrConflict {
				task.LastError = "SKU 与已有商品冲突，事务已回滚，没有部分写入"
			} else {
				task.LastError = "交付事务没有提交"
			}
			if task.Status == domain.StatusFinalizing {
				task.UpdatedAt = e.now()
				return tx.UpdateTask(ctx, task)
			}
			return nil
		})
		return false, nil
	}
	return err == nil, err
}

func productFrom(task domain.Task, shop domain.Shop, sku codexcli.Product, offer, price, old string, snap json.RawMessage, ruleID *int64, built product.Built, now time.Time) domain.Product {
	rawSKU, _ := json.Marshal(sku)
	costSrc := map[string]any{"currency": "CNY"}
	if sku.Cost.PurchasePrice != nil {
		costSrc["purchase_price"] = *sku.Cost.PurchasePrice
	}
	if sku.Cost.DomesticShipping != nil {
		costSrc["domestic_shipping"] = *sku.Cost.DomesticShipping
	}
	if sku.Cost.Evidence != nil {
		costSrc["evidence"] = *sku.Cost.Evidence
	}
	costJSON, _ := json.Marshal(costSrc)
	attrJSON, _ := json.Marshal(sku.Attributes)
	complexJSON, _ := json.Marshal(sku.ComplexAttributes)
	images, _ := json.Marshal(sku.Images)
	errs, _ := json.Marshal(built.Errors)
	status := "valid"
	if len(built.Errors) > 0 {
		status = "invalid"
	}
	row := domain.Product{
		ShopID: shop.ID, GenerationTaskID: &task.ID, BaseSKU: task.BaseSKU,
		DescriptionCategoryID: task.DescriptionCategoryID, TypeID: task.TypeID,
		CostCurrencyCode: "CNY", CostSourceJSON: costJSON, PricingSnapshot: snap,
		Images: images, PDFList: []byte("[]"), Promotions: []byte("[]"),
		Attributes: attrJSON, ComplexAttributes: complexJSON, AIStructuredJSON: rawSKU,
		APIItemJSON: built.Item, APIJSONRevision: 1, ValidationStatus: status, ValidationErrors: errs,
		TextReviewStatus:  reviewState(task.CopyApproved || task.CopyAutoApprove),
		ImageReviewStatus: reviewState(task.VisualResultApproved || task.VisualResultAutoApprove),
		PublishStatus:     "draft", DimensionUnit: "mm", WeightUnit: "g",
		OzonStatusRaw: []byte("{}"), LastErrorJSON: []byte("{}"),
		CurrencyCode: strPtr(shop.CurrencyCode), VAT: strPtr(shop.VAT), PricingRuleID: ruleID,
		CreatedAt: now, UpdatedAt: now,
	}
	if offer != "" {
		row.OfferID = &offer
	}
	if task.OriginLink != "" {
		row.OriginLink = &task.OriginLink
	}
	row.VariantLabel = sku.VariantLabel
	row.Name = sku.Name
	row.PrimaryImage = sku.PrimaryImage
	row.ColorImage = sku.ColorImage
	if sku.Cost.Total != nil {
		total := strconv.FormatFloat(*sku.Cost.Total, 'f', -1, 64)
		row.Cost = &total
	}
	if price != "" {
		row.Price = &price
	}
	if old != "" {
		row.OldPrice = &old
	}
	row.Depth = sku.Dimensions.Depth
	row.Width = sku.Dimensions.Width
	row.Height = sku.Dimensions.Height
	row.Weight = sku.Dimensions.Weight
	return row
}

func reviewState(ok bool) string {
	if ok {
		return "approved"
	}
	return "pending"
}

func strPtr(v string) *string { return &v }

func pickPricing(ctx context.Context, tx store.Tx) (json.RawMessage, *int64) {
	rules, err := tx.ListPricingRules(ctx)
	if err != nil {
		return nil, nil
	}
	active := int64(configInt(ctx, tx, "active_pricing_rule_id", 0))
	var enabled []domain.PricingRule
	for _, rule := range rules {
		if rule.Enabled && (active == 0 || rule.ID == active) {
			enabled = append(enabled, rule)
		}
	}
	if active == 0 && len(enabled) != 1 {
		return nil, nil
	}
	if len(enabled) == 0 {
		return nil, nil
	}
	id := enabled[0].ID
	return enabled[0].ParametersJSON, &id
}

func (e *Engine) attrSnapshot(ctx context.Context, tx store.Tx, shopID, categoryID, typeID int64) ([]ozonapi.Attribute, map[int64]map[int64]bool, bool) {
	raw, _, expires, ok, err := tx.GetAttrCache(ctx, shopID, categoryID, typeID, "RU")
	if err != nil || !ok || expired(e.now(), expires) {
		return nil, nil, false
	}
	var attrs []ozonapi.Attribute
	if err := json.Unmarshal(raw, &attrs); err != nil {
		return nil, nil, false
	}
	index := map[int64]map[int64]bool{}
	for _, attr := range attrs {
		rows, err := tx.ListDict(ctx, domain.DictQuery{
			ShopID: shopID, CategoryID: categoryID, TypeID: typeID, AttributeID: attr.ID, Language: "RU", Limit: 100000,
		})
		if err != nil {
			continue
		}
		for _, row := range rows {
			if index[attr.ID] == nil {
				index[attr.ID] = map[int64]bool{}
			}
			index[attr.ID][row.ValueID] = true
		}
	}
	return attrs, index, true
}
