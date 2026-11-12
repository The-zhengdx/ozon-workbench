package biz

import "context"

// RefreshPlatform reads /v3/product/info/list for one offer and stores the platform status.
func (uc *ListingUsecase) RefreshPlatform(ctx context.Context, shopID, id int) (*ListingDetail, error) {
	sku, err := uc.repo.GetSKU(ctx, shopID, id)
	if err != nil {
		return nil, err
	}
	if uc.gateway == nil {
		return nil, bad("NO_OZON", "Ozon 客户端未配置。", nil)
	}
	cred, err := uc.shops.Credentials(ctx, shopID)
	if err != nil {
		return nil, err
	}
	cards, err := uc.gateway.ProductInfo(ctx, cred, []string{sku.OfferID})
	if err != nil {
		return nil, ozonFailure(err)
	}
	patch := SKUPatch{ID: sku.ID, Checked: true}
	if len(cards) == 0 {
		status := PlatformUnread
		reason := "平台还没有这件商品的状态"
		patch.PlatformStatus = &status
		patch.PlatformReason = &reason
	} else {
		card := cards[0]
		status, reason := MapPlatform(card.Status, card.Moderate, card.Failed, card.Name, card.Description, card.Tooltip, card.Archived)
		patch.PlatformStatus = &status
		patch.PlatformReason = &reason
		if card.ProductID > 0 {
			pid := card.ProductID
			patch.OzonProductID = &pid
		}
		if card.SKU > 0 {
			ozonSKU := card.SKU
			patch.OzonSKU = &ozonSKU
		}
	}
	if err := uc.repo.SaveBatch(ctx, nil, []SKUPatch{patch}); err != nil {
		return nil, err
	}
	return uc.Detail(ctx, shopID, id)
}
