package biz

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
)

// Protocol is appended to the prompt actually sent to Codex. The saved business prompt
// stays free of this instruction so the timeline can show what the operator wrote.
const Protocol = "请在可读说明之外给出一个机器可读代码块，语言标记为 ozon-signal。\n" +
	"signal 只能是 need_input、text_draft 或 visual_plan。\n" +
	"need_input 的 missing_fields 每项包含 key、label、required、hint。\n" +
	"text_draft 包含 sub_skus（offer_id 形如主SKU-序号）、title_ru、selling_points、attributes。\n" +
	"visual_plan 的 items 每项包含 offer_id、primary_image、notes。\n" +
	"示例：\n```ozon-signal\n{\"signal\":\"text_draft\",\"sub_skus\":[{\"offer_id\":\"30-1\",\"specs\":\"灰色\"}],\"title_ru\":\"...\",\"selling_points\":[],\"attributes\":{}}\n```"

// MissingField is one structured question Codex asks the operator to fill in.
type MissingField struct {
	Key      string `json:"key"`
	Label    string `json:"label"`
	Required bool   `json:"required"`
	Hint     string `json:"hint,omitempty"`
}

// SubSKU is one sellable offer derived from the master SKU.
type SubSKU struct {
	OfferID string `json:"offer_id"`
	Specs   string `json:"specs,omitempty"`
}

// VisualItem is one line of the visual plan.
type VisualItem struct {
	OfferID      string `json:"offer_id"`
	PrimaryImage string `json:"primary_image,omitempty"`
	Notes        string `json:"notes,omitempty"`
}

// Signal is the structured block Codex embeds in a reply.
type Signal struct {
	Signal        string         `json:"signal"`
	MissingFields []MissingField `json:"missing_fields"`
	SubSKUs       []SubSKU       `json:"sub_skus"`
	TitleRU       string         `json:"title_ru"`
	SellingPoints []string       `json:"selling_points"`
	Attributes    map[string]any `json:"attributes"`
	Items         []VisualItem   `json:"items"`
}

// TextDraft is the reviewed copy payload stored on the task.
type TextDraft struct {
	TitleRU       string         `json:"title_ru"`
	SellingPoints []string       `json:"selling_points"`
	Attributes    map[string]any `json:"attributes"`
	SubSKUs       []SubSKU       `json:"sub_skus"`
}

// VisualPlan is the reviewed media plan stored on the task.
type VisualPlan struct {
	Items []VisualItem `json:"items"`
}

// ParseSignal finds an ozon-signal block. A nil signal with a nil error means the
// reply is ordinary prose (a follow-up) and does not move the state machine.
func ParseSignal(text string) (*Signal, error) {
	block := fenceBlock(text, "ozon-signal")
	if block == "" {
		block = jsonObjectContaining(text, `"signal"`)
	}
	if block == "" {
		return nil, nil
	}
	var sig Signal
	if err := json.Unmarshal([]byte(block), &sig); err != nil {
		return nil, fmt.Errorf("ozon-signal 不是合法 JSON")
	}
	sig.Signal = strings.ToLower(strings.TrimSpace(sig.Signal))
	if sig.Signal == "" {
		return nil, nil
	}
	return &sig, nil
}

func fenceBlock(text, lang string) string {
	marker := "```" + lang
	rest := text
	for {
		i := strings.Index(rest, marker)
		if i < 0 {
			return ""
		}
		rest = rest[i+len(marker):]
		if nl := strings.IndexByte(rest, '\n'); nl >= 0 {
			rest = rest[nl+1:]
		} else {
			return ""
		}
		end := strings.Index(rest, "```")
		if end < 0 {
			return ""
		}
		return strings.TrimSpace(rest[:end])
	}
}

func jsonObjectContaining(text, needle string) string {
	for i := 0; i < len(text); i++ {
		if text[i] != '{' {
			continue
		}
		dec := json.NewDecoder(strings.NewReader(text[i:]))
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			continue
		}
		if strings.Contains(string(raw), needle) {
			return string(raw)
		}
	}
	return ""
}

// MergeSubSKUs keeps structured offer ids and also picks up master-N tokens in the prose.
func MergeSubSKUs(master string, structured []SubSKU, prose string) []SubSKU {
	seen := map[string]struct{}{}
	var out []SubSKU
	add := func(id, specs string) {
		id = strings.TrimSpace(id)
		if id == "" {
			return
		}
		if _, ok := seen[id]; ok {
			return
		}
		seen[id] = struct{}{}
		out = append(out, SubSKU{OfferID: id, Specs: strings.TrimSpace(specs)})
	}
	for _, sku := range structured {
		add(sku.OfferID, sku.Specs)
	}
	master = strings.TrimSpace(master)
	if master == "" {
		return out
	}
	re := regexp.MustCompile(`(?:^|[^0-9A-Za-z])` + regexp.QuoteMeta(master) + `-(\d+)\b`)
	for _, match := range re.FindAllStringSubmatch(prose, -1) {
		add(master+"-"+match[1], "")
	}
	return out
}

// ParseItemObject reads the ozon-item block Codex returns for one import object.
func ParseItemObject(text string) (json.RawMessage, error) {
	block := fenceBlock(text, "ozon-item")
	if block == "" {
		return nil, fmt.Errorf("Codex 输出缺少 ozon-item")
	}
	if !json.Valid([]byte(block)) {
		return nil, fmt.Errorf("ozon-item 不是合法 JSON")
	}
	return json.RawMessage(block), nil
}

// SignalFenceItem wraps one import object.
func SignalFenceItem(v any) string {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		raw = []byte(`{}`)
	}
	return "```ozon-item\n" + string(raw) + "\n```"
}

// SignalFence wraps a payload the same way the real prompt asks Codex to.
func SignalFence(v any) string {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		raw = []byte(`{"signal":"error"}`)
	}
	return "```ozon-signal\n" + string(raw) + "\n```"
}
