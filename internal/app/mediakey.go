package app

import (
	"path/filepath"
	"strings"
	"unicode"
)

// MediaObjectKey 是 {prefix}/{client_id}/{sku}/images|videos/{file}。
// sku 为空时用主 SKU。路径段只保留字母、数字、点、下划线和连字符。
func MediaObjectKey(prefix, clientID, sku, role, mime, filename string) string {
	folder := "images"
	blob := strings.ToLower(role + " " + mime + " " + filename)
	if strings.Contains(blob, "video") {
		folder = "videos"
	}
	if strings.TrimSpace(sku) == "" {
		sku = "sku"
	}
	name := safeFileName(filename)
	parts := make([]string, 0, 6)
	for _, part := range strings.Split(prefix, "/") {
		if clean := sanitizeSegment(part); clean != "" && part != "" {
			parts = append(parts, clean)
		}
	}
	parts = append(parts, sanitizeSegment(clientID), sanitizeSegment(sku), folder, name)
	return strings.Join(parts, "/")
}

func sanitizeSegment(value string) string {
	value = strings.TrimSpace(value)
	var b strings.Builder
	for _, r := range value {
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '.' || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	out := strings.Trim(b.String(), "._")
	if out == "" || out == "." || out == ".." {
		return "x"
	}
	return out
}

func safeFileName(filename string) string {
	base := filepath.Base(strings.TrimSpace(filename))
	ext := strings.ToLower(filepath.Ext(base))
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	if len(ext) > 8 || strings.Contains(ext, "/") || strings.Contains(ext, "\\") {
		ext = ""
	}
	name := sanitizeSegment(stem)
	if ext != "" {
		name += ext
	}
	if name == "" || name == ext {
		name = "file" + ext
	}
	return name
}
