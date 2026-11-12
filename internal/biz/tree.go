package biz

import (
	"fmt"
	"strings"
	"time"

	"ozon-workbench/internal/catalog"
)

// FlatNode is one persisted category or product type.
type FlatNode struct {
	TempID                int
	ParentTempID          int
	LocalID               int
	ParentLocalID         *int
	DescriptionCategoryID int64
	TypeID                int64
	Name                  string
	Disabled              bool
	AncestorDisabled      bool
	IsLeaf                bool
	Level                 int
	Path                  []string
	SortOrder             int
}

// TreeNode is the nested shape returned to the UI.
type TreeNode struct {
	ID                    int         `json:"id"`
	DescriptionCategoryID int64       `json:"description_category_id"`
	TypeID                int64       `json:"type_id"`
	Name                  string      `json:"name"`
	Disabled              bool        `json:"disabled"`
	AncestorDisabled      bool        `json:"ancestor_disabled"`
	IsLeaf                bool        `json:"is_leaf"`
	Level                 int         `json:"level"`
	Path                  []string    `json:"path"`
	Children              []*TreeNode `json:"children"`
}

// Snapshot is the current tree metadata for one language.
type Snapshot struct {
	Language    string
	Source      string
	TypeCount   int
	NodeCount   int
	SnapshotAt  time.Time
	LastError   string
	LastErrorAt *time.Time
	ShopID      *int
	HasRow      bool
}

// TreeView is the category page payload.
type TreeView struct {
	Language    string      `json:"language"`
	Source      string      `json:"source"`
	SourceLabel string      `json:"source_label"`
	SnapshotAt  *time.Time  `json:"snapshot_at"`
	TypeCount   int         `json:"type_count"`
	NodeCount   int         `json:"node_count"`
	LastError   string      `json:"last_error"`
	LastErrorAt *time.Time  `json:"last_error_at"`
	ShopID      *int        `json:"shop_id"`
	Roots       []*TreeNode `json:"roots"`
}

// SourceLabel maps a stored source code to the label shown in the UI.
func SourceLabel(source string) string {
	switch source {
	case "offline_example":
		return "离线示例"
	case "ozon":
		return "Ozon 官方同步"
	case "import":
		return "导入快照"
	case "":
		return "尚未同步"
	default:
		return source
	}
}

// FlattenTree walks every branch and keeps level, disabled flag and leaf types.
func FlattenTree(roots []catalog.TreeNode) ([]FlatNode, error) {
	var out []FlatNode
	var walk func(n catalog.TreeNode, parentCat int64, parentTemp int, level int, path []string, ancestorDisabled bool) error
	walk = func(n catalog.TreeNode, parentCat int64, parentTemp int, level int, path []string, ancestorDisabled bool) error {
		typeID := n.TypeID
		categoryID := n.DescriptionCategoryID
		name := strings.TrimSpace(n.CategoryName)
		isLeaf := false
		if typeID != 0 {
			isLeaf = true
			name = strings.TrimSpace(n.TypeName)
			if categoryID == 0 {
				categoryID = parentCat
			}
		}
		if name == "" || categoryID == 0 {
			return bad("BAD_TREE", "节点缺少名称或所属类目 ID。", nil)
		}
		temp := len(out) + 1
		trail := append(append([]string{}, path...), name)
		out = append(out, FlatNode{
			TempID:                temp,
			ParentTempID:          parentTemp,
			DescriptionCategoryID: categoryID,
			TypeID:                typeID,
			Name:                  name,
			Disabled:              n.Disabled,
			AncestorDisabled:      ancestorDisabled,
			IsLeaf:                isLeaf,
			Level:                 level,
			Path:                  trail,
			SortOrder:             temp - 1,
		})
		nextDisabled := ancestorDisabled || n.Disabled
		for _, child := range n.Children {
			if err := walk(child, categoryID, temp, level+1, trail, nextDisabled); err != nil {
				return err
			}
		}
		return nil
	}
	if len(roots) == 0 {
		return nil, bad("BAD_TREE", "类目树为空，或缺少 result 数组。", nil)
	}
	for _, root := range roots {
		if err := walk(root, 0, 0, 1, nil, false); err != nil {
			return nil, err
		}
	}
	seen := map[string]struct{}{}
	leaves := 0
	for _, node := range out {
		key := fmt.Sprintf("%d:%d", node.DescriptionCategoryID, node.TypeID)
		if _, ok := seen[key]; ok {
			return nil, bad("BAD_TREE", "快照含有重复的类目和类型组合。", nil)
		}
		seen[key] = struct{}{}
		if node.IsLeaf {
			leaves++
		}
	}
	if leaves == 0 {
		return nil, bad("BAD_TREE", "快照没有末级商品类型。", nil)
	}
	return out, nil
}

// BuildTree restores the nested tree from parent links.
func BuildTree(nodes []FlatNode) []*TreeNode {
	ordered := append([]FlatNode(nil), nodes...)
	for i := 1; i < len(ordered); i++ {
		j := i
		for j > 0 && ordered[j].SortOrder < ordered[j-1].SortOrder {
			ordered[j], ordered[j-1] = ordered[j-1], ordered[j]
			j--
		}
	}
	index := map[int]*TreeNode{}
	var roots []*TreeNode
	for _, node := range ordered {
		current := &TreeNode{
			ID:                    node.LocalID,
			DescriptionCategoryID: node.DescriptionCategoryID,
			TypeID:                node.TypeID,
			Name:                  node.Name,
			Disabled:              node.Disabled,
			AncestorDisabled:      node.AncestorDisabled,
			IsLeaf:                node.IsLeaf,
			Level:                 node.Level,
			Path:                  append([]string(nil), node.Path...),
			Children:              []*TreeNode{},
		}
		index[node.LocalID] = current
		if node.ParentLocalID == nil {
			roots = append(roots, current)
			continue
		}
		parent := index[*node.ParentLocalID]
		if parent == nil {
			roots = append(roots, current)
			continue
		}
		parent.Children = append(parent.Children, current)
	}
	if roots == nil {
		roots = []*TreeNode{}
	}
	return roots
}

func viewTree(snap *Snapshot, nodes []FlatNode) *TreeView {
	view := &TreeView{
		Language:    snap.Language,
		Source:      snap.Source,
		SourceLabel: SourceLabel(snap.Source),
		TypeCount:   snap.TypeCount,
		NodeCount:   snap.NodeCount,
		LastError:   snap.LastError,
		LastErrorAt: snap.LastErrorAt,
		ShopID:      snap.ShopID,
		Roots:       BuildTree(nodes),
	}
	if snap.HasRow && !snap.SnapshotAt.IsZero() {
		at := snap.SnapshotAt
		view.SnapshotAt = &at
	}
	return view
}
