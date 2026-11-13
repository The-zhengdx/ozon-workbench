package app

import (
	"encoding/json"
	"fmt"
)

// TreeNode 是给前端树控件的一层。类型节点是叶子，类目节点只负责展开。
type TreeNode struct {
	Key                   string     `json:"key"`
	Title                 string     `json:"title"`
	DescriptionCategoryID int64      `json:"description_category_id,omitempty"`
	CategoryName          string     `json:"category_name,omitempty"`
	TypeID                int64      `json:"type_id,omitempty"`
	TypeName              string     `json:"type_name,omitempty"`
	Leaf                  bool       `json:"leaf"`
	Disabled              bool       `json:"disabled,omitempty"`
	Children              []TreeNode `json:"children,omitempty"`
}

func ParseCategoryTree(raw []byte) []TreeNode {
	var node any
	if err := json.Unmarshal(raw, &node); err != nil || node == nil {
		return []TreeNode{}
	}
	if object, ok := node.(map[string]any); ok {
		if result, ok := object["result"]; ok {
			node = result
		}
	}
	nodes := walkTree(node, TreeNode{})
	if nodes == nil {
		return []TreeNode{}
	}
	return nodes
}

func walkTree(v any, parent TreeNode) []TreeNode {
	switch n := v.(type) {
	case []any:
		out := make([]TreeNode, 0, len(n))
		for _, child := range n {
			out = append(out, walkTree(child, parent)...)
		}
		return out
	case map[string]any:
		node := parent
		node.Children = nil
		if id := int64From(n["description_category_id"]); id > 0 {
			node.DescriptionCategoryID = id
		}
		if name, _ := n["category_name"].(string); name != "" {
			node.CategoryName = name
		}
		if id := int64From(n["type_id"]); id > 0 {
			node.TypeID = id
		}
		if name, _ := n["type_name"].(string); name != "" {
			node.TypeName = name
		}
		if disabled, _ := n["disabled"].(bool); disabled {
			node.Disabled = true
		}
		if node.TypeID > 0 {
			node.Leaf = true
			node.Key = fmt.Sprintf("t-%d-%d", node.DescriptionCategoryID, node.TypeID)
			node.Title = node.TypeName
			if node.Title == "" {
				node.Title = fmt.Sprintf("类型 %d", node.TypeID)
			}
			return []TreeNode{node}
		}
		children := walkTree(n["children"], node)
		if node.DescriptionCategoryID == 0 && node.CategoryName == "" {
			return children
		}
		node.Leaf = false
		node.Key = fmt.Sprintf("c-%d", node.DescriptionCategoryID)
		node.Title = node.CategoryName
		if node.Title == "" {
			node.Title = fmt.Sprintf("类目 %d", node.DescriptionCategoryID)
		}
		node.Children = children
		return []TreeNode{node}
	default:
		return nil
	}
}
