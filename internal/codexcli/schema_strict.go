package codexcli

import (
	"encoding/json"
	"fmt"
)

// StrictProblems reports keywords Codex/OpenAI structured outputs reject,
// and objects that are not closed (additionalProperties false, every property required).
func StrictProblems(schema []byte) []string {
	var node any
	if err := json.Unmarshal(schema, &node); err != nil {
		return []string{fmt.Sprintf("schema 无法解析: %v", err)}
	}
	var problems []string
	walkStrict(node, "$", &problems)
	return problems
}

func walkStrict(node any, path string, problems *[]string) {
	obj, ok := node.(map[string]any)
	if !ok {
		return
	}
	for _, banned := range []string{"oneOf", "allOf", "not", "pattern", "format", "minLength", "maxLength", "minimum", "maximum", "minItems", "maxItems", "uniqueItems", "patternProperties", "unevaluatedProperties"} {
		if _, exists := obj[banned]; exists {
			*problems = append(*problems, fmt.Sprintf("%s 含有不受支持的关键字 %s", path, banned))
		}
	}
	props, hasProps := obj["properties"].(map[string]any)
	if hasProps {
		ap, exists := obj["additionalProperties"]
		if !exists || ap != false {
			*problems = append(*problems, fmt.Sprintf("%s 的 additionalProperties 必须是 false", path))
		}
		required := map[string]bool{}
		if list, ok := obj["required"].([]any); ok {
			for _, item := range list {
				name, _ := item.(string)
				required[name] = true
			}
		}
		for name, child := range props {
			if !required[name] {
				*problems = append(*problems, fmt.Sprintf("%s 的属性 %s 没有列入 required", path, name))
			}
			walkStrict(child, path+"."+name, problems)
		}
	}
	if items, exists := obj["items"]; exists {
		walkStrict(items, path+"[]", problems)
	}
	if defs, exists := obj["$defs"]; exists {
		walkStrict(defs, path+".$defs", problems)
	}
	for _, key := range []string{"anyOf", "oneOf", "allOf"} {
		list, ok := obj[key].([]any)
		if !ok {
			continue
		}
		for i, child := range list {
			walkStrict(child, fmt.Sprintf("%s.%s[%d]", path, key, i), problems)
		}
	}
}
