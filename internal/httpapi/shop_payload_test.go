package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"ozon-workbench/internal/app"
	"ozon-workbench/internal/codexcli"
	"ozon-workbench/internal/store"
)

type denyCodex struct{}

func (denyCodex) Exec(context.Context, codexcli.ExecRequest) (codexcli.ExecResult, error) {
	return codexcli.ExecResult{ExitCode: 1}, context.Canceled
}

func TestCreateShopAcceptsFrontendPayload(t *testing.T) {
	handler := testHandler()

	// 与 web/src/api/payloads.ts 的 shopCreateBody 一致：蛇形字段，Client ID 是字符串。
	created := postJSON(t, handler, http.MethodPost, "/api/shops", `{"name":"俄罗斯帽子店","client_id":"6134344","api_key":"secret-key"}`)
	if created.Code != http.StatusCreated {
		t.Fatalf("string client_id status %d body %s", created.Code, created.Body.String())
	}
	var shop map[string]any
	if err := json.Unmarshal(created.Body.Bytes(), &shop); err != nil {
		t.Fatal(err)
	}
	if shop["name"] != "俄罗斯帽子店" || shop["client_id"] != "6134344" {
		t.Fatalf("created = %s", created.Body.String())
	}
	if shop["currency_code"] != "CNY" || shop["vat"] != "0.1" || shop["enable"] != true {
		t.Fatalf("defaults = %s", created.Body.String())
	}

	numeric := postJSON(t, handler, http.MethodPost, "/api/shops", `{"name":"数字店","client_id":6134345,"api_key":"secret-key"}`)
	if numeric.Code != http.StatusCreated || !strings.Contains(numeric.Body.String(), `"client_id":"6134345"`) {
		t.Fatalf("numeric client_id status %d body %s", numeric.Code, numeric.Body.String())
	}

	padded := postJSON(t, handler, http.MethodPost, "/api/shops", `{"name":"  空格店  ","client_id":"  77  ","api_key":"  secret-key  "}`)
	if padded.Code != http.StatusCreated || !strings.Contains(padded.Body.String(), `"name":"空格店"`) || !strings.Contains(padded.Body.String(), `"client_id":"77"`) {
		t.Fatalf("trim status %d body %s", padded.Code, padded.Body.String())
	}

	id := int64(shop["id"].(float64))
	patched := postJSON(t, handler, http.MethodPatch, "/api/shops/"+strconv.FormatInt(id, 10), `{"enable":false}`)
	if patched.Code != http.StatusOK || !strings.Contains(patched.Body.String(), `"enable":false`) {
		t.Fatalf("enable patch status %d body %s", patched.Code, patched.Body.String())
	}
	renamed := postJSON(t, handler, http.MethodPatch, "/api/shops/"+strconv.FormatInt(id, 10), `{"name":"新帽子店","client_id":"7001","api_key":"next-key","currency_code":"usd","vat":"0.2"}`)
	if renamed.Code != http.StatusOK || !strings.Contains(renamed.Body.String(), `"name":"新帽子店"`) || !strings.Contains(renamed.Body.String(), `"client_id":"7001"`) || !strings.Contains(renamed.Body.String(), `"currency_code":"USD"`) {
		t.Fatalf("shop patch status %d body %s", renamed.Code, renamed.Body.String())
	}
}

func TestOtherFormsAcceptFrontendPayload(t *testing.T) {
	handler := testHandler()
	shop := postJSON(t, handler, http.MethodPost, "/api/shops", `{"name":"俄罗斯帽子店","client_id":"6134344","api_key":"secret-key"}`)
	if shop.Code != http.StatusCreated {
		t.Fatal(shop.Body.String())
	}

	settings := postJSON(t, handler, http.MethodPatch, "/api/system/settings", `{"max_active_tasks":4,"prompt_fallback":"block","generic_prompt":"通用","max_infra_retries":1,"codex_binary":"codex"}`)
	if settings.Code != http.StatusOK || !strings.Contains(settings.Body.String(), `"max_active_tasks":4`) || !strings.Contains(settings.Body.String(), `"generic_prompt":"通用"`) {
		t.Fatalf("settings status %d body %s", settings.Code, settings.Body.String())
	}

	pricing := postJSON(t, handler, http.MethodPost, "/api/pricing-rules", `{"name":"默认","enabled":true,"parameters_json":{"price_expression":"cost","old_price_expression":"","variables":{},"round_unit":""}}`)
	if pricing.Code != http.StatusCreated || !strings.Contains(pricing.Body.String(), `"name":"默认"`) {
		t.Fatalf("pricing status %d body %s", pricing.Code, pricing.Body.String())
	}

	rule := postJSON(t, handler, http.MethodPut, "/api/category-rules/10/20", `{"prompt":"完整提示词","enabled":true}`)
	if rule.Code != http.StatusOK || !strings.Contains(rule.Body.String(), `"prompt":"完整提示词"`) {
		t.Fatalf("prompt status %d body %s", rule.Code, rule.Body.String())
	}

	review := postJSON(t, handler, http.MethodPost, "/api/processing-tasks/1/review", `{"kind":"copy","decision":"approve","note":""}`)
	if review.Code == http.StatusBadRequest || strings.Contains(review.Body.String(), "审核类型无效") {
		t.Fatalf("review did not bind kind: %d %s", review.Code, review.Body.String())
	}

	message := postJSON(t, handler, http.MethodPost, "/api/processing-tasks/1/messages", `{"content":"补充要求"}`)
	if message.Code == http.StatusBadRequest {
		t.Fatalf("message status %d body %s", message.Code, message.Body.String())
	}

	publish := postJSON(t, handler, http.MethodPost, "/api/products/publish", `{"shop_id":1,"ids":[1]}`)
	if !strings.Contains(publish.Body.String(), "OZON_ACCOUNT_UNVERIFIED") {
		t.Fatalf("publish did not bind shop_id and ids: %d %s", publish.Code, publish.Body.String())
	}

	product := postJSON(t, handler, http.MethodPatch, "/api/products/1", `{"name":"帽子","price":"10"}`)
	if product.Code == http.StatusBadRequest {
		t.Fatalf("product patch status %d body %s", product.Code, product.Body.String())
	}

	var form bytes.Buffer
	writer := multipart.NewWriter(&form)
	for _, field := range [][2]string{
		{"shop_id", "1"},
		{"description_category_id", "10"},
		{"type_id", "20"},
		{"base_sku", "HAT-1"},
		{"origin_link", "https://example.test/hat"},
		{"copy_auto_approve", "false"},
		{"visual_plan_auto_approve", "true"},
		{"visual_result_auto_approve", "false"},
	} {
		if err := writer.WriteField(field[0], field[1]); err != nil {
			t.Fatal(err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, "/api/processing-tasks", &form)
	req.Header.Set("Content-Type", writer.FormDataContentType())
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusCreated || !strings.Contains(rec.Body.String(), `"base_sku":"HAT-1"`) || !strings.Contains(rec.Body.String(), `"description_category_id":10`) || !strings.Contains(rec.Body.String(), `"visual_plan_auto_approve":true`) {
		t.Fatalf("task form did not bind fields: %d %s", rec.Code, rec.Body.String())
	}
}

func testHandler() http.Handler {
	return New(&app.Engine{DB: store.NewMem(), Codex: denyCodex{}}).Handler()
}

func postJSON(t *testing.T, handler http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}
