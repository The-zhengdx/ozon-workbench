package biz

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	PublishDraft      = "待完善"
	PublishReady      = "待上架"
	PublishSubmitting = "提交中"
	PublishVerify     = "待核对"
	PublishFailed     = "导入失败"
	PublishImported   = "已导入"

	PlatformUnread     = "待回读"
	PlatformReview     = "审核中"
	PlatformUnsellable = "不可售"
	PlatformSellable   = "可售"

	MaxImportItems   = 100
	MaxStockPairs    = 100
	StockMinInterval = 30 * time.Second
)

// ItemExpect is the shop and contract a single import object must satisfy.
type ItemExpect struct {
	OfferID               string
	DescriptionCategoryID int64
	TypeID                int64
	ShopCurrency          string
	Contract              []map[string]any
}

// ValidateItem checks one /v3/product/import items[] object against the attribute contract.
// An empty issue list means the object can be sent as-is.
func ValidateItem(raw json.RawMessage, expect ItemExpect) []string {
	var item map[string]any
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	if err := dec.Decode(&item); err != nil || item == nil {
		return []string{"商品参数不是合法的 JSON 对象"}
	}
	var issues []string
	offer := strings.TrimSpace(asString(item["offer_id"]))
	if offer == "" {
		issues = append(issues, "缺少 offer_id")
	} else if expect.OfferID != "" && offer != expect.OfferID {
		issues = append(issues, "卖家 SKU 必须与当前记录相同")
	}
	if expect.DescriptionCategoryID > 0 && asInt64(item["description_category_id"]) != expect.DescriptionCategoryID {
		issues = append(issues, "description_category_id 与任务选定的类目不一致")
	}
	if expect.TypeID > 0 && asInt64(item["type_id"]) != expect.TypeID {
		issues = append(issues, "type_id 与任务选定的商品类型不一致")
	}
	if strings.TrimSpace(asString(item["name"])) == "" {
		issues = append(issues, "俄文标题不能为空")
	}
	price := strings.TrimSpace(asString(item["price"]))
	if price == "" {
		issues = append(issues, "请填写售价")
	} else if _, err := strconv.ParseFloat(price, 64); err != nil {
		issues = append(issues, "售价必须是数字")
	}
	currency := strings.ToUpper(strings.TrimSpace(asString(item["currency_code"])))
	shopCurrency := strings.ToUpper(strings.TrimSpace(expect.ShopCurrency))
	if currency == "" {
		issues = append(issues, "请填写三位币种。店铺币种尚未核实时，以本次商品参数为准")
	} else if utf8.RuneCountInString(currency) != 3 {
		issues = append(issues, "币种必须是三位代码")
	} else if shopCurrency != "" && currency != shopCurrency {
		issues = append(issues, "币种与店铺已核实的合同币种不一致")
	}
	for _, pair := range []struct{ key, label string }{
		{"depth", "包装长度"}, {"width", "包装宽度"}, {"height", "包装高度"}, {"weight", "包装重量"},
	} {
		if asFloat(item[pair.key]) <= 0 {
			issues = append(issues, pair.label+"必须大于 0")
		}
	}
	if unit := strings.ToLower(strings.TrimSpace(asString(item["dimension_unit"]))); unit != "mm" && unit != "cm" && unit != "in" {
		issues = append(issues, "dimension_unit 只能是 mm、cm 或 in")
	}
	if unit := strings.ToLower(strings.TrimSpace(asString(item["weight_unit"]))); unit != "g" && unit != "kg" {
		issues = append(issues, "weight_unit 只能是 g 或 kg")
	}
	if msg := mediaIssue("主图", asString(item["primary_image"])); msg != "" {
		issues = append(issues, msg)
	}
	issues = append(issues, mediaListIssues("图片", item["images"])...)
	issues = append(issues, mediaListIssues("视频", item["videos"])...)
	if color := strings.TrimSpace(asString(item["color_image"])); color != "" {
		if msg := mediaIssue("营销图", color); msg != "" {
			issues = append(issues, msg)
		}
	}
	issues = append(issues, validateItemAttributes(item["attributes"], expect.Contract)...)
	return issues
}

func validateItemAttributes(raw any, contract []map[string]any) []string {
	rows, _ := raw.([]any)
	byID := map[int64]map[string]any{}
	for _, row := range rows {
		obj, ok := row.(map[string]any)
		if !ok {
			return []string{"attributes 里有无法识别的条目"}
		}
		byID[asInt64(obj["id"])] = obj
	}
	var issues []string
	for _, attr := range contract {
		if !asBool(attr["is_required"]) {
			continue
		}
		id := asInt64(attr["id"])
		name := strings.TrimSpace(asString(attr["name"]))
		if name == "" {
			name = fmt.Sprintf("属性 %d", id)
		}
		obj := byID[id]
		if obj == nil {
			issues = append(issues, "缺少必填属性 "+name)
			continue
		}
		values, _ := obj["values"].([]any)
		collection := asBool(attr["is_collection"])
		if len(values) == 0 {
			issues = append(issues, "必填属性 "+name+" 没有取值")
			continue
		}
		if !collection && len(values) != 1 {
			issues = append(issues, "属性 "+name+" 只能有一个值")
		}
		dictID := asInt64(attr["dictionary_id"])
		for _, value := range values {
			vm, ok := value.(map[string]any)
			if !ok {
				issues = append(issues, "属性 "+name+" 的取值格式不正确")
				continue
			}
			if dictID > 0 {
				if asInt64(vm["dictionary_value_id"]) <= 0 {
					issues = append(issues, "属性 "+name+" 必须选择字典值 ID")
				}
				continue
			}
			if strings.TrimSpace(asString(vm["value"])) == "" {
				issues = append(issues, "属性 "+name+" 的值不能为空")
			}
		}
	}
	return issues
}

func mediaListIssues(label string, raw any) []string {
	if raw == nil {
		return nil
	}
	list, ok := raw.([]any)
	if !ok {
		return []string{label + "必须是字符串数组"}
	}
	var issues []string
	for _, item := range list {
		if msg := mediaIssue(label, asString(item)); msg != "" {
			issues = append(issues, msg)
		}
	}
	return issues
}

func mediaIssue(label, raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		if label == "主图" {
			return "请提供主图的公网 HTTPS 地址"
		}
		return ""
	}
	if !IsPublicHTTPS(raw) {
		if isLandingPage(raw) {
			return label + "是网盘分享页，不是可直接下载的文件地址"
		}
		return label + "必须是公网 HTTPS 地址。本地路径不能提交给 Ozon"
	}
	return ""
}

// IsPublicHTTPS reports whether raw is an https URL with a host.
// Yandex public page hosts are rejected: they return HTML, not the file.
func IsPublicHTTPS(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !strings.EqualFold(u.Scheme, "https") || u.Host == "" {
		return false
	}
	if isLandingHost(u.Hostname()) {
		return false
	}
	return true
}

func isLandingPage(raw string) bool {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	return isLandingHost(u.Hostname())
}

func isLandingHost(host string) bool {
	switch strings.ToLower(host) {
	case "yadi.sk", "disk.yandex.ru", "disk.yandex.com", "disk.360.yandex.ru":
		return true
	default:
		return false
	}
}

// ChunkRaw splits items into blocks of at most size. 101 items become 100 and 1.
func ChunkRaw(items []json.RawMessage, size int) [][]json.RawMessage {
	if size < 1 {
		size = MaxImportItems
	}
	if len(items) == 0 {
		return nil
	}
	var out [][]json.RawMessage
	for start := 0; start < len(items); start += size {
		end := start + size
		if end > len(items) {
			end = len(items)
		}
		out = append(out, append([]json.RawMessage(nil), items[start:end]...))
	}
	return out
}

// BuildImportBody is the single function behind one-SKU and multi-SKU publish.
// The outer items array is added exactly once.
func BuildImportBody(items []json.RawMessage) (json.RawMessage, error) {
	if items == nil {
		items = []json.RawMessage{}
	}
	body, err := json.Marshal(struct {
		Items []json.RawMessage `json:"items"`
	}{Items: items})
	if err != nil {
		return nil, err
	}
	return body, nil
}

// SelectionHash identifies a publish selection. The same key with a different hash is a conflict.
func SelectionHash(pairs [][2]int) string {
	cp := append([][2]int(nil), pairs...)
	sort.Slice(cp, func(i, j int) bool {
		if cp[i][0] == cp[j][0] {
			return cp[i][1] < cp[j][1]
		}
		return cp[i][0] < cp[j][0]
	})
	var b strings.Builder
	for _, pair := range cp {
		fmt.Fprintf(&b, "%d:%d,", pair[0], pair[1])
	}
	sum := sha256.Sum256([]byte(b.String()))
	return hex.EncodeToString(sum[:])
}

// JSONEqual compares two JSON values by structure, ignoring key order.
func JSONEqual(a, b []byte) bool {
	var left, right any
	if json.Unmarshal(a, &left) != nil || json.Unmarshal(b, &right) != nil {
		return false
	}
	lb, err1 := json.Marshal(left)
	rb, err2 := json.Marshal(right)
	return err1 == nil && err2 == nil && bytes.Equal(lb, rb)
}

// StatusAfterEdit is the local publish status of a newly saved version.
func StatusAfterEdit(issues []string) string {
	if len(issues) == 0 {
		return PublishReady
	}
	return PublishDraft
}

// RemoteKind is one import-info or transport outcome for a single offer.
type RemoteKind string

const (
	RemotePending  RemoteKind = "pending"
	RemoteImported RemoteKind = "imported"
	RemoteFailed   RemoteKind = "failed"
	RemoteUnknown  RemoteKind = "unknown"
)

// ApplyRemoteResult updates the local publish status only when the result belongs to the current version.
// A late result for an older version returns apply=false and leaves the newer status untouched.
func ApplyRemoteResult(currentVersion, itemVersion int, currentStatus string, kind RemoteKind) (next string, apply bool) {
	if itemVersion <= 0 || currentVersion != itemVersion {
		return currentStatus, false
	}
	switch kind {
	case RemoteImported:
		return PublishImported, true
	case RemoteFailed:
		return PublishFailed, true
	case RemoteUnknown:
		return PublishVerify, true
	case RemotePending:
		return PublishSubmitting, true
	default:
		return currentStatus, false
	}
}

// MapPlatform turns a /v3/product/info/list status block into the workbench platform status and a reason.
func MapPlatform(status, moderate, failed, name, description, tooltip string, archived bool) (string, string) {
	reason := firstText(failed, description, tooltip, name)
	moderate = strings.ToLower(strings.TrimSpace(moderate))
	status = strings.ToLower(strings.TrimSpace(status))
	if archived || moderate == "declined" || status == "failed" || status == "declined" || strings.TrimSpace(failed) != "" && moderate != "approved" {
		if reason == "" {
			reason = "平台未给出具体原因"
		}
		return PlatformUnsellable, reason
	}
	if moderate == "approved" && (status == "price_sent" || status == "processed" || strings.Contains(name, "Прода")) {
		return PlatformSellable, ""
	}
	if moderate == "" && status == "" && strings.TrimSpace(name) == "" {
		return PlatformUnread, ""
	}
	return PlatformReview, reason
}

// FormatImportErrors flattens Ozon import-info errors into one readable line.
func FormatImportErrors(errors []map[string]any) (code, detail string) {
	if len(errors) == 0 {
		return "", ""
	}
	var codes []string
	var parts []string
	for _, item := range errors {
		c := strings.TrimSpace(asString(item["code"]))
		if c != "" {
			codes = append(codes, c)
		}
		bits := []string{}
		for _, key := range []string{"message", "description", "field", "attribute_name"} {
			if v := strings.TrimSpace(asString(item[key])); v != "" {
				bits = append(bits, v)
			}
		}
		if len(bits) > 0 {
			parts = append(parts, strings.Join(bits, " · "))
		}
	}
	return strings.Join(codes, ","), strings.Join(parts, "；")
}

// StockTooSoon is the local guard for one offer on one warehouse.
// Ozon can still answer TOO_MANY_REQUESTS if its own window is longer.
func StockTooSoon(last, now time.Time) bool {
	return !last.IsZero() && now.Sub(last) < StockMinInterval
}

// MatchListing is the shop-scoped filter shared by tests and the in-memory repository.
func MatchListing(sku ListingSKU, f ListingFilter) bool {
	if sku.ShopID != f.ShopID {
		return false
	}
	if f.Platform != "" && sku.PlatformStatus != f.Platform {
		return false
	}
	if len(f.Publish) > 0 {
		found := false
		for _, status := range f.Publish {
			if sku.PublishStatus == status {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}
	switch f.Mode {
	case "master":
		return strings.TrimSpace(f.Query) == "" || sku.MasterSKU == f.Query
	case "multi":
		if len(f.Offers) == 0 {
			return false
		}
		for _, offer := range f.Offers {
			if sku.OfferID == offer {
				return true
			}
		}
		return false
	default:
		return strings.TrimSpace(f.Query) == "" || strings.Contains(sku.OfferID, f.Query)
	}
}

func Eligible(status string, currentVersion, lastImported int) bool {
	switch status {
	case PublishReady, PublishFailed, PublishVerify:
		return true
	case PublishImported:
		return lastImported > 0 && currentVersion != lastImported
	default:
		return false
	}
}

func ActionLabel(status string, currentVersion, lastImported int) string {
	if status == PublishFailed {
		return "重试"
	}
	if status == PublishImported && currentVersion != lastImported {
		return "提交更新"
	}
	return "上架"
}

func firstText(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

func asFloat(v any) float64 {
	switch t := v.(type) {
	case float64:
		return t
	case int:
		return float64(t)
	case int64:
		return float64(t)
	case json.Number:
		f, _ := t.Float64()
		return f
	case string:
		f, _ := strconv.ParseFloat(strings.TrimSpace(t), 64)
		return f
	default:
		return 0
	}
}

func asBool(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return t == "true" || t == "1"
	default:
		return false
	}
}
