package biz

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func contractSample() []map[string]any {
	return []map[string]any{
		{"id": 85, "name": "品牌", "is_required": true, "dictionary_id": 85},
		{"id": 10300, "name": "安装方式", "is_required": true, "type": "String"},
		{"id": 10097, "name": "颜色名称", "is_required": false},
	}
}

func goodItem() map[string]any {
	return map[string]any{
		"offer_id":                "30-1",
		"description_category_id": 17028922,
		"type_id":                 97062518,
		"name":                    "Держатель 30",
		"price":                   "79.00",
		"currency_code":           "CNY",
		"depth":                   120,
		"width":                   80,
		"height":                  40,
		"dimension_unit":          "mm",
		"weight":                  180,
		"weight_unit":             "g",
		"primary_image":           "https://cdn.example.com/30-1.jpg",
		"images":                  []string{"https://cdn.example.com/30-1b.jpg"},
		"attributes": []map[string]any{
			{"complex_id": 0, "id": 85, "values": []map[string]any{{"dictionary_value_id": 5055881, "value": "无品牌"}}},
			{"complex_id": 0, "id": 10300, "values": []map[string]any{{"value": "出风口"}}},
		},
	}
}

func TestValidateItemAcceptsContractAndRejectsLocalMedia(t *testing.T) {
	raw, _ := json.Marshal(goodItem())
	expect := ItemExpect{OfferID: "30-1", DescriptionCategoryID: 17028922, TypeID: 97062518, Contract: contractSample()}
	if issues := ValidateItem(raw, expect); len(issues) != 0 {
		t.Fatalf("issues %v", issues)
	}
	item := goodItem()
	item["primary_image"] = "images/a.jpg"
	item["offer_id"] = "30-2"
	raw, _ = json.Marshal(item)
	issues := ValidateItem(raw, expect)
	text := strings.Join(issues, " | ")
	if !strings.Contains(text, "卖家 SKU") || !strings.Contains(text, "HTTPS") {
		t.Fatalf("issues %v", issues)
	}
	item = goodItem()
	item["primary_image"] = "https://yadi.sk/i/abc"
	delete(item, "attributes")
	raw, _ = json.Marshal(item)
	issues = ValidateItem(raw, expect)
	text = strings.Join(issues, " | ")
	if !strings.Contains(text, "分享页") || !strings.Contains(text, "品牌") || !strings.Contains(text, "安装方式") {
		t.Fatalf("issues %v", issues)
	}
	if StatusAfterEdit(issues) != PublishDraft {
		t.Fatal(StatusAfterEdit(issues))
	}
	if StatusAfterEdit(nil) != PublishReady {
		t.Fatal("ready")
	}
}

func TestBuildImportBodyAndChunks(t *testing.T) {
	grey, _ := json.Marshal(map[string]any{"offer_id": "30-1", "price": "79.00", "name": "灰"})
	blue, _ := json.Marshal(map[string]any{"offer_id": "30-2", "price": "89.00", "name": "蓝"})
	single, err := BuildImportBody([]json.RawMessage{grey})
	if err != nil {
		t.Fatal(err)
	}
	var one struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(single, &one); err != nil {
		t.Fatal(err)
	}
	if len(one.Items) != 1 || !JSONEqual(one.Items[0], grey) {
		t.Fatalf("single %s", single)
	}
	if bytesCount(single, `"items"`) != 1 {
		t.Fatalf("items key repeated %s", single)
	}
	batch, err := BuildImportBody([]json.RawMessage{grey, blue})
	if err != nil {
		t.Fatal(err)
	}
	var many struct {
		Items []json.RawMessage `json:"items"`
	}
	if err := json.Unmarshal(batch, &many); err != nil {
		t.Fatal(err)
	}
	if len(many.Items) != 2 || !JSONEqual(many.Items[0], grey) || !JSONEqual(many.Items[1], blue) {
		t.Fatalf("batch %s", batch)
	}
	items := make([]json.RawMessage, 101)
	for i := range items {
		items[i] = grey
	}
	chunks := ChunkRaw(items, MaxImportItems)
	if len(chunks) != 2 || len(chunks[0]) != 100 || len(chunks[1]) != 1 {
		t.Fatalf("chunks %d %d", len(chunks), len(chunks[0]))
	}
}

func TestApplyRemoteResultDoesNotClobberNewerVersion(t *testing.T) {
	next, ok := ApplyRemoteResult(2, 2, PublishSubmitting, RemoteImported)
	if !ok || next != PublishImported {
		t.Fatalf("current %s %v", next, ok)
	}
	next, ok = ApplyRemoteResult(2, 1, PublishReady, RemoteImported)
	if ok || next != PublishReady {
		t.Fatalf("stale %s %v", next, ok)
	}
	next, ok = ApplyRemoteResult(1, 1, PublishSubmitting, RemoteFailed)
	if !ok || next != PublishFailed {
		t.Fatalf("fail %s %v", next, ok)
	}
	next, ok = ApplyRemoteResult(1, 1, PublishSubmitting, RemoteUnknown)
	if !ok || next != PublishVerify {
		t.Fatalf("unknown %s %v", next, ok)
	}
	if Eligible(PublishDraft, 1, 0) || !Eligible(PublishFailed, 1, 0) || !Eligible(PublishImported, 3, 2) {
		t.Fatal("eligibility")
	}
	if ActionLabel(PublishImported, 3, 2) != "提交更新" || ActionLabel(PublishFailed, 2, 1) != "重试" {
		t.Fatal("labels")
	}
}

func TestMapPlatformAndImportErrors(t *testing.T) {
	status, reason := MapPlatform("failed", "declined", "", "Не продается", "主图无法打开", "", false)
	if status != PlatformUnsellable || !strings.Contains(reason, "主图") {
		t.Fatalf("%s %s", status, reason)
	}
	status, reason = MapPlatform("price_sent", "approved", "", "Продается", "", "", false)
	if status != PlatformSellable || reason != "" {
		t.Fatalf("sell %s %s", status, reason)
	}
	status, _ = MapPlatform("", "in-moderating", "", "", "", "", false)
	if status != PlatformReview {
		t.Fatal(status)
	}
	code, detail := FormatImportErrors([]map[string]any{{
		"code": "INVALID_IMAGE", "message": "Не удалось загрузить изображение", "field": "primary_image", "description": "URL недоступен",
	}})
	if code != "INVALID_IMAGE" || !strings.Contains(detail, "primary_image") || !strings.Contains(detail, "URL") {
		t.Fatalf("%s %s", code, detail)
	}
}

func TestStockInterval(t *testing.T) {
	now := time.Now()
	if StockTooSoon(time.Time{}, now) {
		t.Fatal("first update should pass")
	}
	if !StockTooSoon(now.Add(-10*time.Second), now) {
		t.Fatal("10s is too soon")
	}
	if StockTooSoon(now.Add(-31*time.Second), now) {
		t.Fatal("31s should pass")
	}
}

func TestSelectionHashStable(t *testing.T) {
	a := SelectionHash([][2]int{{2, 1}, {1, 3}})
	b := SelectionHash([][2]int{{1, 3}, {2, 1}})
	if a == "" || a != b {
		t.Fatalf("%s %s", a, b)
	}
	if SelectionHash([][2]int{{1, 4}}) == a {
		t.Fatal("different selection")
	}
}

func bytesCount(raw []byte, needle string) int {
	return strings.Count(string(raw), needle)
}
