package app

import "strings"

// normLang 把页面上的语言收成 Ozon description-category 接口接受的值。
// 中文必须是 ZH_HANS。只写 ZH 时 Ozon 会忽略语言，返回俄文名称。
func normLang(language string) string {
	switch strings.TrimSpace(language) {
	case "中文", "简体中文":
		return "ZH_HANS"
	case "英文", "英语":
		return "EN"
	case "俄文", "俄语":
		return "RU"
	}
	switch strings.ToUpper(strings.TrimSpace(strings.ReplaceAll(language, "-", "_"))) {
	case "", "RU", "RUSSIAN":
		return "RU"
	case "EN", "ENGLISH":
		return "EN"
	case "ZH", "ZH_HANS", "ZH_CN", "CN":
		return "ZH_HANS"
	case "DEFAULT":
		return "DEFAULT"
	default:
		value := strings.ToUpper(strings.TrimSpace(language))
		if value == "" {
			return "RU"
		}
		return value
	}
}

func displayLangs(requested string) []string {
	requested = normLang(requested)
	out := []string{requested}
	for _, candidate := range []string{"ZH_HANS", "EN", "RU"} {
		if candidate != requested {
			out = append(out, candidate)
		}
	}
	return out
}
