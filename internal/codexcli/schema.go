package codexcli

import (
	"encoding/json"
	"fmt"
	"math"
	"strings"
)

// ValidateSchema 按 02_codex_reply_v1_2.schema.json 的子集校验：type、enum、required、additionalProperties=false、items。
func ValidateSchema(schema, doc []byte) error {
	var schemaV any
	var docV any
	if err := json.Unmarshal(schema, &schemaV); err != nil {
		return fmt.Errorf("schema 无法解析: %w", err)
	}
	if err := json.Unmarshal(doc, &docV); err != nil {
		return fmt.Errorf("回复不是 JSON: %w", err)
	}
	return walkSchema(schemaV, docV, "$")
}

func walkSchema(schema, val any, path string) error {
	sm, ok := schema.(map[string]any)
	if !ok {
		return fmt.Errorf("%s schema 不是对象", path)
	}
	if err := checkType(sm["type"], val, path); err != nil {
		return err
	}
	if enum, ok := sm["enum"].([]any); ok {
		if !inEnum(enum, val) {
			return fmt.Errorf("%s 不在枚举内", path)
		}
	}
	if val == nil {
		return nil
	}
	if obj, ok := val.(map[string]any); ok {
		props, _ := sm["properties"].(map[string]any)
		if req, ok := sm["required"].([]any); ok {
			for _, item := range req {
				name, _ := item.(string)
				if _, exists := obj[name]; !exists {
					return fmt.Errorf("%s 缺少 %s", path, name)
				}
			}
		}
		if ap, ok := sm["additionalProperties"]; ok && ap == false {
			for key := range obj {
				if _, exists := props[key]; !exists {
					return fmt.Errorf("%s 含有未定义字段 %s", path, key)
				}
			}
		}
		for key, child := range obj {
			if props == nil {
				continue
			}
			if ps, exists := props[key]; exists {
				if err := walkSchema(ps, child, path+"."+key); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if arr, ok := val.([]any); ok {
		if items, exists := sm["items"]; exists {
			for i, child := range arr {
				if err := walkSchema(items, child, fmt.Sprintf("%s[%d]", path, i)); err != nil {
					return err
				}
			}
		}
	}
	return nil
}

func checkType(spec, val any, path string) error {
	if spec == nil {
		return nil
	}
	allowed := []string{}
	switch t := spec.(type) {
	case string:
		allowed = []string{t}
	case []any:
		for _, item := range t {
			name, _ := item.(string)
			allowed = append(allowed, name)
		}
	default:
		return fmt.Errorf("%s 的 type 无法识别", path)
	}
	got := jsonType(val)
	for _, name := range allowed {
		if name == got || (got == "integer" && name == "number") {
			return nil
		}
	}
	return fmt.Errorf("%s 类型是 %s，需要 %s", path, got, strings.Join(allowed, "|"))
}

func jsonType(val any) string {
	switch v := val.(type) {
	case nil:
		return "null"
	case string:
		return "string"
	case bool:
		return "boolean"
	case float64:
		if math.Trunc(v) == v && !math.IsInf(v, 0) {
			return "integer"
		}
		return "number"
	case []any:
		return "array"
	case map[string]any:
		return "object"
	default:
		return "unknown"
	}
}

func inEnum(enum []any, val any) bool {
	raw, _ := json.Marshal(val)
	for _, item := range enum {
		other, _ := json.Marshal(item)
		if string(raw) == string(other) {
			return true
		}
	}
	return false
}

// BusinessError 是 Schema 通过之后仍不满足状态约定的原因。
func BusinessError(reply Reply) error {
	switch reply.Status {
	case "needs_input":
		if len(reply.Questions) == 0 {
			return fmt.Errorf("needs_input 必须带 questions")
		}
	case "pending_review":
		if reply.Review == nil {
			return fmt.Errorf("pending_review 必须带 review")
		}
	case "media_ready":
		if len(reply.Data.Media) == 0 {
			return fmt.Errorf("media_ready 必须带 data.media")
		}
	case "completed":
		if len(reply.Data.Products) == 0 {
			return fmt.Errorf("completed 必须带 data.products")
		}
		seen := map[string]bool{}
		for _, product := range reply.Data.Products {
			if product.SKURef == "" || seen[product.SKURef] {
				return fmt.Errorf("sku_ref 为空或重复")
			}
			seen[product.SKURef] = true
			if product.Cost.DomesticShipping == nil || product.Cost.PurchasePrice == nil || product.Cost.Total == nil {
				return fmt.Errorf("completed 商品 %s 的成本不完整，缺失运费不能当成 0", product.SKURef)
			}
		}
	case "failed":
		if reply.Error == nil || reply.Error.Code == "" || reply.Error.Message == "" {
			return fmt.Errorf("failed 必须带 error.code 和 error.message")
		}
	default:
		return fmt.Errorf("未知 status")
	}
	return nil
}
