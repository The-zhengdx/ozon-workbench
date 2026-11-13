package codexcli

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode"
)

func isWindowsBatch(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".bat", ".cmd":
		return true
	default:
		return false
	}
}

// batCommandLine 把 .cmd/.bat 交给 cmd.exe。CreateProcess 不能直接启动批处理。
func batCommandLine(script string, args []string) (string, error) {
	if script == "" || strings.ContainsAny(script, "\"\r\n\x00") || strings.HasSuffix(script, `\`) {
		return "", fmt.Errorf("Codex CLI 路径无效")
	}
	for _, arg := range args {
		if strings.ContainsAny(arg, "\r\n\x00") {
			return "", fmt.Errorf("Codex 参数无效")
		}
	}
	var b strings.Builder
	b.WriteString(`cmd.exe /e:ON /v:OFF /d /c "`)
	b.WriteByte('"')
	writeBatText(&b, script)
	b.WriteByte('"')
	for _, arg := range args {
		b.WriteByte(' ')
		appendBatArg(&b, arg)
	}
	b.WriteByte('"')
	return b.String(), nil
}

func writeBatText(b *strings.Builder, s string) {
	for _, r := range s {
		if r == '%' || r == '\r' {
			b.WriteString("%%cd:~,")
		}
		b.WriteRune(r)
	}
}

func appendBatArg(b *strings.Builder, arg string) {
	quote := arg == "" || strings.HasSuffix(arg, `\`)
	const unquoted = "#$*+-./:?@_\\"
	for _, r := range arg {
		safeASCII := r < 0x80 && (isASCIIAlnum(r) || strings.ContainsRune(unquoted, r))
		if (r < 0x80 && !safeASCII) || (r >= 0x80 && unicode.IsControl(r)) {
			quote = true
		}
	}
	if quote {
		b.WriteByte('"')
	}
	backslashes := 0
	for _, r := range arg {
		if r == '\\' {
			backslashes++
			b.WriteRune(r)
			continue
		}
		if r == '"' {
			for i := 0; i < backslashes; i++ {
				b.WriteByte('\\')
			}
			b.WriteByte('"')
		} else if r == '%' || r == '\r' {
			b.WriteString("%%cd:~,")
		}
		backslashes = 0
		b.WriteRune(r)
	}
	if quote {
		for i := 0; i < backslashes; i++ {
			b.WriteByte('\\')
		}
		b.WriteByte('"')
	}
}

func isASCIIAlnum(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9')
}
