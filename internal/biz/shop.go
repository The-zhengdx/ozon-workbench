package biz

import (
	"context"
	"strings"
	"time"

	"github.com/go-kratos/kratos/v2/log"
)

// ShopUsecase owns shop credentials and the warehouse connectivity check.
type ShopUsecase struct {
	repo   ShopRepo
	cipher Cipher
	ozon   OzonGateway
	log    *log.Helper
}

func NewShopUsecase(repo ShopRepo, cipher Cipher, ozon OzonGateway, logger log.Logger) *ShopUsecase {
	return &ShopUsecase{repo: repo, cipher: cipher, ozon: ozon, log: log.NewHelper(logger)}
}

func (uc *ShopUsecase) List(ctx context.Context) ([]ShopView, error) {
	rows, err := uc.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]ShopView, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.View())
	}
	return out, nil
}

func (uc *ShopUsecase) Get(ctx context.Context, id int) (ShopView, error) {
	row, err := uc.repo.Get(ctx, id)
	if err != nil {
		return ShopView{}, err
	}
	return row.View(), nil
}

// Create stores a shop. Currency is left unknown. A connectivity check runs after the row exists.
func (uc *ShopUsecase) Create(ctx context.Context, name, clientID, apiKey string) (*ShopWriteResult, error) {
	name, clientID, apiKey, fields := normalizeShop(name, clientID, apiKey, false)
	if len(fields) > 0 {
		return nil, fieldError(fields)
	}
	if exist, err := uc.repo.GetByClientID(ctx, clientID); err != nil {
		return nil, err
	} else if exist != nil {
		return nil, bad("CLIENT_ID_EXISTS", "这个 Client ID 已有店铺记录。", map[string]string{"client_id": "这个 Client ID 已有店铺记录。"})
	}
	cipherText, err := uc.cipher.Encrypt(apiKey)
	if err != nil {
		return nil, wrap(500, "ENCRYPT_FAILED", "凭证加密失败。")
	}
	rows, err := uc.repo.List(ctx)
	if err != nil {
		return nil, err
	}
	isDefault := !hasActive(rows)
	created, err := uc.repo.Create(ctx, &Shop{
		Name:             name,
		ClientID:         clientID,
		APIKeyCiphertext: cipherText,
		IsDefault:        isDefault,
		Warehouses:       []Warehouse{},
	})
	if err != nil {
		return nil, err
	}
	return uc.finishWithTest(ctx, created)
}

// Update changes name, client id and, when a new key is supplied, the stored secret.
// An empty API key keeps the previous ciphertext.
func (uc *ShopUsecase) Update(ctx context.Context, id int, name, clientID, apiKey string) (*ShopWriteResult, error) {
	current, err := uc.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	keyProvided := strings.TrimSpace(apiKey) != ""
	name, clientID, apiKey, fields := normalizeShop(name, clientID, apiKey, !keyProvided)
	if !keyProvided {
		delete(fields, "api_key")
	}
	if len(fields) > 0 {
		return nil, fieldError(fields)
	}
	if exist, err := uc.repo.GetByClientID(ctx, clientID); err != nil {
		return nil, err
	} else if exist != nil && exist.ID != current.ID {
		return nil, bad("CLIENT_ID_EXISTS", "这个 Client ID 已有店铺记录。", map[string]string{"client_id": "这个 Client ID 已有店铺记录。"})
	}
	credentialsChanged := clientID != current.ClientID || keyProvided
	if keyProvided {
		cipherText, err := uc.cipher.Encrypt(apiKey)
		if err != nil {
			return nil, wrap(500, "ENCRYPT_FAILED", "凭证加密失败。")
		}
		current.APIKeyCiphertext = cipherText
	}
	current.Name = name
	current.ClientID = clientID
	updated, err := uc.repo.Update(ctx, current)
	if err != nil {
		return nil, err
	}
	if !credentialsChanged {
		view := updated.View()
		return &ShopWriteResult{Shop: view}, nil
	}
	return uc.finishWithTest(ctx, updated)
}

func (uc *ShopUsecase) Archive(ctx context.Context, id int) (ShopView, error) {
	current, err := uc.repo.Get(ctx, id)
	if err != nil {
		return ShopView{}, err
	}
	current.Archived = true
	wasDefault := current.IsDefault
	current.IsDefault = false
	updated, err := uc.repo.Update(ctx, current)
	if err != nil {
		return ShopView{}, err
	}
	if wasDefault {
		rows, err := uc.repo.List(ctx)
		if err != nil {
			return ShopView{}, err
		}
		for _, row := range rows {
			if !row.Archived {
				if err := uc.repo.SetDefault(ctx, row.ID); err != nil {
					return ShopView{}, err
				}
				break
			}
		}
		updated, err = uc.repo.Get(ctx, id)
		if err != nil {
			return ShopView{}, err
		}
	}
	return updated.View(), nil
}

func (uc *ShopUsecase) Restore(ctx context.Context, id int) (ShopView, error) {
	current, err := uc.repo.Get(ctx, id)
	if err != nil {
		return ShopView{}, err
	}
	current.Archived = false
	rows, err := uc.repo.List(ctx)
	if err != nil {
		return ShopView{}, err
	}
	if !hasActive(rows) {
		current.IsDefault = true
	}
	updated, err := uc.repo.Update(ctx, current)
	if err != nil {
		return ShopView{}, err
	}
	return updated.View(), nil
}

func (uc *ShopUsecase) SetDefault(ctx context.Context, id int) (ShopView, error) {
	current, err := uc.repo.Get(ctx, id)
	if err != nil {
		return ShopView{}, err
	}
	if current.Archived {
		return ShopView{}, bad("SHOP_ARCHIVED", "已移除的店铺不能设为当前店铺。", nil)
	}
	if err := uc.repo.SetDefault(ctx, id); err != nil {
		return ShopView{}, err
	}
	current, err = uc.repo.Get(ctx, id)
	if err != nil {
		return ShopView{}, err
	}
	return current.View(), nil
}

// TestConnection calls POST /v2/warehouse/list and stores every page.
func (uc *ShopUsecase) TestConnection(ctx context.Context, id int) (*ShopWriteResult, error) {
	current, err := uc.repo.Get(ctx, id)
	if err != nil {
		return nil, err
	}
	return uc.finishWithTest(ctx, current)
}

func (uc *ShopUsecase) finishWithTest(ctx context.Context, shop *Shop) (*ShopWriteResult, error) {
	result, tested := uc.test(ctx, shop)
	view := tested.View()
	return &ShopWriteResult{Shop: view, Connection: result}, nil
}

func (uc *ShopUsecase) test(ctx context.Context, shop *Shop) (*ConnectionResult, *Shop) {
	now := time.Now().UTC()
	shop.LastTestedAt = &now
	if len(shop.APIKeyCiphertext) == 0 {
		msg := "请先配置 API Key。"
		ok := false
		shop.LastTestOK = &ok
		shop.LastTestMessage = &msg
		saved, err := uc.repo.Update(ctx, shop)
		if err != nil {
			uc.log.Errorf("save shop test result: %v", err)
			return &ConnectionResult{OK: false, Message: msg}, shop
		}
		return &ConnectionResult{OK: false, Message: msg}, saved
	}
	plain, err := uc.cipher.Decrypt(shop.APIKeyCiphertext)
	if err != nil {
		msg := "已保存的 API Key 无法解密，请重新填写。"
		ok := false
		shop.LastTestOK = &ok
		shop.LastTestMessage = &msg
		saved, uerr := uc.repo.Update(ctx, shop)
		if uerr != nil {
			uc.log.Errorf("save shop test result: %v", uerr)
			saved = shop
		}
		return &ConnectionResult{OK: false, Message: msg}, saved
	}
	warehouses, err := uc.ozon.ListWarehouses(ctx, OzonCred{ClientID: shop.ClientID, APIKey: plain})
	if err != nil {
		msg := err.Error()
		if len(msg) > 2000 {
			msg = msg[:2000]
		}
		ok := false
		shop.LastTestOK = &ok
		shop.LastTestMessage = &msg
		saved, uerr := uc.repo.Update(ctx, shop)
		if uerr != nil {
			uc.log.Errorf("save shop test result: %v", uerr)
			saved = shop
		}
		return &ConnectionResult{OK: false, Message: msg}, saved
	}
	if warehouses == nil {
		warehouses = []Warehouse{}
	}
	shop.Warehouses = warehouses
	ok := true
	msg := "连接成功，已同步仓库。"
	if len(warehouses) == 0 {
		msg = "连接成功，该账号没有可访问的仓库。"
	}
	shop.LastTestOK = &ok
	shop.LastTestMessage = &msg
	saved, uerr := uc.repo.Update(ctx, shop)
	if uerr != nil {
		uc.log.Errorf("save warehouses: %v", uerr)
		saved = shop
	}
	return &ConnectionResult{OK: true, Message: msg, WarehouseCount: len(warehouses)}, saved
}

// Credentials decrypts a shop key for other usecases. The plaintext must not be logged.
func (uc *ShopUsecase) Credentials(ctx context.Context, id int) (OzonCred, error) {
	if id <= 0 {
		return OzonCred{}, bad("SHOP_REQUIRED", "请先在顶部选择店铺。", nil)
	}
	shop, err := uc.repo.Get(ctx, id)
	if err != nil {
		return OzonCred{}, err
	}
	if shop.Archived {
		return OzonCred{}, bad("SHOP_ARCHIVED", "该店铺已移除。", nil)
	}
	if len(shop.APIKeyCiphertext) == 0 {
		return OzonCred{}, bad("KEY_MISSING", "请先在店铺管理中配置 API Key。", nil)
	}
	plain, err := uc.cipher.Decrypt(shop.APIKeyCiphertext)
	if err != nil {
		return OzonCred{}, bad("KEY_DECRYPT", "已保存的 API Key 无法解密，请重新填写。", nil)
	}
	return OzonCred{ClientID: shop.ClientID, APIKey: plain}, nil
}

func normalizeShop(name, clientID, apiKey string, allowEmptyKey bool) (string, string, string, map[string]string) {
	name = strings.TrimSpace(name)
	clientID = strings.TrimSpace(clientID)
	apiKey = strings.TrimSpace(apiKey)
	fields := map[string]string{}
	if name == "" {
		fields["name"] = "请填写店铺名称。"
	} else if len([]rune(name)) > 100 {
		fields["name"] = "店铺名称不能超过 100 个字。"
	}
	if clientID == "" {
		fields["client_id"] = "请填写 Client ID。"
	} else if len(clientID) > 64 {
		fields["client_id"] = "Client ID 不能超过 64 个字符。"
	}
	if apiKey == "" && !allowEmptyKey {
		fields["api_key"] = "请填写 API Key。"
	} else if len(apiKey) > 512 {
		fields["api_key"] = "API Key 过长。"
	}
	return name, clientID, apiKey, fields
}

func hasActive(rows []*Shop) bool {
	for _, row := range rows {
		if row != nil && !row.Archived {
			return true
		}
	}
	return false
}
