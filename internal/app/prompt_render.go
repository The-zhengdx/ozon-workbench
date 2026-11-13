package app

import (
	"regexp"
	"strings"
)

// knownPromptPlaceholders 是通用提示词里允许被替换的占位符。
// 未知的 {{name}} 原样留下，避免把用户没声明的词悄悄换成空。
var promptPlaceholder = regexp.MustCompile(`\{\{\s*([A-Za-z0-9_]+)\s*\}\}`)

func ComposePrompt(generic, category string) string {
	generic = strings.TrimSpace(generic)
	category = strings.TrimSpace(category)
	switch {
	case generic == "":
		return category
	case category == "":
		return generic
	default:
		return generic + "\n\n" + category
	}
}

func RenderPrompt(template, mainSKU string) string {
	mainSKU = strings.TrimSpace(mainSKU)
	return promptPlaceholder.ReplaceAllStringFunc(template, func(match string) string {
		parts := promptPlaceholder.FindStringSubmatch(match)
		if len(parts) != 2 || parts[1] != "main_sku" {
			return match
		}
		return mainSKU
	})
}
