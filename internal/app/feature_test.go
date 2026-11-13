package app

import (
	"context"
	"testing"

	"ozon-workbench/internal/ozonapi"
)

func TestLanguageStoredSeparatelyAndTreeIsHierarchical(t *testing.T) {
	fix := newEngine(t)
	eng, mock := fix.eng, fix.mock
	ctx := context.Background()
	shop := mustShop(t, eng)
	mock.TreeBody = []byte(`[{"description_category_id":1,"category_name":"服饰","children":[{"description_category_id":2,"category_name":"帽子","children":[{"type_id":9,"type_name":"针织帽"}]}]}]`)
	view, err := eng.SyncTree(ctx, shop.ID, "ZH")
	if err != nil {
		t.Fatal(err)
	}
	if mock.LastTreeLanguage != "ZH_HANS" || view.Language != "ZH_HANS" || view.LanguageUsed != "ZH_HANS" {
		t.Fatalf("language sent=%s view=%+v", mock.LastTreeLanguage, view.Language)
	}
	leaf := view.Nodes
	if len(leaf) != 1 || leaf[0].Title != "服饰" || len(leaf[0].Children) != 1 || leaf[0].Children[0].Title != "帽子" {
		t.Fatalf("nodes = %+v", leaf)
	}
	hat := leaf[0].Children[0].Children
	if len(hat) != 1 || !hat[0].Leaf || hat[0].TypeID != 9 || hat[0].DescriptionCategoryID != 2 || hat[0].TypeName != "针织帽" {
		t.Fatalf("leaf = %+v", hat)
	}
	mock.TreeBody = []byte(`[{"description_category_id":1,"category_name":"Одежда","children":[{"type_id":9,"type_name":"Шапка"}]}]`)
	if _, err := eng.SyncTree(ctx, shop.ID, "RU"); err != nil {
		t.Fatal(err)
	}
	zh, err := eng.GetTree(ctx, "ZH_HANS")
	if err != nil || zh.Fallback || zh.Nodes[0].Title != "服饰" {
		t.Fatalf("chinese overwritten: %+v %v", zh.Nodes, err)
	}
	ru, err := eng.GetTree(ctx, "EN")
	if err != nil || !ru.Fallback || ru.LanguageUsed != "ZH_HANS" {
		t.Fatalf("missing english should fall back before russian, got %+v %v", ru, err)
	}
	ru, err = eng.GetTree(ctx, "RU")
	if err != nil || ru.Nodes[0].Title != "Одежда" || ru.Nodes[0].Children[0].TypeName != "Шапка" {
		t.Fatalf("russian = %+v %v", ru.Nodes, err)
	}

	mock.Attrs = []ozonapi.Attribute{{ID: 85, Name: "品牌"}}
	if _, err := eng.SyncAttributes(ctx, shop.ID, 2, 9, "中文", ""); err != nil {
		t.Fatal(err)
	}
	if mock.LastAttrLanguage != "ZH_HANS" {
		t.Fatalf("attr language %s", mock.LastAttrLanguage)
	}
	mock.Attrs = []ozonapi.Attribute{{ID: 85, Name: "Бренд"}}
	if _, err := eng.SyncAttributes(ctx, shop.ID, 2, 9, "RU", ""); err != nil {
		t.Fatal(err)
	}
	local, err := eng.LocalAttributes(ctx, shop.ID, 2, 9, "ZH_HANS")
	if err != nil || !local.Synced || !contains(string(local.Attributes), "品牌") || contains(string(local.Attributes), "Бренд") {
		t.Fatalf("local zh = %+v %v", local, err)
	}
	missing, err := eng.LocalAttributes(ctx, shop.ID, 2, 8, "ZH_HANS")
	if err != nil || missing.Synced || string(missing.Attributes) != "[]" {
		t.Fatalf("unsynced = %+v %v", missing, err)
	}
}

func TestPromptRendersMainSKUAndKeepsUnknown(t *testing.T) {
	eng := newEngine(t).eng
	ctx := context.Background()
	shop := mustShop(t, eng)
	generic := "主SKU {{main_sku}} 保留 {{other}}"
	if _, err := eng.PatchSettings(ctx, SettingsPatch{GenericPrompt: &generic}); err != nil {
		t.Fatal(err)
	}
	mustRule(t, eng, 2, 9, "类目提示词")
	task, err := eng.CreateTask(ctx, NewTask{ShopID: shop.ID, CategoryID: 2, TypeID: 9, BaseSKU: "HAT-1"})
	if err != nil {
		t.Fatal(err)
	}
	full, err := eng.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatal(err)
	}
	want := "主SKU HAT-1 保留 {{other}}\n\n类目提示词"
	if full.PromptSnapshot != want {
		t.Fatalf("snapshot = %q", full.PromptSnapshot)
	}
}

func TestMediaObjectKeyLayout(t *testing.T) {
	got := MediaObjectKey("ozon/", "6134344", "HAT-1", "main", "image/jpeg", `..\hat photo.jpg`)
	if got != "ozon/6134344/HAT-1/images/hat_photo.jpg" {
		t.Fatalf("image key %s", got)
	}
	video := MediaObjectKey("", "client/id", "", "cover", "video/mp4", "clip.mp4")
	if video != "client_id/sku/videos/clip.mp4" {
		t.Fatalf("video key %s", video)
	}
	shared := MediaObjectKey("", "6134344", "MAIN", "gallery", "image/png", "a.png")
	if shared != "6134344/MAIN/images/a.png" {
		t.Fatalf("shared key %s", shared)
	}
}

func TestS3SettingsRoundTripPlaintext(t *testing.T) {
	eng := newEngine(t).eng
	ctx := context.Background()
	cfg := S3Settings{
		Endpoint: "https://s3.example.com", Region: "ru-1", Bucket: "goods",
		AccessKeyID: "AKID", SecretAccessKey: "plain-secret", PathStyle: true,
		PublicBaseURL: "https://cdn.example.com", KeyPrefix: "ozon",
	}
	if _, err := eng.PatchSettings(ctx, SettingsPatch{Storage: &cfg}); err != nil {
		t.Fatal(err)
	}
	eng.Uploader = nil
	got, err := eng.GetSettings(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if got.Storage.SecretAccessKey != "plain-secret" || got.Storage.Bucket != "goods" || got.Storage.KeyPrefix != "ozon" || !got.Storage.PathStyle {
		t.Fatalf("storage = %+v", got.Storage)
	}
	if !got.OSSConfigured || got.OSSProvider != "s3" {
		t.Fatalf("oss = %s %v %s", got.OSSProvider, got.OSSConfigured, got.OSSMessage)
	}
	generic := "只改提示词"
	if _, err := eng.PatchSettings(ctx, SettingsPatch{GenericPrompt: &generic}); err != nil {
		t.Fatal(err)
	}
	again, err := eng.GetSettings(ctx)
	if err != nil || again.Storage.SecretAccessKey != "plain-secret" || again.GenericPrompt != "只改提示词" {
		t.Fatalf("storage wiped %+v %v", again.Storage, err)
	}
}
