package biz

import (
	"strings"
	"testing"
)

func TestRenderPromptInjectsMasterSKUAndAttributes(t *testing.T) {
	got, err := RenderPrompt("按 {{master_sku}} 生成子 SKU，例如 {{master_sku}}-1。", "30", []map[string]any{
		{"id": 85, "name": "品牌", "is_required": true},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "{{master_sku}}") {
		t.Fatalf("placeholder left in prompt: %s", got)
	}
	if !strings.Contains(got, "按 30 生成子 SKU，例如 30-1。") {
		t.Fatalf("master sku was not rendered: %s", got)
	}
	if !strings.Contains(got, `"name": "品牌"`) || !strings.Contains(got, "类目属性契约") {
		t.Fatalf("attribute contract missing: %s", got)
	}
}

func TestRenderPromptReplacesAttributesPlaceholder(t *testing.T) {
	got, err := RenderPrompt("主 SKU {{master_sku}}\n属性：\n{{attributes}}\n结束", "058", []map[string]any{
		{"id": 1, "name": "颜色"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(got, "{{attributes}}") || strings.Contains(got, "## 类目属性契约") {
		t.Fatalf("attributes placeholder was not consumed: %s", got)
	}
	if !strings.Contains(got, "主 SKU 058") || !strings.Contains(got, `"name": "颜色"`) || !strings.Contains(got, "结束") {
		t.Fatalf("rendered = %s", got)
	}
}

func TestRenderPromptStillInjectsWhenPlaceholdersAreMissing(t *testing.T) {
	got, err := RenderPrompt("只写提取规则。", "30", nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "主 SKU：30\n") {
		t.Fatalf("master sku was not injected: %s", got)
	}
	if !strings.Contains(got, "类目属性契约") || !strings.Contains(got, "[]") {
		t.Fatalf("empty contract missing: %s", got)
	}
}

func TestRenderPromptRejectsEmptyInputs(t *testing.T) {
	if _, err := RenderPrompt("  ", "30", nil); err == nil {
		t.Fatal("expected empty template to fail")
	}
	if _, err := RenderPrompt("规则 {{master_sku}}", "  ", nil); err == nil {
		t.Fatal("expected empty master sku to fail")
	}
}
