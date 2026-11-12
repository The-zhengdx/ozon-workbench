package data

import (
	"context"
	"encoding/json"
	"time"

	"entgo.io/ent/dialect/sql"

	"ozon-workbench/internal/biz"
	"ozon-workbench/internal/data/ent"
	"ozon-workbench/internal/data/ent/attributecontract"
	"ozon-workbench/internal/data/ent/categorynode"
	"ozon-workbench/internal/data/ent/categoryprompt"
	"ozon-workbench/internal/data/ent/categorysnapshot"
	"ozon-workbench/internal/data/ent/dictionarysnapshot"
)

type categoryRepo struct {
	db *ent.Client
}

func NewCategoryRepo(data *Data) biz.CategoryRepo {
	return &categoryRepo{db: data.Client}
}

func (r *categoryRepo) GetSnapshot(ctx context.Context, language string) (*biz.Snapshot, error) {
	row, err := r.db.CategorySnapshot.Query().Where(categorysnapshot.LanguageEQ(language)).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return &biz.Snapshot{Language: language}, nil
		}
		return nil, err
	}
	return toBizSnapshot(row), nil
}

func (r *categoryRepo) ListNodes(ctx context.Context, language string) ([]biz.FlatNode, error) {
	rows, err := r.db.CategoryNode.Query().
		Where(categorynode.LanguageEQ(language)).
		Order(categorynode.BySortOrder()).
		All(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]biz.FlatNode, 0, len(rows))
	for _, row := range rows {
		out = append(out, biz.FlatNode{
			LocalID:               row.ID,
			ParentLocalID:         row.ParentLocalID,
			DescriptionCategoryID: row.DescriptionCategoryID,
			TypeID:                row.TypeID,
			Name:                  row.Name,
			Disabled:              row.Disabled,
			AncestorDisabled:      row.AncestorDisabled,
			IsLeaf:                row.IsLeaf,
			Level:                 row.Level,
			Path:                  append([]string(nil), row.Path...),
			SortOrder:             row.SortOrder,
		})
	}
	return out, nil
}

func (r *categoryRepo) SaveTree(ctx context.Context, language, source string, shopID *int, nodes []biz.FlatNode) (*biz.Snapshot, error) {
	tx, err := r.db.Tx(ctx)
	if err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			_ = tx.Rollback()
		}
	}()
	if _, err = tx.CategoryNode.Delete().Where(categorynode.LanguageEQ(language)).Exec(ctx); err != nil {
		return nil, err
	}
	idMap := map[int]int{}
	leaves := 0
	for _, node := range nodes {
		if node.IsLeaf {
			leaves++
		}
		builder := tx.CategoryNode.Create().
			SetLanguage(language).
			SetDescriptionCategoryID(node.DescriptionCategoryID).
			SetTypeID(node.TypeID).
			SetName(node.Name).
			SetDisabled(node.Disabled).
			SetAncestorDisabled(node.AncestorDisabled).
			SetIsLeaf(node.IsLeaf).
			SetLevel(node.Level).
			SetPath(node.Path).
			SetSortOrder(node.SortOrder)
		if node.ParentTempID != 0 {
			parent, ok := idMap[node.ParentTempID]
			if !ok {
				err = &biz.Error{Status: 500, Reason: "TREE_PARENT", Message: "类目树父子关系无法落库。"}
				return nil, err
			}
			builder.SetParentLocalID(parent)
		}
		saved, serr := builder.Save(ctx)
		if serr != nil {
			err = serr
			return nil, err
		}
		idMap[node.TempID] = saved.ID
	}
	now := time.Now().UTC()
	existing, gerr := tx.CategorySnapshot.Query().Where(categorysnapshot.LanguageEQ(language)).Only(ctx)
	if ent.IsNotFound(gerr) {
		create := tx.CategorySnapshot.Create().
			SetLanguage(language).
			SetSource(source).
			SetTypeCount(leaves).
			SetNodeCount(len(nodes)).
			SetSnapshotAt(now).
			SetNillableShopID(shopID)
		if _, err = create.Save(ctx); err != nil {
			return nil, err
		}
	} else if gerr != nil {
		err = gerr
		return nil, err
	} else {
		update := tx.CategorySnapshot.UpdateOne(existing).
			SetSource(source).
			SetTypeCount(leaves).
			SetNodeCount(len(nodes)).
			SetSnapshotAt(now).
			ClearLastError().
			ClearLastErrorAt()
		if shopID != nil {
			update.SetShopID(*shopID)
		} else {
			update.ClearShopID()
		}
		if _, err = update.Save(ctx); err != nil {
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return &biz.Snapshot{
		Language:   language,
		Source:     source,
		TypeCount:  leaves,
		NodeCount:  len(nodes),
		SnapshotAt: now,
		ShopID:     shopID,
		HasRow:     true,
	}, nil
}

func (r *categoryRepo) RecordSyncError(ctx context.Context, language string, shopID *int, message string) (*biz.Snapshot, error) {
	if len(message) > 4000 {
		message = message[:4000]
	}
	now := time.Now().UTC()
	row, err := r.db.CategorySnapshot.Query().Where(categorysnapshot.LanguageEQ(language)).Only(ctx)
	if ent.IsNotFound(err) {
		created, cerr := r.db.CategorySnapshot.Create().
			SetLanguage(language).
			SetSource("").
			SetTypeCount(0).
			SetNodeCount(0).
			SetSnapshotAt(now).
			SetLastError(message).
			SetLastErrorAt(now).
			SetNillableShopID(shopID).
			Save(ctx)
		if cerr != nil {
			return nil, cerr
		}
		return toBizSnapshot(created), nil
	}
	if err != nil {
		return nil, err
	}
	update := r.db.CategorySnapshot.UpdateOne(row).
		SetLastError(message).
		SetLastErrorAt(now)
	if shopID != nil {
		update.SetShopID(*shopID)
	}
	saved, err := update.Save(ctx)
	if err != nil {
		return nil, err
	}
	return toBizSnapshot(saved), nil
}

func (r *categoryRepo) LatestAttributes(ctx context.Context, categoryID, typeID int64, language string) (*biz.AttrSnapshot, error) {
	row, err := r.db.AttributeContract.Query().
		Where(
			attributecontract.DescriptionCategoryIDEQ(categoryID),
			attributecontract.TypeIDEQ(typeID),
			attributecontract.LanguageEQ(language),
		).
		Order(attributecontract.ByVersion(sql.OrderDesc())).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	attrs, err := decodeAttrs(row.AttributesJSON)
	if err != nil {
		return nil, err
	}
	return &biz.AttrSnapshot{
		DescriptionCategoryID: row.DescriptionCategoryID,
		TypeID:                row.TypeID,
		Language:              row.Language,
		Version:               row.Version,
		Attributes:            attrs,
		Source:                row.Source,
		SyncedAt:              row.SyncedAt,
	}, nil
}

func (r *categoryRepo) SaveAttributes(ctx context.Context, snap *biz.AttrSnapshot) (*biz.AttrSnapshot, error) {
	latest, err := r.LatestAttributes(ctx, snap.DescriptionCategoryID, snap.TypeID, snap.Language)
	if err != nil {
		return nil, err
	}
	version := 1
	if latest != nil {
		version = latest.Version + 1
	}
	raw, err := encodeAttrs(snap.Attributes)
	if err != nil {
		return nil, err
	}
	synced := snap.SyncedAt
	if synced.IsZero() {
		synced = time.Now().UTC()
	}
	if _, err := r.db.AttributeContract.Create().
		SetDescriptionCategoryID(snap.DescriptionCategoryID).
		SetTypeID(snap.TypeID).
		SetLanguage(snap.Language).
		SetVersion(version).
		SetAttributesJSON(raw).
		SetSource(snap.Source).
		SetSyncedAt(synced).
		Save(ctx); err != nil {
		return nil, err
	}
	doc, _ := json.Marshal(map[string]any{
		"language":   snap.Language,
		"version":    version,
		"source":     snap.Source,
		"synced_at":  synced,
		"attributes": snap.Attributes,
	})
	_, _ = r.db.CategoryNode.Update().
		Where(
			categorynode.LanguageEQ(snap.Language),
			categorynode.DescriptionCategoryIDEQ(snap.DescriptionCategoryID),
			categorynode.TypeIDEQ(snap.TypeID),
		).
		SetAttributesSchema(string(doc)).
		Save(ctx)
	out := *snap
	out.Version = version
	out.SyncedAt = synced
	if out.Attributes == nil {
		out.Attributes = []map[string]any{}
	}
	return &out, nil
}

func (r *categoryRepo) GetDictionary(ctx context.Context, categoryID, typeID, attributeID int64, language, query string) (*biz.DictSnapshot, error) {
	row, err := r.db.DictionarySnapshot.Query().Where(
		dictionarysnapshot.DescriptionCategoryIDEQ(categoryID),
		dictionarysnapshot.TypeIDEQ(typeID),
		dictionarysnapshot.AttributeIDEQ(attributeID),
		dictionarysnapshot.LanguageEQ(language),
		dictionarysnapshot.QueryEQ(query),
	).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	values, err := decodeAttrs(row.ValuesJSON)
	if err != nil {
		return nil, err
	}
	return &biz.DictSnapshot{
		DescriptionCategoryID: row.DescriptionCategoryID,
		TypeID:                row.TypeID,
		AttributeID:           row.AttributeID,
		Language:              row.Language,
		Query:                 row.Query,
		Version:               row.Version,
		Values:                values,
		HasNext:               row.HasNext,
		LastValueID:           row.LastValueID,
		SyncedAt:              row.SyncedAt,
	}, nil
}

func (r *categoryRepo) SaveDictionary(ctx context.Context, snap *biz.DictSnapshot) (*biz.DictSnapshot, error) {
	raw, err := encodeAttrs(snap.Values)
	if err != nil {
		return nil, err
	}
	synced := snap.SyncedAt
	if synced.IsZero() {
		synced = time.Now().UTC()
	}
	existing, err := r.db.DictionarySnapshot.Query().Where(
		dictionarysnapshot.DescriptionCategoryIDEQ(snap.DescriptionCategoryID),
		dictionarysnapshot.TypeIDEQ(snap.TypeID),
		dictionarysnapshot.AttributeIDEQ(snap.AttributeID),
		dictionarysnapshot.LanguageEQ(snap.Language),
		dictionarysnapshot.QueryEQ(snap.Query),
	).Only(ctx)
	if ent.IsNotFound(err) {
		row, cerr := r.db.DictionarySnapshot.Create().
			SetDescriptionCategoryID(snap.DescriptionCategoryID).
			SetTypeID(snap.TypeID).
			SetAttributeID(snap.AttributeID).
			SetLanguage(snap.Language).
			SetQuery(snap.Query).
			SetVersion(1).
			SetValuesJSON(raw).
			SetHasNext(snap.HasNext).
			SetLastValueID(snap.LastValueID).
			SetSyncedAt(synced).
			Save(ctx)
		if cerr != nil {
			return nil, cerr
		}
		out := *snap
		out.Version = row.Version
		out.SyncedAt = row.SyncedAt
		return &out, nil
	}
	if err != nil {
		return nil, err
	}
	row, err := r.db.DictionarySnapshot.UpdateOne(existing).
		SetVersion(existing.Version + 1).
		SetValuesJSON(raw).
		SetHasNext(snap.HasNext).
		SetLastValueID(snap.LastValueID).
		SetSyncedAt(synced).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	out := *snap
	out.Version = row.Version
	out.SyncedAt = row.SyncedAt
	return &out, nil
}

func (r *categoryRepo) GetPrompt(ctx context.Context, categoryID, typeID int64) (*biz.Prompt, error) {
	row, err := r.db.CategoryPrompt.Query().Where(
		categoryprompt.DescriptionCategoryIDEQ(categoryID),
		categoryprompt.TypeIDEQ(typeID),
	).Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return &biz.Prompt{
		ID:                    row.ID,
		DescriptionCategoryID: row.DescriptionCategoryID,
		TypeID:                row.TypeID,
		CodexPrompt:           row.CodexPrompt,
		UpdatedAt:             row.UpdatedAt,
	}, nil
}

func (r *categoryRepo) SavePrompt(ctx context.Context, prompt *biz.Prompt) (*biz.Prompt, error) {
	existing, err := r.db.CategoryPrompt.Query().Where(
		categoryprompt.DescriptionCategoryIDEQ(prompt.DescriptionCategoryID),
		categoryprompt.TypeIDEQ(prompt.TypeID),
	).Only(ctx)
	if ent.IsNotFound(err) {
		row, cerr := r.db.CategoryPrompt.Create().
			SetDescriptionCategoryID(prompt.DescriptionCategoryID).
			SetTypeID(prompt.TypeID).
			SetCodexPrompt(prompt.CodexPrompt).
			SetUpdatedAt(prompt.UpdatedAt).
			Save(ctx)
		if cerr != nil {
			return nil, cerr
		}
		return &biz.Prompt{ID: row.ID, DescriptionCategoryID: row.DescriptionCategoryID, TypeID: row.TypeID, CodexPrompt: row.CodexPrompt, UpdatedAt: row.UpdatedAt}, nil
	}
	if err != nil {
		return nil, err
	}
	row, err := r.db.CategoryPrompt.UpdateOne(existing).
		SetCodexPrompt(prompt.CodexPrompt).
		SetUpdatedAt(prompt.UpdatedAt).
		Save(ctx)
	if err != nil {
		return nil, err
	}
	return &biz.Prompt{ID: row.ID, DescriptionCategoryID: row.DescriptionCategoryID, TypeID: row.TypeID, CodexPrompt: row.CodexPrompt, UpdatedAt: row.UpdatedAt}, nil
}

func toBizSnapshot(row *ent.CategorySnapshot) *biz.Snapshot {
	snap := &biz.Snapshot{
		Language:   row.Language,
		Source:     row.Source,
		TypeCount:  row.TypeCount,
		NodeCount:  row.NodeCount,
		SnapshotAt: row.SnapshotAt,
		ShopID:     row.ShopID,
		HasRow:     true,
	}
	if row.LastError != nil {
		snap.LastError = *row.LastError
	}
	snap.LastErrorAt = row.LastErrorAt
	return snap
}

func encodeAttrs(attrs []map[string]any) (string, error) {
	if attrs == nil {
		attrs = []map[string]any{}
	}
	raw, err := json.Marshal(attrs)
	if err != nil {
		return "", err
	}
	return string(raw), nil
}

func decodeAttrs(raw string) ([]map[string]any, error) {
	if raw == "" {
		return []map[string]any{}, nil
	}
	var attrs []map[string]any
	if err := json.Unmarshal([]byte(raw), &attrs); err != nil {
		return nil, err
	}
	if attrs == nil {
		attrs = []map[string]any{}
	}
	return attrs, nil
}
