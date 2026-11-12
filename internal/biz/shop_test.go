package biz

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"

	"ozon-workbench/internal/catalog"
)

type memShops struct {
	mu   sync.Mutex
	seq  int
	rows map[int]*Shop
}

func newMemShops() *memShops {
	return &memShops{rows: map[int]*Shop{}}
}

func cloneShop(in *Shop) *Shop {
	if in == nil {
		return nil
	}
	out := *in
	out.APIKeyCiphertext = append([]byte(nil), in.APIKeyCiphertext...)
	out.Warehouses = append([]Warehouse(nil), in.Warehouses...)
	if in.CurrencyCode != nil {
		v := *in.CurrencyCode
		out.CurrencyCode = &v
	}
	if in.LastTestedAt != nil {
		v := *in.LastTestedAt
		out.LastTestedAt = &v
	}
	if in.LastTestOK != nil {
		v := *in.LastTestOK
		out.LastTestOK = &v
	}
	if in.LastTestMessage != nil {
		v := *in.LastTestMessage
		out.LastTestMessage = &v
	}
	if in.WarehousesSyncedAt != nil {
		v := *in.WarehousesSyncedAt
		out.WarehousesSyncedAt = &v
	}
	return &out
}

func (m *memShops) List(context.Context) ([]*Shop, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]*Shop, 0, len(m.rows))
	for id := 1; id <= m.seq; id++ {
		if row, ok := m.rows[id]; ok {
			out = append(out, cloneShop(row))
		}
	}
	return out, nil
}

func (m *memShops) Get(_ context.Context, id int) (*Shop, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	row, ok := m.rows[id]
	if !ok {
		return nil, notFound("店铺不存在。")
	}
	return cloneShop(row), nil
}

func (m *memShops) GetByClientID(_ context.Context, clientID string) (*Shop, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, row := range m.rows {
		if row.ClientID == clientID {
			return cloneShop(row), nil
		}
	}
	return nil, nil
}

func (m *memShops) Create(_ context.Context, shop *Shop) (*Shop, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	row := cloneShop(shop)
	row.ID = m.seq
	now := time.Now().UTC()
	row.CreatedAt = now
	row.UpdatedAt = now
	m.rows[row.ID] = row
	return cloneShop(row), nil
}

func (m *memShops) Update(_ context.Context, shop *Shop) (*Shop, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	prev, ok := m.rows[shop.ID]
	if !ok {
		return nil, notFound("店铺不存在。")
	}
	row := cloneShop(shop)
	row.CreatedAt = prev.CreatedAt
	row.UpdatedAt = time.Now().UTC()
	m.rows[row.ID] = row
	return cloneShop(row), nil
}

func (m *memShops) SetDefault(_ context.Context, id int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.rows[id]; !ok {
		return notFound("店铺不存在。")
	}
	for _, row := range m.rows {
		row.IsDefault = row.ID == id
	}
	return nil
}

type testCipher struct{}

func (testCipher) Encrypt(plain string) ([]byte, error) {
	return append([]byte("v1:"), []byte(plain)...), nil
}

func (testCipher) Decrypt(ciphertext []byte) (string, error) {
	if len(ciphertext) < 3 || string(ciphertext[:3]) != "v1:" {
		return "", errors.New("bad ciphertext")
	}
	return string(ciphertext[3:]), nil
}

type fakeOzon struct {
	warehouses []Warehouse
	err        error
	calls      int
	lastCred   OzonCred
	mocked     bool
}

func (f *fakeOzon) ListWarehouses(_ context.Context, cred OzonCred) ([]Warehouse, error) {
	f.calls++
	f.lastCred = cred
	if f.err != nil {
		return nil, f.err
	}
	return append([]Warehouse(nil), f.warehouses...), nil
}

func (f *fakeOzon) CategoryTree(context.Context, OzonCred, string) ([]catalog.TreeNode, error) {
	return nil, errors.New("unused")
}

func (f *fakeOzon) Attributes(context.Context, OzonCred, int64, int64, string) ([]map[string]any, error) {
	return nil, errors.New("unused")
}

func (f *fakeOzon) AttributeValues(context.Context, OzonCred, int64, int64, int64, int64, int, string) (*ValuePage, error) {
	return nil, errors.New("unused")
}

func (f *fakeOzon) SearchAttributeValues(context.Context, OzonCred, int64, int64, int64, string, int) ([]map[string]any, error) {
	return nil, errors.New("unused")
}

func (f *fakeOzon) Mocked() bool { return f.mocked }

func newShopUC(oz OzonGateway) (*ShopUsecase, *memShops) {
	repo := newMemShops()
	if oz == nil {
		oz = &fakeOzon{warehouses: []Warehouse{{WarehouseID: 11, Name: "莫斯科一号仓", Status: "created"}}}
	}
	return NewShopUsecase(repo, testCipher{}, oz, discardLogger()), repo
}

func TestCreateShopKeepsCurrencyUnknownAndHidesKey(t *testing.T) {
	uc, repo := newShopUC(nil)
	got, err := uc.Create(context.Background(), " 跨境汽配店 ", "CN-100", "secret-key")
	if err != nil {
		t.Fatal(err)
	}
	if got.Shop.CurrencyCode != nil {
		t.Fatalf("currency prefilled: %#v", got.Shop.CurrencyCode)
	}
	if !got.Shop.APIKeyConfigured {
		t.Fatal("expected configured status")
	}
	raw, _ := json.Marshal(got.Shop)
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		t.Fatal(err)
	}
	if _, ok := payload["api_key"]; ok {
		t.Fatal("response contains api_key")
	}
	if _, ok := payload["api_key_ciphertext"]; ok {
		t.Fatal("response contains ciphertext")
	}
	if payload["currency_code"] != nil {
		t.Fatalf("currency_code = %#v", payload["currency_code"])
	}
	if got.Connection == nil || !got.Connection.OK || got.Connection.WarehouseCount != 1 {
		t.Fatalf("connection: %#v", got.Connection)
	}
	stored, _ := repo.Get(context.Background(), got.Shop.ID)
	if string(stored.APIKeyCiphertext) == "secret-key" || len(stored.APIKeyCiphertext) == 0 {
		t.Fatal("key was not encrypted at rest")
	}
	if string(stored.APIKeyCiphertext) != "v1:secret-key" {
		t.Fatalf("ciphertext = %q", stored.APIKeyCiphertext)
	}
}

func TestCreateRequiresFieldsAndUniqueClientID(t *testing.T) {
	uc, _ := newShopUC(nil)
	_, err := uc.Create(context.Background(), "", "", "")
	be, ok := err.(*Error)
	if !ok || be.Fields["name"] == "" || be.Fields["client_id"] == "" || be.Fields["api_key"] == "" {
		t.Fatalf("fields: %#v", err)
	}
	if _, err := uc.Create(context.Background(), "甲店", "SAME", "k1"); err != nil {
		t.Fatal(err)
	}
	_, err = uc.Create(context.Background(), "乙店", "SAME", "k2")
	be, ok = err.(*Error)
	if !ok || be.Reason != "CLIENT_ID_EXISTS" {
		t.Fatalf("duplicate: %#v", err)
	}
}

func TestUpdateEmptyAPIKeyKeepsExistingAndNewKeyRetests(t *testing.T) {
	oz := &fakeOzon{warehouses: []Warehouse{{WarehouseID: 7, Name: "仓"}}}
	uc, repo := newShopUC(oz)
	created, err := uc.Create(context.Background(), "甲店", "CID-1", "original")
	if err != nil {
		t.Fatal(err)
	}
	if oz.calls != 1 || oz.lastCred.APIKey != "original" || oz.lastCred.ClientID != "CID-1" {
		t.Fatalf("first call: %+v calls=%d", oz.lastCred, oz.calls)
	}
	renamed, err := uc.Update(context.Background(), created.Shop.ID, "甲店改名", "CID-1", "   ")
	if err != nil {
		t.Fatal(err)
	}
	if oz.calls != 1 {
		t.Fatalf("name-only update retested, calls=%d", oz.calls)
	}
	if renamed.Connection != nil {
		t.Fatal("name-only update should not return a new connection check")
	}
	stored, _ := repo.Get(context.Background(), created.Shop.ID)
	if string(stored.APIKeyCiphertext) != "v1:original" {
		t.Fatalf("key changed: %q", stored.APIKeyCiphertext)
	}
	if stored.CurrencyCode != nil {
		t.Fatal("currency changed")
	}
	updated, err := uc.Update(context.Background(), created.Shop.ID, "甲店改名", "CID-2", "rotated")
	if err != nil {
		t.Fatal(err)
	}
	if oz.calls != 2 || oz.lastCred.APIKey != "rotated" || oz.lastCred.ClientID != "CID-2" {
		t.Fatalf("retest creds %+v calls=%d", oz.lastCred, oz.calls)
	}
	if updated.Connection == nil || !updated.Connection.OK {
		t.Fatalf("connection %#v", updated.Connection)
	}
	stored, _ = repo.Get(context.Background(), created.Shop.ID)
	if string(stored.APIKeyCiphertext) != "v1:rotated" {
		t.Fatalf("rotated key = %q", stored.APIKeyCiphertext)
	}
}

func TestConnectionFailureIsSurfacedAndShopRemains(t *testing.T) {
	oz := &fakeOzon{err: errors.New("Ozon HTTP 403: Invalid Api-Key, please check the key and try again")}
	uc, _ := newShopUC(oz)
	got, err := uc.Create(context.Background(), "甲店", "CID", "bad")
	if err != nil {
		t.Fatal(err)
	}
	if got.Connection == nil || got.Connection.OK || got.Connection.Message != oz.err.Error() {
		t.Fatalf("connection %#v", got.Connection)
	}
	if got.Shop.LastTestOK == nil || *got.Shop.LastTestOK {
		t.Fatalf("last_test_ok %#v", got.Shop.LastTestOK)
	}
	if got.Shop.CurrencyCode != nil {
		t.Fatal("currency set after failed test")
	}
}

func TestArchiveAndDefault(t *testing.T) {
	uc, _ := newShopUC(nil)
	a, err := uc.Create(context.Background(), "甲", "A", "k")
	if err != nil {
		t.Fatal(err)
	}
	b, err := uc.Create(context.Background(), "乙", "B", "k")
	if err != nil {
		t.Fatal(err)
	}
	if !a.Shop.IsDefault || b.Shop.IsDefault {
		t.Fatalf("defaults a=%v b=%v", a.Shop.IsDefault, b.Shop.IsDefault)
	}
	archived, err := uc.Archive(context.Background(), a.Shop.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !archived.Archived || archived.IsDefault {
		t.Fatalf("archived %#v", archived)
	}
	second, err := uc.Get(context.Background(), b.Shop.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !second.IsDefault {
		t.Fatal("expected the remaining shop to become default")
	}
}

func TestRealSyncKeepsLastSuccessWhenRetestFails(t *testing.T) {
	oz := &fakeOzon{warehouses: []Warehouse{{WarehouseID: 99, Name: "真实仓", Status: "created"}}}
	uc, _ := newShopUC(oz)
	created, err := uc.Create(context.Background(), "甲店", "6134344", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if created.Shop.WarehousesMock || created.Shop.WarehousesSyncedAt == nil || created.Shop.Warehouses[0].WarehouseID != 99 {
		t.Fatalf("sync %#v", created.Shop)
	}
	synced := *created.Shop.WarehousesSyncedAt
	oz.err = errors.New("无法连接 Ozon")
	again, err := uc.TestConnection(context.Background(), created.Shop.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again.Connection.OK || len(again.Shop.Warehouses) != 1 || again.Shop.Warehouses[0].WarehouseID != 99 {
		t.Fatalf("kept %#v msg %s", again.Shop.Warehouses, again.Connection.Message)
	}
	if again.Shop.WarehousesSyncedAt == nil || !again.Shop.WarehousesSyncedAt.Equal(synced) {
		t.Fatalf("synced at changed: %#v", again.Shop.WarehousesSyncedAt)
	}
	if again.Shop.WarehousesMock {
		t.Fatal("real warehouses marked mock")
	}
}

func TestFailedRealSyncDropsStoredSamples(t *testing.T) {
	oz := &fakeOzon{warehouses: SampleWarehouses(), mocked: true}
	uc, repo := newShopUC(oz)
	created, err := uc.Create(context.Background(), "甲店", "6134344", "secret")
	if err != nil {
		t.Fatal(err)
	}
	if !created.Shop.WarehousesMock || len(created.Shop.Warehouses) != 3 || created.Connection.Message == "" {
		t.Fatalf("mock create %#v %s", created.Shop.Warehouses, created.Connection.Message)
	}
	oz.mocked = false
	oz.err = errors.New(`Post "https://api-seller.ozon.ru/v2/warehouse/list": EOF`)
	again, err := uc.TestConnection(context.Background(), created.Shop.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again.Connection.OK || len(again.Shop.Warehouses) != 0 || again.Shop.WarehousesMock || again.Shop.WarehousesSyncedAt != nil {
		t.Fatalf("after failure %#v msg %s", again.Shop, again.Connection.Message)
	}
	stored, err := repo.Get(context.Background(), created.Shop.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(stored.Warehouses) != 0 || stored.WarehousesMock {
		t.Fatalf("persisted %#v", stored.Warehouses)
	}
}

func TestListHidesSamplesWhenNotMocked(t *testing.T) {
	oz := &fakeOzon{warehouses: SampleWarehouses(), mocked: true}
	uc, _ := newShopUC(oz)
	if _, err := uc.Create(context.Background(), "甲店", "6134344", "secret"); err != nil {
		t.Fatal(err)
	}
	oz.mocked = false
	rows, err := uc.List(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || len(rows[0].Warehouses) != 0 || rows[0].WarehousesMock {
		t.Fatalf("listed %#v", rows[0])
	}
}
