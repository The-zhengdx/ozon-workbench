package data

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"ozon-workbench/internal/biz"
)

func testClient(t *testing.T, handler http.HandlerFunc) (*OzonClient, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	client := NewOzonClient(srv.URL, 5*time.Second, 1000, false)
	client.retryBase = time.Millisecond
	return client, srv
}

func TestListWarehousesPaginatesAndSendsAuth(t *testing.T) {
	var mu sync.Mutex
	var bodies []map[string]any
	var sawAuth bool
	client, srv := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v2/warehouse/list" || r.Method != http.MethodPost {
			t.Errorf("path %s %s", r.Method, r.URL.Path)
		}
		if r.Header.Get("Client-Id") != "shop-1" || r.Header.Get("Api-Key") != "secret" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("headers = %#v", r.Header)
		}
		sawAuth = true
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		mu.Lock()
		bodies = append(bodies, body)
		page := len(bodies)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		if page == 1 {
			_, _ = w.Write([]byte(`{"warehouses":[{"warehouse_id":11,"name":"一号","status":"created","is_rfbs":false,"warehouse_type":"FBS"},{"warehouse_id":12,"name":"二号","status":"created","is_rfbs":true,"warehouse_type":"RFBS"}],"cursor":"p2","has_next":true}`))
			return
		}
		_, _ = w.Write([]byte(`{"warehouses":[{"warehouse_id":13,"name":"三号","status":"disabled","is_rfbs":false,"warehouse_type":"FBS"}],"cursor":"","has_next":false}`))
	})
	defer srv.Close()

	got, err := client.ListWarehouses(context.Background(), biz.OzonCred{ClientID: "shop-1", APIKey: "secret"})
	if err != nil {
		t.Fatal(err)
	}
	if !sawAuth || len(got) != 3 || got[2].WarehouseID != 13 || !got[1].IsRFBS {
		t.Fatalf("warehouses %#v auth %v", got, sawAuth)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(bodies) != 2 {
		t.Fatalf("pages %d", len(bodies))
	}
	if bodies[0]["limit"] != float64(200) {
		t.Fatalf("limit %#v", bodies[0]["limit"])
	}
	if _, ok := bodies[0]["cursor"]; ok {
		t.Fatal("first page should omit cursor")
	}
	if bodies[1]["cursor"] != "p2" {
		t.Fatalf("second cursor %#v", bodies[1]["cursor"])
	}
}

func TestOzonErrorMessageAndRateLimitRetry(t *testing.T) {
	client, srv := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"code":7,"message":"Invalid Api-Key, please check the key and try again","details":[]}`))
	})
	defer srv.Close()
	_, err := client.CategoryTree(context.Background(), biz.OzonCred{ClientID: "a", APIKey: "b"}, "ZH_HANS")
	if err == nil || err.Error() != "Ozon HTTP 403: Invalid Api-Key, please check the key and try again" {
		t.Fatalf("err %v", err)
	}

	var hits int
	client, srv = testClient(t, func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.Header().Set("Content-Type", "application/json")
		if hits == 1 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"code":8,"message":"You have reached request rate limit per second"}`))
			return
		}
		_, _ = w.Write([]byte(`{"result":[{"description_category_id":1,"category_name":"根","disabled":false,"children":[{"type_id":9,"type_name":"叶子","disabled":false,"children":[]}]}]}`))
	})
	defer srv.Close()
	tree, err := client.CategoryTree(context.Background(), biz.OzonCred{ClientID: "a", APIKey: "b"}, "ZH_HANS")
	if err != nil {
		t.Fatal(err)
	}
	if hits != 2 || len(tree) != 1 || tree[0].Children[0].TypeID != 9 {
		t.Fatalf("hits %d tree %#v", hits, tree)
	}
}

func TestCategoryAttributeRequestsUseOfficialBodies(t *testing.T) {
	var paths []string
	var bodies []map[string]any
	client, srv := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		paths = append(paths, r.URL.Path)
		bodies = append(bodies, body)
		if r.Header.Get("Client-Id") != "cid" || r.Header.Get("Api-Key") != "key" {
			t.Errorf("auth headers missing")
		}
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/v1/description-category/attribute":
			_, _ = w.Write([]byte(`{"result":[{"id":85,"name":"品牌","type":"String","is_required":true,"is_collection":false,"dictionary_id":85,"description":"品牌","group_name":"基本信息","group_id":1,"attribute_complex_id":0,"max_value_count":1,"category_dependent":true,"complex_is_collection":false,"is_aspect":false}]}`))
		case "/v1/description-category/attribute/values":
			last, _ := body["last_value_id"].(float64)
			if last == 0 {
				_, _ = w.Write([]byte(`{"result":[{"id":5055881,"value":"无品牌","info":"","picture":""}],"has_next":true}`))
				return
			}
			_, _ = w.Write([]byte(`{"result":[{"id":5056737,"value":"Bosch","info":"博世","picture":""}],"has_next":false}`))
		case "/v1/description-category/attribute/values/search":
			_, _ = w.Write([]byte(`{"result":[{"id":5056737,"value":"Bosch","info":"博世","picture":""}]}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	})
	defer srv.Close()
	cred := biz.OzonCred{ClientID: "cid", APIKey: "key"}
	attrs, err := client.Attributes(context.Background(), cred, 17028922, 97062518, "ZH_HANS")
	if err != nil {
		t.Fatal(err)
	}
	if len(attrs) != 1 || attrs[0]["name"] != "品牌" || attrs[0]["id"] != int64(85) {
		t.Fatalf("attrs %#v", attrs)
	}
	first, err := client.AttributeValues(context.Background(), cred, 17028922, 97062518, 85, 0, 100, "ZH_HANS")
	if err != nil {
		t.Fatal(err)
	}
	second, err := client.AttributeValues(context.Background(), cred, 17028922, 97062518, 85, first.LastValueID, 100, "ZH_HANS")
	if err != nil {
		t.Fatal(err)
	}
	if !first.HasNext || first.LastValueID != 5055881 || second.HasNext || second.Values[0]["value"] != "Bosch" {
		t.Fatalf("pages %#v %#v", first, second)
	}
	found, err := client.SearchAttributeValues(context.Background(), cred, 17028922, 97062518, 85, "Bo", 50)
	if err != nil {
		t.Fatal(err)
	}
	if found[0]["value"] != "Bosch" {
		t.Fatalf("search %#v", found)
	}
	if bodies[0]["language"] != "ZH_HANS" || bodies[0]["description_category_id"] != float64(17028922) || bodies[0]["type_id"] != float64(97062518) {
		t.Fatalf("attribute body %#v", bodies[0])
	}
	if bodies[1]["limit"] != float64(100) || bodies[1]["last_value_id"] != float64(0) || bodies[1]["attribute_id"] != float64(85) {
		t.Fatalf("values body %#v", bodies[1])
	}
	if bodies[2]["last_value_id"] != float64(5055881) {
		t.Fatalf("next page %#v", bodies[2])
	}
	if _, ok := bodies[3]["language"]; ok {
		t.Fatal("search body should not send language; the official schema has no language field")
	}
	if bodies[3]["value"] != "Bo" || bodies[3]["limit"] != float64(50) {
		t.Fatalf("search body %#v", bodies[3])
	}
	want := []string{
		"/v1/description-category/attribute",
		"/v1/description-category/attribute/values",
		"/v1/description-category/attribute/values",
		"/v1/description-category/attribute/values/search",
	}
	for i := range want {
		if paths[i] != want[i] {
			t.Fatalf("path %d = %s", i, paths[i])
		}
	}
}
