package biz

import (
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/google/uuid"
)

// MockRunner walks the task state machine without a Codex binary.
// The first turn always asks for missing required attributes. After the operator
// answers, it emits a text draft with sub-SKUs, then a visual plan, and it
// answers follow-ups in the same session id.
type MockRunner struct{}

func NewMockRunner() *MockRunner { return &MockRunner{} }

func (m *MockRunner) Run(ctx context.Context, req RunRequest) (*RunResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	session := req.SessionID
	if session == "" {
		session = "mock-" + uuid.NewString()
	}
	switch req.Stage {
	case StageExtract:
		fields := missingFrom(req.Attributes)
		body := "素材里缺少 Ozon 必填信息，需要补充后再继续。\n\n" + SignalFence(map[string]any{
			"signal":         "need_input",
			"missing_fields": fields,
		})
		return &RunResult{SessionID: session, Messages: []RunnerMessage{
			{Role: RoleToolCall, Stage: "解析", ToolCallID: session + "-manifest", Content: mustJSON(map[string]any{
				"tool": "read_material_manifest", "arguments": map[string]any{"master_sku": req.MasterSKU, "dir": req.WorkDir},
			})},
			{Role: RoleToolResult, Stage: "解析", ToolCallID: session + "-manifest", Content: mustJSON(map[string]any{
				"tool": "read_material_manifest", "master_sku": req.MasterSKU, "ok": true,
			})},
			{Role: RoleAssistant, Stage: "解析", Content: body},
		}}, nil
	case StageResume:
		first, second := req.MasterSKU+"-1", req.MasterSKU+"-2"
		body := fmt.Sprintf("文案草稿\n俄文标题：Товар %s\n变体：%s、%s\n请核对名称和规格。\n\n", req.MasterSKU, first, second)
		body += SignalFence(map[string]any{
			"signal": "text_draft",
			"sub_skus": []map[string]string{
				{"offer_id": first, "specs": "规格甲"},
				{"offer_id": second, "specs": "规格乙"},
			},
			"title_ru":       "Товар " + req.MasterSKU,
			"selling_points": []string{"本地素材已解析", "按主 SKU 生成变体"},
			"attributes":     req.Answers,
		})
		return &RunResult{SessionID: session, Messages: []RunnerMessage{
			{Role: RoleAssistant, Stage: "文案", Content: body},
		}}, nil
	case StageVisual:
		images := listImages(req.WorkDir)
		items := []map[string]string{}
		for _, offer := range []string{req.MasterSKU + "-1", req.MasterSKU + "-2"} {
			image := ""
			if len(images) > 0 {
				image = images[0]
			}
			note := "使用素材包中的原图作为主图，并按变体核对颜色。"
			if image == "" {
				note = "素材包中没有图片文件，视觉方案保留为待生成占位。"
			}
			items = append(items, map[string]string{"offer_id": offer, "primary_image": image, "notes": note})
		}
		body := "视觉方案\n主图沿用素材包图片，副图说明使用场景。\n\n" + SignalFence(map[string]any{
			"signal": "visual_plan",
			"items":  items,
		})
		return &RunResult{SessionID: session, Messages: []RunnerMessage{
			{Role: RoleToolCall, Stage: "视觉方案", ToolCallID: session + "-plan", Content: mustJSON(map[string]any{
				"tool": "save_visual_plan", "arguments": map[string]any{"master_sku": req.MasterSKU},
			})},
			{Role: RoleToolResult, Stage: "视觉方案", ToolCallID: session + "-plan", Content: mustJSON(map[string]any{
				"tool": "save_visual_plan", "stored": "visual_plan", "master_sku": req.MasterSKU,
			})},
			{Role: RoleAssistant, Stage: "视觉方案", Content: body},
		}}, nil
	case StageListing:
		body := "商品参数\n\n" + SignalFenceItem(mockListingItem(req))
		return &RunResult{SessionID: session, Messages: []RunnerMessage{
			{Role: RoleAssistant, Stage: "商品参数", Content: body},
		}}, nil
	case StageFollowUp:
		reply := fmt.Sprintf("当前主 SKU 为 %s。已在同一会话记录这次追问：\n%s", req.MasterSKU, req.Prompt)
		return &RunResult{SessionID: session, Messages: []RunnerMessage{
			{Role: RoleAssistant, Stage: "追问", Content: reply},
		}}, nil
	default:
		return nil, fmt.Errorf("未知的 Codex 阶段 %s", req.Stage)
	}
}

func missingFrom(attrs []map[string]any) []MissingField {
	var out []MissingField
	for _, attr := range attrs {
		if !truthy(attr["is_required"]) {
			continue
		}
		id := strings.TrimSpace(fmt.Sprint(attr["id"]))
		if id == "" || id == "<nil>" {
			id = fmt.Sprintf("%d", len(out)+1)
		}
		label := strings.TrimSpace(fmt.Sprint(attr["name"]))
		if label == "" || label == "<nil>" {
			label = "属性 " + id
		}
		out = append(out, MissingField{
			Key: "attr_" + id, Label: label, Required: true, Hint: "素材中未找到该必填属性",
		})
	}
	if len(out) == 0 {
		out = append(out, MissingField{Key: "brand", Label: "品牌", Required: true, Hint: "素材中未找到品牌"})
	}
	return out
}

func listImages(dir string) []string {
	if dir == "" {
		return nil
	}
	var out []string
	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return nil
		}
		switch strings.ToLower(filepath.Ext(path)) {
		case ".jpg", ".jpeg", ".png", ".webp", ".gif":
			rel, relErr := filepath.Rel(dir, path)
			if relErr == nil {
				out = append(out, filepath.ToSlash(rel))
			}
		}
		if len(out) >= 20 {
			return fs.SkipAll
		}
		return nil
	})
	return out
}

func mockListingItem(req RunRequest) map[string]any {
	facts := req.Listing
	if facts == nil {
		facts = &ListingFacts{OfferID: req.MasterSKU, Title: req.MasterSKU, Attributes: req.Attributes, Answers: req.Answers}
	}
	price := strings.TrimSpace(facts.Price)
	if price == "" {
		price = "79.00"
	}
	currency := strings.TrimSpace(facts.Currency)
	image := strings.TrimSpace(facts.PrimaryImage)
	images := append([]string(nil), facts.Images...)
	attrs := make([]map[string]any, 0)
	for _, attr := range facts.Attributes {
		if !truthy(attr["is_required"]) {
			continue
		}
		id := int64FromAny(attr["id"])
		value := ""
		if facts.Answers != nil {
			value = facts.Answers[fmt.Sprintf("attr_%d", id)]
		}
		if value == "" {
			value = facts.Specs
		}
		if value == "" {
			value = "未填写"
		}
		entry := map[string]any{"complex_id": int64FromAny(attr["attribute_complex_id"]), "id": id}
		if int64FromAny(attr["dictionary_id"]) > 0 {
			dictValue := int64(5055881)
			if id != 85 {
				dictValue = 1
			}
			entry["values"] = []map[string]any{{"dictionary_value_id": dictValue, "value": value}}
		} else {
			entry["values"] = []map[string]any{{"value": value}}
		}
		attrs = append(attrs, entry)
	}
	return map[string]any{
		"offer_id":                facts.OfferID,
		"description_category_id": facts.DescriptionCategoryID,
		"type_id":                 facts.TypeID,
		"name":                    firstNonEmpty(facts.Title, "Товар "+facts.OfferID),
		"price":                   price,
		"currency_code":           currency,
		"depth":                   120,
		"width":                   80,
		"height":                  40,
		"dimension_unit":          "mm",
		"weight":                  180,
		"weight_unit":             "g",
		"vat":                     "0",
		"primary_image":           image,
		"images":                  images,
		"attributes":              attrs,
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func int64FromAny(v any) int64 {
	switch t := v.(type) {
	case int64:
		return t
	case int:
		return int64(t)
	case float64:
		return int64(t)
	case json.Number:
		n, _ := t.Int64()
		return n
	default:
		return 0
	}
}

func mustJSON(v any) string {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return "{}"
	}
	return string(raw)
}

// ReadableDir reports whether the extracted package is still on disk.
func ReadableDir(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.IsDir()
}
