package data

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"ozon-workbench/internal/biz"
	"ozon-workbench/internal/catalog"
)

// OzonClient calls api-seller.ozon.ru with per-shop Client-Id and Api-Key headers.
type OzonClient struct {
	baseURL    string
	httpClient *http.Client
	rps        int
	retryBase  time.Duration
	mu         sync.Mutex
	limits     map[string]*rate.Limiter
}

func NewOzonClient(baseURL string, timeout time.Duration, rps int, mock bool) *OzonClient {
	if rps <= 0 {
		rps = 10
	}
	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	transport := http.DefaultTransport
	if mock {
		transport = mockTransport{}
	}
	return &OzonClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout:   timeout,
			Transport: transport,
		},
		rps:       rps,
		retryBase: time.Second,
		limits:    map[string]*rate.Limiter{},
	}
}

func (c *OzonClient) limiter(clientID string) *rate.Limiter {
	c.mu.Lock()
	defer c.mu.Unlock()
	if lim, ok := c.limits[clientID]; ok {
		return lim
	}
	lim := rate.NewLimiter(rate.Limit(c.rps), c.rps)
	c.limits[clientID] = lim
	return lim
}

type ozonStatusError struct {
	Status  int
	Message string
}

func (e *ozonStatusError) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("Ozon HTTP %d", e.Status)
	}
	return fmt.Sprintf("Ozon HTTP %d: %s", e.Status, e.Message)
}

func (c *OzonClient) do(ctx context.Context, path string, cred biz.OzonCred, payload any, out any) error {
	if strings.TrimSpace(cred.ClientID) == "" || strings.TrimSpace(cred.APIKey) == "" {
		return fmt.Errorf("Ozon 凭证不完整")
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	var last error
	for attempt := 0; attempt < 3; attempt++ {
		if err := c.limiter(cred.ClientID).Wait(ctx); err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(raw))
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Client-Id", cred.ClientID)
		req.Header.Set("Api-Key", cred.APIKey)
		resp, err := c.httpClient.Do(req)
		if err != nil {
			return fmt.Errorf("连接 Ozon 失败: %w", err)
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		if readErr != nil {
			return readErr
		}
		if resp.StatusCode == http.StatusTooManyRequests && attempt < 2 {
			last = &ozonStatusError{Status: resp.StatusCode, Message: extractOzonMessage(body)}
			timer := time.NewTimer(c.backoff(attempt, resp.Header.Get("Retry-After")))
			select {
			case <-ctx.Done():
				timer.Stop()
				return ctx.Err()
			case <-timer.C:
			}
			continue
		}
		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return &ozonStatusError{Status: resp.StatusCode, Message: extractOzonMessage(body)}
		}
		if out == nil {
			return nil
		}
		dec := json.NewDecoder(bytes.NewReader(body))
		dec.UseNumber()
		if err := dec.Decode(out); err != nil {
			return fmt.Errorf("解析 Ozon 响应失败: %w", err)
		}
		return nil
	}
	if last != nil {
		return last
	}
	return fmt.Errorf("Ozon 请求失败")
}

func (c *OzonClient) backoff(attempt int, retryAfter string) time.Duration {
	if retryAfter != "" {
		if seconds, err := strconv.Atoi(retryAfter); err == nil && seconds > 0 {
			return time.Duration(seconds) * time.Second
		}
	}
	base := c.retryBase
	if base <= 0 {
		base = time.Second
	}
	return base * time.Duration(attempt+1)
}

func extractOzonMessage(body []byte) string {
	var rpc struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(body, &rpc) == nil && strings.TrimSpace(rpc.Message) != "" {
		return strings.TrimSpace(rpc.Message)
	}
	text := strings.TrimSpace(string(body))
	if text == "" {
		return "Ozon 返回了空的错误响应"
	}
	if len(text) > 800 {
		text = text[:800]
	}
	return text
}

// ListWarehouses pages through POST /v2/warehouse/list until has_next is false.
func (c *OzonClient) ListWarehouses(ctx context.Context, cred biz.OzonCred) ([]biz.Warehouse, error) {
	var all []biz.Warehouse
	cursor := ""
	for page := 0; page < 100; page++ {
		payload := map[string]any{"limit": 200}
		if cursor != "" {
			payload["cursor"] = cursor
		}
		var resp struct {
			Warehouses []struct {
				WarehouseID   int64  `json:"warehouse_id"`
				Name          string `json:"name"`
				Status        string `json:"status"`
				IsRFBS        bool   `json:"is_rfbs"`
				WarehouseType string `json:"warehouse_type"`
			} `json:"warehouses"`
			Cursor  string `json:"cursor"`
			HasNext bool   `json:"has_next"`
		}
		if err := c.do(ctx, "/v2/warehouse/list", cred, payload, &resp); err != nil {
			return nil, err
		}
		for _, row := range resp.Warehouses {
			all = append(all, biz.Warehouse{
				WarehouseID:   row.WarehouseID,
				Name:          row.Name,
				Status:        row.Status,
				IsRFBS:        row.IsRFBS,
				WarehouseType: row.WarehouseType,
			})
		}
		if !resp.HasNext || resp.Cursor == "" || resp.Cursor == cursor {
			break
		}
		cursor = resp.Cursor
	}
	if all == nil {
		all = []biz.Warehouse{}
	}
	return all, nil
}

// CategoryTree calls POST /v1/description-category/tree.
func (c *OzonClient) CategoryTree(ctx context.Context, cred biz.OzonCred, language string) ([]catalog.TreeNode, error) {
	var resp struct {
		Result []catalog.TreeNode `json:"result"`
	}
	if err := c.do(ctx, "/v1/description-category/tree", cred, map[string]any{"language": language}, &resp); err != nil {
		return nil, err
	}
	if len(resp.Result) == 0 {
		return nil, fmt.Errorf("Ozon 返回的类目树为空")
	}
	return resp.Result, nil
}

// Attributes calls POST /v1/description-category/attribute and keeps every returned field.
func (c *OzonClient) Attributes(ctx context.Context, cred biz.OzonCred, categoryID, typeID int64, language string) ([]map[string]any, error) {
	var resp struct {
		Result []map[string]any `json:"result"`
	}
	payload := map[string]any{
		"description_category_id": categoryID,
		"type_id":                 typeID,
		"language":                language,
	}
	if err := c.do(ctx, "/v1/description-category/attribute", cred, payload, &resp); err != nil {
		return nil, err
	}
	if resp.Result == nil {
		resp.Result = []map[string]any{}
	}
	for i := range resp.Result {
		resp.Result[i] = normalizeMap(resp.Result[i])
	}
	return resp.Result, nil
}

// AttributeValues calls POST /v1/description-category/attribute/values for one page.
func (c *OzonClient) AttributeValues(ctx context.Context, cred biz.OzonCred, categoryID, typeID, attributeID, lastValueID int64, limit int, language string) (*biz.ValuePage, error) {
	if limit < 1 {
		limit = 1
	}
	if limit > 2000 {
		limit = 2000
	}
	var resp struct {
		Result  []map[string]any `json:"result"`
		HasNext bool             `json:"has_next"`
	}
	payload := map[string]any{
		"attribute_id":            attributeID,
		"description_category_id": categoryID,
		"type_id":                 typeID,
		"language":                language,
		"last_value_id":           lastValueID,
		"limit":                   limit,
	}
	if err := c.do(ctx, "/v1/description-category/attribute/values", cred, payload, &resp); err != nil {
		return nil, err
	}
	if resp.Result == nil {
		resp.Result = []map[string]any{}
	}
	var last int64
	for i := range resp.Result {
		resp.Result[i] = normalizeMap(resp.Result[i])
		if id := int64From(resp.Result[i]["id"]); id != 0 {
			last = id
		}
	}
	return &biz.ValuePage{Values: resp.Result, HasNext: resp.HasNext, LastValueID: last}, nil
}

// SearchAttributeValues calls POST /v1/description-category/attribute/values/search.
func (c *OzonClient) SearchAttributeValues(ctx context.Context, cred biz.OzonCred, categoryID, typeID, attributeID int64, value string, limit int) ([]map[string]any, error) {
	if limit < 1 {
		limit = 1
	}
	if limit > 100 {
		limit = 100
	}
	var resp struct {
		Result []map[string]any `json:"result"`
	}
	payload := map[string]any{
		"attribute_id":            attributeID,
		"description_category_id": categoryID,
		"type_id":                 typeID,
		"limit":                   limit,
		"value":                   value,
	}
	if err := c.do(ctx, "/v1/description-category/attribute/values/search", cred, payload, &resp); err != nil {
		return nil, err
	}
	if resp.Result == nil {
		resp.Result = []map[string]any{}
	}
	for i := range resp.Result {
		resp.Result[i] = normalizeMap(resp.Result[i])
	}
	return resp.Result, nil
}

func normalizeMap(in map[string]any) map[string]any {
	if in == nil {
		return map[string]any{}
	}
	out := make(map[string]any, len(in))
	for k, v := range in {
		out[k] = normalizeValue(v)
	}
	return out
}

func normalizeValue(v any) any {
	switch t := v.(type) {
	case json.Number:
		if i, err := t.Int64(); err == nil {
			return i
		}
		f, _ := t.Float64()
		return f
	case map[string]any:
		return normalizeMap(t)
	case []any:
		for i := range t {
			t[i] = normalizeValue(t[i])
		}
		return t
	default:
		return v
	}
}

func int64From(v any) int64 {
	switch t := v.(type) {
	case int64:
		return t
	case int:
		return int64(t)
	case float64:
		return int64(t)
	case json.Number:
		i, _ := t.Int64()
		return i
	default:
		return 0
	}
}
