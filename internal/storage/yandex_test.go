package storage

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

func TestNoneRefusesUpload(t *testing.T) {
	_, err := None{}.Upload(context.Background(), "x", "a.jpg")
	if !Unconfigured(err) {
		t.Fatalf("err %v", err)
	}
	store, err := FromConfig(Config{})
	if err != nil || store.Configured() || store.Name() != "none" {
		t.Fatalf("open %#v %v", store, err)
	}
}

func TestMockUploadAndRefresh(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.jpg")
	if err := os.WriteFile(file, []byte("img"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewMock("")
	m.FailLeft = 1
	if _, err := m.Upload(context.Background(), file, "shop/1/a.jpg"); err == nil {
		t.Fatal("expected the first upload to fail")
	}
	obj, err := m.Upload(context.Background(), file, "shop/1/a.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if obj.PublicURL != "https://mock-storage.local/shop/1/a.jpg" {
		t.Fatalf("url %s", obj.PublicURL)
	}
	again, err := m.Refresh(context.Background(), obj.PublicKey)
	if err != nil || again != obj.PublicURL {
		t.Fatalf("refresh %s %v", again, err)
	}
	if err := m.Delete(context.Background(), obj.Key); err != nil {
		t.Fatal(err)
	}
}

func TestYandexUploadPublishesDirectLink(t *testing.T) {
	var puts atomic.Int32
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "OAuth test-token" && r.URL.Path != "/upload-target" {
			t.Errorf("auth %q path %s", got, r.URL.Path)
		}
		switch {
		case r.Method == http.MethodPut && strings.HasPrefix(r.URL.Path, "/v1/disk/resources/publish"):
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"href":"https://cloud-api.yandex.net/v1/disk/resources?path=disk:/ozon-workbench/a.jpg","method":"GET","templated":false}`))
		case r.Method == http.MethodPut && r.URL.Path == "/v1/disk/resources":
			if !strings.Contains(r.URL.Query().Get("path"), "ozon-workbench") {
				t.Errorf("mkdir path %s", r.URL.RawQuery)
			}
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"href":"ok"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/disk/resources/upload":
			if r.URL.Query().Get("overwrite") != "true" {
				t.Errorf("overwrite %s", r.URL.RawQuery)
			}
			_, _ = w.Write([]byte(`{"href":"` + srv.URL + `/upload-target","method":"PUT","templated":false}`))
		case r.Method == http.MethodPut && r.URL.Path == "/upload-target":
			puts.Add(1)
			w.WriteHeader(http.StatusCreated)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/disk/resources":
			_, _ = w.Write([]byte(`{"name":"a.jpg","public_url":"https://yadi.sk/i/abc","path":"disk:/ozon-workbench/a.jpg","type":"file"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/disk/public/resources/download":
			if r.URL.Query().Get("public_key") != "https://yadi.sk/i/abc" {
				t.Errorf("public_key %s", r.URL.Query().Get("public_key"))
			}
			_, _ = w.Write([]byte(`{"href":"https://downloader.disk.yandex.ru/disk/file.jpg","method":"GET","templated":false}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	dir := t.TempDir()
	file := filepath.Join(dir, "a.jpg")
	if err := os.WriteFile(file, []byte("jpeg-bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	y := NewYandex("test-token", "ozon-workbench", srv.URL)
	obj, err := y.Upload(context.Background(), file, "shop/30-1/a.jpg")
	if err != nil {
		t.Fatal(err)
	}
	if obj.PublicKey != "https://yadi.sk/i/abc" {
		t.Fatalf("public key %s", obj.PublicKey)
	}
	if obj.PublicURL != "https://downloader.disk.yandex.ru/disk/file.jpg" {
		t.Fatalf("direct url %s", obj.PublicURL)
	}
	if strings.Contains(obj.PublicURL, "yadi.sk") {
		t.Fatal("page url must not be sent to Ozon")
	}
	if puts.Load() != 1 {
		t.Fatalf("puts %d", puts.Load())
	}
	fresh, err := y.Refresh(context.Background(), obj.PublicKey)
	if err != nil || fresh != obj.PublicURL {
		t.Fatalf("refresh %s %v", fresh, err)
	}
}

func TestYandexRetriesTransientPublish(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/disk/" {
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"total_space":1}`))
			return
		}
		n := hits.Add(1)
		if n == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			_, _ = w.Write([]byte(`{"message":"unavailable"}`))
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"href":"https://downloader.disk.yandex.ru/disk/x","method":"GET","templated":false}`))
	}))
	defer srv.Close()
	y := NewYandex("test-token", "", srv.URL)
	y.Attempts = 2
	if err := y.Health(context.Background()); err != nil {
		t.Fatal(err)
	}
	got, err := y.Refresh(context.Background(), "https://yadi.sk/i/abc")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://downloader.disk.yandex.ru/disk/x" {
		t.Fatalf("href %s", got)
	}
	if hits.Load() != 2 {
		t.Fatalf("hits %d", hits.Load())
	}
}

func TestYandexRejectsLandingPageAsDirect(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"href":"https://yadi.sk/i/abc","method":"GET","templated":false}`))
	}))
	defer srv.Close()
	y := NewYandex("test-token", "", srv.URL)
	if _, err := y.Refresh(context.Background(), "https://yadi.sk/i/abc"); err == nil {
		t.Fatal("landing page should be rejected")
	}
}
