package data

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"ozon-workbench/internal/biz"
)

func testClient(t *testing.T, handler http.HandlerFunc) (*OzonClient, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(handler)
	client, err := NewOzonClient(srv.URL, 5*time.Second, 1000, false, "direct")
	if err != nil {
		t.Fatal(err)
	}
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
		if r.ProtoMajor != 1 {
			t.Errorf("proto %s", r.Proto)
		}
		if r.Header.Get("Client-Id") != "shop-1" || r.Header.Get("Api-Key") != "secret" || r.Header.Get("Content-Type") != "application/json" || r.Header.Get("User-Agent") != "ozon-workbench" {
			t.Errorf("headers = %#v proto %s", r.Header, r.Proto)
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
	var forbiddenHits int
	client, srv := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		forbiddenHits++
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"code":7,"message":"Invalid Api-Key, please check the key and try again","details":[]}`))
	})
	defer srv.Close()
	_, err := client.CategoryTree(context.Background(), biz.OzonCred{ClientID: "a", APIKey: "b"}, "ZH_HANS")
	if err == nil || err.Error() != "Ozon HTTP 403: Invalid Api-Key, please check the key and try again" || forbiddenHits != 1 {
		t.Fatalf("err %v hits %d", err, forbiddenHits)
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

func TestTransportAllowsTLSRenegotiationAndHTTP1(t *testing.T) {
	client, err := NewOzonClient("https://api-seller.ozon.ru", time.Second, 10, false, "direct")
	if err != nil {
		t.Fatal(err)
	}
	tr, ok := client.httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("transport %T", client.httpClient.Transport)
	}
	if tr.ForceAttemptHTTP2 || tr.TLSNextProto == nil {
		t.Fatal("HTTP/2 is still enabled")
	}
	if tr.TLSClientConfig == nil || tr.TLSClientConfig.Renegotiation != tls.RenegotiateFreelyAsClient {
		t.Fatalf("renegotiation %#v", tr.TLSClientConfig)
	}
	if len(tr.TLSClientConfig.NextProtos) != 1 || tr.TLSClientConfig.NextProtos[0] != "http/1.1" {
		t.Fatalf("alpn %#v", tr.TLSClientConfig.NextProtos)
	}
	if tr.Proxy != nil {
		t.Fatal("direct mode installed a proxy function")
	}
	if _, err := NewOzonClient("https://api-seller.ozon.ru", time.Second, 1, true, "ftp://proxy"); err == nil {
		t.Fatal("invalid proxy should fail even in mock mode")
	}
}

func TestClientIDAndKeyAreTrimmed(t *testing.T) {
	client, srv := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Client-Id") != "6134344" || r.Header.Get("Api-Key") != "secret" {
			t.Errorf("headers %#v", r.Header)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"warehouses":[],"cursor":"","has_next":false}`))
	})
	defer srv.Close()
	if _, err := client.ListWarehouses(context.Background(), biz.OzonCred{ClientID: " 6134344\n", APIKey: "\tsecret\r\n"}); err != nil {
		t.Fatal(err)
	}
}

func TestTransportEOFIsRetriedAndExplained(t *testing.T) {
	var hits int
	client, srv := testClient(t, func(w http.ResponseWriter, r *http.Request) {
		hits++
		if hits == 1 {
			hj, ok := w.(http.Hijacker)
			if !ok {
				t.Fatal("no hijacker")
			}
			conn, _, err := hj.Hijack()
			if err != nil {
				t.Fatal(err)
			}
			conn.Close()
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":[{"description_category_id":1,"category_name":"根","disabled":false,"children":[]}]}`))
	})
	defer srv.Close()
	tree, err := client.CategoryTree(context.Background(), biz.OzonCred{ClientID: "1", APIKey: "k"}, "ZH_HANS")
	if err != nil {
		t.Fatal(err)
	}
	if hits != 2 || len(tree) != 1 {
		t.Fatalf("hits %d tree %#v", hits, tree)
	}

	client, srv = testClient(t, func(w http.ResponseWriter, r *http.Request) {
		hj, ok := w.(http.Hijacker)
		if !ok {
			t.Fatal("no hijacker")
		}
		conn, _, err := hj.Hijack()
		if err != nil {
			t.Fatal(err)
		}
		conn.Close()
	})
	defer srv.Close()
	_, err = client.CategoryTree(context.Background(), biz.OzonCred{ClientID: "1", APIKey: "k"}, "ZH_HANS")
	if err == nil {
		t.Fatal("expected transport error")
	}
	msg := err.Error()
	for _, part := range []string{"无法连接 Ozon", "不是 Ozon 返回的业务错误", "EOF", "direct", "HTTP_PROXY"} {
		if !strings.Contains(msg, part) {
			t.Fatalf("missing %q in %s", part, msg)
		}
	}
	if strings.Contains(msg, "Ozon HTTP") {
		t.Fatal(msg)
	}
}

func TestProxyModes(t *testing.T) {
	proxyURL, hits := startRecordingProxy(t)
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Client-Id") != "1" {
			t.Errorf("client id %q", r.Header.Get("Client-Id"))
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"warehouses":[],"has_next":false}`))
	}))
	defer target.Close()

	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	t.Setenv("ALL_PROXY", "http://127.0.0.1:1")
	t.Setenv("NO_PROXY", "")
	direct, err := NewOzonClient(target.URL, 5*time.Second, 1000, false, "")
	if err != nil {
		t.Fatal(err)
	}
	direct.retryBase = time.Millisecond
	if tr, ok := direct.httpClient.Transport.(*http.Transport); !ok || tr.Proxy != nil {
		t.Fatal("direct mode should ignore HTTP_PROXY")
	}
	// 127.0.0.1 is loopback, which ProxyFromEnvironment would also skip, so this request
	// only checks that a direct client can still complete. The nil Proxy above is the guard.
	if _, err := direct.ListWarehouses(context.Background(), biz.OzonCred{ClientID: "1", APIKey: "k"}); err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(hits) != 0 {
		t.Fatalf("direct mode used the recording proxy, hits %d", hits)
	}

	explicit, err := NewOzonClient(target.URL, 5*time.Second, 1000, false, proxyURL)
	if err != nil {
		t.Fatal(err)
	}
	explicit.retryBase = time.Millisecond
	if _, err := explicit.ListWarehouses(context.Background(), biz.OzonCred{ClientID: "1", APIKey: "k"}); err != nil {
		t.Fatal(err)
	}
	if atomic.LoadInt32(hits) == 0 {
		t.Fatal("explicit proxy was not used")
	}

	t.Setenv("HTTP_PROXY", proxyURL)
	t.Setenv("HTTPS_PROXY", proxyURL)
	t.Setenv("ALL_PROXY", "")
	// Loopback targets skip HTTP_PROXY, so resolve the env proxy against the real Seller host.
	viaEnv, err := NewOzonClient("https://api-seller.ozon.ru", 5*time.Second, 1000, false, "env")
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, "https://api-seller.ozon.ru/v2/warehouse/list", nil)
	if err != nil {
		t.Fatal(err)
	}
	chosen, err := viaEnv.proxyFn(req)
	if err != nil {
		t.Fatal(err)
	}
	if chosen == nil || chosen.String() != proxyURL {
		t.Fatalf("env proxy = %#v, want %s", chosen, proxyURL)
	}
	if !strings.Contains(viaEnv.describeProxy(), proxyURL) {
		t.Fatalf("describe %s", viaEnv.describeProxy())
	}
}

func startRecordingProxy(t *testing.T) (string, *int32) {
	t.Helper()
	var hits int32
	direct := &http.Transport{Proxy: nil, TLSNextProto: map[string]func(string, *tls.Conn) http.RoundTripper{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		if !r.URL.IsAbs() {
			http.Error(w, "not a proxy request", http.StatusBadRequest)
			return
		}
		out, err := http.NewRequestWithContext(r.Context(), r.Method, r.URL.String(), r.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
			return
		}
		for key, values := range r.Header {
			for _, value := range values {
				out.Header.Add(key, value)
			}
		}
		resp, err := direct.RoundTrip(out)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		for key, values := range resp.Header {
			for _, value := range values {
				w.Header().Add(key, value)
			}
		}
		w.WriteHeader(resp.StatusCode)
		_, _ = io.Copy(w, resp.Body)
	}))
	t.Cleanup(srv.Close)
	return srv.URL, &hits
}
