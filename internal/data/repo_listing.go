package data

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"entgo.io/ent/dialect/sql"

	"ozon-workbench/internal/biz"
	"ozon-workbench/internal/data/ent"
	"ozon-workbench/internal/data/ent/listingmedia"
	"ozon-workbench/internal/data/ent/listingproduct"
	"ozon-workbench/internal/data/ent/listingrevision"
	"ozon-workbench/internal/data/ent/listingsku"
	"ozon-workbench/internal/data/ent/predicate"
	"ozon-workbench/internal/data/ent/publishbatch"
	"ozon-workbench/internal/data/ent/publishitem"
	"ozon-workbench/internal/data/ent/publishrequest"
	"ozon-workbench/internal/data/ent/skuwarehousestock"
	"ozon-workbench/internal/data/ent/workbenchtask"
)

type listingRepo struct{ db *ent.Client }

func NewListingRepo(data *Data) biz.ListingRepo { return &listingRepo{db: data.Client} }

func (r *listingRepo) SaveProduct(ctx context.Context, p *biz.ListingProduct) (*biz.ListingProduct, error) {
	row, err := r.db.ListingProduct.Create().
		SetShopID(p.ShopID).
		SetTaskID(p.TaskID).
		SetMasterSku(p.MasterSKU).
		SetSupplierURL(cut(p.SupplierURL, 500)).
		SetDescriptionCategoryID(p.DescriptionCategoryID).
		SetTypeID(p.TypeID).
		SetTypeName(cut(p.TypeName, 255)).
		SetTypePath(orStrings(p.TypePath)).
		SetLanguage(defaultString(p.Language, "ZH_HANS")).
		SetAttributesJSON(rawText(p.Attributes, "[]")).
		SetCodexSessionID(cut(p.CodexSessionID, 128)).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	return productToBiz(row), nil
}

func (r *listingRepo) FindProduct(ctx context.Context, shopID, taskID int, master string) (*biz.ListingProduct, error) {
	row, err := r.db.ListingProduct.Query().Where(
		listingproduct.ShopIDEQ(shopID), listingproduct.TaskIDEQ(taskID), listingproduct.MasterSkuEQ(master),
	).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return productToBiz(row), nil
}

func (r *listingRepo) GetProduct(ctx context.Context, shopID, id int) (*biz.ListingProduct, error) {
	row, err := r.db.ListingProduct.Query().Where(listingproduct.IDEQ(id), listingproduct.ShopIDEQ(shopID)).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return productToBiz(row), nil
}

func (r *listingRepo) FindOffer(ctx context.Context, shopID int, offer string) (*biz.ListingSKU, error) {
	row, err := r.db.ListingSKU.Query().Where(listingsku.ShopIDEQ(shopID), listingsku.OfferIDEQ(offer)).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sku := skuToBiz(row)
	_ = r.fillProducts(ctx, []*biz.ListingSKU{sku})
	return sku, nil
}

func (r *listingRepo) CreateSKU(ctx context.Context, sku *biz.ListingSKU, rev *biz.ListingRevision, media []biz.ListingMedia) (*biz.ListingSKU, error) {
	tx, err := r.db.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	row, err := tx.ListingSKU.Create().
		SetShopID(sku.ShopID).SetProductID(sku.ProductID).SetTaskID(sku.TaskID).
		SetOfferID(sku.OfferID).SetMasterSku(sku.MasterSKU).SetSupplierVariantRef(cut(sku.SupplierVariantRef, 128)).
		SetVariantSpecs(cut(sku.VariantSpecs, 500)).SetCategoryLabel(cut(sku.CategoryLabel, 500)).
		SetCurrentVersion(orInt(sku.CurrentVersion, 1)).SetPublishStatus(defaultString(sku.PublishStatus, biz.PublishDraft)).
		SetPlatformStatus(defaultString(sku.PlatformStatus, biz.PlatformUnread)).SetPlatformReason(cut(sku.PlatformReason, 2000)).
		SetLastImportedVersion(sku.LastImportedVersion).SetOzonProductID(sku.OzonProductID).SetOzonSku(sku.OzonSKU).
		SetCurrencyCode(sku.CurrencyCode).SetPrice(sku.Price).SetTitleRu(cut(sku.TitleRU, 1000)).
		SetPrimaryImage(cut(sku.PrimaryImage, 2000)).SetPreviewPath(cut(sku.PreviewPath, 500)).
		SetLastError(cut(sku.LastError, 4000)).SetCodexAttempted(sku.CodexAttempted).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	if _, err := tx.ListingRevision.Create().
		SetListingSkuID(row.ID).SetShopID(sku.ShopID).SetVersion(orInt(rev.Version, 1)).
		SetSource(defaultString(rev.Source, "draft")).SetCodexItemJSON(rawText(rev.CodexItem, "{}")).
		SetItemJSON(rawText(rev.ItemJSON, "{}")).SetValidationJSON(issuesText(rev.Issues)).SetValid(rev.Valid).
		Save(ctx); err != nil {
		return nil, err
	}
	for _, item := range media {
		if _, err := tx.ListingMedia.Create().
			SetListingSkuID(row.ID).SetShopID(sku.ShopID).SetRole(defaultString(item.Role, "image")).
			SetLocalPath(cut(item.LocalPath, 500)).SetStorageKey(cut(item.StorageKey, 500)).
			SetPublicURL(cut(item.PublicURL, 2000)).SetPublicKey(cut(item.PublicKey, 2000)).
			SetStatus(defaultString(item.Status, "local")).SetErrorMessage(cut(item.Error, 2000)).SetAttempts(item.Attempts).
			Save(ctx); err != nil {
			return nil, err
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetSKU(ctx, sku.ShopID, row.ID)
}

func (r *listingRepo) GetSKU(ctx context.Context, shopID, id int) (*biz.ListingSKU, error) {
	row, err := r.db.ListingSKU.Query().Where(listingsku.IDEQ(id), listingsku.ShopIDEQ(shopID)).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, &biz.Error{Status: 404, Reason: "NOT_FOUND", Message: "SKU 不存在。"}
	}
	if err != nil {
		return nil, err
	}
	sku := skuToBiz(row)
	_ = r.fillProducts(ctx, []*biz.ListingSKU{sku})
	return sku, nil
}

func (r *listingRepo) ListSKUs(ctx context.Context, f biz.ListingFilter) ([]biz.ListingSKU, int, error) {
	preds := []predicate.ListingSKU{listingsku.ShopIDEQ(f.ShopID)}
	if f.Platform != "" {
		preds = append(preds, listingsku.PlatformStatusEQ(f.Platform))
	}
	if len(f.Publish) > 0 {
		preds = append(preds, listingsku.PublishStatusIn(f.Publish...))
	}
	switch f.Mode {
	case "master":
		if strings.TrimSpace(f.Query) != "" {
			preds = append(preds, listingsku.MasterSkuEQ(f.Query))
		}
	case "multi":
		if len(f.Offers) == 0 {
			return []biz.ListingSKU{}, 0, nil
		}
		preds = append(preds, listingsku.OfferIDIn(f.Offers...))
	default:
		if strings.TrimSpace(f.Query) != "" {
			preds = append(preds, listingsku.OfferIDContains(f.Query))
		}
	}
	total, err := r.db.ListingSKU.Query().Where(preds...).Count(ctx)
	if err != nil {
		return nil, 0, err
	}
	page, size := f.Page, f.PageSize
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	rows, err := r.db.ListingSKU.Query().Where(preds...).
		Order(listingsku.ByID(sql.OrderDesc())).
		Offset((page - 1) * size).Limit(size).All(ctx)
	if err != nil {
		return nil, 0, err
	}
	out := make([]biz.ListingSKU, 0, len(rows))
	ptrs := make([]*biz.ListingSKU, 0, len(rows))
	for _, row := range rows {
		sku := skuToBiz(row)
		out = append(out, *sku)
		ptrs = append(ptrs, &out[len(out)-1])
	}
	_ = r.fillProducts(ctx, ptrs)
	return out, total, nil
}

func (r *listingRepo) ListRevisions(ctx context.Context, shopID, skuID int) ([]biz.ListingRevision, error) {
	rows, err := r.db.ListingRevision.Query().Where(
		listingrevision.ShopIDEQ(shopID), listingrevision.ListingSkuIDEQ(skuID),
	).Order(listingrevision.ByVersion()).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]biz.ListingRevision, 0, len(rows))
	for _, row := range rows {
		out = append(out, revToBiz(row))
	}
	return out, nil
}

func (r *listingRepo) GetRevision(ctx context.Context, shopID, skuID, version int) (*biz.ListingRevision, error) {
	row, err := r.db.ListingRevision.Query().Where(
		listingrevision.ShopIDEQ(shopID), listingrevision.ListingSkuIDEQ(skuID), listingrevision.VersionEQ(version),
	).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	rev := revToBiz(row)
	return &rev, nil
}

func (r *listingRepo) AddRevision(ctx context.Context, sku *biz.ListingSKU, rev *biz.ListingRevision) (*biz.ListingSKU, error) {
	tx, err := r.db.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ListingRevision.Create().
		SetListingSkuID(sku.ID).SetShopID(sku.ShopID).SetVersion(rev.Version).
		SetSource(defaultString(rev.Source, "manual")).SetCodexItemJSON(rawText(rev.CodexItem, "{}")).
		SetItemJSON(rawText(rev.ItemJSON, "{}")).SetValidationJSON(issuesText(rev.Issues)).SetValid(rev.Valid).
		Save(ctx); err != nil {
		return nil, err
	}
	if _, err := tx.ListingSKU.UpdateOneID(sku.ID).
		SetCurrentVersion(sku.CurrentVersion).SetPublishStatus(sku.PublishStatus).
		SetCurrencyCode(sku.CurrencyCode).SetPrice(sku.Price).SetTitleRu(cut(sku.TitleRU, 1000)).
		SetPrimaryImage(cut(sku.PrimaryImage, 2000)).SetPreviewPath(cut(sku.PreviewPath, 500)).
		SetLastError(cut(sku.LastError, 4000)).SetCodexAttempted(sku.CodexAttempted).
		Save(ctx); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetSKU(ctx, sku.ShopID, sku.ID)
}

func (r *listingRepo) ListMedia(ctx context.Context, skuID int) ([]biz.ListingMedia, error) {
	rows, err := r.db.ListingMedia.Query().Where(listingmedia.ListingSkuIDEQ(skuID)).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]biz.ListingMedia, 0, len(rows))
	for _, row := range rows {
		out = append(out, mediaToBiz(row))
	}
	return out, nil
}

func (r *listingRepo) SaveMedia(ctx context.Context, row *biz.ListingMedia) (*biz.ListingMedia, error) {
	if row.ID == 0 {
		saved, err := r.db.ListingMedia.Create().
			SetListingSkuID(row.SKUID).SetShopID(row.ShopID).SetRole(defaultString(row.Role, "image")).
			SetLocalPath(cut(row.LocalPath, 500)).SetStorageKey(cut(row.StorageKey, 500)).
			SetPublicURL(cut(row.PublicURL, 2000)).SetPublicKey(cut(row.PublicKey, 2000)).
			SetStatus(row.Status).SetErrorMessage(cut(row.Error, 2000)).SetAttempts(row.Attempts).
			Save(ctx)
		if err != nil {
			return nil, err
		}
		got := mediaToBiz(saved)
		return &got, nil
	}
	saved, err := r.db.ListingMedia.UpdateOneID(row.ID).
		SetStorageKey(cut(row.StorageKey, 500)).SetPublicURL(cut(row.PublicURL, 2000)).SetPublicKey(cut(row.PublicKey, 2000)).
		SetStatus(row.Status).SetErrorMessage(cut(row.Error, 2000)).SetAttempts(row.Attempts).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	got := mediaToBiz(saved)
	return &got, nil
}

func (r *listingRepo) LookupBatch(ctx context.Context, shopID int, key string) (*biz.PublishBatch, error) {
	row, err := r.db.PublishBatch.Query().Where(publishbatch.ShopIDEQ(shopID), publishbatch.IdempotencyKeyEQ(key)).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return r.hydrateBatch(ctx, row)
}

func (r *listingRepo) CreateBatch(ctx context.Context, batch *biz.PublishBatch) (*biz.PublishBatch, error) {
	tx, err := r.db.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()
	row, err := tx.PublishBatch.Create().
		SetShopID(batch.ShopID).SetIdempotencyKey(batch.IdempotencyKey).
		SetSelectionHash(batch.SelectionHash).SetStatus(defaultString(batch.Status, "open")).
		Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			return nil, &biz.Error{Status: 409, Reason: "IDEMPOTENCY_CONFLICT", Message: "这个操作号已经用于另一组 SKU。请重新发起。"}
		}
		return nil, err
	}
	for i := range batch.Requests {
		req := &batch.Requests[i]
		saved, err := tx.PublishRequest.Create().
			SetBatchID(row.ID).SetShopID(batch.ShopID).SetChunkIndex(req.Chunk).
			SetRequestBodyJSON(rawText(req.Body, "{}")).SetTaskID(req.TaskID).
			SetStatus(defaultString(req.Status, "pending")).SetErrorMessage(cut(req.Error, 2000)).
			Save(ctx)
		if err != nil {
			return nil, err
		}
		req.ID = saved.ID
		req.BatchID = row.ID
		for j := range req.Items {
			item := &req.Items[j]
			n, err := tx.ListingSKU.Update().Where(
				listingsku.IDEQ(item.SKUID), listingsku.ShopIDEQ(batch.ShopID), listingsku.CurrentVersionEQ(item.Version),
			).SetPublishStatus(biz.PublishSubmitting).Save(ctx)
			if err != nil {
				return nil, err
			}
			if n != 1 {
				return nil, &biz.Error{Status: 409, Reason: "VERSION_CONFLICT", Message: "版本已变化，请刷新后再提交。"}
			}
			created, err := tx.PublishItem.Create().
				SetRequestID(saved.ID).SetBatchID(row.ID).SetListingSkuID(item.SKUID).SetShopID(batch.ShopID).
				SetVersion(item.Version).SetOfferID(item.OfferID).SetResultStatus(defaultString(item.Result, "pending")).
				Save(ctx)
			if err != nil {
				return nil, err
			}
			item.ID = created.ID
			item.RequestID = saved.ID
			item.BatchID = row.ID
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return r.GetBatch(ctx, batch.ShopID, row.ID)
}

func (r *listingRepo) GetBatch(ctx context.Context, shopID, id int) (*biz.PublishBatch, error) {
	row, err := r.db.PublishBatch.Query().Where(publishbatch.IDEQ(id), publishbatch.ShopIDEQ(shopID)).Only(ctx)
	if ent.IsNotFound(err) {
		return nil, &biz.Error{Status: 404, Reason: "NOT_FOUND", Message: "批次不存在。"}
	}
	if err != nil {
		return nil, err
	}
	return r.hydrateBatch(ctx, row)
}

func (r *listingRepo) SaveBatch(ctx context.Context, batch *biz.PublishBatch, patches []biz.SKUPatch) error {
	tx, err := r.db.Tx(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if batch != nil && batch.ID > 0 {
		if _, err := tx.PublishBatch.UpdateOneID(batch.ID).SetStatus(defaultString(batch.Status, "open")).Save(ctx); err != nil {
			return err
		}
		for _, req := range batch.Requests {
			upd := tx.PublishRequest.UpdateOneID(req.ID).SetTaskID(req.TaskID).SetStatus(req.Status).SetErrorMessage(cut(req.Error, 2000))
			if req.SentAt != nil {
				upd.SetSentAt(*req.SentAt)
			}
			if req.Finished != nil {
				upd.SetFinishedAt(*req.Finished)
			}
			if _, err := upd.Save(ctx); err != nil {
				return err
			}
			for _, item := range req.Items {
				if _, err := tx.PublishItem.UpdateOneID(item.ID).
					SetResultStatus(defaultString(item.Result, "pending")).
					SetOzonProductID(item.ProductID).SetOzonSku(item.OzonSKU).
					SetErrorCode(cut(item.ErrorCode, 128)).SetErrorDetail(cut(item.ErrorDetail, 4000)).
					Save(ctx); err != nil {
					return err
				}
			}
		}
	}
	for _, patch := range patches {
		row, err := tx.ListingSKU.Get(ctx, patch.ID)
		if ent.IsNotFound(err) {
			continue
		}
		if err != nil {
			return err
		}
		upd := tx.ListingSKU.UpdateOneID(patch.ID)
		touched := false
		if patch.PublishStatus != nil && (patch.ExpectVersion == 0 || row.CurrentVersion == patch.ExpectVersion) {
			upd.SetPublishStatus(*patch.PublishStatus)
			touched = true
			if patch.LastError != nil {
				upd.SetLastError(cut(*patch.LastError, 4000))
			}
		}
		if patch.LastImportedVersion != nil && row.LastImportedVersion < *patch.LastImportedVersion {
			upd.SetLastImportedVersion(*patch.LastImportedVersion)
			touched = true
		}
		if patch.PlatformStatus != nil {
			upd.SetPlatformStatus(*patch.PlatformStatus)
			touched = true
		}
		if patch.PlatformReason != nil {
			upd.SetPlatformReason(cut(*patch.PlatformReason, 2000))
			touched = true
		}
		if patch.OzonProductID != nil && *patch.OzonProductID > 0 {
			upd.SetOzonProductID(*patch.OzonProductID)
			touched = true
		}
		if patch.OzonSKU != nil && *patch.OzonSKU > 0 {
			upd.SetOzonSku(*patch.OzonSKU)
			touched = true
		}
		if patch.Checked {
			now := time.Now().UTC()
			upd.SetLastCheckedAt(now)
			touched = true
		}
		if touched {
			if _, err := upd.Save(ctx); err != nil {
				return err
			}
		}
	}
	return tx.Commit()
}

func (r *listingRepo) ListInflight(ctx context.Context) ([]*biz.PublishBatch, error) {
	rows, err := r.db.PublishBatch.Query().Where(publishbatch.StatusNEQ("done")).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*biz.PublishBatch, 0, len(rows))
	for _, row := range rows {
		batch, err := r.hydrateBatch(ctx, row)
		if err != nil {
			return nil, err
		}
		out = append(out, batch)
	}
	return out, nil
}

func (r *listingRepo) SaveStock(ctx context.Context, row *biz.WarehouseStock) (*biz.WarehouseStock, error) {
	existing, err := r.db.SKUWarehouseStock.Query().Where(
		skuwarehousestock.ListingSkuIDEQ(row.SKUID), skuwarehousestock.WarehouseIDEQ(row.WarehouseID),
	).Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return nil, err
	}
	if existing == nil {
		created, err := r.db.SKUWarehouseStock.Create().
			SetListingSkuID(row.SKUID).SetShopID(row.ShopID).SetWarehouseID(row.WarehouseID).
			SetTargetQty(row.TargetQty).SetPlatformQty(row.PlatformQty).
			SetSyncStatus(row.SyncStatus).SetSyncMessage(cut(row.SyncMessage, 2000)).
			SetNillableSyncedAt(row.SyncedAt).
			Save(ctx)
		if err != nil {
			return nil, err
		}
		got := stockToBiz(created)
		return &got, nil
	}
	upd := r.db.SKUWarehouseStock.UpdateOneID(existing.ID).
		SetTargetQty(row.TargetQty).SetPlatformQty(row.PlatformQty).
		SetSyncStatus(row.SyncStatus).SetSyncMessage(cut(row.SyncMessage, 2000))
	if row.SyncedAt != nil {
		upd.SetSyncedAt(*row.SyncedAt)
	}
	saved, err := upd.Save(ctx)
	if err != nil {
		return nil, err
	}
	got := stockToBiz(saved)
	return &got, nil
}

func (r *listingRepo) ListStocks(ctx context.Context, shopID, skuID int) ([]biz.WarehouseStock, error) {
	rows, err := r.db.SKUWarehouseStock.Query().Where(
		skuwarehousestock.ShopIDEQ(shopID), skuwarehousestock.ListingSkuIDEQ(skuID),
	).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]biz.WarehouseStock, 0, len(rows))
	for _, row := range rows {
		out = append(out, stockToBiz(row))
	}
	return out, nil
}

func (r *listingRepo) ListCompletedTasks(ctx context.Context) ([]*biz.Task, error) {
	rows, err := r.db.WorkbenchTask.Query().Where(workbenchtask.StatusEQ(biz.StatusCompleted)).All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]*biz.Task, 0, len(rows))
	for _, row := range rows {
		task, err := toBizTask(row)
		if err != nil {
			return nil, err
		}
		out = append(out, task)
	}
	return out, nil
}

func (r *listingRepo) hydrateBatch(ctx context.Context, row *ent.PublishBatch) (*biz.PublishBatch, error) {
	batch := &biz.PublishBatch{
		ID: row.ID, ShopID: row.ShopID, IdempotencyKey: row.IdempotencyKey, SelectionHash: row.SelectionHash,
		Status: row.Status, CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
	reqs, err := r.db.PublishRequest.Query().Where(publishrequest.BatchIDEQ(row.ID)).Order(publishrequest.ByChunkIndex()).All(ctx)
	if err != nil {
		return nil, err
	}
	items, err := r.db.PublishItem.Query().Where(publishitem.BatchIDEQ(row.ID)).All(ctx)
	if err != nil {
		return nil, err
	}
	for _, req := range reqs {
		view := biz.PublishRequest{
			ID: req.ID, BatchID: req.BatchID, ShopID: req.ShopID, Chunk: req.ChunkIndex,
			Body: json.RawMessage(req.RequestBodyJSON), TaskID: req.TaskID, Status: req.Status,
			Error: req.ErrorMessage, SentAt: req.SentAt, Finished: req.FinishedAt,
		}
		for _, item := range items {
			if item.RequestID != req.ID {
				continue
			}
			view.Items = append(view.Items, biz.PublishItem{
				ID: item.ID, RequestID: item.RequestID, BatchID: item.BatchID, SKUID: item.ListingSkuID,
				ShopID: item.ShopID, Version: item.Version, OfferID: item.OfferID, Result: item.ResultStatus,
				ProductID: item.OzonProductID, OzonSKU: item.OzonSku, ErrorCode: item.ErrorCode, ErrorDetail: item.ErrorDetail,
			})
		}
		batch.Requests = append(batch.Requests, view)
	}
	return batch, nil
}

func (r *listingRepo) fillProducts(ctx context.Context, skus []*biz.ListingSKU) error {
	if len(skus) == 0 {
		return nil
	}
	ids := make([]int, 0, len(skus))
	seen := map[int]struct{}{}
	for _, sku := range skus {
		if sku == nil || sku.ProductID == 0 {
			continue
		}
		if _, ok := seen[sku.ProductID]; ok {
			continue
		}
		seen[sku.ProductID] = struct{}{}
		ids = append(ids, sku.ProductID)
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := r.db.ListingProduct.Query().Where(listingproduct.IDIn(ids...)).All(ctx)
	if err != nil {
		return err
	}
	byID := map[int]*ent.ListingProduct{}
	for _, row := range rows {
		byID[row.ID] = row
	}
	for _, sku := range skus {
		row := byID[sku.ProductID]
		if row == nil {
			continue
		}
		sku.SupplierURL = row.SupplierURL
		sku.DescriptionCategory = row.DescriptionCategoryID
		sku.TypeID = row.TypeID
		sku.TypeName = row.TypeName
		sku.Attributes = json.RawMessage(row.AttributesJSON)
		sku.SessionID = row.CodexSessionID
	}
	return nil
}

func productToBiz(row *ent.ListingProduct) *biz.ListingProduct {
	path := row.TypePath
	if path == nil {
		path = []string{}
	}
	return &biz.ListingProduct{
		ID: row.ID, ShopID: row.ShopID, TaskID: row.TaskID, MasterSKU: row.MasterSku, SupplierURL: row.SupplierURL,
		DescriptionCategoryID: row.DescriptionCategoryID, TypeID: row.TypeID, TypeName: row.TypeName, TypePath: path,
		Language: row.Language, Attributes: json.RawMessage(row.AttributesJSON), CodexSessionID: row.CodexSessionID,
	}
}

func skuToBiz(row *ent.ListingSKU) *biz.ListingSKU {
	return &biz.ListingSKU{
		ID: row.ID, ShopID: row.ShopID, ProductID: row.ProductID, TaskID: row.TaskID, OfferID: row.OfferID,
		MasterSKU: row.MasterSku, SupplierVariantRef: row.SupplierVariantRef, VariantSpecs: row.VariantSpecs,
		CategoryLabel: row.CategoryLabel, CurrentVersion: row.CurrentVersion, PublishStatus: row.PublishStatus,
		PlatformStatus: row.PlatformStatus, PlatformReason: row.PlatformReason, LastImportedVersion: row.LastImportedVersion,
		OzonProductID: row.OzonProductID, OzonSKU: row.OzonSku, LastCheckedAt: row.LastCheckedAt,
		CurrencyCode: row.CurrencyCode, Price: row.Price, TitleRU: row.TitleRu, PrimaryImage: row.PrimaryImage,
		PreviewPath: row.PreviewPath, LastError: row.LastError, CodexAttempted: row.CodexAttempted,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

func revToBiz(row *ent.ListingRevision) biz.ListingRevision {
	var issues []string
	_ = json.Unmarshal([]byte(row.ValidationJSON), &issues)
	if issues == nil {
		issues = []string{}
	}
	return biz.ListingRevision{
		ID: row.ID, SKUID: row.ListingSkuID, ShopID: row.ShopID, Version: row.Version, Source: row.Source,
		CodexItem: json.RawMessage(row.CodexItemJSON), ItemJSON: json.RawMessage(row.ItemJSON),
		Issues: issues, Valid: row.Valid, CreatedAt: row.CreatedAt,
	}
}

func mediaToBiz(row *ent.ListingMedia) biz.ListingMedia {
	return biz.ListingMedia{
		ID: row.ID, SKUID: row.ListingSkuID, ShopID: row.ShopID, Role: row.Role, LocalPath: row.LocalPath,
		StorageKey: row.StorageKey, PublicURL: row.PublicURL, PublicKey: row.PublicKey, Status: row.Status,
		Error: row.ErrorMessage, Attempts: row.Attempts,
	}
}

func stockToBiz(row *ent.SKUWarehouseStock) biz.WarehouseStock {
	return biz.WarehouseStock{
		ID: row.ID, SKUID: row.ListingSkuID, ShopID: row.ShopID, WarehouseID: row.WarehouseID,
		TargetQty: row.TargetQty, PlatformQty: row.PlatformQty, SyncStatus: row.SyncStatus,
		SyncMessage: row.SyncMessage, SyncedAt: row.SyncedAt,
	}
}

func issuesText(issues []string) string {
	if issues == nil {
		issues = []string{}
	}
	raw, err := json.Marshal(issues)
	if err != nil {
		return "[]"
	}
	return string(raw)
}

func orInt(v, fallback int) int {
	if v == 0 {
		return fallback
	}
	return v
}
