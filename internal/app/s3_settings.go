package app

import (
	"context"
	"encoding/json"
	"net/url"
	"strings"

	"ozon-workbench/internal/storage"
	"ozon-workbench/internal/store"
)

func (s S3Settings) ready() bool {
	return strings.TrimSpace(s.Endpoint) != "" &&
		strings.TrimSpace(s.Bucket) != "" &&
		strings.TrimSpace(s.AccessKeyID) != "" &&
		strings.TrimSpace(s.SecretAccessKey) != "" &&
		strings.TrimSpace(s.PublicBaseURL) != ""
}

func (s S3Settings) s3Config() storage.S3Config {
	region := strings.TrimSpace(s.Region)
	if region == "" {
		region = "us-east-1"
	}
	return storage.S3Config{
		Endpoint:    strings.TrimRight(strings.TrimSpace(s.Endpoint), "/"),
		Region:      region,
		Bucket:      strings.TrimSpace(s.Bucket),
		AccessKey:   strings.TrimSpace(s.AccessKeyID),
		SecretKey:   s.SecretAccessKey,
		PublicBase:  strings.TrimRight(strings.TrimSpace(s.PublicBaseURL), "/"),
		VirtualHost: !s.PathStyle,
	}
}

func validateS3(s S3Settings) error {
	if !s.ready() {
		if strings.TrimSpace(s.Endpoint) == "" && strings.TrimSpace(s.Bucket) == "" &&
			strings.TrimSpace(s.AccessKeyID) == "" && strings.TrimSpace(s.SecretAccessKey) == "" &&
			strings.TrimSpace(s.PublicBaseURL) == "" {
			return nil
		}
		return fail(400, "S3_INVALID", "S3 需要 Endpoint、Bucket、Access Key、Secret Key 和公开基址，或者全部留空", nil)
	}
	parsed, err := url.Parse(strings.TrimSpace(s.PublicBaseURL))
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return fail(400, "S3_INVALID", "公开基址必须是 https 地址", nil)
	}
	if _, err := url.Parse(strings.TrimSpace(s.Endpoint)); err != nil {
		return fail(400, "S3_INVALID", "Endpoint 不是合法地址", nil)
	}
	return nil
}

func (e *Engine) loadS3(ctx context.Context) S3Settings {
	out := S3Settings{PathStyle: true}
	if e == nil || e.DB == nil {
		return out
	}
	_ = e.DB.InTx(ctx, func(tx store.Tx) error {
		raw, ok, err := tx.GetConfig(ctx, "s3")
		if err != nil || !ok {
			return err
		}
		return json.Unmarshal(raw, &out)
	})
	return out
}

// TestS3 写入一个小对象，确认对象和公开地址都能读到，然后删掉。
func (e *Engine) TestS3(ctx context.Context, cfg S3Settings) error {
	if err := validateS3(cfg); err != nil {
		return err
	}
	if !cfg.ready() {
		return fail(400, "S3_INVALID", "先把 S3 配置填完整再测试", nil)
	}
	client := storage.NewS3(cfg.s3Config())
	key := MediaObjectKey(cfg.KeyPrefix, "_health", "ping", "image", "text/plain", "ping.txt")
	body := []byte("ozon-workbench-s3-ping")
	if err := client.Put(ctx, key, body, "text/plain"); err != nil {
		return fail(502, "S3_UNAVAILABLE", "写入测试对象失败", err.Error())
	}
	if err := client.Head(ctx, key); err != nil {
		_ = client.Delete(ctx, key)
		return fail(502, "S3_UNAVAILABLE", "读回测试对象失败", err.Error())
	}
	if err := client.CheckPublic(ctx, client.PublicURL(key)); err != nil {
		_ = client.Delete(ctx, key)
		return fail(502, "S3_UNAVAILABLE", "公开地址读不到测试对象", err.Error())
	}
	if err := client.Delete(ctx, key); err != nil {
		return fail(502, "S3_UNAVAILABLE", "删除测试对象失败", err.Error())
	}
	return nil
}
