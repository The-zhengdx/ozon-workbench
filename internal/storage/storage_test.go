package storage

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
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

func TestS3PutHeadPublicAndDelete(t *testing.T) {
	var got []string
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = append(got, r.Method+" "+r.URL.Path)
		switch r.Method {
		case http.MethodPut:
			raw, _ := io.ReadAll(r.Body)
			body = string(raw)
			w.WriteHeader(http.StatusOK)
		case http.MethodHead:
			w.WriteHeader(http.StatusOK)
		case http.MethodGet:
			_, _ = w.Write([]byte(body))
		case http.MethodDelete:
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusMethodNotAllowed)
		}
	}))
	defer srv.Close()
	client := NewS3(S3Config{
		Endpoint: srv.URL, Region: "us-east-1", Bucket: "goods",
		AccessKey: "key", SecretKey: "secret", PublicBase: srv.URL + "/public",
	})
	key := "6134344/HAT-1/images/a.jpg"
	if err := client.Put(context.Background(), key, []byte("ozon-workbench-s3-ping"), "text/plain"); err != nil {
		t.Fatal(err)
	}
	if err := client.Head(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	if err := client.CheckPublic(context.Background(), client.PublicURL(key)); err != nil {
		t.Fatal(err)
	}
	if err := client.Delete(context.Background(), key); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(got, "\n")
	for _, want := range []string{
		"PUT /goods/6134344/HAT-1/images/a.jpg",
		"HEAD /goods/6134344/HAT-1/images/a.jpg",
		"GET /public/6134344/HAT-1/images/a.jpg",
		"DELETE /goods/6134344/HAT-1/images/a.jpg",
	} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %s in\n%s", want, joined)
		}
	}
}
