package biz

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

// Publish previews or submits the explicitly selected versions.
func (uc *ListingUsecase) Publish(ctx context.Context, in PublishInput) (*PublishPlan, error) {
	if _, err := uc.shops.Get(ctx, in.ShopID); err != nil {
		return nil, err
	}
	ready, blocked, loaded, items, err := uc.preflight(ctx, in)
	if err != nil {
		return nil, err
	}
	bodies, err := bodiesOf(ready, items)
	if err != nil {
		return nil, err
	}
	plan := &PublishPlan{
		Ready: ready, Blocked: blocked, Chunks: len(bodies),
		Bodies: bodies, NeedConfirm: len(blocked) > 0 && len(ready) > 0,
	}
	if !in.Execute {
		return plan, nil
	}
	if strings.TrimSpace(in.IdempotencyKey) == "" || utf8.RuneCountInString(in.IdempotencyKey) > 80 {
		return nil, bad("IDEMPOTENCY_KEY", "请提供本次操作的唯一键。", nil)
	}
	selection := SelectionHash(inputPairs(in.Items))
	if existing, err := uc.repo.LookupBatch(ctx, in.ShopID, in.IdempotencyKey); err != nil {
		return nil, err
	} else if existing != nil {
		if existing.SelectionHash != selection {
			return nil, &Error{Status: 409, Reason: "IDEMPOTENCY_CONFLICT", Message: "这个操作号已经用于另一组 SKU。请重新发起。"}
		}
		view := existing.View()
		plan.Batch = &view
		plan.NeedConfirm = false
		uc.schedule(func() { uc.dispatch(context.Background(), existing.ShopID, existing.ID) })
		return plan, nil
	}
	chosen := ready
	if len(blocked) > 0 && !in.OnlyValid {
		return plan, nil
	}
	if len(chosen) == 0 {
		return nil, bad("NOTHING_TO_SEND", "没有可以通过校验的 SKU。", nil)
	}
	batch, err := uc.createBatch(ctx, in.ShopID, in.IdempotencyKey, selection, chosen, items)
	if err != nil {
		return nil, err
	}
	uc.rememberCurrency(ctx, in.ShopID, chosen, loaded)
	view := batch.View()
	plan.Batch = &view
	plan.NeedConfirm = false
	plan.Blocked = blocked
	uc.dispatch(ctx, batch.ShopID, batch.ID)
	fresh, err := uc.repo.GetBatch(ctx, batch.ShopID, batch.ID)
	if err != nil {
		return plan, nil
	}
	again := fresh.View()
	plan.Batch = &again
	return plan, nil
}

// GetBatch returns one batch for the shop that created it.
func (uc *ListingUsecase) GetBatch(ctx context.Context, shopID, id int) (*BatchView, error) {
	batch, err := uc.repo.GetBatch(ctx, shopID, id)
	if err != nil {
		return nil, err
	}
	view := batch.View()
	return &view, nil
}

func (uc *ListingUsecase) preflight(ctx context.Context, in PublishInput) ([]PublishCheck, []PublishCheck, map[int]*ListingSKU, map[int]json.RawMessage, error) {
	if len(in.Items) == 0 {
		return nil, nil, nil, nil, bad("EMPTY_SELECTION", "请选择要上架的 SKU。", nil)
	}
	if len(in.Items) > 500 {
		return nil, nil, nil, nil, bad("TOO_MANY", "一次最多预检 500 条。", nil)
	}
	seenID := map[int]int{}
	seenOffer := map[string]int{}
	loaded := map[int]*ListingSKU{}
	items := map[int]json.RawMessage{}
	var ready, blocked []PublishCheck
	for _, pick := range in.Items {
		if prev, ok := seenID[pick.ID]; ok {
			if prev != pick.Version {
				blocked = append(blocked, PublishCheck{ID: pick.ID, Version: pick.Version, Errors: []string{"同一 SKU 不能同时提交两个版本"}})
			}
			continue
		}
		seenID[pick.ID] = pick.Version
		sku, err := uc.repo.GetSKU(ctx, in.ShopID, pick.ID)
		if err != nil {
			blocked = append(blocked, PublishCheck{ID: pick.ID, Version: pick.Version, Errors: []string{"SKU 不存在或不属于当前店铺"}})
			continue
		}
		loaded[sku.ID] = sku
		check := PublishCheck{ID: sku.ID, OfferID: sku.OfferID, Version: pick.Version, OK: true}
		if sku.PublishStatus == PublishSubmitting {
			check.OK = false
			check.Errors = append(check.Errors, "正在提交，请等待结果")
		}
		if pick.Version != sku.CurrentVersion {
			check.OK = false
			check.Errors = append(check.Errors, "版本已变化，请刷新")
		}
		if !Eligible(sku.PublishStatus, sku.CurrentVersion, sku.LastImportedVersion) && sku.PublishStatus != PublishSubmitting {
			check.OK = false
			check.Errors = append(check.Errors, "当前状态不能上架")
		}
		if other, ok := seenOffer[sku.OfferID]; ok && other != sku.ID {
			check.OK = false
			check.Errors = append(check.Errors, "选择里有重复的卖家 SKU")
		}
		seenOffer[sku.OfferID] = sku.ID
		rev, err := uc.repo.GetRevision(ctx, in.ShopID, sku.ID, pick.Version)
		if err != nil || rev == nil {
			check.OK = false
			check.Errors = append(check.Errors, "找不到这个版本")
		} else {
			expect, err := uc.expectSKU(ctx, sku)
			if err != nil {
				return nil, nil, nil, nil, err
			}
			items[sku.ID] = rev.ItemJSON
			issues := ValidateItem(rev.ItemJSON, expect)
			if len(issues) > 0 {
				check.OK = false
				check.Errors = append(check.Errors, issues...)
			}
			sku.PrimaryImage = asString(mustMap(rev.ItemJSON)["primary_image"])
		}
		if check.OK {
			ready = append(ready, check)
		} else {
			blocked = append(blocked, check)
		}
	}
	return ready, blocked, loaded, items, nil
}

func (uc *ListingUsecase) createBatch(ctx context.Context, shopID int, key, selection string, ready []PublishCheck, items map[int]json.RawMessage) (*PublishBatch, error) {
	itemsJSON := make([]json.RawMessage, 0, len(ready))
	picks := make([]PublishItem, 0, len(ready))
	for _, row := range ready {
		raw := items[row.ID]
		if len(raw) == 0 {
			return nil, bad("VERSION_MISSING", "找不到要提交的版本。", nil)
		}
		itemsJSON = append(itemsJSON, raw)
		picks = append(picks, PublishItem{
			SKUID: row.ID, ShopID: shopID, Version: row.Version, OfferID: row.OfferID, Result: string(RemotePending),
		})
	}
	chunks := ChunkRaw(itemsJSON, MaxImportItems)
	batch := &PublishBatch{
		ShopID: shopID, IdempotencyKey: key, SelectionHash: selection, Status: "open",
	}
	offset := 0
	for i, chunk := range chunks {
		body, err := BuildImportBody(chunk)
		if err != nil {
			return nil, err
		}
		req := PublishRequest{ShopID: shopID, Chunk: i, Body: body, Status: "pending"}
		req.Items = append([]PublishItem(nil), picks[offset:offset+len(chunk)]...)
		offset += len(chunk)
		batch.Requests = append(batch.Requests, req)
	}
	return uc.repo.CreateBatch(ctx, batch)
}

func (uc *ListingUsecase) dispatch(ctx context.Context, shopID, batchID int) {
	unlock := uc.lock("batch:" + itoa(batchID))
	defer unlock()
	batch, err := uc.repo.GetBatch(ctx, shopID, batchID)
	if err != nil {
		uc.log.Errorf("batch %d: %v", batchID, err)
		return
	}
	if uc.gateway == nil {
		uc.log.Errorf("batch %d: ozon gateway is not configured", batchID)
		return
	}
	cred, credErr := uc.shops.Credentials(ctx, shopID)
	patches := []SKUPatch{}
	for i := range batch.Requests {
		req := &batch.Requests[i]
		if req.TaskID != "" || req.Status == "failed" || req.Status == "unknown" || req.Status == "done" {
			continue
		}
		if credErr != nil {
			req.Status = "failed"
			req.Error = credErr.Error()
			patches = append(patches, failPatches(req.Items, credErr.Error())...)
			continue
		}
		now := time.Now().UTC()
		req.SentAt = &now
		taskID, err := uc.gateway.ImportProducts(ctx, cred, req.Body)
		if err != nil {
			var unknown *AcceptUnknownError
			if errors.As(err, &unknown) {
				req.Status = "unknown"
				req.Error = unknown.Error()
				patches = append(patches, unknownPatches(req.Items, unknown.Error())...)
				continue
			}
			req.Status = "failed"
			req.Error = err.Error()
			patches = append(patches, failPatches(req.Items, err.Error())...)
			continue
		}
		req.TaskID = taskID
		req.Status = "accepted"
	}
	if err := uc.repo.SaveBatch(ctx, batch, patches); err != nil {
		uc.log.Errorf("batch %d save: %v", batchID, err)
		return
	}
	uc.poll(ctx, shopID, batchID)
}

func (uc *ListingUsecase) poll(ctx context.Context, shopID, batchID int) {
	batch, err := uc.repo.GetBatch(ctx, shopID, batchID)
	if err != nil {
		return
	}
	cred, err := uc.shops.Credentials(ctx, shopID)
	if err != nil {
		return
	}
	pending := false
	var patches []SKUPatch
	var imported []string
	for i := range batch.Requests {
		req := &batch.Requests[i]
		if req.TaskID == "" {
			if req.Status == "pending" {
				pending = true
			}
			continue
		}
		offers, err := uc.gateway.ImportInfo(ctx, cred, req.TaskID)
		if err != nil {
			pending = true
			continue
		}
		byOffer := map[string]ImportOffer{}
		for _, offer := range offers {
			byOffer[offer.OfferID] = offer
		}
		reqDone := true
		for j := range req.Items {
			item := &req.Items[j]
			offer, ok := byOffer[item.OfferID]
			if !ok {
				reqDone = false
				pending = true
				continue
			}
			kind := RemoteKind(strings.ToLower(offer.Status))
			switch kind {
			case RemoteImported, RemoteFailed, RemoteUnknown:
			case "skipped":
				kind = RemoteFailed
			default:
				kind = RemotePending
			}
			if kind == RemotePending {
				reqDone = false
				pending = true
				item.Result = string(RemotePending)
				continue
			}
			code, detail := FormatImportErrors(offer.Errors)
			item.Result = string(kind)
			item.ProductID = offer.ProductID
			item.ErrorCode = code
			item.ErrorDetail = detail
			patch := SKUPatch{ID: item.SKUID, ExpectVersion: item.Version}
			if next, apply := ApplyRemoteResult(item.Version, item.Version, PublishSubmitting, kind); apply {
				status := next
				patch.PublishStatus = &status
			}
			if kind == RemoteImported {
				version := item.Version
				patch.LastImportedVersion = &version
				pid := offer.ProductID
				patch.OzonProductID = &pid
				patch.SetChecked()
				imported = append(imported, item.OfferID)
				empty := ""
				patch.LastError = &empty
			} else {
				msg := detail
				if msg == "" {
					msg = code
				}
				patch.LastError = &msg
			}
			patches = append(patches, patch)
		}
		if reqDone {
			now := time.Now().UTC()
			req.Finished = &now
			req.Status = "done"
		}
	}
	if cards, err := uc.gateway.ProductInfo(ctx, cred, imported); err == nil {
		patches = applyCards(batch, patches, cards)
	}
	if !pending && batchSettled(batch) {
		batch.Status = "done"
	}
	if err := uc.repo.SaveBatch(ctx, batch, patches); err != nil {
		uc.log.Errorf("poll %d: %v", batchID, err)
	}
}

func (p *SKUPatch) SetChecked() { p.Checked = true }

func applyCards(batch *PublishBatch, patches []SKUPatch, cards []PlatformCard) []SKUPatch {
	byOffer := map[string]int{}
	for i := range patches {
		for _, req := range batch.Requests {
			for _, item := range req.Items {
				if item.SKUID == patches[i].ID {
					byOffer[item.OfferID] = i
				}
			}
		}
	}
	for _, card := range cards {
		idx, ok := byOffer[card.OfferID]
		if !ok {
			continue
		}
		status, reason := MapPlatform(card.Status, card.Moderate, card.Failed, card.Name, card.Description, card.Tooltip, card.Archived)
		patches[idx].PlatformStatus = &status
		patches[idx].PlatformReason = &reason
		patches[idx].Checked = true
		if card.ProductID > 0 && patches[idx].OzonProductID == nil {
			pid := card.ProductID
			patches[idx].OzonProductID = &pid
		}
		if card.SKU > 0 {
			sku := card.SKU
			patches[idx].OzonSKU = &sku
		}
	}
	return patches
}

func failPatches(items []PublishItem, message string) []SKUPatch {
	status := PublishFailed
	out := make([]SKUPatch, 0, len(items))
	for _, item := range items {
		msg := message
		out = append(out, SKUPatch{ID: item.SKUID, ExpectVersion: item.Version, PublishStatus: &status, LastError: &msg})
	}
	return out
}

func unknownPatches(items []PublishItem, message string) []SKUPatch {
	status := PublishVerify
	out := make([]SKUPatch, 0, len(items))
	for _, item := range items {
		msg := message
		out = append(out, SKUPatch{ID: item.SKUID, ExpectVersion: item.Version, PublishStatus: &status, LastError: &msg})
	}
	return out
}

func batchSettled(batch *PublishBatch) bool {
	if batch == nil || len(batch.Requests) == 0 {
		return false
	}
	for _, req := range batch.Requests {
		if req.Status == "pending" || req.Status == "accepted" {
			return false
		}
		for _, item := range req.Items {
			if item.Result == "" || item.Result == string(RemotePending) {
				return false
			}
		}
	}
	return true
}

// UpdateStock pushes one warehouse quantity through /v2/products/stocks.
func (uc *ListingUsecase) UpdateStock(ctx context.Context, shopID, skuID int, warehouseID int64, qty int) (*StockView, error) {
	if qty < 0 {
		return nil, bad("BAD_STOCK", "库存不能为负数。", nil)
	}
	if warehouseID <= 0 {
		return nil, bad("BAD_WAREHOUSE", "请选择仓库。", nil)
	}
	sku, err := uc.repo.GetSKU(ctx, shopID, skuID)
	if err != nil {
		return nil, err
	}
	if sku.OzonProductID == 0 && sku.PublishStatus != PublishImported {
		return nil, bad("NOT_IMPORTED", "商品还没有导入成功，暂时不能改库存。", nil)
	}
	rows, err := uc.repo.ListStocks(ctx, shopID, skuID)
	if err != nil {
		return nil, err
	}
	var current *WarehouseStock
	for i := range rows {
		if rows[i].WarehouseID == warehouseID {
			current = &rows[i]
			break
		}
	}
	now := time.Now().UTC()
	if current != nil && current.SyncedAt != nil && StockTooSoon(*current.SyncedAt, now) {
		return nil, bad("STOCK_TOO_SOON", "同一商品在同一仓库 30 秒内只能更新一次。Ozon 仍可能以更长的窗口返回 TOO_MANY_REQUESTS。", nil)
	}
	cred, err := uc.shops.Credentials(ctx, shopID)
	if err != nil {
		return nil, err
	}
	results, err := uc.gateway.UpdateStocks(ctx, cred, []StockLine{{
		OfferID: sku.OfferID, ProductID: sku.OzonProductID, WarehouseID: warehouseID, Stock: qty,
	}})
	row := &WarehouseStock{SKUID: skuID, ShopID: shopID, WarehouseID: warehouseID, TargetQty: qty, PlatformQty: -1}
	if current != nil {
		row.ID = current.ID
		row.PlatformQty = current.PlatformQty
	}
	if err != nil {
		row.SyncStatus = "failed"
		row.SyncMessage = err.Error()
		saved, saveErr := uc.repo.SaveStock(ctx, row)
		if saveErr != nil {
			return nil, saveErr
		}
		view := stockViews([]WarehouseStock{*saved})[0]
		return &view, &Error{Status: 502, Reason: "OZON_ERROR", Message: err.Error()}
	}
	if len(results) == 0 {
		return nil, bad("OZON_EMPTY", "Ozon 没有返回库存结果。", nil)
	}
	result := results[0]
	if !result.Updated {
		code, detail := FormatImportErrors(result.Errors)
		if detail == "" {
			detail = "Ozon 没有更新这条库存"
		}
		if code != "" {
			detail = code + "：" + detail
		}
		row.SyncStatus = "failed"
		row.SyncMessage = detail
		saved, err := uc.repo.SaveStock(ctx, row)
		if err != nil {
			return nil, err
		}
		view := stockViews([]WarehouseStock{*saved})[0]
		return &view, &Error{Status: 409, Reason: "STOCK_REJECTED", Message: detail}
	}
	row.SyncStatus = "ok"
	row.SyncMessage = ""
	row.PlatformQty = qty
	row.SyncedAt = &now
	saved, err := uc.repo.SaveStock(ctx, row)
	if err != nil {
		return nil, err
	}
	view := stockViews([]WarehouseStock{*saved})[0]
	return &view, nil
}

func (uc *ListingUsecase) rememberCurrency(ctx context.Context, shopID int, ready []PublishCheck, loaded map[int]*ListingSKU) {
	var code string
	for _, row := range ready {
		sku := loaded[row.ID]
		if sku == nil || strings.TrimSpace(sku.CurrencyCode) == "" {
			continue
		}
		if code == "" {
			code = sku.CurrencyCode
			continue
		}
		if !strings.EqualFold(code, sku.CurrencyCode) {
			return
		}
	}
	if code != "" {
		_ = uc.shops.RememberCurrency(ctx, shopID, code)
	}
}

func bodiesOf(ready []PublishCheck, items map[int]json.RawMessage) ([]json.RawMessage, error) {
	raws := make([]json.RawMessage, 0, len(ready))
	for _, row := range ready {
		if len(items[row.ID]) == 0 {
			continue
		}
		raws = append(raws, items[row.ID])
	}
	chunks := ChunkRaw(raws, MaxImportItems)
	out := make([]json.RawMessage, 0, len(chunks))
	for _, chunk := range chunks {
		body, err := BuildImportBody(chunk)
		if err != nil {
			return nil, err
		}
		out = append(out, body)
	}
	return out, nil
}

func inputPairs(rows []PublishPick) [][2]int {
	out := make([][2]int, 0, len(rows))
	for _, row := range rows {
		out = append(out, [2]int{row.ID, row.Version})
	}
	return out
}

func mustMap(raw json.RawMessage) map[string]any {
	var obj map[string]any
	_ = json.Unmarshal(raw, &obj)
	if obj == nil {
		obj = map[string]any{}
	}
	return obj
}
