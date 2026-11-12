package biz

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"ozon-workbench/internal/storage"
)

func TestCompletedTaskBecomesRowsAndPublishWriteback(t *testing.T) {
	repo := newMemListings()
	shops := &staticShop{view: ShopView{ID: 1, Name: "演示店"}, cred: OzonCred{ClientID: "1", APIKey: "k"}}
	seller := &scriptedSeller{}
	uc := NewListingUsecase(repo, shops, NewMockRunner(), storage.None{}, seller, inlineSched{}, discardLogger())
	task := sampleTask(1, "30", []SubSKU{
		{OfferID: "30-1", Specs: "灰"}, {OfferID: "30-2", Specs: "蓝"},
		{OfferID: "30-3", Specs: "黑"}, {OfferID: "30-4", Specs: "白"},
	})
	uc.OnTaskCompleted(context.Background(), task)
	page, err := uc.List(context.Background(), ListingFilter{ShopID: 1, Mode: "master", Query: "30"})
	if err != nil || page.Total != 4 {
		t.Fatalf("master page %#v %v", page, err)
	}
	one, err := uc.List(context.Background(), ListingFilter{ShopID: 1, Mode: "offer", Query: "30-1"})
	if err != nil || one.Total != 1 || one.Rows[0].OfferID != "30-1" {
		t.Fatalf("offer %#v %v", one, err)
	}
	if _, err := uc.UploadMedia(context.Background(), 1, one.Rows[0].ID); err == nil || !strings.Contains(err.Error(), "图床未配置") {
		t.Fatalf("upload err %v", err)
	}
	detail, err := uc.Generate(context.Background(), 1, one.Rows[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.SKU.PublishStatus != PublishDraft || !strings.Contains(strings.Join(detail.Issues, " "), "HTTPS") {
		t.Fatalf("draft %#v %v", detail.SKU.PublishStatus, detail.Issues)
	}
	edited := validItem("30-1", "https://cdn.example.com/30-1.jpg")
	raw, _ := json.Marshal(edited)
	detail, err = uc.SaveVersion(context.Background(), 1, detail.SKU.ID, detail.Version, raw)
	if err != nil || detail.SKU.PublishStatus != PublishReady {
		t.Fatalf("save %#v %v %v", detail, err, detail.Issues)
	}
	seller.onImport = func() {
		sku, _ := repo.GetSKU(context.Background(), 1, detail.SKU.ID)
		if sku.PublishStatus != PublishSubmitting {
			t.Errorf("status during import %s", sku.PublishStatus)
		}
	}
	plan, err := uc.Publish(context.Background(), PublishInput{
		ShopID: 1, Execute: true, IdempotencyKey: "op-1",
		Items: []PublishPick{{ID: detail.SKU.ID, Version: detail.Version}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(seller.bodies) != 1 {
		t.Fatalf("calls %d", len(seller.bodies))
	}
	var body struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(seller.bodies[0], &body); err != nil || len(body.Items) != 1 || !JSONEqual(body.Items[0], raw) {
		t.Fatalf("body %s", seller.bodies[0])
	}
	seller.onImport = nil
	if plan.Batch == nil || plan.Batch.Imported != 1 {
		t.Fatalf("batch %#v", plan.Batch)
	}
	got, _ := uc.Detail(context.Background(), 1, detail.SKU.ID)
	if got.SKU.PublishStatus != PublishImported || got.SKU.PlatformStatus != PlatformSellable {
		t.Fatalf("after import %s %s %s", got.SKU.PublishStatus, got.SKU.PlatformStatus, got.SKU.PlatformReason)
	}
	if shops.currency != "CNY" {
		t.Fatalf("currency %s", shops.currency)
	}
	again, err := uc.Publish(context.Background(), PublishInput{
		ShopID: 1, Execute: true, IdempotencyKey: "op-1",
		Items: []PublishPick{{ID: detail.SKU.ID, Version: detail.Version}},
	})
	if err != nil || again.Batch == nil || again.Batch.ID != plan.Batch.ID || len(seller.bodies) != 1 {
		t.Fatalf("idempotent %#v bodies %d err %v", again, len(seller.bodies), err)
	}
	if _, err := uc.Publish(context.Background(), PublishInput{
		ShopID: 1, Execute: true, IdempotencyKey: "op-1",
		Items: []PublishPick{{ID: detail.SKU.ID, Version: detail.Version + 9}},
	}); err == nil || !strings.Contains(err.Error(), "另一组") {
		t.Fatalf("conflict %v", err)
	}

	second, _ := repo.FindOffer(context.Background(), 1, "30-2")
	third, _ := repo.FindOffer(context.Background(), 1, "30-3")
	fourth, _ := repo.FindOffer(context.Background(), 1, "30-4")
	for _, sku := range []*ListingSKU{second, third} {
		item, _ := json.Marshal(validItem(sku.OfferID, "https://cdn.example.com/"+sku.OfferID+".jpg"))
		if _, err := uc.SaveVersion(context.Background(), 1, sku.ID, sku.CurrentVersion, item); err != nil {
			t.Fatal(err)
		}
	}
	badItem, _ := json.Marshal(validItem("30-4-FAIL", "https://cdn.example.com/bad.jpg"))
	var obj map[string]any
	_ = json.Unmarshal(badItem, &obj)
	obj["offer_id"] = "30-4"
	badItem, _ = json.Marshal(obj)
	savedBad, err := uc.SaveVersion(context.Background(), 1, fourth.ID, fourth.CurrentVersion, badItem)
	if err != nil {
		t.Fatal(err)
	}
	seller.fail = map[string]bool{"30-4": true}
	fresh2, _ := repo.GetSKU(context.Background(), 1, second.ID)
	fresh3, _ := repo.GetSKU(context.Background(), 1, third.ID)
	mixed, err := uc.Publish(context.Background(), PublishInput{
		ShopID: 1, Execute: true, IdempotencyKey: "op-mix", OnlyValid: true,
		Items: []PublishPick{
			{ID: fresh2.ID, Version: fresh2.CurrentVersion},
			{ID: fresh3.ID, Version: fresh3.CurrentVersion},
			{ID: savedBad.SKU.ID, Version: savedBad.Version},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if mixed.Batch == nil || mixed.Batch.Imported != 2 || mixed.Batch.Failed != 1 {
		t.Fatalf("partial %#v", mixed.Batch)
	}
	failed, _ := uc.Detail(context.Background(), 1, fourth.ID)
	if failed.SKU.PublishStatus != PublishFailed || !strings.Contains(failed.SKU.LastError, "primary_image") {
		t.Fatalf("fail row %s %s", failed.SKU.PublishStatus, failed.SKU.LastError)
	}
	before := len(seller.bodies)
	retry, err := uc.Publish(context.Background(), PublishInput{
		ShopID: 1, Execute: true, IdempotencyKey: "op-retry",
		Items: []PublishPick{{ID: fourth.ID, Version: failed.Version}},
	})
	if err != nil {
		t.Fatal(err)
	}
	var retryBody struct {
		Items []map[string]any `json:"items"`
	}
	_ = json.Unmarshal(seller.bodies[len(seller.bodies)-1], &retryBody)
	if len(seller.bodies) == before || len(retryBody.Items) != 1 || retryBody.Items[0]["offer_id"] != "30-4" {
		t.Fatalf("retry body %s", seller.bodies[len(seller.bodies)-1])
	}
	_ = retry

	newerRaw, _ := json.Marshal(validItem("30-1", "https://cdn.example.com/30-1-v2.jpg"))
	newer, err := uc.SaveVersion(context.Background(), 1, detail.SKU.ID, got.Version, newerRaw)
	if err != nil || newer.SKU.PublishStatus != PublishReady {
		t.Fatalf("newer %v %#v", err, newer)
	}
	uc.poll(context.Background(), 1, plan.Batch.ID)
	after, _ := uc.Detail(context.Background(), 1, detail.SKU.ID)
	if after.SKU.PublishStatus != PublishReady || after.SKU.CurrentVersion != newer.Version {
		t.Fatalf("late result clobbered %s v%d", after.SKU.PublishStatus, after.SKU.CurrentVersion)
	}
	if after.SKU.LastImportedVersion != got.Version || after.SKU.PlatformStatus != PlatformSellable {
		t.Fatalf("online version %d %s", after.SKU.LastImportedVersion, after.SKU.PlatformStatus)
	}

	filtered, err := uc.List(context.Background(), ListingFilter{ShopID: 1, Mode: "offer", Query: "30-", Publish: []string{PublishFailed}})
	if err != nil {
		t.Fatal(err)
	}
	if filtered.Total != 1 || filtered.Rows[0].OfferID != "30-4" {
		t.Fatalf("failed filter %#v", filtered.Rows)
	}
	cleared, err := uc.List(context.Background(), ListingFilter{ShopID: 1, Mode: "master", Query: "30"})
	if err != nil || cleared.Total != 4 {
		t.Fatalf("cleared %#v %v", cleared, err)
	}
}

func TestPublishChunksAndUnknown(t *testing.T) {
	repo := newMemListings()
	shops := &staticShop{view: ShopView{ID: 7, Name: "批量"}, cred: OzonCred{ClientID: "7", APIKey: "k"}}
	product, err := repo.SaveProduct(context.Background(), &ListingProduct{ShopID: 7, TaskID: 9, MasterSKU: "batch", DescriptionCategoryID: 17028922, TypeID: 97062518, Attributes: json.RawMessage(`[]`)})
	if err != nil {
		t.Fatal(err)
	}
	picks := make([]PublishPick, 0, 101)
	for i := 1; i <= 101; i++ {
		offer := "B-" + pad3(i)
		item, _ := json.Marshal(validItem(offer, "https://cdn.example.com/"+offer+".jpg"))
		sku, err := repo.CreateSKU(context.Background(), &ListingSKU{
			ShopID: 7, ProductID: product.ID, OfferID: offer, MasterSKU: "batch", CurrentVersion: 1,
			PublishStatus: PublishReady, PlatformStatus: PlatformUnread, CurrencyCode: "CNY", Price: "10", TitleRU: offer,
		}, &ListingRevision{Version: 1, Source: "manual", CodexItem: item, ItemJSON: item, Valid: true}, nil)
		if err != nil {
			t.Fatal(err)
		}
		picks = append(picks, PublishPick{ID: sku.ID, Version: 1})
	}
	seller := &scriptedSeller{}
	uc := NewListingUsecase(repo, shops, NewMockRunner(), storage.None{}, seller, inlineSched{}, discardLogger())
	plan, err := uc.Publish(context.Background(), PublishInput{ShopID: 7, Execute: true, IdempotencyKey: "bulk", Items: picks})
	if err != nil {
		t.Fatal(err)
	}
	if len(seller.bodies) != 2 || plan.Batch == nil || plan.Batch.Imported != 101 || len(plan.Batch.Requests) != 2 {
		t.Fatalf("chunks calls %d imported %#v", len(seller.bodies), plan.Batch)
	}
	var first, second struct {
		Items []json.RawMessage `json:"items"`
	}
	_ = json.Unmarshal(seller.bodies[0], &first)
	_ = json.Unmarshal(seller.bodies[1], &second)
	if len(first.Items) != 100 || len(second.Items) != 1 || plan.Batch.Requests[0].TaskID == "" || plan.Batch.Requests[1].TaskID == "" || plan.Batch.Requests[0].TaskID == plan.Batch.Requests[1].TaskID {
		t.Fatalf("sizes %d %d tasks %s %s", len(first.Items), len(second.Items), plan.Batch.Requests[0].TaskID, plan.Batch.Requests[1].TaskID)
	}

	unknownSeller := &scriptedSeller{unknown: true}
	uc2 := NewListingUsecase(repo, shops, NewMockRunner(), storage.None{}, unknownSeller, inlineSched{}, discardLogger())
	sku, _ := repo.FindOffer(context.Background(), 7, "B-001")
	item, _ := json.Marshal(validItem("B-001", "https://cdn.example.com/B-001.jpg"))
	saved, err := uc2.SaveVersion(context.Background(), 7, sku.ID, sku.CurrentVersion, item)
	if err != nil {
		t.Fatal(err)
	}
	unknownPlan, err := uc2.Publish(context.Background(), PublishInput{
		ShopID: 7, Execute: true, IdempotencyKey: "unknown-1",
		Items: []PublishPick{{ID: sku.ID, Version: saved.Version}},
	})
	if err != nil {
		t.Fatal(err)
	}
	row, _ := repo.GetSKU(context.Background(), 7, sku.ID)
	if row.PublishStatus != PublishVerify || unknownPlan.Batch == nil || unknownPlan.Batch.Requests[0].TaskID != "" {
		t.Fatalf("unknown %s task %v", row.PublishStatus, unknownPlan.Batch)
	}
}

func TestStockSpacingAndRejection(t *testing.T) {
	repo := newMemListings()
	shops := &staticShop{view: ShopView{ID: 3, Name: "库存"}, cred: OzonCred{ClientID: "3", APIKey: "k"}}
	product, _ := repo.SaveProduct(context.Background(), &ListingProduct{ShopID: 3, MasterSKU: "s", TaskID: 1})
	item, _ := json.Marshal(validItem("S-1", "https://cdn.example.com/s.jpg"))
	sku, err := repo.CreateSKU(context.Background(), &ListingSKU{
		ShopID: 3, ProductID: product.ID, OfferID: "S-1", PublishStatus: PublishImported, OzonProductID: 55, CurrentVersion: 1,
	}, &ListingRevision{Version: 1, ItemJSON: item, CodexItem: item, Valid: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	seller := &scriptedSeller{}
	uc := NewListingUsecase(repo, shops, NewMockRunner(), storage.None{}, seller, inlineSched{}, discardLogger())
	if _, err := uc.UpdateStock(context.Background(), 3, sku.ID, 20605650762000, 4); err != nil {
		t.Fatal(err)
	}
	if _, err := uc.UpdateStock(context.Background(), 3, sku.ID, 20605650762000, 5); err == nil || !strings.Contains(err.Error(), "30 秒") {
		t.Fatalf("spacing %v", err)
	}
	seller.rejectStock = true
	repo.stocks[0].SyncedAt = ptrTime(time.Now().Add(-time.Minute))
	if _, err := uc.UpdateStock(context.Background(), 3, sku.ID, 20605650762000, 6); err == nil || !strings.Contains(err.Error(), "TOO_MANY_REQUESTS") {
		t.Fatalf("reject %v", err)
	}
}

func sampleTask(shop int, master string, skus []SubSKU) *Task {
	draft, _ := json.Marshal(TextDraft{TitleRU: "Товар " + master, SubSKUs: skus})
	items := make([]VisualItem, 0, len(skus))
	for _, sku := range skus {
		items = append(items, VisualItem{OfferID: sku.OfferID, PrimaryImage: "images/" + sku.OfferID + ".jpg"})
	}
	plan, _ := json.Marshal(VisualPlan{Items: items})
	attrs, _ := json.Marshal([]map[string]any{
		{"id": int64(85), "name": "品牌", "is_required": true, "dictionary_id": int64(85)},
		{"id": int64(10300), "name": "安装方式", "is_required": true},
	})
	return &Task{
		ID: 11, ShopID: shop, MasterSKU: master, DescriptionCategoryID: 17028922, TypeID: 97062518,
		TypeName: "车载手机支架", TypePath: []string{"汽车用品", "车载手机支架"}, Status: StatusCompleted,
		SubSKUs: skus, TextDraft: draft, VisualPlan: plan, Attributes: attrs, ZipLocalPath: tDir(),
	}
}

func tDir() string { return "/tmp/ozon-task" }

func validItem(offer, image string) map[string]any {
	return map[string]any{
		"offer_id": offer, "description_category_id": 17028922, "type_id": 97062518,
		"name": "Товар " + offer, "price": "79.00", "currency_code": "CNY",
		"depth": 120, "width": 80, "height": 40, "dimension_unit": "mm", "weight": 180, "weight_unit": "g",
		"primary_image": image, "images": []any{},
		"attributes": []any{
			map[string]any{"id": 85, "values": []any{map[string]any{"dictionary_value_id": 5055881, "value": "无品牌"}}},
			map[string]any{"id": 10300, "values": []any{map[string]any{"value": "出风口"}}},
		},
	}
}

func pad3(n int) string {
	s := ""
	if n < 10 {
		s = "00"
	} else if n < 100 {
		s = "0"
	}
	return s + itoa(n)
}

func ptrTime(t time.Time) *time.Time { return &t }

type staticShop struct {
	view     ShopView
	cred     OzonCred
	currency string
}

func (s *staticShop) Get(context.Context, int) (ShopView, error) {
	view := s.view
	if s.currency != "" {
		code := s.currency
		view.CurrencyCode = &code
	}
	return view, nil
}
func (s *staticShop) Credentials(context.Context, int) (OzonCred, error) { return s.cred, nil }
func (s *staticShop) RememberCurrency(_ context.Context, _ int, code string) error {
	if s.currency == "" {
		s.currency = strings.ToUpper(code)
	}
	return nil
}

type scriptedSeller struct {
	mu          sync.Mutex
	bodies      [][]byte
	fail        map[string]bool
	unknown     bool
	rejectStock bool
	onImport    func()
	tasks       map[string][]ImportOffer
	next        int
}

func (s *scriptedSeller) ImportProducts(_ context.Context, _ OzonCred, body json.RawMessage) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.onImport != nil {
		s.onImport()
	}
	if s.unknown {
		return "", &AcceptUnknownError{Msg: "提交超时，平台是否受理未知"}
	}
	var parsed struct {
		Items []struct {
			OfferID string `json:"offer_id"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &parsed); err != nil {
		return "", err
	}
	s.bodies = append(s.bodies, append([]byte(nil), body...))
	s.next++
	id := itoa(s.next)
	if s.tasks == nil {
		s.tasks = map[string][]ImportOffer{}
	}
	for i, item := range parsed.Items {
		offer := ImportOffer{OfferID: item.OfferID, Status: "imported", ProductID: int64(1000 + i)}
		if s.fail[item.OfferID] {
			offer.Status = "failed"
			offer.ProductID = 0
			offer.Errors = []map[string]any{{"code": "INVALID_IMAGE", "message": "Не удалось загрузить изображение", "field": "primary_image", "description": "URL недоступен"}}
		}
		s.tasks[id] = append(s.tasks[id], offer)
	}
	return id, nil
}

func (s *scriptedSeller) ImportInfo(_ context.Context, _ OzonCred, taskID string) ([]ImportOffer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]ImportOffer(nil), s.tasks[taskID]...), nil
}

func (s *scriptedSeller) ProductInfo(_ context.Context, _ OzonCred, offerIDs []string) ([]PlatformCard, error) {
	out := make([]PlatformCard, 0, len(offerIDs))
	for _, offer := range offerIDs {
		if s.fail[offer] {
			out = append(out, PlatformCard{OfferID: offer, Status: "failed", Moderate: "declined", Description: "主图无法打开"})
			continue
		}
		out = append(out, PlatformCard{OfferID: offer, ProductID: 1000, SKU: 700, Status: "price_sent", Moderate: "approved", Name: "Продается"})
	}
	return out, nil
}

func (s *scriptedSeller) UpdateStocks(_ context.Context, _ OzonCred, lines []StockLine) ([]StockLineResult, error) {
	out := make([]StockLineResult, 0, len(lines))
	for _, line := range lines {
		if s.rejectStock {
			out = append(out, StockLineResult{OfferID: line.OfferID, WarehouseID: line.WarehouseID, Updated: false, Errors: []map[string]any{{"code": "TOO_MANY_REQUESTS", "message": "Stock is updated too frequently"}}})
			continue
		}
		out = append(out, StockLineResult{OfferID: line.OfferID, ProductID: line.ProductID, WarehouseID: line.WarehouseID, Updated: true})
	}
	return out, nil
}

type memListings struct {
	mu       sync.Mutex
	next     int
	products map[int]*ListingProduct
	skus     map[int]*ListingSKU
	revs     map[int][]ListingRevision
	media    map[int][]ListingMedia
	batches  map[int]*PublishBatch
	byKey    map[string]int
	stocks   []WarehouseStock
}

func newMemListings() *memListings {
	return &memListings{
		next: 1, products: map[int]*ListingProduct{}, skus: map[int]*ListingSKU{},
		revs: map[int][]ListingRevision{}, media: map[int][]ListingMedia{}, batches: map[int]*PublishBatch{}, byKey: map[string]int{},
	}
}

func (m *memListings) id() int { m.next++; return m.next - 1 }

func (m *memListings) SaveProduct(_ context.Context, p *ListingProduct) (*ListingProduct, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *p
	cp.ID = m.id()
	m.products[cp.ID] = &cp
	return &cp, nil
}
func (m *memListings) FindProduct(_ context.Context, shopID, taskID int, master string) (*ListingProduct, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range m.products {
		if p.ShopID == shopID && p.TaskID == taskID && p.MasterSKU == master {
			cp := *p
			return &cp, nil
		}
	}
	return nil, nil
}
func (m *memListings) GetProduct(_ context.Context, shopID, id int) (*ListingProduct, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.products[id]
	if p == nil || p.ShopID != shopID {
		return nil, nil
	}
	cp := *p
	return &cp, nil
}
func (m *memListings) FindOffer(_ context.Context, shopID int, offer string) (*ListingSKU, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, sku := range m.skus {
		if sku.ShopID == shopID && sku.OfferID == offer {
			return m.copySKU(sku), nil
		}
	}
	return nil, nil
}
func (m *memListings) CreateSKU(_ context.Context, sku *ListingSKU, rev *ListingRevision, media []ListingMedia) (*ListingSKU, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cp := *sku
	cp.ID = m.id()
	if cp.CurrentVersion == 0 {
		cp.CurrentVersion = 1
	}
	m.skus[cp.ID] = &cp
	rev.ID = m.id()
	rev.SKUID = cp.ID
	rev.ShopID = cp.ShopID
	if rev.Version == 0 {
		rev.Version = 1
	}
	m.revs[cp.ID] = []ListingRevision{*rev}
	for _, item := range media {
		item.ID = m.id()
		item.SKUID = cp.ID
		m.media[cp.ID] = append(m.media[cp.ID], item)
	}
	return m.copySKU(&cp), nil
}
func (m *memListings) GetSKU(_ context.Context, shopID, id int) (*ListingSKU, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	sku := m.skus[id]
	if sku == nil || sku.ShopID != shopID {
		return nil, notFound("SKU 不存在。")
	}
	return m.copySKU(sku), nil
}
func (m *memListings) ListSKUs(_ context.Context, f ListingFilter) ([]ListingSKU, int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var rows []ListingSKU
	for id := m.next; id >= 1; id-- {
		sku := m.skus[id]
		if sku == nil || !MatchListing(*sku, f) {
			continue
		}
		rows = append(rows, *m.copySKU(sku))
	}
	total := len(rows)
	page, size := f.Page, f.PageSize
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	start := (page - 1) * size
	if start > len(rows) {
		start = len(rows)
	}
	end := start + size
	if end > len(rows) {
		end = len(rows)
	}
	return rows[start:end], total, nil
}
func (m *memListings) ListRevisions(_ context.Context, shopID, skuID int) ([]ListingRevision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []ListingRevision
	for _, rev := range m.revs[skuID] {
		if rev.ShopID == shopID || rev.ShopID == 0 {
			cp := rev
			out = append(out, cp)
		}
	}
	return out, nil
}
func (m *memListings) GetRevision(_ context.Context, shopID, skuID, version int) (*ListingRevision, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, rev := range m.revs[skuID] {
		if rev.Version == version && (rev.ShopID == shopID || rev.ShopID == 0) {
			cp := rev
			return &cp, nil
		}
	}
	return nil, nil
}
func (m *memListings) AddRevision(_ context.Context, sku *ListingSKU, rev *ListingRevision) (*ListingSKU, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	current := m.skus[sku.ID]
	if current == nil {
		return nil, notFound("SKU 不存在。")
	}
	*current = *sku
	rev.ID = m.id()
	rev.SKUID = sku.ID
	m.revs[sku.ID] = append(m.revs[sku.ID], *rev)
	return m.copySKU(current), nil
}
func (m *memListings) ListMedia(_ context.Context, skuID int) ([]ListingMedia, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]ListingMedia(nil), m.media[skuID]...), nil
}
func (m *memListings) SaveMedia(_ context.Context, row *ListingMedia) (*ListingMedia, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	rows := m.media[row.SKUID]
	for i := range rows {
		if rows[i].ID == row.ID {
			rows[i] = *row
			m.media[row.SKUID] = rows
			cp := rows[i]
			return &cp, nil
		}
	}
	row.ID = m.id()
	m.media[row.SKUID] = append(rows, *row)
	cp := *row
	return &cp, nil
}
func (m *memListings) LookupBatch(_ context.Context, shopID int, key string) (*PublishBatch, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := m.byKey[keyOf(shopID, key)]
	if id == 0 {
		return nil, nil
	}
	return cloneBatch(m.batches[id]), nil
}
func (m *memListings) CreateBatch(_ context.Context, batch *PublishBatch) (*PublishBatch, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.byKey[keyOf(batch.ShopID, batch.IdempotencyKey)] != 0 {
		return nil, &Error{Status: 409, Reason: "IDEMPOTENCY_CONFLICT", Message: "这个操作号已经用于另一组 SKU。请重新发起。"}
	}
	cp := *batch
	cp.ID = m.id()
	cp.CreatedAt = time.Now().UTC()
	for i := range cp.Requests {
		cp.Requests[i].ID = m.id()
		cp.Requests[i].BatchID = cp.ID
		for j := range cp.Requests[i].Items {
			item := &cp.Requests[i].Items[j]
			sku := m.skus[item.SKUID]
			if sku == nil || sku.CurrentVersion != item.Version {
				return nil, versionConflict()
			}
			sku.PublishStatus = PublishSubmitting
			item.ID = m.id()
			item.RequestID = cp.Requests[i].ID
			item.BatchID = cp.ID
		}
	}
	m.batches[cp.ID] = &cp
	m.byKey[keyOf(cp.ShopID, cp.IdempotencyKey)] = cp.ID
	return cloneBatch(&cp), nil
}
func (m *memListings) GetBatch(_ context.Context, shopID, id int) (*PublishBatch, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	batch := m.batches[id]
	if batch == nil || batch.ShopID != shopID {
		return nil, notFound("批次不存在。")
	}
	return cloneBatch(batch), nil
}
func (m *memListings) SaveBatch(_ context.Context, batch *PublishBatch, patches []SKUPatch) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if batch != nil && batch.ID > 0 {
		m.batches[batch.ID] = cloneBatch(batch)
	}
	for _, patch := range patches {
		sku := m.skus[patch.ID]
		if sku == nil {
			continue
		}
		if patch.PublishStatus != nil && (patch.ExpectVersion == 0 || sku.CurrentVersion == patch.ExpectVersion) {
			sku.PublishStatus = *patch.PublishStatus
			if patch.LastError != nil {
				sku.LastError = *patch.LastError
			}
		}
		if patch.LastImportedVersion != nil && sku.LastImportedVersion < *patch.LastImportedVersion {
			sku.LastImportedVersion = *patch.LastImportedVersion
		}
		if patch.PlatformStatus != nil {
			sku.PlatformStatus = *patch.PlatformStatus
		}
		if patch.PlatformReason != nil {
			sku.PlatformReason = *patch.PlatformReason
		}
		if patch.OzonProductID != nil && *patch.OzonProductID > 0 {
			sku.OzonProductID = *patch.OzonProductID
		}
		if patch.OzonSKU != nil {
			sku.OzonSKU = *patch.OzonSKU
		}
	}
	return nil
}
func (m *memListings) ListInflight(context.Context) ([]*PublishBatch, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []*PublishBatch
	for _, batch := range m.batches {
		if batch.Status != "done" {
			out = append(out, cloneBatch(batch))
		}
	}
	return out, nil
}
func (m *memListings) SaveStock(_ context.Context, row *WarehouseStock) (*WarehouseStock, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i := range m.stocks {
		if m.stocks[i].SKUID == row.SKUID && m.stocks[i].WarehouseID == row.WarehouseID {
			row.ID = m.stocks[i].ID
			m.stocks[i] = *row
			cp := m.stocks[i]
			return &cp, nil
		}
	}
	row.ID = m.id()
	m.stocks = append(m.stocks, *row)
	cp := *row
	return &cp, nil
}
func (m *memListings) ListStocks(_ context.Context, shopID, skuID int) ([]WarehouseStock, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var out []WarehouseStock
	for _, row := range m.stocks {
		if row.ShopID == shopID && row.SKUID == skuID {
			out = append(out, row)
		}
	}
	return out, nil
}
func (m *memListings) ListCompletedTasks(context.Context) ([]*Task, error) { return nil, nil }

func (m *memListings) copySKU(sku *ListingSKU) *ListingSKU {
	cp := *sku
	if p := m.products[sku.ProductID]; p != nil {
		cp.TypeID = p.TypeID
		cp.DescriptionCategory = p.DescriptionCategoryID
		cp.Attributes = p.Attributes
		cp.SupplierURL = p.SupplierURL
		cp.SessionID = p.CodexSessionID
	}
	return &cp
}

func cloneBatch(batch *PublishBatch) *PublishBatch {
	if batch == nil {
		return nil
	}
	cp := *batch
	cp.Requests = append([]PublishRequest(nil), batch.Requests...)
	for i := range cp.Requests {
		cp.Requests[i].Items = append([]PublishItem(nil), batch.Requests[i].Items...)
		cp.Requests[i].Body = append(json.RawMessage(nil), batch.Requests[i].Body...)
	}
	return &cp
}

func keyOf(shop int, key string) string { return itoa(shop) + ":" + key }
