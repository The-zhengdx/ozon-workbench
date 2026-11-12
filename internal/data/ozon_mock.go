package data

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"strings"

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
	all := []map[string]any{
		{"warehouse_id": int64(20605650762000), "name": "莫斯科一号仓", "status": "created", "is_rfbs": false, "warehouse_type": "FBS"},
		{"warehouse_id": int64(20605650762001), "name": "圣彼得堡转运仓", "status": "created", "is_rfbs": true, "warehouse_type": "RFBS"},
		{"warehouse_id": int64(20605650762002), "name": "喀山备货仓", "status": "created", "is_rfbs": false, "warehouse_type": "FBS"},
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
