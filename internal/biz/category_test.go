package biz

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"ozon-workbench/internal/catalog"
)

func TestFlattenTreeKeepsBranchesLevelsAndDisabled(t *testing.T) {
	nodes, err := FlattenTree(catalog.ExampleTree("ZH_HANS"))
	if err != nil {
		t.Fatal(err)
	}
	var leaves, disabledLeaves, inherited int
	foundMount := false
	for _, node := range nodes {
		if node.Level < 1 {
			t.Fatalf("level: %+v", node)
		}
		if node.IsLeaf {
			leaves++
			if node.TypeID == 0 || node.DescriptionCategoryID == 0 || node.Name == "" {
				t.Fatalf("leaf missing ids: %+v", node)
			}
			if node.Disabled {
				disabledLeaves++
			}
			if node.AncestorDisabled && !node.Disabled {
				inherited++
			}
		}
		if node.TypeID == catalog.TypeMount {
			foundMount = true
			if node.DescriptionCategoryID != catalog.CatParts || node.Level != 3 {
				t.Fatalf("mount node: %+v", node)
			}
			if len(node.Path) != 3 || node.Path[2] != "车载手机支架" {
				t.Fatalf("path: %#v", node.Path)
			}
		}
		if node.TypeID == catalog.TypeRetired && !node.AncestorDisabled {
			t.Fatal("retired type should inherit the disabled parent")
		}
		if node.TypeID == catalog.TypeCap && !node.Disabled {
			t.Fatal("cap should keep its own disabled flag")
		}
	}
	if !foundMount || leaves < 7 || disabledLeaves == 0 || inherited == 0 {
		t.Fatalf("leaves=%d disabled=%d inherited=%d mount=%v", leaves, disabledLeaves, inherited, foundMount)
	}
	roots := BuildTree(attachLocalIDs(nodes))
	if len(roots) != 3 {
		t.Fatalf("roots %d", len(roots))
	}
}

func attachLocalIDs(nodes []FlatNode) []FlatNode {
	out := append([]FlatNode(nil), nodes...)
	index := map[int]int{}
	for i := range out {
		out[i].LocalID = out[i].TempID
		index[out[i].TempID] = out[i].LocalID
	}
	for i := range out {
		if out[i].ParentTempID == 0 {
			continue
		}
		id := index[out[i].ParentTempID]
		out[i].ParentLocalID = &id
	}
	return out
}

type memCats struct {
	mu       sync.Mutex
	snaps    map[string]*Snapshot
	nodes    map[string][]FlatNode
	attrs    []*AttrSnapshot
	dicts    []*DictSnapshot
	prompts  []*Prompt
	saveTree int
}

func newMemCats() *memCats {
	return &memCats{snaps: map[string]*Snapshot{}, nodes: map[string][]FlatNode{}}
}

func (m *memCats) GetSnapshot(_ context.Context, language string) (*Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if row := m.snaps[language]; row != nil {
		cp := *row
		return &cp, nil
	}
	return &Snapshot{Language: language}, nil
}

func (m *memCats) ListNodes(_ context.Context, language string) ([]FlatNode, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]FlatNode(nil), m.nodes[language]...), nil
}

func (m *memCats) SaveTree(_ context.Context, language, source string, shopID *int, nodes []FlatNode) (*Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.saveTree++
	stored := attachLocalIDs(nodes)
	m.nodes[language] = stored
	leaves := 0
	for _, node := range stored {
		if node.IsLeaf {
			leaves++
		}
	}
	snap := &Snapshot{
		Language: language, Source: source, TypeCount: leaves, NodeCount: len(stored),
		SnapshotAt: time.Now().UTC(), ShopID: shopID, HasRow: true,
	}
	m.snaps[language] = snap
	cp := *snap
	return &cp, nil
}

func (m *memCats) RecordSyncError(_ context.Context, language string, shopID *int, message string) (*Snapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	snap := m.snaps[language]
	if snap == nil {
		snap = &Snapshot{Language: language, HasRow: true}
	}
	at := time.Now().UTC()
	snap.LastError = message
	snap.LastErrorAt = &at
	snap.ShopID = shopID
	snap.HasRow = true
	m.snaps[language] = snap
	cp := *snap
	return &cp, nil
}

func (m *memCats) LatestAttributes(_ context.Context, categoryID, typeID int64, language string) (*AttrSnapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var best *AttrSnapshot
	for _, row := range m.attrs {
		if row.DescriptionCategoryID == categoryID && row.TypeID == typeID && row.Language == language {
			if best == nil || row.Version > best.Version {
				cp := *row
				cp.Attributes = append([]map[string]any(nil), row.Attributes...)
				best = &cp
			}
		}
	}
	return best, nil
}

func (m *memCats) SaveAttributes(_ context.Context, snap *AttrSnapshot) (*AttrSnapshot, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	version := 1
	for _, row := range m.attrs {
		if row.DescriptionCategoryID == snap.DescriptionCategoryID && row.TypeID == snap.TypeID && row.Language == snap.Language && row.Version >= version {
			version = row.Version + 1
		}
	}
	cp := *snap
	cp.Version = version
	cp.Attributes = append([]map[string]any(nil), snap.Attributes...)
	m.attrs = append(m.attrs, &cp)
	out := cp
	return &out, nil
}

func (m *memCats) GetDictionary(context.Context, int64, int64, int64, string, string) (*DictSnapshot, error) {
	return nil, nil
}

func (m *memCats) SaveDictionary(_ context.Context, snap *DictSnapshot) (*DictSnapshot, error) {
	cp := *snap
	cp.Version = 1
	m.dicts = append(m.dicts, &cp)
	return &cp, nil
}

func (m *memCats) GetPrompt(_ context.Context, categoryID, typeID int64) (*Prompt, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, row := range m.prompts {
		if row.DescriptionCategoryID == categoryID && row.TypeID == typeID {
			cp := *row
			return &cp, nil
		}
	}
	return nil, nil
}

func (m *memCats) SavePrompt(_ context.Context, prompt *Prompt) (*Prompt, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for i, row := range m.prompts {
		if row.DescriptionCategoryID == prompt.DescriptionCategoryID && row.TypeID == prompt.TypeID {
			cp := *prompt
			cp.ID = row.ID
			m.prompts[i] = &cp
			return &cp, nil
		}
	}
	cp := *prompt
	cp.ID = len(m.prompts) + 1
	m.prompts = append(m.prompts, &cp)
	return &cp, nil
}

type scriptedOzon struct {
	fakeOzon
	treeErr    error
	tree       []catalog.TreeNode
	attrErr    error
	attrByType map[int64][]map[string]any
	attrCalls  []int64
}

func (s *scriptedOzon) CategoryTree(context.Context, OzonCred, string) ([]catalog.TreeNode, error) {
	if s.treeErr != nil {
		return nil, s.treeErr
	}
	return s.tree, nil
}

func (s *scriptedOzon) Attributes(_ context.Context, _ OzonCred, _, typeID int64, _ string) ([]map[string]any, error) {
	s.attrCalls = append(s.attrCalls, typeID)
	if s.attrErr != nil {
		return nil, s.attrErr
	}
	return s.attrByType[typeID], nil
}

func (s *scriptedOzon) AttributeValues(context.Context, OzonCred, int64, int64, int64, int64, int, string) (*ValuePage, error) {
	return &ValuePage{Values: []map[string]any{{"id": int64(1), "value": "无品牌"}}, HasNext: false, LastValueID: 1}, nil
}

func (s *scriptedOzon) SearchAttributeValues(context.Context, OzonCred, int64, int64, int64, string, int) ([]map[string]any, error) {
	return []map[string]any{{"id": int64(2), "value": "Bosch"}}, nil
}

func TestSyncFailureKeepsLastSnapshot(t *testing.T) {
	repo := newMemCats()
	flat, err := FlattenTree(catalog.ExampleTree("ZH_HANS"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SaveTree(context.Background(), "ZH_HANS", "offline_example", nil, flat); err != nil {
		t.Fatal(err)
	}
	before := repo.saveTree
	oz := &scriptedOzon{treeErr: errors.New("Ozon HTTP 403: Api-Key is missing a required role")}
	shops, _ := newShopUC(oz)
	if _, err := shops.Create(context.Background(), "甲", "CID", "k"); err != nil {
		t.Fatal(err)
	}
	uc := NewCategoryUsecase(repo, shops, oz, discardLogger())
	_, err = uc.SyncTree(context.Background(), 1, "ZH_HANS")
	be, ok := err.(*Error)
	if !ok || be.Reason != "OZON_ERROR" {
		t.Fatalf("err %#v", err)
	}
	if be.Message != oz.treeErr.Error() {
		t.Fatalf("message %q", be.Message)
	}
	if repo.saveTree != before {
		t.Fatal("failed sync replaced the snapshot")
	}
	view, ok := be.Snapshot.(*TreeView)
	if !ok || view.Source != "offline_example" || view.TypeCount == 0 || view.LastError == "" {
		t.Fatalf("snapshot %#v", be.Snapshot)
	}
	loaded, err := uc.Tree(context.Background(), "zh_hans")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Source != "offline_example" || loaded.LastError == "" || len(loaded.Roots) == 0 {
		t.Fatalf("kept tree: %#v", loaded)
	}
}

func TestSiblingTypesKeepSeparateAttributesAndPrompts(t *testing.T) {
	repo := newMemCats()
	flat, err := FlattenTree(catalog.ExampleTree("ZH_HANS"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := repo.SaveTree(context.Background(), "ZH_HANS", "ozon", nil, flat); err != nil {
		t.Fatal(err)
	}
	mount := catalog.ExampleAttributes(catalog.TypeMount, "ZH_HANS")
	if _, err := repo.SaveAttributes(context.Background(), &AttrSnapshot{
		DescriptionCategoryID: catalog.CatParts,
		TypeID:                catalog.TypeMount,
		Language:              "ZH_HANS",
		Attributes:            mount,
		Source:                "ozon",
		SyncedAt:              time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	charger := catalog.ExampleAttributes(catalog.TypeCharger, "ZH_HANS")
	oz := &scriptedOzon{attrByType: map[int64][]map[string]any{catalog.TypeCharger: charger}}
	shops, _ := newShopUC(oz)
	if _, err := shops.Create(context.Background(), "甲", "CID", "k"); err != nil {
		t.Fatal(err)
	}
	uc := NewCategoryUsecase(repo, shops, oz, discardLogger())
	got, err := uc.SyncAttributes(context.Background(), 1, catalog.CatParts, catalog.TypeCharger, "ZH_HANS")
	if err != nil {
		t.Fatal(err)
	}
	if got.Total != len(charger) || got.RequiredCount == 0 || got.OptionalCount == 0 {
		t.Fatalf("counts total=%d required=%d optional=%d", got.Total, got.RequiredCount, got.OptionalCount)
	}
	mountView, err := uc.Attributes(context.Background(), catalog.CatParts, catalog.TypeMount, "ZH_HANS")
	if err != nil {
		t.Fatal(err)
	}
	if mountView.Total != len(mount) || mountView.Attributes[len(mountView.Attributes)-1]["name"] == got.Attributes[len(got.Attributes)-1]["name"] {
		t.Fatal("sibling type attributes were overwritten")
	}
	oz.attrErr = errors.New("Ozon HTTP 429: rate limit exceeded")
	_, err = uc.SyncAttributes(context.Background(), 1, catalog.CatParts, catalog.TypeCharger, "ZH_HANS")
	be, ok := err.(*Error)
	if !ok || be.Snapshot == nil {
		t.Fatalf("expected previous snapshot on failure: %#v", err)
	}
	again, err := uc.Attributes(context.Background(), catalog.CatParts, catalog.TypeCharger, "ZH_HANS")
	if err != nil || again.Total != len(charger) {
		t.Fatalf("cache lost: %v %#v", err, again)
	}
	saved, err := uc.SavePrompt(context.Background(), catalog.CatParts, catalog.TypeMount, "主 SKU {{master_sku}} 生成支架子 SKU")
	if err != nil {
		t.Fatal(err)
	}
	if !saved.Saved || saved.CodexPrompt != "主 SKU {{master_sku}} 生成支架子 SKU" {
		t.Fatalf("prompt %#v", saved)
	}
	other, err := uc.SavePrompt(context.Background(), catalog.CatParts, catalog.TypeCharger, "充电器 {{master_sku}}-电源")
	if err != nil {
		t.Fatal(err)
	}
	mountPrompt, err := uc.GetPrompt(context.Background(), catalog.CatParts, catalog.TypeMount)
	if err != nil {
		t.Fatal(err)
	}
	if mountPrompt.CodexPrompt != saved.CodexPrompt || other.CodexPrompt == mountPrompt.CodexPrompt {
		t.Fatal("prompts are not independent")
	}
}

func TestSearchFindsCollapsedTypeID(t *testing.T) {
	repo := newMemCats()
	uc := NewCategoryUsecase(repo, nil, nil, discardLogger())
	hits, err := uc.Search(context.Background(), "ZH_HANS", "97100345")
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, hit := range hits {
		if hit.TypeID == catalog.TypeKnit && len(hit.Path) >= 3 {
			found = true
		}
	}
	if !found {
		t.Fatalf("hits %#v", hits)
	}
}
