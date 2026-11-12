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
