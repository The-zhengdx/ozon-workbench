package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"ozon-workbench/internal/domain"
)

func (t *pgTx) CreateProduct(ctx context.Context, product domain.Product) (domain.Product, error) {
	err := t.tx.QueryRow(ctx, `
		INSERT INTO ozon_products(
			shop_id, generation_task_id, base_sku, offer_id, variant_label, origin_link,
			description_category_id, type_id, new_description_category_id, name, barcode,
			cost, cost_currency_code, cost_source_json, price, old_price, currency_code, vat, pricing_rule_id, pricing_snapshot,
			primary_image, images, color_image, pdf_list, depth, height, width, dimension_unit, weight, weight_unit,
			promotions, attributes, complex_attributes, ai_structured_json,
			api_item_json, api_json_revision, last_imported_revision, validation_status, validation_errors,
			text_review_status, image_review_status, publish_status, ozon_moderation_status, ozon_sale_status, ozon_product_id, ozon_sku, ozon_status_raw, last_error_json,
			last_submitted_at, last_imported_at, last_platform_synced_at, created_at, updated_at)
		VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12::numeric,$13,$14,$15::numeric,$16::numeric,$17,$18,$19,$20,
			$21,$22,$23,$24,$25,$26,$27,$28,$29,$30,$31,$32,$33,$34,$35,$36,$37,$38,$39,$40,$41,$42,$43,$44,$45,$46,$47,$48,$49,$50,$51,$52,$53)
		RETURNING id`,
		product.ShopID, product.GenerationTaskID, product.BaseSKU, product.OfferID, product.VariantLabel, product.OriginLink,
		product.DescriptionCategoryID, product.TypeID, product.NewDescriptionCategoryID, product.Name, product.Barcode,
		product.Cost, product.CostCurrencyCode, jobj(product.CostSourceJSON), product.Price, product.OldPrice, product.CurrencyCode, product.VAT, product.PricingRuleID, jobj(product.PricingSnapshot),
		product.PrimaryImage, jarr(product.Images), product.ColorImage, jarr(product.PDFList), product.Depth, product.Height, product.Width, product.DimensionUnit, product.Weight, product.WeightUnit,
		jarr(product.Promotions), jarr(product.Attributes), jarr(product.ComplexAttributes), nraw(product.AIStructuredJSON),
		nraw(product.APIItemJSON), product.APIJSONRevision, product.LastImportedRevision, product.ValidationStatus, jarr(product.ValidationErrors),
		product.TextReviewStatus, product.ImageReviewStatus, product.PublishStatus, product.OzonModerationStatus, product.OzonSaleStatus, product.OzonProductID, product.OzonSKU, jobj(product.OzonStatusRaw), jobj(product.LastErrorJSON),
		product.LastSubmittedAt, product.LastImportedAt, product.LastPlatformSyncedAt, product.CreatedAt, product.UpdatedAt,
	).Scan(&product.ID)
	if isUnique(err) {
		return domain.Product{}, ErrConflict
	}
	return product, err
}

func (t *pgTx) UpdateProduct(ctx context.Context, product domain.Product) error {
	tag, err := t.tx.Exec(ctx, `
		UPDATE ozon_products SET
			shop_id=$2, generation_task_id=$3, base_sku=$4, offer_id=$5, variant_label=$6, origin_link=$7,
			description_category_id=$8, type_id=$9, new_description_category_id=$10, name=$11, barcode=$12,
			cost=$13::numeric, cost_currency_code=$14, cost_source_json=$15, price=$16::numeric, old_price=$17::numeric, currency_code=$18, vat=$19, pricing_rule_id=$20, pricing_snapshot=$21,
			primary_image=$22, images=$23, color_image=$24, pdf_list=$25, depth=$26, height=$27, width=$28, dimension_unit=$29, weight=$30, weight_unit=$31,
			promotions=$32, attributes=$33, complex_attributes=$34, ai_structured_json=$35,
			api_item_json=$36, api_json_revision=$37, last_imported_revision=$38, validation_status=$39, validation_errors=$40,
			text_review_status=$41, image_review_status=$42, publish_status=$43, ozon_moderation_status=$44, ozon_sale_status=$45, ozon_product_id=$46, ozon_sku=$47, ozon_status_raw=$48, last_error_json=$49,
			last_submitted_at=$50, last_imported_at=$51, last_platform_synced_at=$52, updated_at=$53
		WHERE id=$1`,
		product.ID, product.ShopID, product.GenerationTaskID, product.BaseSKU, product.OfferID, product.VariantLabel, product.OriginLink,
		product.DescriptionCategoryID, product.TypeID, product.NewDescriptionCategoryID, product.Name, product.Barcode,
		product.Cost, product.CostCurrencyCode, jobj(product.CostSourceJSON), product.Price, product.OldPrice, product.CurrencyCode, product.VAT, product.PricingRuleID, jobj(product.PricingSnapshot),
		product.PrimaryImage, jarr(product.Images), product.ColorImage, jarr(product.PDFList), product.Depth, product.Height, product.Width, product.DimensionUnit, product.Weight, product.WeightUnit,
		jarr(product.Promotions), jarr(product.Attributes), jarr(product.ComplexAttributes), nraw(product.AIStructuredJSON),
		nraw(product.APIItemJSON), product.APIJSONRevision, product.LastImportedRevision, product.ValidationStatus, jarr(product.ValidationErrors),
		product.TextReviewStatus, product.ImageReviewStatus, product.PublishStatus, product.OzonModerationStatus, product.OzonSaleStatus, product.OzonProductID, product.OzonSKU, jobj(product.OzonStatusRaw), jobj(product.LastErrorJSON),
		product.LastSubmittedAt, product.LastImportedAt, product.LastPlatformSyncedAt, product.UpdatedAt)
	if isUnique(err) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

const productSelect = `SELECT id, shop_id, generation_task_id, base_sku, offer_id, variant_label, origin_link,
	description_category_id, type_id, new_description_category_id, name, barcode,
	cost::text, cost_currency_code, cost_source_json, price::text, old_price::text, currency_code, vat, pricing_rule_id, pricing_snapshot,
	primary_image, images, color_image, pdf_list, depth, height, width, dimension_unit, weight, weight_unit,
	promotions, attributes, complex_attributes, ai_structured_json,
	api_item_json, api_json_revision, last_imported_revision, validation_status, validation_errors,
	text_review_status, image_review_status, publish_status, ozon_moderation_status, ozon_sale_status, ozon_product_id, ozon_sku, ozon_status_raw, last_error_json,
	last_submitted_at, last_imported_at, last_platform_synced_at, created_at, updated_at FROM ozon_products`

func scanProduct(row pgx.Row) (domain.Product, error) {
	var p domain.Product
	var cost, price, oldPrice *string
	var costSrc, priceSnap, images, pdf, promo, attrs, complexA, ai, api, valErr, statusRaw, lastErr []byte
	err := row.Scan(
		&p.ID, &p.ShopID, &p.GenerationTaskID, &p.BaseSKU, &p.OfferID, &p.VariantLabel, &p.OriginLink,
		&p.DescriptionCategoryID, &p.TypeID, &p.NewDescriptionCategoryID, &p.Name, &p.Barcode,
		&cost, &p.CostCurrencyCode, &costSrc, &price, &oldPrice, &p.CurrencyCode, &p.VAT, &p.PricingRuleID, &priceSnap,
		&p.PrimaryImage, &images, &p.ColorImage, &pdf, &p.Depth, &p.Height, &p.Width, &p.DimensionUnit, &p.Weight, &p.WeightUnit,
		&promo, &attrs, &complexA, &ai, &api, &p.APIJSONRevision, &p.LastImportedRevision, &p.ValidationStatus, &valErr,
		&p.TextReviewStatus, &p.ImageReviewStatus, &p.PublishStatus, &p.OzonModerationStatus, &p.OzonSaleStatus, &p.OzonProductID, &p.OzonSKU, &statusRaw, &lastErr,
		&p.LastSubmittedAt, &p.LastImportedAt, &p.LastPlatformSyncedAt, &p.CreatedAt, &p.UpdatedAt,
	)
	p.Cost, p.Price, p.OldPrice = cost, price, oldPrice
	p.CostSourceJSON = costSrc
	p.PricingSnapshot = priceSnap
	p.Images = images
	p.PDFList = pdf
	p.Promotions = promo
	p.Attributes = attrs
	p.ComplexAttributes = complexA
	p.AIStructuredJSON = ai
	p.APIItemJSON = api
	p.ValidationErrors = valErr
	p.OzonStatusRaw = statusRaw
	p.LastErrorJSON = lastErr
	return p, err
}

func (t *pgTx) GetProduct(ctx context.Context, id int64) (domain.Product, error) {
	q := productSelect + ` WHERE id=$1`
	if wantsLock(ctx) {
		q += ` FOR UPDATE`
	}
	rows, err := t.tx.Query(ctx, q, id)
	if err != nil {
		return domain.Product{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return domain.Product{}, ErrNotFound
	}
	return scanProduct(rows)
}

func (t *pgTx) FindProductByOffer(ctx context.Context, shopID int64, offerID string) (domain.Product, bool, error) {
	rows, err := t.tx.Query(ctx, productSelect+` WHERE shop_id=$1 AND offer_id=$2`, shopID, offerID)
	if err != nil {
		return domain.Product{}, false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return domain.Product{}, false, rows.Err()
	}
	p, err := scanProduct(rows)
	return p, err == nil, err
}

func (t *pgTx) ListProducts(ctx context.Context, f domain.ProductFilter) ([]domain.Product, error) {
	limit := f.Limit
	if limit <= 0 {
		limit = 100000
	}
	rows, err := t.tx.Query(ctx, productSelect+`
		WHERE ($1=0 OR shop_id=$1)
		  AND ($2='' OR lower(offer_id) LIKE '%'||lower($2)||'%')
		  AND ($3='' OR lower(base_sku) LIKE '%'||lower($3)||'%')
		  AND ($4='' OR publish_status=$4)
		  AND ($5=0 OR description_category_id=$5)
		  AND ($6=0 OR type_id=$6)
		  AND ($7='' OR validation_status=$7)
		  AND ($8='' OR text_review_status=$8)
		ORDER BY updated_at DESC, id DESC OFFSET $9 LIMIT $10`,
		f.ShopID, f.OfferID, f.BaseSKU, f.PublishStatus, f.CategoryID, f.TypeID, f.ValidationStatus, f.TextReviewStatus, f.Offset, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Product, 0)
	for rows.Next() {
		p, err := scanProduct(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (t *pgTx) GetDelivery(ctx context.Context, taskID int64, skuRef string) (domain.Delivery, bool, error) {
	var row domain.Delivery
	err := t.tx.QueryRow(ctx, `SELECT task_id, sku_ref, product_id, delivered_at FROM ozon_processing_deliveries WHERE task_id=$1 AND sku_ref=$2`, taskID, skuRef).
		Scan(&row.TaskID, &row.SKURef, &row.ProductID, &row.DeliveredAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Delivery{}, false, nil
	}
	return row, err == nil, err
}

func (t *pgTx) CreateDelivery(ctx context.Context, row domain.Delivery) error {
	existing, ok, err := t.GetDelivery(ctx, row.TaskID, row.SKURef)
	if err != nil {
		return err
	}
	if ok {
		if existing.ProductID == row.ProductID {
			return nil
		}
		return ErrConflict
	}
	_, err = t.tx.Exec(ctx, `INSERT INTO ozon_processing_deliveries(task_id, sku_ref, product_id, delivered_at) VALUES ($1,$2,$3,$4)`,
		row.TaskID, row.SKURef, row.ProductID, row.DeliveredAt)
	if isUnique(err) {
		return ErrConflict
	}
	return err
}

func (t *pgTx) ListDeliveries(ctx context.Context, taskID int64) ([]domain.Delivery, error) {
	rows, err := t.tx.Query(ctx, `SELECT task_id, sku_ref, product_id, delivered_at FROM ozon_processing_deliveries WHERE task_id=$1 ORDER BY sku_ref`, taskID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.Delivery, 0)
	for rows.Next() {
		var row domain.Delivery
		if err := rows.Scan(&row.TaskID, &row.SKURef, &row.ProductID, &row.DeliveredAt); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (t *pgTx) CreatePricingRule(ctx context.Context, rule domain.PricingRule) (domain.PricingRule, error) {
	err := t.tx.QueryRow(ctx, `INSERT INTO ozon_pricing_rules(name, enabled, parameters_json, created_at, updated_at) VALUES ($1,$2,$3,$4,$5) RETURNING id`,
		rule.Name, rule.Enabled, jobj(rule.ParametersJSON), rule.CreatedAt, rule.UpdatedAt).Scan(&rule.ID)
	return rule, err
}

func (t *pgTx) UpdatePricingRule(ctx context.Context, rule domain.PricingRule) error {
	tag, err := t.tx.Exec(ctx, `UPDATE ozon_pricing_rules SET name=$2, enabled=$3, parameters_json=$4, updated_at=$5 WHERE id=$1`,
		rule.ID, rule.Name, rule.Enabled, jobj(rule.ParametersJSON), rule.UpdatedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (t *pgTx) ListPricingRules(ctx context.Context) ([]domain.PricingRule, error) {
	rows, err := t.tx.Query(ctx, `SELECT id, name, enabled, parameters_json, created_at, updated_at FROM ozon_pricing_rules ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.PricingRule, 0)
	for rows.Next() {
		rule, err := scanPrice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, rule)
	}
	return out, rows.Err()
}

func (t *pgTx) GetPricingRule(ctx context.Context, id int64) (domain.PricingRule, error) {
	rows, err := t.tx.Query(ctx, `SELECT id, name, enabled, parameters_json, created_at, updated_at FROM ozon_pricing_rules WHERE id=$1`, id)
	if err != nil {
		return domain.PricingRule{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return domain.PricingRule{}, ErrNotFound
	}
	return scanPrice(rows)
}

func scanPrice(row pgx.Row) (domain.PricingRule, error) {
	var rule domain.PricingRule
	var raw []byte
	err := row.Scan(&rule.ID, &rule.Name, &rule.Enabled, &raw, &rule.CreatedAt, &rule.UpdatedAt)
	rule.ParametersJSON = raw
	return rule, err
}

func (t *pgTx) CreateBatch(ctx context.Context, batch domain.ImportBatch) (domain.ImportBatch, error) {
	err := t.tx.QueryRow(ctx, `
		INSERT INTO ozon_import_batches(shop_id, ozon_task_id, status, request_json, response_json, last_error, created_at, submitted_at, finished_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9) RETURNING id`,
		batch.ShopID, batch.OzonTaskID, batch.Status, jobj(batch.RequestJSON), nraw(batch.ResponseJSON), nraw(batch.LastError), batch.CreatedAt, batch.SubmittedAt, batch.FinishedAt,
	).Scan(&batch.ID)
	if isUnique(err) {
		return domain.ImportBatch{}, ErrConflict
	}
	return batch, err
}

func (t *pgTx) UpdateBatch(ctx context.Context, batch domain.ImportBatch) error {
	tag, err := t.tx.Exec(ctx, `
		UPDATE ozon_import_batches SET shop_id=$2, ozon_task_id=$3, status=$4, request_json=$5, response_json=$6, last_error=$7, submitted_at=$8, finished_at=$9
		WHERE id=$1`,
		batch.ID, batch.ShopID, batch.OzonTaskID, batch.Status, jobj(batch.RequestJSON), nraw(batch.ResponseJSON), nraw(batch.LastError), batch.SubmittedAt, batch.FinishedAt)
	if isUnique(err) {
		return ErrConflict
	}
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func scanBatch(row pgx.Row) (domain.ImportBatch, error) {
	var batch domain.ImportBatch
	var req, resp, last []byte
	err := row.Scan(&batch.ID, &batch.ShopID, &batch.OzonTaskID, &batch.Status, &req, &resp, &last, &batch.CreatedAt, &batch.SubmittedAt, &batch.FinishedAt)
	batch.RequestJSON = req
	batch.ResponseJSON = resp
	batch.LastError = last
	return batch, err
}

const batchSelect = `SELECT id, shop_id, ozon_task_id, status, request_json, response_json, last_error, created_at, submitted_at, finished_at FROM ozon_import_batches`

func (t *pgTx) GetBatch(ctx context.Context, id int64) (domain.ImportBatch, error) {
	rows, err := t.tx.Query(ctx, batchSelect+` WHERE id=$1`, id)
	if err != nil {
		return domain.ImportBatch{}, err
	}
	defer rows.Close()
	if !rows.Next() {
		return domain.ImportBatch{}, ErrNotFound
	}
	return scanBatch(rows)
}

func (t *pgTx) ListBatches(ctx context.Context, shopID int64) ([]domain.ImportBatch, error) {
	rows, err := t.tx.Query(ctx, batchSelect+` WHERE ($1=0 OR shop_id=$1) ORDER BY id DESC`, shopID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.ImportBatch, 0)
	for rows.Next() {
		batch, err := scanBatch(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, batch)
	}
	return out, rows.Err()
}

func (t *pgTx) CreateBatchItem(ctx context.Context, item domain.ImportBatchItem) (domain.ImportBatchItem, error) {
	err := t.tx.QueryRow(ctx, `
		INSERT INTO ozon_import_batch_items(batch_id, product_id, offer_id, submitted_revision, request_item_json, status, ozon_product_id, error_json, created_at, updated_at)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id`,
		item.BatchID, item.ProductID, item.OfferID, item.SubmittedRevision, jobj(item.RequestItemJSON), item.Status, item.OzonProductID, nraw(item.ErrorJSON), item.CreatedAt, item.UpdatedAt,
	).Scan(&item.ID)
	if isUnique(err) {
		return domain.ImportBatchItem{}, ErrConflict
	}
	return item, err
}

func (t *pgTx) UpdateBatchItem(ctx context.Context, item domain.ImportBatchItem) error {
	tag, err := t.tx.Exec(ctx, `
		UPDATE ozon_import_batch_items SET batch_id=$2, product_id=$3, offer_id=$4, submitted_revision=$5, request_item_json=$6, status=$7, ozon_product_id=$8, error_json=$9, updated_at=$10
		WHERE id=$1`,
		item.ID, item.BatchID, item.ProductID, item.OfferID, item.SubmittedRevision, jobj(item.RequestItemJSON), item.Status, item.OzonProductID, nraw(item.ErrorJSON), item.UpdatedAt)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (t *pgTx) ListBatchItems(ctx context.Context, batchID int64) ([]domain.ImportBatchItem, error) {
	rows, err := t.tx.Query(ctx, `
		SELECT id, batch_id, product_id, offer_id, submitted_revision, request_item_json, status, ozon_product_id, error_json, created_at, updated_at
		FROM ozon_import_batch_items WHERE batch_id=$1 ORDER BY id`, batchID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]domain.ImportBatchItem, 0)
	for rows.Next() {
		var item domain.ImportBatchItem
		var req, errJSON []byte
		if err := rows.Scan(&item.ID, &item.BatchID, &item.ProductID, &item.OfferID, &item.SubmittedRevision, &req, &item.Status, &item.OzonProductID, &errJSON, &item.CreatedAt, &item.UpdatedAt); err != nil {
			return nil, err
		}
		item.RequestItemJSON = req
		item.ErrorJSON = errJSON
		out = append(out, item)
	}
	return out, rows.Err()
}

func (t *pgTx) FindBatchByOzonTask(ctx context.Context, shopID, ozonTaskID int64) (domain.ImportBatch, bool, error) {
	rows, err := t.tx.Query(ctx, batchSelect+` WHERE shop_id=$1 AND ozon_task_id=$2`, shopID, ozonTaskID)
	if err != nil {
		return domain.ImportBatch{}, false, err
	}
	defer rows.Close()
	if !rows.Next() {
		return domain.ImportBatch{}, false, rows.Err()
	}
	batch, err := scanBatch(rows)
	return batch, err == nil, err
}

func (t *pgTx) GetIdem(ctx context.Context, scope, key string) (json.RawMessage, bool, error) {
	var raw []byte
	err := t.tx.QueryRow(ctx, `SELECT response_json FROM ozon_idempotency_keys WHERE scope=$1 AND idem_key=$2`, scope, key).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	return raw, err == nil, err
}

func (t *pgTx) PutIdem(ctx context.Context, scope, key string, resp json.RawMessage, now time.Time) error {
	_, err := t.tx.Exec(ctx, `
		INSERT INTO ozon_idempotency_keys(scope, idem_key, response_json, created_at) VALUES ($1,$2,$3,$4)
		ON CONFLICT (scope, idem_key) DO NOTHING`, scope, key, []byte(resp), now)
	return err
}
