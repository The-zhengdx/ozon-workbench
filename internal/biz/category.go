package biz

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/go-kratos/kratos/v2/log"

	"ozon-workbench/internal/catalog"
)

// AttrSnapshot is a cached attribute contract for one type and language.
type AttrSnapshot struct {
	DescriptionCategoryID int64
	TypeID                int64
	Language              string
	Version               int
	Attributes            []map[string]any
	Source                string
	SyncedAt              time.Time
}

// AttrView is the attribute panel payload.
type AttrView struct {
	DescriptionCategoryID int64            `json:"description_category_id"`
	TypeID                int64            `json:"type_id"`
	Language              string           `json:"language"`
	Version               int              `json:"version"`
	Source                string           `json:"source"`
	SourceLabel           string           `json:"source_label"`
	SyncedAt              time.Time        `json:"synced_at"`
	Total                 int              `json:"total"`
	RequiredCount         int              `json:"required_count"`
	OptionalCount         int              `json:"optional_count"`
	Attributes            []map[string]any `json:"attributes"`
}

// DictSnapshot caches one dictionary page series or one search.
type DictSnapshot struct {
	DescriptionCategoryID int64
	TypeID                int64
	AttributeID           int64
	Language              string
	Query                 string
	Version               int
	Values                []map[string]any
	HasNext               bool
	LastValueID           int64
	SyncedAt              time.Time
}

// DictView is returned for dictionary paging and search.
type DictView struct {
	DescriptionCategoryID int64            `json:"description_category_id"`
	TypeID                int64            `json:"type_id"`
	AttributeID           int64            `json:"attribute_id"`
	Language              string           `json:"language"`
	Query                 string           `json:"query,omitempty"`
	Version               int              `json:"version"`
	SyncedAt              time.Time        `json:"synced_at"`
	HasNext               bool             `json:"has_next"`
	LastValueID           int64            `json:"last_value_id"`
	FromCache             bool             `json:"from_cache"`
	Values                []map[string]any `json:"values"`
}

// Prompt is the Codex prompt bound to a category and leaf type.
type Prompt struct {
	ID                    int
	DescriptionCategoryID int64
	TypeID                int64
	CodexPrompt           string
	UpdatedAt             time.Time
}

// PromptView is the editor payload. Saved is false when nothing has been stored.
type PromptView struct {
	DescriptionCategoryID int64      `json:"description_category_id"`
	TypeID                int64      `json:"type_id"`
	CodexPrompt           string     `json:"codex_prompt"`
	UpdatedAt             *time.Time `json:"updated_at"`
	Saved                 bool       `json:"saved"`
}

// CategoryRepo is the category persistence port.
type CategoryRepo interface {
	GetSnapshot(ctx context.Context, language string) (*Snapshot, error)
	ListNodes(ctx context.Context, language string) ([]FlatNode, error)
	SaveTree(ctx context.Context, language, source string, shopID *int, nodes []FlatNode) (*Snapshot, error)
	RecordSyncError(ctx context.Context, language string, shopID *int, message string) (*Snapshot, error)
	LatestAttributes(ctx context.Context, categoryID, typeID int64, language string) (*AttrSnapshot, error)
	SaveAttributes(ctx context.Context, snap *AttrSnapshot) (*AttrSnapshot, error)
	GetDictionary(ctx context.Context, categoryID, typeID, attributeID int64, language, query string) (*DictSnapshot, error)
	SaveDictionary(ctx context.Context, snap *DictSnapshot) (*DictSnapshot, error)
	GetPrompt(ctx context.Context, categoryID, typeID int64) (*Prompt, error)
	SavePrompt(ctx context.Context, prompt *Prompt) (*Prompt, error)
}

// CategoryUsecase syncs the Ozon tree, attribute contracts and per-type prompts.
type CategoryUsecase struct {
	repo  CategoryRepo
	shops *ShopUsecase
	ozon  OzonGateway
	log   *log.Helper
}

func NewCategoryUsecase(repo CategoryRepo, shops *ShopUsecase, ozon OzonGateway, logger log.Logger) *CategoryUsecase {
	return &CategoryUsecase{repo: repo, shops: shops, ozon: ozon, log: log.NewHelper(logger)}
}

// EnsureExample seeds the offline sample once, so the first visit matches the prototype.
func (uc *CategoryUsecase) EnsureExample(ctx context.Context) error {
	const language = "ZH_HANS"
	snap, err := uc.repo.GetSnapshot(ctx, language)
	if err != nil {
		return err
	}
	if snap != nil && snap.HasRow {
		return nil
	}
	nodes, err := FlattenTree(catalog.ExampleTree(language))
	if err != nil {
		return err
	}
	saved, err := uc.repo.SaveTree(ctx, language, "offline_example", nil, nodes)
	if err != nil {
		return err
	}
	for _, node := range nodes {
		if !node.IsLeaf {
			continue
		}
		attrs := catalog.ExampleAttributes(node.TypeID, language)
		if _, err := uc.repo.SaveAttributes(ctx, &AttrSnapshot{
			DescriptionCategoryID: node.DescriptionCategoryID,
			TypeID:                node.TypeID,
			Language:              language,
			Attributes:            attrs,
			Source:                "offline_example",
			SyncedAt:              saved.SnapshotAt,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (uc *CategoryUsecase) Tree(ctx context.Context, language string) (*TreeView, error) {
	language, err := NormalizeLanguage(language)
	if err != nil {
		return nil, err
	}
	if err := uc.EnsureExample(ctx); err != nil {
		return nil, err
	}
	return uc.loadTree(ctx, language)
}

func (uc *CategoryUsecase) loadTree(ctx context.Context, language string) (*TreeView, error) {
	snap, err := uc.repo.GetSnapshot(ctx, language)
	if err != nil {
		return nil, err
	}
	if snap == nil || !snap.HasRow {
		return &TreeView{
			Language:    language,
			Source:      "",
			SourceLabel: SourceLabel(""),
			Roots:       []*TreeNode{},
		}, nil
	}
	nodes, err := uc.repo.ListNodes(ctx, language)
	if err != nil {
		return nil, err
	}
	return viewTree(snap, nodes), nil
}

// SyncTree reads the official tree. A failure keeps the previous snapshot and records the reason.
func (uc *CategoryUsecase) SyncTree(ctx context.Context, shopID int, language string) (*TreeView, error) {
	language, err := NormalizeLanguage(language)
	if err != nil {
		return nil, err
	}
	cred, err := uc.shops.Credentials(ctx, shopID)
	if err != nil {
		return nil, err
	}
	roots, err := uc.ozon.CategoryTree(ctx, cred, language)
	if err != nil {
		shop := shopID
		snap, recErr := uc.repo.RecordSyncError(ctx, language, &shop, err.Error())
		if recErr != nil {
			uc.log.Errorf("record tree sync error: %v", recErr)
		}
		failure := ozonFailure(err)
		if snap != nil {
			nodes, _ := uc.repo.ListNodes(ctx, language)
			failure.Snapshot = viewTree(snap, nodes)
		}
		return nil, failure
	}
	flat, err := FlattenTree(roots)
	if err != nil {
		return nil, err
	}
	shop := shopID
	if _, err := uc.repo.SaveTree(ctx, language, "ozon", &shop, flat); err != nil {
		return nil, err
	}
	return uc.loadTree(ctx, language)
}

// ImportTree accepts an official tree JSON document or its result array.
func (uc *CategoryUsecase) ImportTree(ctx context.Context, language string, payload []byte) (*TreeView, error) {
	language, err := NormalizeLanguage(language)
	if err != nil {
		return nil, err
	}
	roots, err := decodeTreePayload(payload)
	if err != nil {
		return nil, err
	}
	flat, err := FlattenTree(roots)
	if err != nil {
		return nil, err
	}
	if _, err := uc.repo.SaveTree(ctx, language, "import", nil, flat); err != nil {
		return nil, err
	}
	return uc.loadTree(ctx, language)
}

// Search matches name, path, category id and type id, including nodes that are still collapsed in the UI.
func (uc *CategoryUsecase) Search(ctx context.Context, language, query string) ([]*TreeNode, error) {
	language, err := NormalizeLanguage(language)
	if err != nil {
		return nil, err
	}
	view, err := uc.Tree(ctx, language)
	if err != nil {
		return nil, err
	}
	term := strings.ToLower(strings.TrimSpace(query))
	var hits []*TreeNode
	var walk func(nodes []*TreeNode)
	walk = func(nodes []*TreeNode) {
		for _, node := range nodes {
			if term == "" || nodeMatches(node, term) {
				copyNode := *node
				copyNode.Children = []*TreeNode{}
				hits = append(hits, &copyNode)
			}
			walk(node.Children)
		}
	}
	walk(view.Roots)
	if hits == nil {
		hits = []*TreeNode{}
	}
	return hits, nil
}

func nodeMatches(node *TreeNode, term string) bool {
	blob := strings.ToLower(strings.Join(append(append([]string{}, node.Path...), node.Name), " "))
	blob += " " + fmtID(node.DescriptionCategoryID)
	if node.TypeID != 0 {
		blob += " " + fmtID(node.TypeID)
	}
	return strings.Contains(blob, term)
}

func (uc *CategoryUsecase) Attributes(ctx context.Context, categoryID, typeID int64, language string) (*AttrView, error) {
	language, err := NormalizeLanguage(language)
	if err != nil {
		return nil, err
	}
	if err := uc.requireType(ctx, language, categoryID, typeID); err != nil {
		return nil, err
	}
	snap, err := uc.repo.LatestAttributes(ctx, categoryID, typeID, language)
	if err != nil {
		return nil, err
	}
	if snap == nil {
		return nil, &Error{Status: 404, Reason: "NOT_LOADED", Message: "本类型还没有属性快照。可同步属性，或导入该类目与类型的官方属性响应。"}
	}
	return presentAttributes(snap), nil
}

// SyncAttributes loads the full attribute list. On failure the previous snapshot is left in place.
func (uc *CategoryUsecase) SyncAttributes(ctx context.Context, shopID int, categoryID, typeID int64, language string) (*AttrView, error) {
	language, err := NormalizeLanguage(language)
	if err != nil {
		return nil, err
	}
	if err := uc.requireType(ctx, language, categoryID, typeID); err != nil {
		return nil, err
	}
	cred, err := uc.shops.Credentials(ctx, shopID)
	if err != nil {
		return nil, err
	}
	attrs, err := uc.ozon.Attributes(ctx, cred, categoryID, typeID, language)
	if err != nil {
		failure := ozonFailure(err)
		if prev, gerr := uc.repo.LatestAttributes(ctx, categoryID, typeID, language); gerr == nil && prev != nil {
			failure.Snapshot = presentAttributes(prev)
		}
		return nil, failure
	}
	if err := validateAttributes(attrs); err != nil {
		return nil, err
	}
	saved, err := uc.repo.SaveAttributes(ctx, &AttrSnapshot{
		DescriptionCategoryID: categoryID,
		TypeID:                typeID,
		Language:              language,
		Attributes:            attrs,
		Source:                "ozon",
		SyncedAt:              time.Now().UTC(),
	})
	if err != nil {
		return nil, err
	}
	return presentAttributes(saved), nil
}

func (uc *CategoryUsecase) ImportAttributes(ctx context.Context, categoryID, typeID int64, language string, payload []byte) (*AttrView, error) {
	language, err := NormalizeLanguage(language)
	if err != nil {
		return nil, err
	}
	if categoryID <= 0 || typeID <= 0 {
		return nil, bad("BAD_TYPE", "需要类目 ID 和类型 ID。", nil)
	}
	attrs, err := decodeAttributePayload(payload)
	if err != nil {
		return nil, err
	}
	var wrapped struct {
		DescriptionCategoryID int64 `json:"description_category_id"`
		TypeID                int64 `json:"type_id"`
	}
	_ = json.Unmarshal(payload, &wrapped)
	if wrapped.TypeID != 0 && wrapped.TypeID != typeID {
		return nil, bad("TYPE_MISMATCH", "属性文件与当前选中类型不一致。", nil)
	}
	if wrapped.DescriptionCategoryID != 0 && wrapped.DescriptionCategoryID != categoryID {
		return nil, bad("TYPE_MISMATCH", "属性文件与当前选中类型不一致。", nil)
	}
	saved, err := uc.repo.SaveAttributes(ctx, &AttrSnapshot{
		DescriptionCategoryID: categoryID,
		TypeID:                typeID,
		Language:              language,
		Attributes:            attrs,
		Source:                "import",
		SyncedAt:              time.Now().UTC(),
	})
	if err != nil {
		return nil, err
	}
	return presentAttributes(saved), nil
}

func (uc *CategoryUsecase) AttributeValues(ctx context.Context, shopID int, categoryID, typeID, attributeID, lastValueID int64, limit int, language string, refresh bool) (*DictView, error) {
	language, err := NormalizeLanguage(language)
	if err != nil {
		return nil, err
	}
	if attributeID <= 0 {
		return nil, bad("BAD_ATTRIBUTE", "需要属性 ID。", nil)
	}
	limit = clamp(limit, 1, 2000, 100)
	if !refresh && lastValueID == 0 {
		cached, err := uc.repo.GetDictionary(ctx, categoryID, typeID, attributeID, language, "")
		if err != nil {
			return nil, err
		}
		if cached != nil {
			view := presentDict(cached)
			view.FromCache = true
			return view, nil
		}
	}
	cred, err := uc.shops.Credentials(ctx, shopID)
	if err != nil {
		return nil, err
	}
	page, err := uc.ozon.AttributeValues(ctx, cred, categoryID, typeID, attributeID, lastValueID, limit, language)
	if err != nil {
		return nil, ozonFailure(err)
	}
	values := page.Values
	hasNext := page.HasNext
	cursor := page.LastValueID
	if lastValueID > 0 {
		prev, err := uc.repo.GetDictionary(ctx, categoryID, typeID, attributeID, language, "")
		if err != nil {
			return nil, err
		}
		// Only continue a page when the cursor matches the cached series. A mismatched cursor must not duplicate values.
		if prev != nil && prev.LastValueID == lastValueID {
			values = append(append([]map[string]any{}, prev.Values...), page.Values...)
		}
	}
	saved, err := uc.repo.SaveDictionary(ctx, &DictSnapshot{
		DescriptionCategoryID: categoryID,
		TypeID:                typeID,
		AttributeID:           attributeID,
		Language:              language,
		Values:                values,
		HasNext:               hasNext,
		LastValueID:           cursor,
		SyncedAt:              time.Now().UTC(),
	})
	if err != nil {
		return nil, err
	}
	return presentDict(saved), nil
}

func (uc *CategoryUsecase) SearchAttributeValues(ctx context.Context, shopID int, categoryID, typeID, attributeID int64, value string, limit int, language string, refresh bool) (*DictView, error) {
	language, err := NormalizeLanguage(language)
	if err != nil {
		return nil, err
	}
	value = strings.TrimSpace(value)
	if len([]rune(value)) < 2 {
		return nil, bad("QUERY_SHORT", "字典搜索至少输入 2 个字符。", map[string]string{"value": "至少 2 个字符。"})
	}
	limit = clamp(limit, 1, 100, 100)
	queryKey := "q:" + strings.ToLower(value)
	if !refresh {
		cached, err := uc.repo.GetDictionary(ctx, categoryID, typeID, attributeID, language, queryKey)
		if err != nil {
			return nil, err
		}
		if cached != nil {
			view := presentDict(cached)
			view.FromCache = true
			view.Query = value
			return view, nil
		}
	}
	cred, err := uc.shops.Credentials(ctx, shopID)
	if err != nil {
		return nil, err
	}
	values, err := uc.ozon.SearchAttributeValues(ctx, cred, categoryID, typeID, attributeID, value, limit)
	if err != nil {
		return nil, ozonFailure(err)
	}
	saved, err := uc.repo.SaveDictionary(ctx, &DictSnapshot{
		DescriptionCategoryID: categoryID,
		TypeID:                typeID,
		AttributeID:           attributeID,
		Language:              language,
		Query:                 queryKey,
		Values:                values,
		HasNext:               false,
		SyncedAt:              time.Now().UTC(),
	})
	if err != nil {
		return nil, err
	}
	view := presentDict(saved)
	view.Query = value
	return view, nil
}

func (uc *CategoryUsecase) GetPrompt(ctx context.Context, categoryID, typeID int64) (*PromptView, error) {
	if categoryID <= 0 || typeID <= 0 {
		return nil, bad("BAD_TYPE", "需要类目 ID 和类型 ID。", nil)
	}
	row, err := uc.repo.GetPrompt(ctx, categoryID, typeID)
	if err != nil {
		return nil, err
	}
	if row == nil {
		return &PromptView{DescriptionCategoryID: categoryID, TypeID: typeID, CodexPrompt: "", Saved: false}, nil
	}
	at := row.UpdatedAt
	return &PromptView{
		DescriptionCategoryID: row.DescriptionCategoryID,
		TypeID:                row.TypeID,
		CodexPrompt:           row.CodexPrompt,
		UpdatedAt:             &at,
		Saved:                 true,
	}, nil
}

func (uc *CategoryUsecase) SavePrompt(ctx context.Context, categoryID, typeID int64, text string) (*PromptView, error) {
	if categoryID <= 0 || typeID <= 0 {
		return nil, bad("BAD_TYPE", "需要类目 ID 和类型 ID。", nil)
	}
	text = strings.TrimSpace(text)
	if text == "" {
		return nil, bad("PROMPT_EMPTY", "请填写 Codex 提示词。", map[string]string{"codex_prompt": "请填写 Codex 提示词。"})
	}
	if len([]rune(text)) > 100000 {
		return nil, bad("PROMPT_LONG", "提示词过长。", nil)
	}
	saved, err := uc.repo.SavePrompt(ctx, &Prompt{
		DescriptionCategoryID: categoryID,
		TypeID:                typeID,
		CodexPrompt:           text,
		UpdatedAt:             time.Now().UTC(),
	})
	if err != nil {
		return nil, err
	}
	at := saved.UpdatedAt
	return &PromptView{
		DescriptionCategoryID: saved.DescriptionCategoryID,
		TypeID:                saved.TypeID,
		CodexPrompt:           saved.CodexPrompt,
		UpdatedAt:             &at,
		Saved:                 true,
	}, nil
}

func (uc *CategoryUsecase) requireType(ctx context.Context, language string, categoryID, typeID int64) error {
	if categoryID <= 0 || typeID <= 0 {
		return bad("BAD_TYPE", "需要类目 ID 和类型 ID。", nil)
	}
	nodes, err := uc.repo.ListNodes(ctx, language)
	if err != nil {
		return err
	}
	if len(nodes) == 0 {
		return nil
	}
	for _, node := range nodes {
		if node.IsLeaf && node.DescriptionCategoryID == categoryID && node.TypeID == typeID {
			return nil
		}
	}
	return notFound("当前语言的类目树中没有这个商品类型。")
}

func presentAttributes(snap *AttrSnapshot) *AttrView {
	attrs := snap.Attributes
	if attrs == nil {
		attrs = []map[string]any{}
	}
	required := 0
	for _, attr := range attrs {
		if truthy(attr["is_required"]) {
			required++
		}
	}
	return &AttrView{
		DescriptionCategoryID: snap.DescriptionCategoryID,
		TypeID:                snap.TypeID,
		Language:              snap.Language,
		Version:               snap.Version,
		Source:                snap.Source,
		SourceLabel:           SourceLabel(snap.Source),
		SyncedAt:              snap.SyncedAt,
		Total:                 len(attrs),
		RequiredCount:         required,
		OptionalCount:         len(attrs) - required,
		Attributes:            attrs,
	}
}

func presentDict(snap *DictSnapshot) *DictView {
	values := snap.Values
	if values == nil {
		values = []map[string]any{}
	}
	return &DictView{
		DescriptionCategoryID: snap.DescriptionCategoryID,
		TypeID:                snap.TypeID,
		AttributeID:           snap.AttributeID,
		Language:              snap.Language,
		Version:               snap.Version,
		SyncedAt:              snap.SyncedAt,
		HasNext:               snap.HasNext,
		LastValueID:           snap.LastValueID,
		Values:                values,
	}
}

func truthy(v any) bool {
	switch t := v.(type) {
	case bool:
		return t
	case string:
		return t == "true" || t == "1"
	default:
		return false
	}
}

func clamp(v, min, max, fallback int) int {
	if v == 0 {
		return fallback
	}
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}

// NormalizeLanguage accepts the Seller API language enum. Empty means ZH_HANS.
func NormalizeLanguage(language string) (string, error) {
	language = strings.ToUpper(strings.TrimSpace(language))
	if language == "" {
		return "ZH_HANS", nil
	}
	switch language {
	case "DEFAULT", "RU", "EN", "TR", "ZH_HANS":
		return language, nil
	default:
		return "", bad("BAD_LANGUAGE", "language 仅支持 DEFAULT、RU、EN、TR、ZH_HANS。", nil)
	}
}

func decodeTreePayload(payload []byte) ([]catalog.TreeNode, error) {
	payload = bytesTrimBOM(payload)
	if len(payload) == 0 {
		return nil, bad("BAD_TREE", "类目树为空，或缺少 result 数组。", nil)
	}
	var roots []catalog.TreeNode
	if err := json.Unmarshal(payload, &roots); err == nil && len(roots) > 0 {
		return roots, nil
	}
	var wrapped struct {
		Result []catalog.TreeNode `json:"result"`
	}
	if err := json.Unmarshal(payload, &wrapped); err != nil || len(wrapped.Result) == 0 {
		return nil, bad("BAD_TREE", "类目树为空，或缺少 result 数组。", nil)
	}
	return wrapped.Result, nil
}

func decodeAttributePayload(payload []byte) ([]map[string]any, error) {
	payload = bytesTrimBOM(payload)
	var list []map[string]any
	if err := json.Unmarshal(payload, &list); err == nil {
		if err := validateAttributes(list); err != nil {
			return nil, err
		}
		return list, nil
	}
	var wrapped struct {
		Result []map[string]any `json:"result"`
	}
	if err := json.Unmarshal(payload, &wrapped); err != nil || wrapped.Result == nil {
		return nil, bad("BAD_ATTRIBUTES", "属性列表格式有误，需要含 id 和 name 的 result 数组。", nil)
	}
	if err := validateAttributes(wrapped.Result); err != nil {
		return nil, err
	}
	return wrapped.Result, nil
}

func validateAttributes(attrs []map[string]any) error {
	if attrs == nil {
		return bad("BAD_ATTRIBUTES", "属性列表格式有误，需要含 id 和 name 的 result 数组。", nil)
	}
	for _, attr := range attrs {
		if attr == nil || asInt64(attr["id"]) == 0 || strings.TrimSpace(asString(attr["name"])) == "" {
			return bad("BAD_ATTRIBUTES", "属性列表格式有误，需要含 id 和 name 的 result 数组。", nil)
		}
	}
	return nil
}

func asInt64(v any) int64 {
	switch t := v.(type) {
	case int64:
		return t
	case int:
		return int64(t)
	case float64:
		return int64(t)
	case json.Number:
		i, err := t.Int64()
		if err != nil {
			f, ferr := t.Float64()
			if ferr != nil {
				return 0
			}
			return int64(f)
		}
		return i
	case string:
		var n int64
		for _, r := range t {
			if r < '0' || r > '9' {
				return 0
			}
			n = n*10 + int64(r-'0')
		}
		return n
	default:
		return 0
	}
}

func asString(v any) string {
	s, _ := v.(string)
	return s
}

func bytesTrimBOM(b []byte) []byte {
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		return b[3:]
	}
	return b
}
