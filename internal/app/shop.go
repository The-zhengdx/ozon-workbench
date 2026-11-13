package app

import (
	"context"
	"strings"
	"unicode"

	"ozon-workbench/internal/domain"
	"ozon-workbench/internal/store"
)

type ShopInput struct {
	Name         string
	ClientID     string
	APIKey       string
	CurrencyCode *string
	VAT          *string
	Enable       *bool
}

func (e *Engine) CreateShop(ctx context.Context, in ShopInput) (domain.Shop, error) {
	name := strings.TrimSpace(in.Name)
	clientID := strings.TrimSpace(in.ClientID)
	apiKey := strings.TrimSpace(in.APIKey)
	if name == "" || clientID == "" || apiKey == "" {
		return domain.Shop{}, fail(400, "SHOP_FIELDS", "店铺需要 name、client_id 和 api_key", nil)
	}
	now := e.now()
	shop := domain.Shop{
		Name: name, ClientID: clientID, APIKey: apiKey,
		CurrencyCode: domain.ShopCurrencyDefault, VAT: domain.ShopVATDefault, Enable: true,
		CreatedAt: now, UpdatedAt: now,
	}
	if in.CurrencyCode != nil {
		shop.CurrencyCode = strings.ToUpper(strings.TrimSpace(*in.CurrencyCode))
	}
	if in.VAT != nil {
		shop.VAT = strings.TrimSpace(*in.VAT)
	}
	if in.Enable != nil {
		shop.Enable = *in.Enable
	}
	if err := validateShop(&shop); err != nil {
		return domain.Shop{}, err
	}
	var created domain.Shop
	err := e.DB.InTx(ctx, func(tx store.Tx) error {
		var err error
		created, err = tx.CreateShop(ctx, shop)
		return err
	})
	if err == store.ErrConflict {
		return domain.Shop{}, fail(409, "SHOP_CLIENT_ID", "client_id 已存在", nil)
	}
	return created, err
}

func (e *Engine) UpdateShop(ctx context.Context, id int64, in ShopInput) (domain.Shop, error) {
	var updated domain.Shop
	err := e.DB.InTx(ctx, func(tx store.Tx) error {
		shop, err := tx.GetShop(ctx, id)
		if err != nil {
			return err
		}
		if strings.TrimSpace(in.Name) != "" {
			shop.Name = strings.TrimSpace(in.Name)
		}
		if strings.TrimSpace(in.ClientID) != "" {
			shop.ClientID = strings.TrimSpace(in.ClientID)
		}
		if strings.TrimSpace(in.APIKey) != "" {
			shop.APIKey = strings.TrimSpace(in.APIKey)
		}
		if in.CurrencyCode != nil {
			shop.CurrencyCode = strings.ToUpper(strings.TrimSpace(*in.CurrencyCode))
		}
		if in.VAT != nil {
			shop.VAT = strings.TrimSpace(*in.VAT)
		}
		if in.Enable != nil {
			shop.Enable = *in.Enable
		}
		shop.UpdatedAt = e.now()
		if err := validateShop(&shop); err != nil {
			return err
		}
		updated, err = tx.UpdateShop(ctx, shop)
		return err
	})
	if err == store.ErrNotFound {
		return domain.Shop{}, fail(404, "SHOP_NOT_FOUND", "店铺不存在", nil)
	}
	if err == store.ErrConflict {
		return domain.Shop{}, fail(409, "SHOP_CLIENT_ID", "client_id 已存在", nil)
	}
	return updated, err
}

func (e *Engine) ListShops(ctx context.Context) ([]domain.Shop, error) {
	var shops []domain.Shop
	err := e.DB.InTx(ctx, func(tx store.Tx) error {
		var err error
		shops, err = tx.ListShops(ctx)
		return err
	})
	return shops, err
}

func (e *Engine) GetShop(ctx context.Context, id int64) (domain.Shop, error) {
	var shop domain.Shop
	err := e.DB.InTx(ctx, func(tx store.Tx) error {
		var err error
		shop, err = tx.GetShop(ctx, id)
		return err
	})
	if err == store.ErrNotFound {
		return domain.Shop{}, fail(404, "SHOP_NOT_FOUND", "店铺不存在", nil)
	}
	return shop, err
}

func validateShop(shop *domain.Shop) error {
	if shop.Name == "" || shop.ClientID == "" || shop.APIKey == "" {
		return fail(400, "SHOP_FIELDS", "店铺需要 name、client_id 和 api_key", nil)
	}
	if len(shop.CurrencyCode) != 3 {
		return fail(400, "SHOP_CURRENCY", "currency_code 必须是 3 位字母", nil)
	}
	for _, r := range shop.CurrencyCode {
		if !unicode.IsLetter(r) || unicode.IsLower(r) {
			return fail(400, "SHOP_CURRENCY", "currency_code 必须是 3 位大写字母", nil)
		}
	}
	if shop.VAT == "" {
		return fail(400, "SHOP_VAT", "vat 不能为空", nil)
	}
	return nil
}

func MaskKey(key string) string {
	key = strings.TrimSpace(key)
	if len(key) <= 4 {
		return "****"
	}
	return "****" + key[len(key)-4:]
}
