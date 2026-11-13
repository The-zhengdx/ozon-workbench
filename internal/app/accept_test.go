package app

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"ozon-workbench/internal/codexcli"
	"ozon-workbench/internal/domain"
	"ozon-workbench/internal/ozonapi"
	"ozon-workbench/internal/pricing"
	"ozon-workbench/internal/storage"
	"ozon-workbench/internal/store"
)

func TestShopDefaultsAndColumns(t *testing.T) {
	if len(domain.ShopColumns()) != 9 {
		t.Fatalf("shops columns = %d", len(domain.ShopColumns()))
	}
	eng := newEngine(t).eng
	shop, err := eng.CreateShop(context.Background(), ShopInput{Name: "一号店", ClientID: "c1", APIKey: "secret-key"})
	if err != nil {
		t.Fatal(err)
	}
	if shop.CurrencyCode != "CNY" || shop.VAT != "0.1" || !shop.Enable || shop.CreatedAt.IsZero() || shop.UpdatedAt.IsZero() {
		t.Fatalf("defaults = %+v", shop)
	}
	if shop.APIKey != "secret-key" {
		t.Fatal("plaintext api key was not stored")
	}
	if MaskKey(shop.APIKey) != "****-key" {
		t.Fatalf("mask = %s", MaskKey(shop.APIKey))
	}
}

func TestTaskShopIsFrozen(t *testing.T) {
	eng := newEngine(t).eng
	ctx := context.Background()
	a, _ := eng.CreateShop(ctx, ShopInput{Name: "A", ClientID: "a", APIKey: "ka"})
	b, _ := eng.CreateShop(ctx, ShopInput{Name: "B", ClientID: "b", APIKey: "kb"})
	mustRule(t, eng, 10, 20, "类目提示词甲")
	first, err := eng.CreateTask(ctx, NewTask{ShopID: a.ID, CategoryID: 10, TypeID: 20, BaseSKU: "BASE"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := eng.CreateTask(ctx, NewTask{ShopID: b.ID, CategoryID: 10, TypeID: 20, BaseSKU: "OTHER"}); err != nil {
		t.Fatal(err)
	}
	again, err := eng.GetTask(ctx, first.ID)
	if err != nil {
		t.Fatal(err)
	}
	if again.ShopID != a.ID {
		t.Fatalf("shop changed to %d", again.ShopID)
	}
}

func TestCategorySearchAttributesAndPromptVersion(t *testing.T) {
	fix := newEngine(t)
	eng, mock := fix.eng, fix.mock
	ctx := context.Background()
	shop, _ := eng.CreateShop(ctx, ShopInput{Name: "A", ClientID: "a", APIKey: "ka"})
	mock.TreeBody = []byte(`[{"description_category_id":17028922,"category_name":"电子","children":[{"type_id":91565,"type_name":"Чехол"}]}]`)
	if _, err := eng.SyncTree(ctx, shop.ID, "ZH"); err != nil {
		t.Fatal(err)
	}
	hits, err := eng.SearchTree(ctx, "ZH", "чехол")
	if err != nil || len(hits) != 1 || hits[0].TypeID != 91565 {
		t.Fatalf("hits = %+v %v", hits, err)
	}
	idHits, err := eng.SearchTree(ctx, "ZH", "17028922")
	if err != nil || len(idHits) != 1 {
		t.Fatalf("id hits = %+v", idHits)
	}
	mock.Attrs = []ozonapi.Attribute{{ID: 85, Name: "品牌", IsRequired: true, DictionaryID: 1}}
	if _, err := eng.Attributes(ctx, shop.ID, 17028922, 91565, "RU", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.Attributes(ctx, shop.ID, 17028922, 91565, "RU", ""); err != nil {
		t.Fatal(err)
	}
	if mock.AttrCalls != 1 {
		t.Fatalf("attr calls = %d", mock.AttrCalls)
	}
	mock.SearchErr = errString("down")
	if _, err := eng.Dictionary(ctx, shop.ID, 17028922, 91565, 85, "RU", "серый", 0, 0, ""); err == nil {
		t.Fatal("upstream failure became an empty dictionary")
	}
	mock.SearchErr = nil
	mock.SearchHits = []ozonapi.Value{{ID: 7, Value: "серый"}}
	got, err := eng.Dictionary(ctx, shop.ID, 17028922, 91565, 85, "RU", "серый", 0, 0, "")
	if err != nil {
		t.Fatal(err)
	}
	blob, _ := json.Marshal(got)
	if !json.Valid(blob) || !contains(string(blob), "серый") {
		t.Fatalf("dict = %s", blob)
	}

	rule, err := eng.SaveRule(ctx, 17028922, 91565, "第一版提示词", true)
	if err != nil || rule.Version != 1 {
		t.Fatalf("rule %+v %v", rule, err)
	}
	task, err := eng.CreateTask(ctx, NewTask{ShopID: shop.ID, CategoryID: 17028922, TypeID: 91565, BaseSKU: "S"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := eng.SaveRule(ctx, 17028922, 91565, "第二版提示词", true); err != nil {
		t.Fatal(err)
	}
	again, _ := eng.GetRule(ctx, 17028922, 91565)
	if again.Version != 2 || again.Prompt != "第二版提示词" || again.ID != rule.ID {
		t.Fatalf("rule changed shape %+v", again)
	}
	full, _ := eng.GetTask(ctx, task.ID)
	if full.PromptSnapshot != "第一版提示词" {
		t.Fatalf("snapshot = %s", full.PromptSnapshot)
	}
}

func TestSchedulerFIFOAndWaitingHoldsSlot(t *testing.T) {
	fix := newEngine(t)
	eng, runner := fix.eng, fix.runner
	ctx := context.Background()
	shop := mustShop(t, eng)
	mustRule(t, eng, 1, 2, "提示词")
	runner.replies = repeat(3, pendingReview())
	var ids []int64
	for i := 0; i < 3; i++ {
		task, err := eng.CreateTask(ctx, NewTask{ShopID: shop.ID, CategoryID: 1, TypeID: 2, BaseSKU: "B"})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, task.ID)
	}
	if err := eng.Pump(ctx); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		task, _ := eng.GetTask(ctx, id)
		if task.Status != domain.StatusWaitingReview || !task.HoldsSlot {
			t.Fatalf("task %d status %s slot %v", id, task.Status, task.HoldsSlot)
		}
	}
	extra := make([]int64, 0, 2)
	for i := 0; i < 2; i++ {
		task, err := eng.CreateTask(ctx, NewTask{ShopID: shop.ID, CategoryID: 1, TypeID: 2, BaseSKU: "Q"})
		if err != nil {
			t.Fatal(err)
		}
		extra = append(extra, task.ID)
	}
	if err := eng.Pump(ctx); err != nil {
		t.Fatal(err)
	}
	for _, id := range extra {
		task, _ := eng.GetTask(ctx, id)
		if task.Status != domain.StatusQueued || task.HoldsSlot {
			t.Fatalf("extra %d status %s", id, task.Status)
		}
	}
	if _, err := eng.PatchSettings(ctx, SettingsPatch{MaxActiveTasks: intPtr(1)}); err != nil {
		t.Fatal(err)
	}
	if err := eng.Pump(ctx); err != nil {
		t.Fatal(err)
	}
	for _, id := range ids {
		task, _ := eng.GetTask(ctx, id)
		if task.Status != domain.StatusWaitingReview {
			t.Fatalf("lowered limit killed %s", task.Status)
		}
	}
	if err := eng.CancelTask(ctx, ids[0]); err != nil {
		t.Fatal(err)
	}
	runner.replies = [][]byte{pendingReview()}
	if _, err := eng.PatchSettings(ctx, SettingsPatch{MaxActiveTasks: intPtr(3)}); err != nil {
		t.Fatal(err)
	}
	if err := eng.Pump(ctx); err != nil {
		t.Fatal(err)
	}
	first, _ := eng.GetTask(ctx, extra[0])
	second, _ := eng.GetTask(ctx, extra[1])
	if first.Status != domain.StatusWaitingReview || second.Status != domain.StatusQueued {
		t.Fatalf("fifo first=%s second=%s", first.Status, second.Status)
	}
}

func TestQueuedMessagesDoNotInterruptOrUploadStaleMedia(t *testing.T) {
	fix := newEngine(t)
	eng, runner, up := fix.eng, fix.runner, fix.up
	ctx := context.Background()
	shop := mustShop(t, eng)
	mustRule(t, eng, 1, 2, "提示词")
	task, err := eng.CreateTask(ctx, NewTask{ShopID: shop.ID, CategoryID: 1, TypeID: 2, BaseSKU: "B", VisualResultAutoApprove: boolPtr(true), CopyAutoApprove: boolPtr(true), VisualPlanAutoApprove: boolPtr(true)})
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(eng.taskDir(task.ID), "output", "a.png"))
	release := make(chan struct{})
	started := make(chan struct{}, 1)
	runner.release = release
	runner.started = started
	runner.replies = [][]byte{mediaReady("output/a.png"), needsInput()}
	done := make(chan error, 1)
	go func() { done <- eng.Pump(ctx) }()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("turn did not start")
	}
	if _, err := eng.AddMessage(ctx, task.ID, "改成灰色", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := eng.AddMessage(ctx, task.ID, "再加一张侧面图", ""); err != nil {
		t.Fatal(err)
	}
	if len(runner.snapshot()) != 1 {
		t.Fatalf("cli was interrupted, calls=%d", len(runner.snapshot()))
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	calls := runner.snapshot()
	if len(calls) < 2 {
		t.Fatalf("calls = %d", len(calls))
	}
	if calls[1].SessionID != "sess-1" {
		t.Fatalf("resume session = %s", calls[1].SessionID)
	}
	if !contains(calls[1].Prompt, "改成灰色") || !contains(calls[1].Prompt, "再加一张侧面图") {
		t.Fatalf("prompt missing queued messages:\n%s", calls[1].Prompt)
	}
	if idx(calls[1].Prompt, "改成灰色") > idx(calls[1].Prompt, "再加一张侧面图") {
		t.Fatal("messages were not kept in order")
	}
	if len(up.Files()) != 0 {
		t.Fatalf("stale media uploaded: %+v", up.Files())
	}
	view, _ := eng.GetTask(ctx, task.ID)
	if view.Status == domain.StatusCompleted || view.Status == domain.StatusUploadingMedia {
		t.Fatalf("status = %s", view.Status)
	}
}

func TestNeedsInputAndInvalidJSON(t *testing.T) {
	fix := newEngine(t)
	eng, runner := fix.eng, fix.runner
	ctx := context.Background()
	shop := mustShop(t, eng)
	mustRule(t, eng, 1, 2, "提示词")
	retries := 1
	if _, err := eng.PatchSettings(ctx, SettingsPatch{MaxInfraRetries: &retries}); err != nil {
		t.Fatal(err)
	}
	runner.replies = [][]byte{needsInput()}
	task, _ := eng.CreateTask(ctx, NewTask{ShopID: shop.ID, CategoryID: 1, TypeID: 2, BaseSKU: "B"})
	if err := eng.Pump(ctx); err != nil {
		t.Fatal(err)
	}
	view, _ := eng.GetTask(ctx, task.ID)
	if view.Status != domain.StatusWaitingInput {
		t.Fatalf("status = %s %s", view.Status, view.LastError)
	}
	products, _ := eng.ListProducts(ctx, domain.ProductFilter{ShopID: shop.ID})
	if len(products) != 0 {
		t.Fatal("missing freight became a product")
	}

	runner.replies = [][]byte{[]byte("not-json"), []byte("{")}
	bad, _ := eng.CreateTask(ctx, NewTask{ShopID: shop.ID, CategoryID: 1, TypeID: 2, BaseSKU: "BAD"})
	if err := eng.Pump(ctx); err != nil {
		t.Fatal(err)
	}
	view, _ = eng.GetTask(ctx, bad.ID)
	if view.Status != domain.StatusFailed || view.LastError == "" {
		t.Fatalf("bad json status=%s err=%s", view.Status, view.LastError)
	}
	var turns []domain.Turn
	_ = eng.DB.InTx(ctx, func(tx store.Tx) error {
		var err error
		turns, err = tx.ListTurns(ctx, bad.ID)
		return err
	})
	if len(turns) != 2 {
		t.Fatalf("retries = %d", len(turns))
	}
}

func TestDeliveryDraftsConflictAndRollback(t *testing.T) {
	fix := newEngine(t)
	eng, mem, mock, runner := fix.eng, fix.mem, fix.mock, fix.runner
	ctx := context.Background()
	shop := mustShop(t, eng)
	mustRule(t, eng, 3, 4, "提示词")
	mock.Attrs = []ozonapi.Attribute{{ID: 10096, Name: "颜色", DictionaryID: 9}}
	if _, err := eng.Attributes(ctx, shop.ID, 3, 4, "RU", ""); err != nil {
		t.Fatal(err)
	}
	_ = eng.DB.InTx(ctx, func(tx store.Tx) error {
		return tx.UpsertDict(ctx, []domain.DictValue{{
			ShopID: shop.ID, DescriptionCategoryID: 3, TypeID: 4, AttributeID: 10096, Language: "RU",
			ValueID: 55, ValueText: "серый", Source: "search", FetchedAt: time.Now(),
		}})
	})
	params := []byte(`{"price_expression":"cost * rate + fee","old_price_expression":"price * factor","variables":{"rate":"2","fee":"1","factor":"1"},"round_unit":"0.01"}`)
	rule, err := eng.SavePricingRule(ctx, "测试规则", true, params)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := eng.PatchSettings(ctx, SettingsPatch{ActivePricingRuleID: &rule.ID}); err != nil {
		t.Fatal(err)
	}
	runner.replies = [][]byte{completedSKUs([]string{"S1", "S2", "S3", "S4"}, 55)}
	task, err := eng.CreateTask(ctx, NewTask{ShopID: shop.ID, CategoryID: 3, TypeID: 4, BaseSKU: "BASE", OriginLink: "https://1688.example/item", CopyAutoApprove: boolPtr(true), VisualPlanAutoApprove: boolPtr(true), VisualResultAutoApprove: boolPtr(true)})
	if err != nil {
		t.Fatal(err)
	}
	if err := eng.Pump(ctx); err != nil {
		t.Fatal(err)
	}
	view, _ := eng.GetTask(ctx, task.ID)
	if view.Status != domain.StatusCompleted {
		t.Fatalf("status=%s err=%s", view.Status, view.LastError)
	}
	rows, _ := eng.ListProducts(ctx, domain.ProductFilter{ShopID: shop.ID, BaseSKU: "BASE"})
	if len(rows) != 4 {
		t.Fatalf("products = %d", len(rows))
	}
	for _, row := range rows {
		if row.PublishStatus != "draft" || row.GenerationTaskID == nil || *row.GenerationTaskID != task.ID {
			t.Fatalf("draft = %+v", row.PublishStatus)
		}
		if row.Price == nil || *row.Price != "45" {
			t.Fatalf("price = %v", row.Price)
		}
		var item map[string]any
		if err := json.Unmarshal(row.APIItemJSON, &item); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"cost", "origin_link", "base_sku", "generation_task_id", "items", "video"} {
			if _, ok := item[key]; ok {
				t.Fatalf("forbidden %s in %s", key, row.APIItemJSON)
			}
		}
	}
	filtered, _ := eng.ListProducts(ctx, domain.ProductFilter{ShopID: shop.ID, OfferID: "S1"})
	if len(filtered) != 1 || filtered[0].APIItemJSON == nil {
		t.Fatal("filter or api json missing")
	}

	runner.replies = [][]byte{completedSKUs([]string{"S2", "N2", "N3", "N4"}, 55)}
	conflict, _ := eng.CreateTask(ctx, NewTask{ShopID: shop.ID, CategoryID: 3, TypeID: 4, BaseSKU: "OTHER", CopyAutoApprove: boolPtr(true), VisualPlanAutoApprove: boolPtr(true), VisualResultAutoApprove: boolPtr(true)})
	if err := eng.Pump(ctx); err != nil {
		t.Fatal(err)
	}
	view, _ = eng.GetTask(ctx, conflict.ID)
	if view.Status == domain.StatusCompleted || !view.HoldsSlot {
		t.Fatalf("conflict status=%s slot=%v err=%s", view.Status, view.HoldsSlot, view.LastError)
	}
	all, _ := eng.ListProducts(ctx, domain.ProductFilter{ShopID: shop.ID})
	if len(all) != 4 {
		t.Fatalf("partial insert, products=%d", len(all))
	}

	runner.replies = [][]byte{completedSKUs([]string{"R1"}, 999999)}
	if _, err := eng.CreateTask(ctx, NewTask{ShopID: shop.ID, CategoryID: 3, TypeID: 4, BaseSKU: "DICT", CopyAutoApprove: boolPtr(true), VisualPlanAutoApprove: boolPtr(true), VisualResultAutoApprove: boolPtr(true)}); err != nil {
		t.Fatal(err)
	}
	if err := eng.Pump(ctx); err != nil {
		t.Fatal(err)
	}
	dictRows, _ := eng.ListProducts(ctx, domain.ProductFilter{ShopID: shop.ID, BaseSKU: "DICT"})
	if len(dictRows) != 1 || dictRows[0].ValidationStatus != "invalid" || !contains(string(dictRows[0].ValidationErrors), "ILLEGAL_DICTIONARY") {
		t.Fatalf("dict validation = %s %s", dictRows[0].ValidationStatus, dictRows[0].ValidationErrors)
	}

	runner.replies = [][]byte{completedSKUs([]string{"Z1"}, 55)}
	roll, _ := eng.CreateTask(ctx, NewTask{ShopID: shop.ID, CategoryID: 3, TypeID: 4, BaseSKU: "ROLL", CopyAutoApprove: boolPtr(true), VisualPlanAutoApprove: boolPtr(true), VisualResultAutoApprove: boolPtr(true)})
	_ = eng.DB.InTx(ctx, func(tx store.Tx) error {
		task, err := tx.GetTask(ctx, roll.ID)
		if err != nil {
			return err
		}
		task.Status = domain.StatusFinalizing
		task.LastReplyJSON = completedSKUs([]string{"Z1"}, 55)
		task.CopyAutoApprove, task.VisualPlanAutoApprove, task.VisualResultAutoApprove = true, true, true
		return tx.UpdateTask(ctx, task)
	})
	mem.FailNextCommit()
	if _, err := eng.finalize(ctx, roll.ID); err != nil {
		t.Fatal(err)
	}
	view, _ = eng.GetTask(ctx, roll.ID)
	if view.Status != domain.StatusFinalizing || !view.HoldsSlot {
		t.Fatalf("rollback status=%s slot=%v", view.Status, view.HoldsSlot)
	}
	rolled, _ := eng.ListProducts(ctx, domain.ProductFilter{ShopID: shop.ID, BaseSKU: "ROLL"})
	if len(rolled) != 0 {
		t.Fatalf("rolled back products = %d", len(rolled))
	}
}

func TestPublishSplitPartialAndRevision(t *testing.T) {
	fix := newEngine(t)
	eng, mock := fix.eng, fix.mock
	eng.AllowTestMedia = true
	ctx := context.Background()
	shop := mustShop(t, eng)
	var ids []int64
	for i := 0; i < 101; i++ {
		row := readyProduct(shop.ID, "SKU", i)
		_ = eng.DB.InTx(ctx, func(tx store.Tx) error {
			created, err := tx.CreateProduct(ctx, row)
			if err != nil {
				return err
			}
			ids = append(ids, created.ID)
			return nil
		})
	}
	batches, err := eng.Publish(ctx, shop.ID, ids)
	if err != nil {
		t.Fatal(err)
	}
	if len(batches) != 2 || len(mock.Imports) != 2 || len(mock.Imports[0]) != 100 || len(mock.Imports[1]) != 1 {
		t.Fatalf("batches=%d imports=%v", len(batches), lengths(mock.Imports))
	}
	one := readyProduct(shop.ID, "ONE", 1)
	two := readyProduct(shop.ID, "TWO", 2)
	var oneID, twoID int64
	_ = eng.DB.InTx(ctx, func(tx store.Tx) error {
		a, _ := tx.CreateProduct(ctx, one)
		b, _ := tx.CreateProduct(ctx, two)
		oneID, twoID = a.ID, b.ID
		return nil
	})
	part, err := eng.Publish(ctx, shop.ID, []int64{oneID, twoID})
	if err != nil {
		t.Fatal(err)
	}
	taskID := *part[0].OzonTaskID
	mock.Info = map[int64][]ozonapi.ImportRow{
		taskID: {
			{OfferID: "ONE-1", ProductID: 501, Status: "imported"},
			{OfferID: "TWO-2", Status: "failed", Errors: []byte(`[{"code":"price"}]`)},
		},
	}
	mock.Platforms = []ozonapi.PlatformRow{{OfferID: "ONE-1", Moderation: "moderating", Sale: "not_for_sale"}}
	if _, err := eng.RefreshBatch(ctx, part[0].ID); err != nil {
		t.Fatal(err)
	}
	okRow, _ := eng.GetProduct(ctx, oneID)
	badRow, _ := eng.GetProduct(ctx, twoID)
	if okRow.PublishStatus != "imported" || badRow.PublishStatus != "failed" {
		t.Fatalf("partial %s %s", okRow.PublishStatus, badRow.PublishStatus)
	}
	if OnSale(okRow) || okRow.OzonModerationStatus == nil || *okRow.OzonModerationStatus != "moderating" {
		t.Fatalf("platform = %v %v sale=%v", okRow.OzonModerationStatus, okRow.OzonSaleStatus, OnSale(okRow))
	}
	retry, err := eng.Publish(ctx, shop.ID, []int64{twoID})
	if err != nil {
		t.Fatal(err)
	}
	items, _ := eng.ListBatchItems(ctx, retry[0].ID)
	if len(items) != 1 || items[0].OfferID != "TWO-2" {
		t.Fatalf("retry items = %+v", items)
	}
	name := "新标题"
	updated, err := eng.UpdateProduct(ctx, oneID, ProductPatch{Name: &name})
	if err != nil {
		t.Fatal(err)
	}
	if updated.APIJSONRevision <= derefInt(updated.LastImportedRevision) {
		t.Fatalf("revision %d imported %v", updated.APIJSONRevision, updated.LastImportedRevision)
	}
	if updated.LastImportedRevision == nil || *updated.LastImportedRevision == updated.APIJSONRevision {
		t.Fatal("old import revision was overwritten")
	}
}

func TestRecoverDoesNotDoubleStartOrInsert(t *testing.T) {
	fix := newEngine(t)
	eng, mem := fix.eng, fix.mem
	ctx := context.Background()
	shop := mustShop(t, eng)
	mustRule(t, eng, 1, 2, "提示词")
	queued, _ := eng.CreateTask(ctx, NewTask{ShopID: shop.ID, CategoryID: 1, TypeID: 2, BaseSKU: "R"})
	_ = eng.DB.InTx(ctx, func(tx store.Tx) error {
		task, _ := tx.GetTask(ctx, queued.ID)
		now := time.Now()
		task.Status = domain.StatusDispatching
		task.AdmittedAt = &now
		return tx.UpdateTask(ctx, task)
	})
	if err := eng.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	view, _ := eng.GetTask(ctx, queued.ID)
	if view.Status != domain.StatusQueued || view.HoldsSlot {
		t.Fatalf("dispatching recover = %s", view.Status)
	}
	running, _ := eng.CreateTask(ctx, NewTask{ShopID: shop.ID, CategoryID: 1, TypeID: 2, BaseSKU: "RUN"})
	_ = eng.DB.InTx(ctx, func(tx store.Tx) error {
		task, _ := tx.GetTask(ctx, running.ID)
		task.Status = domain.StatusRunning
		if err := tx.UpdateTask(ctx, task); err != nil {
			return err
		}
		_, err := tx.CreateTurn(ctx, domain.Turn{TaskID: running.ID, Seq: 1, RunStatus: domain.TurnRunning, StartedAt: time.Now()})
		return err
	})
	if err := eng.Recover(ctx); err != nil {
		t.Fatal(err)
	}
	var turns []domain.Turn
	_ = eng.DB.InTx(ctx, func(tx store.Tx) error {
		var err error
		turns, err = tx.ListTurns(ctx, running.ID)
		return err
	})
	if len(turns) != 1 || turns[0].RunStatus != domain.TurnUncertain {
		t.Fatalf("turns = %+v", turns)
	}
	view, _ = eng.GetTask(ctx, running.ID)
	if !view.HoldsSlot {
		t.Fatal("uncertain turn released the slot")
	}
	if err := eng.Pump(ctx); err != nil {
		t.Fatal(err)
	}
	_ = eng.DB.InTx(ctx, func(tx store.Tx) error {
		again, _ := tx.ListTurns(ctx, running.ID)
		if len(again) != 1 {
			t.Fatalf("second turn started: %d", len(again))
		}
		return nil
	})
	_ = mem
}

func TestPricingRefusesMissingFormula(t *testing.T) {
	if _, err := pricing.Evaluate("22", []byte(`{}`)); err == nil {
		t.Fatal("empty rule produced a price")
	}
	quote, err := pricing.Evaluate("22", []byte(`{"price_expression":"cost * rate + fee","old_price_expression":"price * factor","variables":{"rate":"2","fee":"1","factor":"1"},"round_unit":"0.01"}`))
	if err != nil || quote.Price != "45" || quote.OldPrice != "45" {
		t.Fatalf("quote=%+v err=%v", quote, err)
	}
}

func TestMediaUploadUsesSameSession(t *testing.T) {
	fix := newEngine(t)
	eng, runner, up := fix.eng, fix.runner, fix.up
	ctx := context.Background()
	shop := mustShop(t, eng)
	mustRule(t, eng, 1, 2, "提示词")
	task, _ := eng.CreateTask(ctx, NewTask{ShopID: shop.ID, CategoryID: 1, TypeID: 2, BaseSKU: "M", CopyAutoApprove: boolPtr(true), VisualPlanAutoApprove: boolPtr(true), VisualResultAutoApprove: boolPtr(true)})
	writeFile(t, filepath.Join(eng.taskDir(task.ID), "output", "main.png"))
	runner.replies = [][]byte{mediaReady("output/main.png"), completedSKUs([]string{"M1"}, 0)}
	if err := eng.Pump(ctx); err != nil {
		t.Fatal(err)
	}
	calls := runner.snapshot()
	if len(calls) < 2 || calls[1].SessionID != calls[0].SessionID && calls[1].SessionID != "sess-1" {
		t.Fatalf("sessions = %+v", calls)
	}
	if !contains(calls[1].Prompt, "media_uploaded") {
		t.Fatalf("prompt = %s", calls[1].Prompt)
	}
	if len(up.Files()) != 1 {
		t.Fatalf("uploads = %d", len(up.Files()))
	}
}

type scriptRunner struct {
	mu       sync.Mutex
	calls    []codexcli.ExecRequest
	replies  [][]byte
	release  chan struct{}
	started  chan struct{}
	released bool
}

func (s *scriptRunner) Exec(ctx context.Context, req codexcli.ExecRequest) (codexcli.ExecResult, error) {
	s.mu.Lock()
	body := []byte("not-json")
	if len(s.replies) > 0 {
		body = s.replies[0]
		s.replies = s.replies[1:]
	}
	s.calls = append(s.calls, req)
	n := len(s.calls)
	release := s.release
	started := s.started
	s.mu.Unlock()
	if n == 1 && release != nil {
		started <- struct{}{}
		select {
		case <-release:
		case <-ctx.Done():
			return codexcli.ExecResult{}, ctx.Err()
		}
	}
	_ = os.WriteFile(req.OutputPath, body, 0o644)
	return codexcli.ExecResult{SessionID: "sess-1", ExitCode: 0, Output: body}, nil
}

func (s *scriptRunner) snapshot() []codexcli.ExecRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := append([]codexcli.ExecRequest(nil), s.calls...)
	return out
}

type fixture struct {
	eng    *Engine
	mem    *store.Mem
	mock   *ozonapi.Mock
	runner *scriptRunner
	up     *storage.Mock
}

func newEngine(t *testing.T) *fixture {
	t.Helper()
	mem := store.NewMem()
	mock := &ozonapi.Mock{Currency: "CNY"}
	runner := &scriptRunner{}
	up := storage.NewMock("https://cdn.example.com")
	eng := &Engine{
		DB: mem, Codex: runner, WorkRoot: t.TempDir(), AllowTestMedia: true,
		Ozon:     func(domain.Shop) ozonapi.Client { return mock },
		Uploader: func() storage.Store { return up },
		Now:      func() time.Time { return time.Date(2026, 11, 13, 8, 0, 0, 0, time.UTC) },
	}
	return &fixture{eng: eng, mem: mem, mock: mock, runner: runner, up: up}
}

func mustShop(t *testing.T, eng *Engine) domain.Shop {
	t.Helper()
	shop, err := eng.CreateShop(context.Background(), ShopInput{Name: "店", ClientID: "client", APIKey: "key-1234"})
	if err != nil {
		t.Fatal(err)
	}
	return shop
}

func mustRule(t *testing.T, eng *Engine, categoryID, typeID int64, prompt string) {
	t.Helper()
	if _, err := eng.SaveRule(context.Background(), categoryID, typeID, prompt, true); err != nil {
		t.Fatal(err)
	}
}

func envelope(status, stage string, questions []codexcli.Question, review *codexcli.Review, products []codexcli.Product, media []codexcli.MediaFile) []byte {
	if questions == nil {
		questions = []codexcli.Question{}
	}
	if products == nil {
		products = []codexcli.Product{}
	}
	if media == nil {
		media = []codexcli.MediaFile{}
	}
	raw, err := json.Marshal(codexcli.Reply{
		SchemaVersion: "1.2", Status: status, Stage: stage, Message: status,
		Questions: questions, Review: review,
		Data: codexcli.ReplyData{Products: products, VisualPlan: []codexcli.Visual{}, Media: media, Warnings: []string{}},
	})
	if err != nil {
		panic(err)
	}
	return raw
}

func needsInput() []byte {
	return envelope("needs_input", "extract", []codexcli.Question{{Field: "cost.domestic_shipping", Text: "请提供国内运费", Required: true}}, nil, nil, nil)
}

func pendingReview() []byte {
	return envelope("pending_review", "copy", nil, &codexcli.Review{Kind: "copy", Summary: "请审核"}, nil, nil)
}

func mediaReady(path string) []byte {
	return envelope("media_ready", "visual", nil, nil, nil, []codexcli.MediaFile{{
		AssetID: "a-main", SKURef: "M1", Role: "primary", FilePath: path, MimeType: "image/png",
	}})
}

func completedSKUs(offers []string, dictID int) []byte {
	products := make([]codexcli.Product, 0, len(offers))
	purchase, ship, total := 20.0, 2.0, 22.0
	evidence := "素材"
	for _, offer := range offers {
		offerCopy := offer
		name := "Название"
		var dict *int
		if dictID > 0 {
			value := dictID
			dict = &value
		}
		text := "серый"
		products = append(products, codexcli.Product{
			SKURef: offer, OfferIDSuggestion: &offerCopy, Name: &name,
			Cost:         codexcli.Cost{PurchasePrice: &purchase, DomesticShipping: &ship, Total: &total, Currency: "CNY", Evidence: &evidence},
			PrimaryImage: strPtr("https://cdn.example.com/placeholder.png"), Images: []string{},
			Dimensions:        codexcli.Dimensions{Depth: intPtr(10), Width: intPtr(10), Height: intPtr(10), DimensionUnit: "mm", Weight: intPtr(100), WeightUnit: "g"},
			Attributes:        []codexcli.Attr{{ID: 10096, ComplexID: 0, ValueText: &text, DictionaryValueID: dict}},
			ComplexAttributes: []codexcli.Complex{},
		})
	}
	return envelope("completed", "delivery", nil, nil, products, nil)
}

func readyProduct(shopID int64, prefix string, n int) domain.Product {
	offer := prefix + "-" + itoa(n)
	name := "标题"
	price := "10"
	image := "https://cdn.example.com/a.png"
	currency := "CNY"
	vat := "0.1"
	item, _ := json.Marshal(map[string]any{
		"offer_id": offer, "name": name, "price": price, "currency_code": currency, "vat": vat,
		"description_category_id": 3, "type_id": 4, "primary_image": image,
	})
	return domain.Product{
		ShopID: shopID, BaseSKU: prefix, OfferID: &offer, Name: &name, Price: &price, PrimaryImage: &image,
		DescriptionCategoryID: 3, TypeID: 4, CostCurrencyCode: "CNY", CurrencyCode: &currency, VAT: &vat,
		CostSourceJSON: []byte(`{}`), PricingSnapshot: []byte(`{}`), Images: []byte(`[]`), PDFList: []byte(`[]`),
		Promotions: []byte(`[]`), Attributes: []byte(`[]`), ComplexAttributes: []byte(`[]`),
		APIItemJSON: item, APIJSONRevision: 1, ValidationStatus: "valid", ValidationErrors: []byte(`[]`),
		TextReviewStatus: "approved", ImageReviewStatus: "approved", PublishStatus: "draft",
		DimensionUnit: "mm", WeightUnit: "g", OzonStatusRaw: []byte(`{}`), LastErrorJSON: []byte(`{}`),
		CreatedAt: time.Now(), UpdatedAt: time.Now(),
	}
}

func writeFile(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("png"), 0o644); err != nil {
		t.Fatal(err)
	}
}

func repeat(n int, body []byte) [][]byte {
	out := make([][]byte, n)
	for i := range out {
		out[i] = body
	}
	return out
}

func contains(s, part string) bool { return strings.Contains(s, part) }

func idx(s, part string) int {
	for i := 0; i+len(part) <= len(s); i++ {
		if s[i:i+len(part)] == part {
			return i
		}
	}
	return -1
}

func intPtr(v int) *int    { return &v }
func boolPtr(v bool) *bool { return &v }
func derefInt(v *int) int {
	if v == nil {
		return 0
	}
	return *v
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [16]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}

func lengths(items [][]json.RawMessage) []int {
	out := make([]int, len(items))
	for i := range items {
		out[i] = len(items[i])
	}
	return out
}
