// Package ozonapi 访问 Ozon Seller API。测试和 OZON_MOCK 使用 Mock，不把示例数据当成真实类目。
package ozonapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type Attribute struct {
	ID                  int64  `json:"id"`
	Name                string `json:"name"`
	Description         string `json:"description"`
	Type                string `json:"type"`
	IsRequired          bool   `json:"is_required"`
	DictionaryID        int64  `json:"dictionary_id"`
	IsCollection        bool   `json:"is_collection"`
	GroupName           string `json:"group_name"`
	ComplexIsCollection bool   `json:"complex_is_collection"`
}

type Value struct {
	ID      int64  `json:"id"`
	Value   string `json:"value"`
	Info    string `json:"info"`
	Picture string `json:"picture"`
}

type ImportRow struct {
	OfferID   string          `json:"offer_id"`
	ProductID int64           `json:"product_id"`
	Status    string          `json:"status"`
	Errors    json.RawMessage `json:"errors"`
}

type PlatformRow struct {
	OfferID    string          `json:"offer_id"`
	ProductID  int64           `json:"product_id"`
	SKU        int64           `json:"sku"`
	Moderation string          `json:"moderation_status"`
	Sale       string          `json:"sale_status"`
	Raw        json.RawMessage `json:"raw"`
}

type Client interface {
	Source() string
	Tree(ctx context.Context, language string) (json.RawMessage, error)
	Attributes(ctx context.Context, categoryID, typeID int64, language string) ([]Attribute, error)
	SearchValues(ctx context.Context, categoryID, typeID, attributeID int64, language, keyword string, limit int) ([]Value, error)
	ListValues(ctx context.Context, categoryID, typeID, attributeID int64, language string, lastValueID int64, limit int) ([]Value, int64, bool, error)
	Import(ctx context.Context, items []json.RawMessage) (int64, json.RawMessage, error)
	ImportInfo(ctx context.Context, taskID int64) ([]ImportRow, json.RawMessage, error)
	Platform(ctx context.Context, offerIDs []string) ([]PlatformRow, error)
	VerifyAccount(ctx context.Context) (string, error)
}

// Mock 记录调用次数。SearchErr / ListErr 非空时必须把错误返回给调用方，不能变成空列表。
type Mock struct {
	TreeBody         json.RawMessage
	Attrs            []Attribute
	SearchHits       []Value
	ListHits         []Value
	ListLast         int64
	ListDone         bool
	SearchErr        error
	ListErr          error
	AttrErr          error
	TreeErr          error
	Currency         string
	VerifyErr        error
	Imports          [][]json.RawMessage
	ImportTask       int64
	ImportErr        error
	Info             map[int64][]ImportRow
	InfoErr          error
	Platforms        []PlatformRow
	AttrCalls        int
	SearchCalls      int
	LastTreeLanguage string
	LastAttrLanguage string
}

func (m *Mock) Source() string { return "mock" }

func (m *Mock) Tree(_ context.Context, language string) (json.RawMessage, error) {
	m.LastTreeLanguage = language
	if m.TreeErr != nil {
		return nil, m.TreeErr
	}
	if len(m.TreeBody) == 0 {
		return json.RawMessage(`[]`), nil
	}
	return append(json.RawMessage(nil), m.TreeBody...), nil
}

func (m *Mock) Attributes(_ context.Context, _, _ int64, language string) ([]Attribute, error) {
	m.AttrCalls++
	m.LastAttrLanguage = language
	if m.AttrErr != nil {
		return nil, m.AttrErr
	}
	return append([]Attribute{}, m.Attrs...), nil
}

func (m *Mock) SearchValues(context.Context, int64, int64, int64, string, string, int) ([]Value, error) {
	m.SearchCalls++
	if m.SearchErr != nil {
		return nil, m.SearchErr
	}
	return append([]Value{}, m.SearchHits...), nil
}

func (m *Mock) ListValues(context.Context, int64, int64, int64, string, int64, int) ([]Value, int64, bool, error) {
	if m.ListErr != nil {
		return nil, 0, false, m.ListErr
	}
	last := m.ListLast
	if last == 0 && len(m.ListHits) > 0 {
		last = m.ListHits[len(m.ListHits)-1].ID
	}
	return append([]Value{}, m.ListHits...), last, m.ListDone, nil
}

func (m *Mock) Import(_ context.Context, items []json.RawMessage) (int64, json.RawMessage, error) {
	copied := append([]json.RawMessage(nil), items...)
	m.Imports = append(m.Imports, copied)
	if m.ImportErr != nil {
		return 0, nil, m.ImportErr
	}
	m.ImportTask++
	raw, _ := json.Marshal(map[string]any{"result": map[string]any{"task_id": m.ImportTask}})
	return m.ImportTask, raw, nil
}

func (m *Mock) ImportInfo(_ context.Context, taskID int64) ([]ImportRow, json.RawMessage, error) {
	if m.InfoErr != nil {
		return nil, nil, m.InfoErr
	}
	rows := append([]ImportRow{}, m.Info[taskID]...)
	raw, _ := json.Marshal(map[string]any{"result": map[string]any{"items": rows}})
	return rows, raw, nil
}

func (m *Mock) Platform(context.Context, []string) ([]PlatformRow, error) {
	return append([]PlatformRow(nil), m.Platforms...), nil
}

func (m *Mock) VerifyAccount(context.Context) (string, error) {
	if m.VerifyErr != nil {
		return "", m.VerifyErr
	}
	if m.Currency == "" {
		return "", fmt.Errorf("OZON_ACCOUNT_UNVERIFIED")
	}
	return m.Currency, nil
}

// HTTPClient 调用真实 Seller API。账户核验失败时返回错误，不用店铺默认币种冒充。
type HTTPClient struct {
	Base     string
	ClientID string
	APIKey   string
	HTTP     *http.Client
}

func NewHTTP(base, clientID, apiKey string) *HTTPClient {
	base = strings.TrimRight(strings.TrimSpace(base), "/")
	if base == "" {
		base = "https://api-seller.ozon.ru"
	}
	return &HTTPClient{Base: base, ClientID: clientID, APIKey: apiKey, HTTP: &http.Client{Timeout: 60 * time.Second}}
}

func (c *HTTPClient) Source() string { return "ozon" }

func (c *HTTPClient) Tree(ctx context.Context, language string) (json.RawMessage, error) {
	raw, err := c.post(ctx, "/v1/description-category/tree", map[string]any{"language": language})
	if err != nil {
		return nil, err
	}
	return takeResult(raw), nil
}

func (c *HTTPClient) Attributes(ctx context.Context, categoryID, typeID int64, language string) ([]Attribute, error) {
	raw, err := c.post(ctx, "/v1/description-category/attribute", map[string]any{
		"description_category_id": categoryID, "type_id": typeID, "language": language,
	})
	if err != nil {
		return nil, err
	}
	var wrap struct {
		Result []Attribute `json:"result"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return nil, err
	}
	if wrap.Result == nil {
		return []Attribute{}, nil
	}
	return wrap.Result, nil
}

func (c *HTTPClient) SearchValues(ctx context.Context, categoryID, typeID, attributeID int64, language, keyword string, limit int) ([]Value, error) {
	if limit <= 0 {
		limit = 100
	}
	raw, err := c.post(ctx, "/v1/description-category/attribute/values/search", map[string]any{
		"description_category_id": categoryID, "type_id": typeID, "attribute_id": attributeID,
		"value": keyword, "limit": limit, "language": language,
	})
	if err != nil {
		return nil, err
	}
	return decodeValues(raw)
}

func (c *HTTPClient) ListValues(ctx context.Context, categoryID, typeID, attributeID int64, language string, lastValueID int64, limit int) ([]Value, int64, bool, error) {
	if limit <= 0 {
		limit = 100
	}
	raw, err := c.post(ctx, "/v1/description-category/attribute/values", map[string]any{
		"description_category_id": categoryID, "type_id": typeID, "attribute_id": attributeID,
		"last_value_id": lastValueID, "limit": limit, "language": language,
	})
	if err != nil {
		return nil, lastValueID, false, err
	}
	values, err := decodeValues(raw)
	if err != nil {
		return nil, lastValueID, false, err
	}
	var extra struct {
		HasNext bool `json:"has_next"`
	}
	_ = json.Unmarshal(raw, &extra)
	last := lastValueID
	if len(values) > 0 {
		last = values[len(values)-1].ID
	}
	complete := !extra.HasNext && len(values) < limit
	return values, last, complete, nil
}

func (c *HTTPClient) Import(ctx context.Context, items []json.RawMessage) (int64, json.RawMessage, error) {
	payload := map[string]any{"items": items}
	raw, err := c.post(ctx, "/v3/product/import", payload)
	if err != nil {
		return 0, raw, err
	}
	var wrap struct {
		Result struct {
			TaskID int64 `json:"task_id"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return 0, raw, err
	}
	if wrap.Result.TaskID == 0 {
		return 0, raw, fmt.Errorf("Ozon 导入响应没有 task_id")
	}
	return wrap.Result.TaskID, raw, nil
}

func (c *HTTPClient) ImportInfo(ctx context.Context, taskID int64) ([]ImportRow, json.RawMessage, error) {
	raw, err := c.post(ctx, "/v1/product/import/info", map[string]any{"task_id": taskID})
	if err != nil {
		return nil, raw, err
	}
	var wrap struct {
		Result struct {
			Items []struct {
				OfferID   string          `json:"offer_id"`
				ProductID int64           `json:"product_id"`
				Status    string          `json:"status"`
				Errors    json.RawMessage `json:"errors"`
			} `json:"items"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return nil, raw, err
	}
	out := make([]ImportRow, 0, len(wrap.Result.Items))
	for _, item := range wrap.Result.Items {
		out = append(out, ImportRow{OfferID: item.OfferID, ProductID: item.ProductID, Status: item.Status, Errors: item.Errors})
	}
	return out, raw, nil
}

func (c *HTTPClient) Platform(ctx context.Context, offerIDs []string) ([]PlatformRow, error) {
	raw, err := c.post(ctx, "/v3/product/info/list", map[string]any{"offer_id": offerIDs})
	if err != nil {
		return nil, err
	}
	var wrap struct {
		Items  []map[string]any `json:"items"`
		Result struct {
			Items []map[string]any `json:"items"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return nil, err
	}
	items := wrap.Items
	if len(items) == 0 {
		items = wrap.Result.Items
	}
	out := make([]PlatformRow, 0, len(items))
	for _, item := range items {
		blob, _ := json.Marshal(item)
		row := PlatformRow{Raw: blob}
		if v, ok := item["offer_id"].(string); ok {
			row.OfferID = v
		}
		row.ProductID = num(item["id"])
		if row.ProductID == 0 {
			row.ProductID = num(item["product_id"])
		}
		row.SKU = num(item["sku"])
		row.Moderation, row.Sale = statusFields(item)
		out = append(out, row)
	}
	return out, nil
}

func (c *HTTPClient) VerifyAccount(ctx context.Context) (string, error) {
	raw, err := c.post(ctx, "/v1/seller/info", map[string]any{})
	if err != nil {
		return "", fmt.Errorf("OZON_ACCOUNT_UNVERIFIED: %w", err)
	}
	currency := findString(raw, "currency_code", "currency")
	if currency == "" {
		return "", fmt.Errorf("OZON_ACCOUNT_UNVERIFIED")
	}
	return currency, nil
}

func (c *HTTPClient) post(ctx context.Context, path string, body any) ([]byte, error) {
	payload, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.Base+path, bytes.NewReader(payload))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Client-Id", c.ClientID)
	req.Header.Set("Api-Key", c.APIKey)
	req.Header.Set("Content-Type", "application/json")
	res, err := c.HTTP.Do(req)
	if err != nil {
		return nil, fmt.Errorf("OZON_UPSTREAM_UNAVAILABLE: %w", err)
	}
	defer res.Body.Close()
	data, _ := io.ReadAll(io.LimitReader(res.Body, 8<<20))
	if res.StatusCode >= 300 {
		return data, fmt.Errorf("OZON_UPSTREAM_UNAVAILABLE: HTTP %d", res.StatusCode)
	}
	return data, nil
}

func decodeValues(raw []byte) ([]Value, error) {
	var wrap struct {
		Result []Value `json:"result"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return nil, err
	}
	if wrap.Result == nil {
		return []Value{}, nil
	}
	return wrap.Result, nil
}

func takeResult(raw []byte) json.RawMessage {
	var wrap struct {
		Result json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(raw, &wrap); err == nil && len(wrap.Result) > 0 {
		return wrap.Result
	}
	return raw
}

func num(v any) int64 {
	switch n := v.(type) {
	case float64:
		return int64(n)
	default:
		return 0
	}
}

func statusFields(item map[string]any) (string, string) {
	mod, sale := "", ""
	if statuses, ok := item["statuses"].(map[string]any); ok {
		if v, ok := statuses["status"].(string); ok {
			mod = v
		}
		if v, ok := statuses["moderate_status"].(string); ok && mod == "" {
			mod = v
		}
		if v, ok := statuses["validation_status"].(string); ok && sale == "" {
			sale = v
		}
	}
	if v, ok := item["moderation_status"].(string); ok && mod == "" {
		mod = v
	}
	if v, ok := item["sale_status"].(string); ok {
		sale = v
	}
	return mod, sale
}

func findString(raw []byte, keys ...string) string {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return ""
	}
	want := map[string]bool{}
	for _, key := range keys {
		want[key] = true
	}
	var walk func(any) string
	walk = func(node any) string {
		switch n := node.(type) {
		case map[string]any:
			for key, child := range n {
				if want[key] {
					if s, ok := child.(string); ok && s != "" {
						return s
					}
				}
			}
			for _, child := range n {
				if s := walk(child); s != "" {
					return s
				}
			}
		case []any:
			for _, child := range n {
				if s := walk(child); s != "" {
					return s
				}
			}
		}
		return ""
	}
	return walk(v)
}
