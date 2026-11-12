package conf

import (
	"fmt"
	"net/url"
	"strings"
)

// NormalizeOzonProxy accepts three forms:
//   - "" / direct / off / none: dial api-seller.ozon.ru directly and ignore HTTP_PROXY, HTTPS_PROXY, and ALL_PROXY
//   - env / environment: use those variables (and NO_PROXY)
//   - an http, https, or socks5 URL
//
// Go does not read a proxy application's own rule list. TUN/fake-ip interception happens before this choice.
func NormalizeOzonProxy(raw string) (string, error) {
	v := strings.TrimSpace(raw)
	switch strings.ToLower(v) {
	case "", "direct", "off", "none":
		return "direct", nil
	case "env", "environment":
		return "env", nil
	}
	u, err := url.Parse(v)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("ozon.proxy 无效: %q。留空或 direct 表示直连并忽略 HTTP_PROXY/HTTPS_PROXY/ALL_PROXY，env 表示跟随这些环境变量，或填写 http、https、socks5 代理 URL", raw)
	}
	switch strings.ToLower(u.Scheme) {
	case "http", "https", "socks5":
	default:
		return "", fmt.Errorf("ozon.proxy 不支持协议 %q。请使用 http、https 或 socks5 URL", u.Scheme)
	}
	return u.String(), nil
}
