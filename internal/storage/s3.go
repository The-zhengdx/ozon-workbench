package storage

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"strings"
	"time"
)

// S3 是兼容 AWS Signature V4 的对象存储。公开地址来自配置的长期基址，不用预签名链接。
type S3 struct {
	Endpoint   string
	Region     string
	Bucket     string
	AccessKey  string
	SecretKey  string
	PublicBase string
	HTTP       *http.Client
	Now        func() time.Time
}

type S3Config struct {
	Endpoint, Region, Bucket, AccessKey, SecretKey, PublicBase string
}

func NewS3(cfg S3Config) *S3 {
	region := cfg.Region
	if region == "" {
		region = "us-east-1"
	}
	return &S3{
		Endpoint: strings.TrimRight(cfg.Endpoint, "/"), Region: region, Bucket: cfg.Bucket,
		AccessKey: cfg.AccessKey, SecretKey: cfg.SecretKey, PublicBase: strings.TrimRight(cfg.PublicBase, "/"),
		HTTP: &http.Client{Timeout: 60 * time.Second}, Now: time.Now,
	}
}

func (s *S3) Name() string { return "s3" }
func (s *S3) Configured() bool {
	return s.AccessKey != "" && s.SecretKey != "" && s.Bucket != "" && strings.HasPrefix(s.PublicBase, "https://")
}
func (s *S3) Message() string {
	return "S3 兼容存储已配置。商品图使用公开基址，不使用预签名 URL。"
}
func (s *S3) Health(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodHead, s.Endpoint+"/"+s.Bucket, nil)
	if err != nil {
		return err
	}
	s.sign(req, nil)
	res, err := s.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 400 && res.StatusCode != http.StatusNotFound {
		return fmt.Errorf("S3 HTTP %d", res.StatusCode)
	}
	return nil
}

func (s *S3) Upload(ctx context.Context, localPath, key string) (Object, error) {
	key = strings.Trim(key, "/")
	if key == "" || strings.Contains(key, "..") {
		return Object{}, fmt.Errorf("非法的对象键")
	}
	body, err := os.ReadFile(localPath)
	if err != nil {
		return Object{}, err
	}
	endpoint := s.Endpoint + "/" + s.Bucket + "/" + escapeKey(key)
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, endpoint, bytes.NewReader(body))
	if err != nil {
		return Object{}, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	s.sign(req, body)
	res, err := s.HTTP.Do(req)
	if err != nil {
		return Object{}, err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 {
		msg, _ := io.ReadAll(io.LimitReader(res.Body, 2048))
		return Object{}, fmt.Errorf("S3 HTTP %d %s", res.StatusCode, strings.TrimSpace(string(msg)))
	}
	return Object{Key: key, PublicURL: s.PublicBase + "/" + escapeKey(key), Size: int64(len(body))}, nil
}

func (s *S3) Delete(ctx context.Context, key string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, s.Endpoint+"/"+s.Bucket+"/"+escapeKey(key), nil)
	if err != nil {
		return err
	}
	s.sign(req, nil)
	res, err := s.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer res.Body.Close()
	if res.StatusCode >= 300 && res.StatusCode != http.StatusNotFound {
		return fmt.Errorf("S3 HTTP %d", res.StatusCode)
	}
	return nil
}

func (s *S3) Refresh(context.Context, string) (string, error) {
	return "", fmt.Errorf("S3 使用配置的公开基址，不刷新签名")
}

func (s *S3) sign(req *http.Request, body []byte) {
	now := s.Now().UTC()
	amzDate := now.Format("20060102T150405Z")
	short := now.Format("20060102")
	payload := sha256.Sum256(body)
	payloadHex := hex.EncodeToString(payload[:])
	req.Header.Set("x-amz-date", amzDate)
	req.Header.Set("x-amz-content-sha256", payloadHex)
	req.Header.Set("Host", req.URL.Host)
	canonicalHeaders := "host:" + req.URL.Host + "\n" + "x-amz-content-sha256:" + payloadHex + "\n" + "x-amz-date:" + amzDate + "\n"
	signed := "host;x-amz-content-sha256;x-amz-date"
	canonical := strings.Join([]string{req.Method, req.URL.EscapedPath(), req.URL.RawQuery, canonicalHeaders, signed, payloadHex}, "\n")
	hash := sha256.Sum256([]byte(canonical))
	scope := short + "/" + s.Region + "/s3/aws4_request"
	stringToSign := "AWS4-HMAC-SHA256\n" + amzDate + "\n" + scope + "\n" + hex.EncodeToString(hash[:])
	signing := hmacSHA([]byte("AWS4"+s.SecretKey), short)
	signing = hmacSHA(signing, s.Region)
	signing = hmacSHA(signing, "s3")
	signing = hmacSHA(signing, "aws4_request")
	sig := hex.EncodeToString(hmacSHA(signing, stringToSign))
	req.Header.Set("Authorization", fmt.Sprintf("AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s", s.AccessKey, scope, signed, sig))
}

func hmacSHA(key []byte, value string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(value))
	return mac.Sum(nil)
}

func escapeKey(key string) string {
	parts := strings.Split(key, "/")
	for i, part := range parts {
		parts[i] = url.PathEscape(part)
	}
	return path.Clean(strings.Join(parts, "/"))
}
