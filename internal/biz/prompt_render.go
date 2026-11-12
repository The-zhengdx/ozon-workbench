package biz

import (
	"encoding/json"
	"strings"
)

// RenderPrompt injects the master SKU and the attribute contract into a saved category prompt.
// {{master_sku}} is replaced everywhere. {{attributes}} is replaced with the contract JSON.
// When a placeholder is absent, the value is still added so Codex receives both inputs.
func RenderPrompt(template, masterSKU string, attributes []map[string]any) (string, error) {
	template = strings.TrimSpace(template)
	masterSKU = strings.TrimSpace(masterSKU)
	if template == "" {
		return "", bad("PROMPT_EMPTY", "这个末级类型还没有保存 Codex 提示词。", map[string]string{
			"type_id": "请先为这个末级类型保存 Codex 提示词。",
		})
	}
	if masterSKU == "" {
		return "", bad("SKU_REQUIRED", "请填写主 SKU。", map[string]string{
			"master_sku": "请填写主 SKU。",
		})
	}
	if attributes == nil {
		attributes = []map[string]any{}
	}
	raw, err := json.MarshalIndent(attributes, "", "  ")
	if err != nil {
		return "", err
	}
	rendered := template
	if strings.Contains(template, "{{master_sku}}") {
		rendered = strings.ReplaceAll(template, "{{master_sku}}", masterSKU)
	} else {
		rendered = "主 SKU：" + masterSKU + "\n\n" + template
	}
	if strings.Contains(rendered, "{{attributes}}") {
		rendered = strings.ReplaceAll(rendered, "{{attributes}}", string(raw))
	} else {
		rendered += "\n\n## 类目属性契约\n" + string(raw) + "\n"
	}
	return rendered, nil
}
