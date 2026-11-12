package storage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// Yandex publishes a file on Yandex Disk and resolves a direct download URL.
//
// Flow (https://yandex.ru/dev/disk-api/doc/ru/reference/upload and /public):
//  1. PUT /v1/disk/resources to create parent folders (409 means the folder exists).
//  2. GET /v1/disk/resources/upload?overwrite=true for a one-shot upload href.
//  3. PUT the file bytes to that href.
//  4. PUT /v1/disk/resources/publish so the file has a public page.
//  5. GET /v1/disk/resources for public_url (a yadi.sk / disk.yandex.ru HTML page).
//  6. GET /v1/disk/public/resources/download?public_key=... for a direct href.
//
// The page URL is not an image. The download href is. Yandex documents that href
// as short-lived, and the downloader often answers 302 to a CDN host. Callers
// should Refresh before handing the URL to Ozon. See the README for the Ozon caveat.
type Yandex struct {
	Token    string
	Root     string
	API      string
	HTTP     *http.Client
	Attempts int
}

func NewYandex(token, root, apiBase string) *Yandex {
	token = strings.TrimSpace(token)
	root = strings.Trim(root, "/")
	if root == "" {
		root = "ozon-workbench"
	}
	apiBase = strings.TrimRight(strings.TrimSpace(apiBase), "/")
	if apiBase == "" {
		apiBase = "https://cloud-api.yandex.net"
	}
	return &Yandex{
		Token:    token,
		Root:     root,
		API:      apiBase,
		Attempts: 3,
		HTTP: &http.Client{
			Timeout: 2 * time.Minute,
			// A 302 from the Disk API itself is an error; the download href is returned in JSON
			// and is not followed here. Ozon follows it later, if it follows redirects at all.
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
	}
}

func (y *Yandex) Name() string     { return "yandex" }
func (y *Yandex) Configured() bool { return strings.TrimSpace(y.Token) != "" }
func (y *Yandex) Message() string {
	if !y.Configured() {
		return "已选择 Yandex Disk，但 YANDEX_DISK_TOKEN 为空。"
	}
	return "Yandex Disk 已配置。上传后会发布资源，并把临时直链交给 Ozon。直链会过期，也可能 302 跳转，正式环境更适合对象存储。"
}

func (y *Yandex) Health(ctx context.Context) error {
	if !y.Configured() {
		return ErrUnconfigured
	}
	_, err := y.api(ctx, http.MethodGet, "/v1/disk/", nil, nil)
	return err
}

func (y *Yandex) Upload(ctx context.Context, localPath, key string) (Object, error) {
	if !y.Configured() {
		return Object{}, ErrUnconfigured
	}
	full, err := y.fullPath(key)
	if err != nil {
		return Object{}, err
	}
	info, err := os.Stat(localPath)
	if err != nil {
		return Object{}, fmt.Errorf("读取本地文件: %w", err)
	}
	obj, err := y.uploadOnce(ctx, localPath, full, info.Size())
	if err != nil {
		return Object{}, err
	}
	obj.Key = strings.Trim(key, "/")
	return obj, nil
}

func (y *Yandex) uploadOnce(ctx context.Context, localPath, full string, size int64) (Object, error) {
	if err := y.ensureParents(ctx, full); err != nil {
		return Object{}, err
	}
	href, err := y.uploadHref(ctx, full)
	if err != nil {
		return Object{}, err
	}
	if err := y.putFile(ctx, href, localPath); err != nil {
		return Object{}, err
	}
	if err := y.publish(ctx, full); err != nil {
		return Object{}, err
	}
	publicURL, err := y.publicURL(ctx, full)
	if err != nil {
		return Object{}, err
	}
	direct, err := y.Refresh(ctx, publicURL)
	if err != nil {
		return Object{}, fmt.Errorf("已发布到 Yandex Disk，但没有拿到可直接下载的地址: %w", err)
	}
	return Object{
		PublicURL: direct,
		PublicKey: publicURL,
		Size:      size,
		MIME:      contentType(localPath),
	}, nil
}

func (y *Yandex) Delete(ctx context.Context, key string) error {
	if !y.Configured() {
		return ErrUnconfigured
	}
	full, err := y.fullPath(key)
	if err != nil {
		return err
	}
	q := url.Values{}
	q.Set("path", full)
	q.Set("permanently", "false")
	_, err = y.api(ctx, http.MethodDelete, "/v1/disk/resources?"+q.Encode(), nil, nil)
	if err != nil && strings.Contains(err.Error(), "404") {
		return nil
	}
	return err
}

// Refresh asks Yandex for a fresh direct href. publicKey is the stable public page URL.
func (y *Yandex) Refresh(ctx context.Context, publicKey string) (string, error) {
	if !y.Configured() {
		return "", ErrUnconfigured
	}
	publicKey = strings.TrimSpace(publicKey)
	if publicKey == "" {
		return "", fmt.Errorf("缺少 Yandex 公开链接")
	}
	q := url.Values{}
	q.Set("public_key", publicKey)
	raw, err := y.api(ctx, http.MethodGet, "/v1/disk/public/resources/download?"+q.Encode(), nil, nil)
	if err != nil {
		return "", err
	}
	var link struct {
		Href string `json:"href"`
	}
	if err := json.Unmarshal(raw, &link); err != nil {
		return "", fmt.Errorf("解析 Yandex 直链: %w", err)
	}
	if !isDirect(link.Href) {
		return "", fmt.Errorf("Yandex 返回的不是可直接下载的 HTTPS 地址")
	}
	return link.Href, nil
}

func (y *Yandex) fullPath(key string) (string, error) {
	key = path.Clean("/" + strings.TrimSpace(key))
	key = strings.TrimPrefix(key, "/")
	if key == "" || key == "." || strings.HasPrefix(key, "../") || strings.Contains(key, "..") {
		return "", fmt.Errorf("非法的对象键")
	}
	return "disk:/" + y.Root + "/" + key, nil
}

func (y *Yandex) ensureParents(ctx context.Context, full string) error {
	// disk:/root/a/b/file.jpg → create disk:/root, disk:/root/a, disk:/root/a/b
	rest := strings.TrimPrefix(full, "disk:/")
	parts := strings.Split(rest, "/")
	if len(parts) < 2 {
		return nil
	}
	cur := "disk:"
	for _, part := range parts[:len(parts)-1] {
		cur += "/" + part
		q := url.Values{}
		q.Set("path", cur)
		_, err := y.api(ctx, http.MethodPut, "/v1/disk/resources?"+q.Encode(), nil, nil)
		if err == nil || strings.Contains(err.Error(), "409") {
			continue
		}
		return err
	}
	return nil
}

func (y *Yandex) uploadHref(ctx context.Context, full string) (string, error) {
	q := url.Values{}
	q.Set("path", full)
	q.Set("overwrite", "true")
	raw, err := y.api(ctx, http.MethodGet, "/v1/disk/resources/upload?"+q.Encode(), nil, nil)
	if err != nil {
		return "", err
	}
	var link struct {
		Href   string `json:"href"`
		Method string `json:"method"`
	}
	if err := json.Unmarshal(raw, &link); err != nil {
		return "", err
	}
	if link.Href == "" {
		return "", fmt.Errorf("Yandex 没有返回上传地址")
	}
	return link.Href, nil
}

func (y *Yandex) putFile(ctx context.Context, href, localPath string) error {
	file, err := os.Open(localPath)
	if err != nil {
		return err
	}
	defer file.Close()
	attempts := y.attempts()
	var last error
	for i := 0; i < attempts; i++ {
		if _, err := file.Seek(0, io.SeekStart); err != nil {
			return err
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodPut, href, file)
		if err != nil {
			return err
		}
		req.Header.Set("Content-Type", contentType(localPath))
		resp, err := y.client().Do(req)
		if err != nil {
			last = err
		} else {
			body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
			resp.Body.Close()
			if resp.StatusCode >= 200 && resp.StatusCode < 300 {
				return nil
			}
			last = &httpStatus{Code: resp.StatusCode, Body: strings.TrimSpace(string(body))}
		}
		if !retryable(last) || i == attempts-1 {
			return last
		}
		if err := sleep(ctx, time.Duration(i+1)*400*time.Millisecond); err != nil {
			return err
		}
	}
	return last
}

func (y *Yandex) publish(ctx context.Context, full string) error {
	q := url.Values{}
	q.Set("path", full)
	_, err := y.api(ctx, http.MethodPut, "/v1/disk/resources/publish?"+q.Encode(), nil, nil)
	return err
}

func (y *Yandex) publicURL(ctx context.Context, full string) (string, error) {
	q := url.Values{}
	q.Set("path", full)
	raw, err := y.api(ctx, http.MethodGet, "/v1/disk/resources?"+q.Encode(), nil, nil)
	if err != nil {
		return "", err
	}
	var meta struct {
		PublicURL string `json:"public_url"`
	}
	if err := json.Unmarshal(raw, &meta); err != nil {
		return "", err
	}
	if strings.TrimSpace(meta.PublicURL) == "" {
		return "", fmt.Errorf("Yandex 没有返回 public_url")
	}
	return meta.PublicURL, nil
}

func (y *Yandex) api(ctx context.Context, method, p string, payload []byte, _ any) ([]byte, error) {
	attempts := y.attempts()
	var last error
	for i := 0; i < attempts; i++ {
		raw, err := y.apiOnce(ctx, method, p, payload)
		if err == nil {
			return raw, nil
		}
		last = err
		if !retryable(err) || i == attempts-1 {
			break
		}
		if err := sleep(ctx, time.Duration(i+1)*200*time.Millisecond); err != nil {
			return nil, err
		}
	}
	return nil, last
}

func (y *Yandex) attempts() int {
	if y.Attempts < 1 {
		return 1
	}
	return y.Attempts
}

func (y *Yandex) apiOnce(ctx context.Context, method, p string, payload []byte) ([]byte, error) {
	var body io.Reader
	if payload != nil {
		body = bytes.NewReader(payload)
	}
	req, err := http.NewRequestWithContext(ctx, method, y.API+p, body)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "OAuth "+y.Token)
	req.Header.Set("Accept", "application/json")
	resp, err := y.client().Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := strings.TrimSpace(string(raw))
		var env struct {
			Message     string `json:"message"`
			Description string `json:"description"`
			Error       string `json:"error"`
		}
		if json.Unmarshal(raw, &env) == nil {
			parts := make([]string, 0, 3)
			for _, part := range []string{env.Error, env.Message, env.Description} {
				if strings.TrimSpace(part) != "" {
					parts = append(parts, strings.TrimSpace(part))
				}
			}
			if len(parts) > 0 {
				msg = strings.Join(parts, ": ")
			}
		}
		return nil, &httpStatus{Code: resp.StatusCode, Body: msg}
	}
	return raw, nil
}

func (y *Yandex) client() *http.Client {
	if y.HTTP != nil {
		return y.HTTP
	}
	return http.DefaultClient
}

type httpStatus struct {
	Code int
	Body string
}

func (e *httpStatus) Error() string {
	if e.Body == "" {
		return fmt.Sprintf("Yandex Disk HTTP %d", e.Code)
	}
	return fmt.Sprintf("Yandex Disk HTTP %d: %s", e.Code, e.Body)
}

func retryable(err error) bool {
	var status *httpStatus
	if errorsAs(err, &status) {
		return status.Code == 429 || status.Code >= 500
	}
	return err != nil && !errorsIsUnconfigured(err)
}

func errorsAs(err error, target **httpStatus) bool {
	if err == nil {
		return false
	}
	if status, ok := err.(*httpStatus); ok {
		*target = status
		return true
	}
	return false
}

func errorsIsUnconfigured(err error) bool {
	return err == ErrUnconfigured
}

func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func contentType(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	if ext == ".jpg" {
		return "image/jpeg"
	}
	if t := mime.TypeByExtension(ext); t != "" {
		return t
	}
	return "application/octet-stream"
}

func isDirect(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme != "https" || u.Host == "" {
		return false
	}
	host := strings.ToLower(u.Hostname())
	switch host {
	case "yadi.sk", "disk.yandex.ru", "disk.yandex.com", "disk.360.yandex.ru":
		return false
	default:
		return true
	}
}
