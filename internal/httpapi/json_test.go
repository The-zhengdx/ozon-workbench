package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"ozon-workbench/internal/app"
	"ozon-workbench/internal/domain"
	"ozon-workbench/internal/store"
)

func TestEmptyListEncodesAsArray(t *testing.T) {
	rec := httptest.NewRecorder()
	var shops []domain.Shop
	writeJSON(rec, http.StatusOK, shops)
	if got := strings.TrimSpace(rec.Body.String()); got != "[]" {
		t.Fatalf("nil shop list encoded as %q", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	writeJSON(rec, http.StatusOK, app.Settings{})
	if !strings.Contains(rec.Body.String(), `"pricing_rules":[]`) {
		t.Fatalf("nil pricing_rules encoded as %s", rec.Body.String())
	}

	when := time.Date(2026, 11, 13, 1, 2, 3, 0, time.UTC)
	rec = httptest.NewRecorder()
	writeJSON(rec, http.StatusOK, []domain.Shop{{
		ID: 7, Name: "空店", ClientID: "c", APIKey: "secret", CurrencyCode: "CNY", VAT: "0.1", Enable: true,
		CreatedAt: when, UpdatedAt: when,
	}})
	if !strings.Contains(rec.Body.String(), `"name":"空店"`) || !strings.Contains(rec.Body.String(), "2026-11-13T01:02:03Z") {
		t.Fatalf("shop payload changed: %s", rec.Body.String())
	}

	rec = httptest.NewRecorder()
	writeJSON(rec, http.StatusOK, struct {
		Images json.RawMessage `json:"images"`
	}{})
	if !strings.Contains(rec.Body.String(), `"images":null`) {
		t.Fatalf("raw JSON null became %s", rec.Body.String())
	}
}

func TestEmptyEndpointsEncodeAsArrays(t *testing.T) {
	handler := New(&app.Engine{DB: store.NewMem()}).Handler()
	for _, path := range []string{"/api/shops", "/api/processing-tasks", "/api/products", "/api/import-batches"} {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("%s status %d body %s", path, rec.Code, rec.Body.String())
		}
		if got := strings.TrimSpace(rec.Body.String()); got != "[]" {
			t.Fatalf("%s body = %q", path, rec.Body.String())
		}
	}

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/api/system/settings", nil))
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"pricing_rules":[]`) {
		t.Fatalf("settings = %d %s", rec.Code, rec.Body.String())
	}
}
