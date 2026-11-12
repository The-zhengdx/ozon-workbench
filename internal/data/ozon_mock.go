package data

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"sync"

	"ozon-workbench/internal/biz"
	"ozon-workbench/internal/catalog"
)

// mockTransport answers Seller API calls in the same shapes as production, without leaving the machine.
type mockTransport struct{}

func (mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	body, _ := io.ReadAll(req.Body)
	if req.Body != nil {
		req.Body.Close()
	}
	clientID := req.Header.Get("Client-Id")
	apiKey := req.Header.Get("Api-Key")
	if clientID == "" || apiKey == "" {
		return mockJSON(http.StatusUnauthorized, map[string]any{
			"code":    16,
			"message": "Client-Id and Api-Key headers are required",
		}), nil
	}
	if clientID == "invalid" || apiKey == "invalid" {
		return mockJSON(http.StatusForbidden, map[string]any{
			"code":    7,
			"message": "Invalid Api-Key, please check the key and try again",
		}), nil
	}
	path := req.URL.Path
	switch {
	case strings.HasSuffix(path, "/v2/warehouse/list"):
		return mockWarehouses(body), nil
	case strings.HasSuffix(path, "/v1/description-category/tree"):
		return mockTree(body), nil
	case strings.HasSuffix(path, "/v1/description-category/attribute/values/search"):
		return mockValueSearch(body), nil
	case strings.HasSuffix(path, "/v1/description-category/attribute/values"):
		return mockValues(body), nil
	case strings.HasSuffix(path, "/v1/description-category/attribute"):
		return mockAttributes(body), nil
	case strings.HasSuffix(path, "/v3/product/import"):
		return mockProductImport(body), nil
	case strings.HasSuffix(path, "/v1/product/import/info"):
		return mockImportInfo(body), nil
	case strings.HasSuffix(path, "/v3/product/info/list"):
		return mockProductInfo(body), nil
	case strings.HasSuffix(path, "/v2/products/stocks"):
		return mockStocks(body), nil
	default:
		return mockJSON(http.StatusNotFound, map[string]any{"code": 5, "message": "mock 未实现该 Ozon 方法: " + path}), nil
	}
}

func mockWarehouses(body []byte) *http.Response {
	var req struct {
		Limit  int    `json:"limit"`
		Cursor string `json:"cursor"`
	}
	_ = json.Unmarshal(body, &req)
	if req.Limit < 1 || req.Limit > 200 {
		return mockJSON(http.StatusBadRequest, map[string]any{"code": 3, "message": "limit 必须在 1 到 200 之间"})
	}
	samples := biz.SampleWarehouses()
	all := make([]map[string]any, len(samples))
	for i, item := range samples {
		all[i] = map[string]any{
			"warehouse_id":   item.WarehouseID,
			"name":           item.Name,
			"status":         item.Status,
			"is_rfbs":        item.IsRFBS,
			"warehouse_type": item.WarehouseType,
		}
	}
	start := 0
	if req.Cursor != "" {
		var n int
		for _, r := range req.Cursor {
			if r < '0' || r > '9' {
				n = 0
				break
			}
			n = n*10 + int(r-'0')
		}
		start = n
	}
	if start > len(all) {
		start = len(all)
	}
	end := start + req.Limit
	if end > len(all) {
		end = len(all)
	}
	next := ""
	hasNext := end < len(all)
	if hasNext {
		next = itoa(end)
	}
	return mockJSON(http.StatusOK, map[string]any{
		"warehouses": all[start:end],
		"cursor":     next,
		"has_next":   hasNext,
	})
}

func mockTree(body []byte) *http.Response {
	var req struct {
		Language string `json:"language"`
	}
	_ = json.Unmarshal(body, &req)
	if req.Language == "" {
		req.Language = "DEFAULT"
	}
	return mockJSON(http.StatusOK, map[string]any{"result": catalog.ExampleTree(req.Language)})
}

func mockAttributes(body []byte) *http.Response {
	var req struct {
		TypeID   int64  `json:"type_id"`
		Language string `json:"language"`
	}
	_ = json.Unmarshal(body, &req)
	if req.Language == "" {
		req.Language = "DEFAULT"
	}
	return mockJSON(http.StatusOK, map[string]any{"result": catalog.ExampleAttributes(req.TypeID, req.Language)})
}

func mockValues(body []byte) *http.Response {
	var req struct {
		AttributeID int64  `json:"attribute_id"`
		LastValueID int64  `json:"last_value_id"`
		Limit       int    `json:"limit"`
		Language    string `json:"language"`
	}
	_ = json.Unmarshal(body, &req)
	entries, ok := catalog.ExampleDictionary(req.AttributeID)
	if !ok {
		return mockJSON(http.StatusBadRequest, map[string]any{"code": 3, "message": "该属性没有字典"})
	}
	if req.Limit < 1 || req.Limit > 2000 {
		return mockJSON(http.StatusBadRequest, map[string]any{"code": 3, "message": "limit 必须在 1 到 2000 之间"})
	}
	start := 0
	if req.LastValueID > 0 {
		start = len(entries)
		for i, entry := range entries {
			if entry.ID == req.LastValueID {
				start = i + 1
				break
			}
		}
	}
	end := start + req.Limit
	if end > len(entries) {
		end = len(entries)
	}
	page := entries[start:end]
	return mockJSON(http.StatusOK, map[string]any{
		"result":   page,
		"has_next": end < len(entries),
	})
}

func mockValueSearch(body []byte) *http.Response {
	var req struct {
		AttributeID int64  `json:"attribute_id"`
		Value       string `json:"value"`
		Limit       int    `json:"limit"`
	}
	_ = json.Unmarshal(body, &req)
	if len([]rune(strings.TrimSpace(req.Value))) < 2 {
		return mockJSON(http.StatusBadRequest, map[string]any{"code": 3, "message": "value 至少 2 个字符"})
	}
	if req.Limit < 1 || req.Limit > 100 {
		return mockJSON(http.StatusBadRequest, map[string]any{"code": 3, "message": "limit 必须在 1 到 100 之间"})
	}
	entries, ok := catalog.ExampleDictionary(req.AttributeID)
	if !ok {
		return mockJSON(http.StatusBadRequest, map[string]any{"code": 3, "message": "该属性没有字典"})
	}
	term := strings.ToLower(req.Value)
	var matched []catalog.DictEntry
	for _, entry := range entries {
		if strings.Contains(strings.ToLower(entry.Value), term) || strings.Contains(strings.ToLower(entry.Info), term) {
			matched = append(matched, entry)
		}
	}
	if len(matched) > req.Limit {
		matched = matched[:req.Limit]
	}
	if matched == nil {
		matched = []catalog.DictEntry{}
	}
	return mockJSON(http.StatusOK, map[string]any{"result": matched})
}

type mockOffer struct {
	OfferID string
	Fail    bool
}

var (
	mockImportMu  sync.Mutex
	mockImportSeq int64
	mockImports   = map[int64][]mockOffer{}
)

func mockProductImport(body []byte) *http.Response {
	var req struct {
		Items []struct {
			OfferID string `json:"offer_id"`
		} `json:"items"`
	}
	_ = json.Unmarshal(body, &req)
	if len(req.Items) == 0 || len(req.Items) > 100 {
		return mockJSON(http.StatusBadRequest, map[string]any{"code": 3, "message": "items 必须为 1 到 100 个"})
	}
	mockImportMu.Lock()
	mockImportSeq++
	id := mockImportSeq
	offers := make([]mockOffer, len(req.Items))
	for i, item := range req.Items {
		offers[i] = mockOffer{OfferID: item.OfferID, Fail: strings.Contains(item.OfferID, "FAIL")}
	}
	mockImports[id] = offers
	mockImportMu.Unlock()
	return mockJSON(http.StatusOK, map[string]any{"result": map[string]any{"task_id": id}})
}

func mockImportInfo(body []byte) *http.Response {
	var req struct {
		TaskID int64 `json:"task_id"`
	}
	_ = json.Unmarshal(body, &req)
	mockImportMu.Lock()
	offers := mockImports[req.TaskID]
	mockImportMu.Unlock()
	if offers == nil {
		return mockJSON(http.StatusNotFound, map[string]any{"code": 5, "message": "task 不存在"})
	}
	items := make([]map[string]any, 0, len(offers))
	for i, offer := range offers {
		if offer.Fail {
			items = append(items, map[string]any{
				"offer_id": offer.OfferID, "product_id": 0, "status": "failed",
				"errors": []map[string]any{{
					"code": "INVALID_IMAGE", "message": "Не удалось загрузить изображение",
					"field": "primary_image", "description": "URL недоступен",
				}},
			})
			continue
		}
		items = append(items, map[string]any{
			"offer_id": offer.OfferID, "product_id": 800000 + int64(i) + req.TaskID, "status": "imported", "errors": []any{},
		})
	}
	return mockJSON(http.StatusOK, map[string]any{"result": map[string]any{"items": items, "total": len(items)}})
}

func mockProductInfo(body []byte) *http.Response {
	var req struct {
		OfferID []string `json:"offer_id"`
	}
	_ = json.Unmarshal(body, &req)
	mockImportMu.Lock()
	defer mockImportMu.Unlock()
	items := make([]map[string]any, 0, len(req.OfferID))
	for _, offer := range req.OfferID {
		fail := strings.Contains(offer, "FAIL")
		found := false
		for _, batch := range mockImports {
			for _, row := range batch {
				if row.OfferID == offer {
					fail = row.Fail
					found = true
				}
			}
		}
		if !found && !fail {
			continue
		}
		if fail {
			items = append(items, map[string]any{
				"id": 0, "offer_id": offer, "sku": 0, "is_archived": false,
				"statuses": map[string]any{
					"status": "failed", "status_failed": "图片不符合要求", "moderate_status": "declined",
					"status_name": "Не продается", "status_description": "主图无法打开", "status_tooltip": "请更换公网 HTTPS 图片",
				},
			})
			continue
		}
		items = append(items, map[string]any{
			"id": 900100, "offer_id": offer, "sku": 700100, "is_archived": false,
			"statuses": map[string]any{
				"status": "price_sent", "status_failed": "", "moderate_status": "approved",
				"status_name": "Продается", "status_description": "", "status_tooltip": "",
			},
		})
	}
	return mockJSON(http.StatusOK, map[string]any{"items": items})
}

func mockStocks(body []byte) *http.Response {
	var req struct {
		Stocks []struct {
			OfferID     string `json:"offer_id"`
			ProductID   int64  `json:"product_id"`
			Stock       int    `json:"stock"`
			WarehouseID int64  `json:"warehouse_id"`
		} `json:"stocks"`
	}
	_ = json.Unmarshal(body, &req)
	if len(req.Stocks) == 0 || len(req.Stocks) > 100 {
		return mockJSON(http.StatusBadRequest, map[string]any{"code": 3, "message": "stocks 必须为 1 到 100 对"})
	}
	result := make([]map[string]any, 0, len(req.Stocks))
	for _, line := range req.Stocks {
		if line.Stock < 0 {
			result = append(result, map[string]any{
				"offer_id": line.OfferID, "product_id": line.ProductID, "warehouse_id": line.WarehouseID, "updated": false,
				"errors": []map[string]any{{"code": "INVALID_STOCK", "message": "库存不能为负"}},
			})
			continue
		}
		result = append(result, map[string]any{
			"offer_id": line.OfferID, "product_id": line.ProductID, "warehouse_id": line.WarehouseID, "updated": true, "errors": []any{},
		})
	}
	return mockJSON(http.StatusOK, map[string]any{"result": result})
}

func mockJSON(status int, payload any) *http.Response {
	raw, _ := json.Marshal(payload)
	return &http.Response{
		StatusCode:    status,
		Status:        http.StatusText(status),
		Header:        http.Header{"Content-Type": []string{"application/json"}},
		Body:          io.NopCloser(bytes.NewReader(raw)),
		ContentLength: int64(len(raw)),
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [16]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
