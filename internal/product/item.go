// Package product 把结构化 SKU 收成 Ozon 单品 JSON。系统字段不会写入导入对象。
package product

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	"ozon-workbench/internal/codexcli"
	"ozon-workbench/internal/domain"
	"ozon-workbench/internal/ozonapi"
)

var forbidden = []string{"cost", "origin_link", "base_sku", "generation_task_id", "task_id", "items", "video"}

type FieldError struct {
	Field   string `json:"field"`
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Built struct {
	Item     json.RawMessage
	Errors   []FieldError
	Blocking bool
}

type Input struct {
	Shop         domain.Shop
	Task         domain.Task
	SKU          codexcli.Product
	Price        string
	OldPrice     string
	Attributes   []ozonapi.Attribute
	DictIDs      map[int64]map[int64]bool
	AssetURLs    map[string]string
	HasAttrCache bool
}

func Build(in Input) (Built, error) {
	var errors []FieldError
	blocking := false
	offer := ""
	if in.SKU.OfferIDSuggestion != nil {
		offer = strings.TrimSpace(*in.SKU.OfferIDSuggestion)
	}
	if offer == "" {
		blocking = true
		errors = append(errors, FieldError{Field: "offer_id", Code: "OFFER_REQUIRED", Message: "没有可用的 offer_id"})
	}
	if strings.TrimSpace(in.SKU.SKURef) == "" {
		blocking = true
		errors = append(errors, FieldError{Field: "sku_ref", Code: "SKU_REQUIRED", Message: "sku_ref 为空"})
	}
	item := map[string]any{
		"description_category_id": in.Task.DescriptionCategoryID,
		"type_id":                 in.Task.TypeID,
		"currency_code":           in.Shop.CurrencyCode,
		"vat":                     in.Shop.VAT,
		"dimension_unit":          "mm",
		"weight_unit":             "g",
	}
	if offer != "" {
		item["offer_id"] = offer
	}
	if in.SKU.Name != nil && strings.TrimSpace(*in.SKU.Name) != "" {
		item["name"] = strings.TrimSpace(*in.SKU.Name)
	} else {
		errors = append(errors, FieldError{Field: "name", Code: "NAME_REQUIRED", Message: "缺少俄文标题"})
	}
	if in.Price != "" {
		item["price"] = in.Price
	} else {
		errors = append(errors, FieldError{Field: "price", Code: "PRICING_UNCONFIGURED", Message: "售价尚未按已配置公式算出"})
	}
	if in.OldPrice != "" {
		item["old_price"] = in.OldPrice
	}
	if in.SKU.PrimaryImage != nil && *in.SKU.PrimaryImage != "" {
		if err := checkURL(*in.SKU.PrimaryImage, in.AssetURLs); err != nil {
			errors = append(errors, FieldError{Field: "primary_image", Code: "MEDIA_URL", Message: err.Error()})
		} else {
			item["primary_image"] = *in.SKU.PrimaryImage
		}
	}
	var images []string
	for _, image := range in.SKU.Images {
		if err := checkURL(image, in.AssetURLs); err != nil {
			errors = append(errors, FieldError{Field: "images", Code: "MEDIA_URL", Message: err.Error()})
			continue
		}
		images = append(images, image)
	}
	if images == nil {
		images = []string{}
	}
	item["images"] = images
	if in.SKU.ColorImage != nil && *in.SKU.ColorImage != "" {
		if err := checkURL(*in.SKU.ColorImage, in.AssetURLs); err != nil {
			errors = append(errors, FieldError{Field: "color_image", Code: "MEDIA_URL", Message: err.Error()})
		} else {
			item["color_image"] = *in.SKU.ColorImage
		}
	}
	if in.SKU.Dimensions.Depth != nil {
		item["depth"] = *in.SKU.Dimensions.Depth
	}
	if in.SKU.Dimensions.Width != nil {
		item["width"] = *in.SKU.Dimensions.Width
	}
	if in.SKU.Dimensions.Height != nil {
		item["height"] = *in.SKU.Dimensions.Height
	}
	if in.SKU.Dimensions.Weight != nil {
		item["weight"] = *in.SKU.Dimensions.Weight
	}
	attrs, attrErrs := convertAttrs(in)
	errors = append(errors, attrErrs...)
	item["attributes"] = attrs
	complexAttrs, complexErrs := convertComplex(in)
	errors = append(errors, complexErrs...)
	item["complex_attributes"] = complexAttrs
	for _, key := range forbidden {
		delete(item, key)
	}
	raw, err := json.Marshal(item)
	if err != nil {
		return Built{}, err
	}
	var check map[string]json.RawMessage
	if err := json.Unmarshal(raw, &check); err != nil {
		return Built{}, err
	}
	for _, key := range forbidden {
		if _, ok := check[key]; ok {
			blocking = true
			errors = append(errors, FieldError{Field: key, Code: "FORBIDDEN_FIELD", Message: "系统字段进入了导入 JSON"})
		}
	}
	return Built{Item: raw, Errors: errors, Blocking: blocking}, nil
}

func checkURL(raw string, allowed map[string]string) error {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Host == "" {
		return fmt.Errorf("不是可长期使用的 HTTPS 地址")
	}
	if len(allowed) == 0 {
		return fmt.Errorf("没有已确认上传的素材")
	}
	for _, assetURL := range allowed {
		if assetURL == raw {
			return nil
		}
	}
	return fmt.Errorf("URL 不是本任务已上传的素材")
}

func convertAttrs(in Input) ([]any, []FieldError) {
	var out []any
	var errors []FieldError
	known := map[int64]ozonapi.Attribute{}
	for _, attr := range in.Attributes {
		known[attr.ID] = attr
	}
	for _, attr := range in.SKU.Attributes {
		id := int64(attr.ID)
		def, ok := known[id]
		if !in.HasAttrCache || !ok {
			errors = append(errors, FieldError{Field: fmt.Sprintf("attributes.%d", attr.ID), Code: "UNKNOWN_ATTRIBUTE", Message: "属性不在当前类目缓存中"})
		}
		if ok && def.DictionaryID > 0 {
			if attr.DictionaryValueID == nil {
				errors = append(errors, FieldError{Field: fmt.Sprintf("attributes.%d", attr.ID), Code: "DICTIONARY_REQUIRED", Message: "字典属性缺少 dictionary_value_id"})
			} else if !dictHas(in.DictIDs, id, int64(*attr.DictionaryValueID)) {
				errors = append(errors, FieldError{Field: fmt.Sprintf("attributes.%d", attr.ID), Code: "ILLEGAL_DICTIONARY", Message: "字典值不在已核验的候选中"})
			}
		}
		value := map[string]any{}
		if attr.ValueText != nil {
			value["value"] = *attr.ValueText
		}
		if attr.DictionaryValueID != nil {
			value["dictionary_value_id"] = *attr.DictionaryValueID
		}
		out = append(out, map[string]any{"id": attr.ID, "complex_id": attr.ComplexID, "values": []any{value}})
	}
	if out == nil {
		out = []any{}
	}
	return out, errors
}

func convertComplex(in Input) ([]any, []FieldError) {
	var out []any
	var errors []FieldError
	known := map[int64]ozonapi.Attribute{}
	for _, attr := range in.Attributes {
		known[attr.ID] = attr
	}
	for _, attr := range in.SKU.ComplexAttributes {
		id := int64(attr.ID)
		if !in.HasAttrCache || known[id].ID == 0 && !hasID(known, id) {
			errors = append(errors, FieldError{Field: fmt.Sprintf("complex_attributes.%d", attr.ID), Code: "UNKNOWN_ATTRIBUTE", Message: "复杂属性不在当前类目缓存中"})
		}
		var values []any
		for _, value := range attr.Values {
			row := map[string]any{}
			if value.Value != nil {
				row["value"] = *value.Value
				if looksURL(*value.Value) {
					if err := checkURL(*value.Value, in.AssetURLs); err != nil {
						errors = append(errors, FieldError{Field: fmt.Sprintf("complex_attributes.%d", attr.ID), Code: "MEDIA_URL", Message: err.Error()})
					}
				}
			}
			if value.DictionaryValueID != nil {
				row["dictionary_value_id"] = *value.DictionaryValueID
			}
			values = append(values, row)
		}
		if values == nil {
			values = []any{}
		}
		out = append(out, map[string]any{"id": attr.ID, "complex_id": attr.ComplexID, "values": values})
	}
	if out == nil {
		out = []any{}
	}
	return out, errors
}

func hasID(known map[int64]ozonapi.Attribute, id int64) bool {
	_, ok := known[id]
	return ok
}

func dictHas(index map[int64]map[int64]bool, attrID, valueID int64) bool {
	if index == nil {
		return false
	}
	return index[attrID][valueID]
}

func looksURL(v string) bool {
	return strings.HasPrefix(v, "http://") || strings.HasPrefix(v, "https://")
}
