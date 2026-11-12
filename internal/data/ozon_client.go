package data

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/time/rate"

	"ozon-workbench/internal/biz"
	"ozon-workbench/internal/catalog"
	"ozon-workbench/internal/conf"
)

const ozonUserAgent = "ozon-workbench"

// OzonClient calls api-seller.ozon.ru with per-shop Client-Id and Api-Key headers.
type OzonClient struct {
	baseURL     string
	httpClient  *http.Client
	mock        bool
	proxyMode   string
	proxyFn     func(*http.Request) (*url.URL, error)
	proxyLabel  string
	rps         int
	retryBase   time.Duration
	mu          sync.Mutex
	limits      map[string]*rate.Limiter
	stockLimits map[string]*rate.Limiter
}

func NewOzonClient(baseURL string, timeout time.Duration, rps int, mock bool, proxy string) (*OzonClient, error) {
	if rps <= 0 {
		rps = 10
	}
	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	mode, proxyFn, label, err := resolveProxy(proxy)
	if err != nil {
		return nil, err
	}
	var transport http.RoundTripper
	if mock {
		// Samples are returned only when mock is explicitly on. A real call never falls back to them.
		transport = mockTransport{}
	} else {
		transport = newOzonTransport(proxyFn, timeout)
	}
	return &OzonClient{
		baseURL: strings.TrimRight(baseURL, "/"),
		httpClient: &http.Client{
			Timeout:   timeout,
			Transport: transport,
		},
		mock:        mock,
		proxyMode:   mode,
		proxyFn:     proxyFn,
		proxyLabel:  label,
		rps:         rps,
		retryBase:   time.Second,
		limits:      map[string]*rate.Limiter{},
		stockLimits: map[string]*rate.Limiter{},
	}, nil
}

// Mocked reports whether this client returns built-in samples instead of calling Ozon.
func (c *OzonClient) Mocked() bool { return c.mock }

// newOzonTransport forces HTTP/1.1 and accepts TLS 1.2 renegotiation.
// Renegotiation is not defined for TLS 1.3 or HTTP/2. Go defaults to RenegotiateNever,
// which answers a HelloRequest with a no_renegotiation alert and surfaces to net/http as EOF.
// The Seller edge prefers TLS 1.3 and does not renegotiate, but a TLS 1.2 path — including
// some TUN/fake-ip proxies — can request it more than once after the POST is sent.
// RenegotiateOnceAsClient rejects the second request, so the client allows repeated
// server-initiated renegotiation. crypto/tls still requires the leaf certificate to stay the same.
func newOzonTransport(proxy func(*http.Request) (*url.URL, error), timeout time.Duration) *http.Transport {
	if timeout <= 0 {
		timeout = 45 * time.Second
	}
	return &http.Transport{
		Proxy: proxy,
		DialContext: (&net.Dialer{
			Timeout:   15 * time.Second,
			KeepAlive: 30 * time.Second,
		}).DialContext,
		ForceAttemptHTTP2: false,
		// A non-nil map is the documented way to keep HTTP/2 disabled.
		TLSNextProto: map[string]func(string, *tls.Conn) http.RoundTripper{},
		TLSClientConfig: &tls.Config{
			MinVersion:    tls.VersionTLS12,
			NextProtos:    []string{"http/1.1"},
			Renegotiation: tls.RenegotiateFreelyAsClient,
		},
		MaxIdleConns:          10,
		MaxIdleConnsPerHost:   2,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   15 * time.Second,
		ResponseHeaderTimeout: timeout,
		ExpectContinueTimeout: time.Second,
	}
}

func resolveProxy(raw string) (mode string, fn func(*http.Request) (*url.URL, error), label string, err error) {
	mode, err = conf.NormalizeOzonProxy(raw)
	if err != nil {
		return "", nil, "", err
	}
	switch mode {
	case "direct":
		return "direct", nil, "direct（强制直连，已忽略 HTTP_PROXY/HTTPS_PROXY/ALL_PROXY）", nil
	case "env":
		return "env", http.ProxyFromEnvironment, "", nil
	default:
		u, perr := url.Parse(mode)
		if perr != nil {
			return "", nil, "", perr
		}
		return "url", http.ProxyURL(u), "url（" + redactProxyURL(u) + "）", nil
	}
}

func redactProxyURL(u *url.URL) string {
	clone := *u
	if clone.User != nil {
		name := clone.User.Username()
		if _, ok := clone.User.Password(); ok {
			clone.User = url.UserPassword(name, "REDACTED")
		} else {
			clone.User = url.User(name)
		}
	}
	return clone.String()
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
	cred.ClientID = strings.TrimSpace(cred.ClientID)
	cred.APIKey = strings.TrimSpace(cred.APIKey)
	if cred.ClientID == "" || cred.APIKey == "" {
		return fmt.Errorf("Ozon 凭证不完整")
	}
	if strings.ContainsAny(cred.ClientID, "\r\n") || strings.ContainsAny(cred.APIKey, "\r\n") {
		return fmt.Errorf("Client ID 或 API Key 含有换行，请重新粘贴，不要带首尾空格或换行")
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
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", ozonUserAgent)
		// Client-Id is the seller id as a decimal string. Ozon rejects a non-integer value with HTTP 400.
		req.Header.Set("Client-Id", cred.ClientID)
		req.Header.Set("Api-Key", cred.APIKey)
		resp, err := c.httpClient.Do(req)
		if err != nil {
			last = err
			if attempt < 2 && retryableTransport(err) {
				c.httpClient.CloseIdleConnections()
				if err := c.sleep(ctx, c.backoff(attempt, "")); err != nil {
					return err
				}
				continue
			}
			return c.transportError(err)
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
		resp.Body.Close()
		if readErr != nil {
			last = readErr
			if attempt < 2 && retryableTransport(readErr) {
				c.httpClient.CloseIdleConnections()
				if err := c.sleep(ctx, c.backoff(attempt, "")); err != nil {
					return err
				}
				continue
			}
			return c.transportError(readErr)
		}
		if resp.StatusCode == http.StatusTooManyRequests && attempt < 2 {
			last = &ozonStatusError{Status: resp.StatusCode, Message: extractOzonMessage(body)}
			retryAfter := resp.Header.Get("Retry-After")
			if v := resp.Header.Get("Item-Retry-After"); v != "" {
				retryAfter = v
			}
			if err := c.sleep(ctx, c.backoff(attempt, retryAfter)); err != nil {
				return err
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
		if _, ok := last.(*ozonStatusError); ok {
			return last
		}
		return c.transportError(last)
	}
	return fmt.Errorf("Ozon 请求失败")
}

func (c *OzonClient) sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func retryableTransport(err error) bool {
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return false
	}
	if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
		return true
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return true
	}
	msg := err.Error()
	for _, needle := range []string{
		"connection reset",
		"connection refused",
		"broken pipe",
		"server sent GOAWAY",
		"client connection lost",
		"use of closed network connection",
		"TLS handshake timeout",
		"no renegotiation",
		"EOF",
	} {
		if strings.Contains(msg, needle) {
			return true
		}
	}
	return false
}

func (c *OzonClient) transportError(err error) error {
	return fmt.Errorf("无法连接 Ozon（%s，不是 Ozon 返回的业务错误）。当前代理：%s。请按 README「连接 Ozon 失败 / EOF」用 curl 对比直连和代理。若 DNS 得到 198.18.0.0/15 或 2001:2::，这是 TUN/fake-ip，代理规则里的 DIRECT 仍会拦截 TLS，Go 不读取代理软件的分流列表。原始错误：%v", classifyTransport(err), c.describeProxy(), err)
}

func classifyTransport(err error) string {
	msg := err.Error()
	switch {
	case strings.Contains(msg, "no renegotiation"):
		return "TLS 1.2 重协商被拒绝"
	case errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) || strings.Contains(msg, "EOF"):
		return "网络、代理或 TLS 中断：连接在收到 HTTP 响应前被关闭（EOF）。TLS 1.2 路径上的重协商，或大陆直连被重置，都会变成这个错误"
	case strings.Contains(msg, "connection reset"):
		return "连接被重置"
	case strings.Contains(msg, "connection refused"):
		return "连接被拒绝（代理地址或端口可能不对）"
	case strings.Contains(strings.ToLower(msg), "tls") || strings.Contains(msg, "handshake"):
		return "TLS 握手失败"
	case strings.Contains(strings.ToLower(msg), "timeout") || strings.Contains(msg, "Timeout"):
		return "连接超时"
	default:
		return "网络或代理失败"
	}
}

func (c *OzonClient) describeProxy() string {
	switch c.proxyMode {
	case "direct":
		return c.proxyLabel
	case "env":
		if c.proxyFn == nil {
			return "env"
		}
		req, err := http.NewRequest(http.MethodPost, c.baseURL+"/v2/warehouse/list", nil)
		if err != nil {
			return "env"
		}
		u, err := c.proxyFn(req)
		if err != nil {
			return "env（读取代理环境变量失败）"
		}
		if u == nil {
			return "env（未设置 HTTP_PROXY/HTTPS_PROXY/ALL_PROXY，实际直连）"
		}
		return "env（" + redactProxyURL(u) + "；只认环境变量，不认代理软件的分流规则）"
	default:
		if c.proxyLabel != "" {
			return c.proxyLabel
		}
		return c.proxyMode
	}
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

func (c *OzonClient) waitStock(ctx context.Context, clientID string) error {
	c.mu.Lock()
	lim := c.stockLimits[clientID]
	if lim == nil {
		// /v2/products/stocks allows about 80 requests per minute per client, on top of the global cap.
		lim = rate.NewLimiter(rate.Every(time.Minute/80), 1)
		c.stockLimits[clientID] = lim
	}
	c.mu.Unlock()
	return lim.Wait(ctx)
}

// ImportProducts posts one /v3/product/import body and returns the task id.
// A transport failure is reported as AcceptUnknownError because Ozon may already have the batch.
func (c *OzonClient) ImportProducts(ctx context.Context, cred biz.OzonCred, body json.RawMessage) (string, error) {
	var resp struct {
		Result struct {
			TaskID json.Number `json:"task_id"`
		} `json:"result"`
	}
	err := c.do(ctx, "/v3/product/import", cred, body, &resp)
	if err != nil {
		var status *ozonStatusError
		if errors.As(err, &status) {
			return "", err
		}
		return "", &biz.AcceptUnknownError{Msg: err.Error()}
	}
	id := strings.TrimSpace(resp.Result.TaskID.String())
	if id == "" || id == "0" {
		return "", fmt.Errorf("Ozon 没有返回 task_id")
	}
	return id, nil
}

// ImportInfo reads /v1/product/import/info and keeps per-offer errors.
func (c *OzonClient) ImportInfo(ctx context.Context, cred biz.OzonCred, taskID string) ([]biz.ImportOffer, error) {
	n, err := strconv.ParseInt(strings.TrimSpace(taskID), 10, 64)
	if err != nil {
		return nil, fmt.Errorf("task_id 不正确")
	}
	var resp struct {
		Result struct {
			Items []struct {
				OfferID   string           `json:"offer_id"`
				ProductID int64            `json:"product_id"`
				Status    string           `json:"status"`
				Errors    []map[string]any `json:"errors"`
			} `json:"items"`
		} `json:"result"`
	}
	if err := c.do(ctx, "/v1/product/import/info", cred, map[string]any{"task_id": n}, &resp); err != nil {
		return nil, err
	}
	out := make([]biz.ImportOffer, 0, len(resp.Result.Items))
	for _, item := range resp.Result.Items {
		out = append(out, biz.ImportOffer{
			OfferID: item.OfferID, ProductID: item.ProductID, Status: item.Status, Errors: item.Errors,
		})
	}
	return out, nil
}

// ProductInfo reads /v3/product/info/list for platform status and rejection text.
func (c *OzonClient) ProductInfo(ctx context.Context, cred biz.OzonCred, offerIDs []string) ([]biz.PlatformCard, error) {
	if len(offerIDs) == 0 {
		return nil, nil
	}
	var resp struct {
		Items []struct {
			ID       int64  `json:"id"`
			OfferID  string `json:"offer_id"`
			SKU      int64  `json:"sku"`
			Archived bool   `json:"is_archived"`
			Statuses struct {
				Status            string `json:"status"`
				StatusFailed      string `json:"status_failed"`
				ModerateStatus    string `json:"moderate_status"`
				StatusName        string `json:"status_name"`
				StatusDescription string `json:"status_description"`
				StatusTooltip     string `json:"status_tooltip"`
			} `json:"statuses"`
		} `json:"items"`
		Result struct {
			Items []json.RawMessage `json:"items"`
		} `json:"result"`
	}
	if err := c.do(ctx, "/v3/product/info/list", cred, map[string]any{"offer_id": offerIDs}, &resp); err != nil {
		return nil, err
	}
	out := make([]biz.PlatformCard, 0, len(resp.Items))
	for _, item := range resp.Items {
		out = append(out, biz.PlatformCard{
			OfferID: item.OfferID, ProductID: item.ID, SKU: item.SKU, Archived: item.Archived,
			Status: item.Statuses.Status, Moderate: item.Statuses.ModerateStatus, Failed: item.Statuses.StatusFailed,
			Name: item.Statuses.StatusName, Description: item.Statuses.StatusDescription, Tooltip: item.Statuses.StatusTooltip,
		})
	}
	return out, nil
}

// UpdateStocks calls the current POST /v2/products/stocks. /v1/product/import/stocks is deprecated.
// quant_size was removed in June 2025 and is not sent. One pair is also locally spaced to 30 seconds by the usecase.
func (c *OzonClient) UpdateStocks(ctx context.Context, cred biz.OzonCred, lines []biz.StockLine) ([]biz.StockLineResult, error) {
	if err := c.waitStock(ctx, strings.TrimSpace(cred.ClientID)); err != nil {
		return nil, err
	}
	payload := make([]map[string]any, 0, len(lines))
	for _, line := range lines {
		payload = append(payload, map[string]any{
			"offer_id": line.OfferID, "product_id": line.ProductID, "stock": line.Stock, "warehouse_id": line.WarehouseID,
		})
	}
	var resp struct {
		Result []struct {
			OfferID     string `json:"offer_id"`
			ProductID   int64  `json:"product_id"`
			WarehouseID int64  `json:"warehouse_id"`
			Updated     bool   `json:"updated"`
			Errors      []struct {
				Code    string `json:"code"`
				Message string `json:"message"`
			} `json:"errors"`
		} `json:"result"`
	}
	if err := c.do(ctx, "/v2/products/stocks", cred, map[string]any{"stocks": payload}, &resp); err != nil {
		return nil, err
	}
	out := make([]biz.StockLineResult, 0, len(resp.Result))
	for _, row := range resp.Result {
		item := biz.StockLineResult{OfferID: row.OfferID, ProductID: row.ProductID, WarehouseID: row.WarehouseID, Updated: row.Updated}
		for _, e := range row.Errors {
			item.Errors = append(item.Errors, map[string]any{"code": e.Code, "message": e.Message})
		}
		out = append(out, item)
	}
	return out, nil
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
