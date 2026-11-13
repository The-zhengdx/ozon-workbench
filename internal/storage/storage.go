// Package storage turns a local media file into a public HTTPS URL.
// Ozon product import only accepts URLs it can download itself.
// The active provider is chosen from configuration. "none" (the default)
// keeps the process usable and refuses uploads with a clear error.
package storage

import (
	"context"
	"errors"
	"strings"
)

// ErrUnconfigured is returned when no provider can publish a public URL.
var ErrUnconfigured = errors.New("OSS 未配置。需要可长期访问的 HTTPS 地址时，先在系统设置里填写 S3 Endpoint、Bucket、密钥和公开基址。")

// Object is one uploaded file.
// PublicURL is the direct download URL handed to Ozon.
// PublicKey is a stable handle used to refresh PublicURL when a provider needs it.
type Object struct {
	Key       string
	PublicURL string
	PublicKey string
	Size      int64
	MIME      string
}

// Store uploads, deletes and checks one media backend.
// Add a provider by implementing this interface and registering it in Open.
type Store interface {
	Name() string
	Configured() bool
	Message() string
	Upload(ctx context.Context, localPath, key string) (Object, error)
	Delete(ctx context.Context, key string) error
	Health(ctx context.Context) error
	Refresh(ctx context.Context, publicKey string) (string, error)
}

// Unconfigured reports whether err is the empty-provider refusal.
func Unconfigured(err error) bool {
	return errors.Is(err, ErrUnconfigured)
}

// Disabled is a named provider that cannot upload yet.
type Disabled struct {
	Provider string
	Reason   string
}

func (d Disabled) Name() string {
	if strings.TrimSpace(d.Provider) == "" {
		return "none"
	}
	return d.Provider
}

func (d Disabled) Configured() bool { return false }

func (d Disabled) Message() string {
	if strings.TrimSpace(d.Reason) != "" {
		return d.Reason
	}
	return ErrUnconfigured.Error()
}

func (d Disabled) Upload(context.Context, string, string) (Object, error) {
	return Object{}, ErrUnconfigured
}

func (d Disabled) Delete(context.Context, string) error { return ErrUnconfigured }

func (d Disabled) Health(context.Context) error { return ErrUnconfigured }

func (d Disabled) Refresh(context.Context, string) (string, error) { return "", ErrUnconfigured }
