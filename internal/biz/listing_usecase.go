package biz

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"sync"
	"unicode"

	"github.com/go-kratos/kratos/v2/log"

	"ozon-workbench/internal/storage"
)

// ListingUsecase turns a completed material task into versioned import objects and publishes them.
type ListingUsecase struct {
	repo    ListingRepo
	shops   ShopAccess
	runner  CodexRunner
	store   storage.Store
	gateway ListingGateway
	sched   Scheduler
	log     *log.Helper
	mu      sync.Mutex
	busy    map[string]*sync.Mutex
}

func NewListingUsecase(repo ListingRepo, shops ShopAccess, runner CodexRunner, store storage.Store, gateway ListingGateway, sched Scheduler, logger log.Logger) *ListingUsecase {
	if store == nil {
		store = storage.None{}
	}
	return &ListingUsecase{
		repo: repo, shops: shops, runner: runner, store: store, gateway: gateway, sched: sched,
		log: log.NewHelper(logger), busy: map[string]*sync.Mutex{},
	}
}

func (uc *ListingUsecase) Store() storage.Store { return uc.store }

// OnTaskCompleted creates one listing row per sellable SKU. Existing offer ids are left in place.
func (uc *ListingUsecase) OnTaskCompleted(ctx context.Context, task *Task) {
	if uc == nil || task == nil || task.Status != StatusCompleted {
		return
	}
	created, err := uc.materialize(ctx, task)
	if err != nil {
		uc.log.Errorf("task %d listings: %v", task.ID, err)
		return
	}
	if !uc.store.Configured() {
		return
	}
	for _, sku := range created {
		sku := sku
		uc.schedule(func() { uc.UploadMedia(context.Background(), sku.ShopID, sku.ID) })
	}
}

// Recover recreates missing rows for completed tasks and resumes import polling.
func (uc *ListingUsecase) Recover(ctx context.Context) error {
	if uc.repo == nil {
		return nil
	}
	tasks, err := uc.repo.ListCompletedTasks(ctx)
	if err != nil {
		return err
	}
	for _, task := range tasks {
		uc.OnTaskCompleted(ctx, task)
	}
	open, err := uc.repo.ListInflight(ctx)
	if err != nil {
		return err
	}
	for _, batch := range open {
		batch := batch
		uc.schedule(func() { uc.dispatch(context.Background(), batch.ShopID, batch.ID) })
	}
	return nil
}

func (uc *ListingUsecase) materialize(ctx context.Context, task *Task) ([]*ListingSKU, error) {
	shop, err := uc.shops.Get(ctx, task.ShopID)
	if err != nil {
		return nil, err
	}
	currency := ""
	if shop.CurrencyCode != nil {
		currency = strings.ToUpper(strings.TrimSpace(*shop.CurrencyCode))
	}
	product, err := uc.repo.FindProduct(ctx, task.ShopID, task.ID, task.MasterSKU)
	if err != nil {
		return nil, err
	}
	if product == nil {
		product, err = uc.repo.SaveProduct(ctx, &ListingProduct{
			ShopID: task.ShopID, TaskID: task.ID, MasterSKU: task.MasterSKU, SupplierURL: task.SupplierURL,
			DescriptionCategoryID: task.DescriptionCategoryID, TypeID: task.TypeID, TypeName: task.TypeName,
			TypePath: append([]string(nil), task.TypePath...), Language: task.Language,
			Attributes: rawOr(task.Attributes, []byte("[]")), CodexSessionID: task.CodexSessionID,
		})
		if err != nil {
			return nil, err
		}
	}
	draft := TextDraft{}
	_ = json.Unmarshal(task.TextDraft, &draft)
	plan := VisualPlan{}
	_ = json.Unmarshal(task.VisualPlan, &plan)
	visual := map[string]VisualItem{}
	for _, item := range plan.Items {
		visual[item.OfferID] = item
	}
	offers := task.SubSKUs
	if len(offers) == 0 && len(draft.SubSKUs) > 0 {
		offers = draft.SubSKUs
	}
	if len(offers) == 0 {
		offers = []SubSKU{{OfferID: task.MasterSKU, Specs: ""}}
	}
	var created []*ListingSKU
	seen := map[string]struct{}{}
	for _, offer := range offers {
		offer.OfferID = strings.TrimSpace(offer.OfferID)
		if offer.OfferID == "" {
			continue
		}
		if _, ok := seen[offer.OfferID]; ok {
			continue
		}
		seen[offer.OfferID] = struct{}{}
		existing, err := uc.repo.FindOffer(ctx, task.ShopID, offer.OfferID)
		if err != nil {
			return created, err
		}
		if existing != nil {
			continue
		}
		image := ""
		if vis, ok := visual[offer.OfferID]; ok {
			image = strings.TrimSpace(vis.PrimaryImage)
		}
		title := strings.TrimSpace(draft.TitleRU)
		if title == "" {
			title = "Товар " + offer.OfferID
		}
		item := draftItem(task, offer.OfferID, title, currency, image)
		issues := ValidateItem(item, expectOf(task, offer.OfferID, currency))
		rev := &ListingRevision{
			ShopID: task.ShopID, Version: 1, Source: "draft",
			CodexItem: item, ItemJSON: item, Issues: issues, Valid: len(issues) == 0,
		}
		media := []ListingMedia{}
		preview := ""
		if image != "" && !IsPublicHTTPS(image) {
			full := image
			if task.ZipLocalPath != "" && !filepath.IsAbs(image) {
				full = filepath.Join(task.ZipLocalPath, filepath.FromSlash(image))
			}
			media = append(media, ListingMedia{
				ShopID: task.ShopID, Role: "primary", LocalPath: full, Status: "local",
			})
			preview = "/media/tasks/" + fmt.Sprintf("%d", task.ID) + "/" + strings.TrimLeft(filepath.ToSlash(image), "/")
		}
		sku := &ListingSKU{
			ShopID: task.ShopID, ProductID: product.ID, TaskID: task.ID, OfferID: offer.OfferID,
			MasterSKU: task.MasterSKU, SupplierVariantRef: offer.OfferID, VariantSpecs: offer.Specs,
			CategoryLabel: strings.Join(task.TypePath, " / "), CurrentVersion: 1,
			PublishStatus: StatusAfterEdit(issues), PlatformStatus: PlatformUnread,
			CurrencyCode: currency, Price: "", TitleRU: title, PrimaryImage: image, PreviewPath: preview,
			LastError: strings.Join(issues, "；"),
		}
		saved, err := uc.repo.CreateSKU(ctx, sku, rev, media)
		if err != nil {
			return created, err
		}
		created = append(created, saved)
	}
	return created, nil
}

func draftItem(task *Task, offer, title, currency, image string) json.RawMessage {
	item := map[string]any{
		"offer_id":                offer,
		"description_category_id": task.DescriptionCategoryID,
		"type_id":                 task.TypeID,
		"name":                    title,
		"price":                   "",
		"currency_code":           currency,
		"primary_image":           image,
		"images":                  []string{},
		"depth":                   0,
		"width":                   0,
		"height":                  0,
		"dimension_unit":          "mm",
		"weight":                  0,
		"weight_unit":             "g",
		"attributes":              []any{},
	}
	raw, _ := json.Marshal(item)
	return raw
}

func expectOf(task *Task, offer, currency string) ItemExpect {
	return ItemExpect{
		OfferID: offer, DescriptionCategoryID: task.DescriptionCategoryID, TypeID: task.TypeID,
		ShopCurrency: currency, Contract: decodeAttrMaps(task.Attributes),
	}
}

func (uc *ListingUsecase) expectSKU(ctx context.Context, sku *ListingSKU) (ItemExpect, error) {
	shop, err := uc.shops.Get(ctx, sku.ShopID)
	if err != nil {
		return ItemExpect{}, err
	}
	currency := ""
	if shop.CurrencyCode != nil {
		currency = strings.TrimSpace(*shop.CurrencyCode)
	}
	product, err := uc.repo.GetProduct(ctx, sku.ShopID, sku.ProductID)
	if err != nil {
		return ItemExpect{}, err
	}
	attrs := sku.Attributes
	category := sku.DescriptionCategory
	typeID := sku.TypeID
	if product != nil {
		attrs = product.Attributes
		category = product.DescriptionCategoryID
		typeID = product.TypeID
	}
	return ItemExpect{
		OfferID: sku.OfferID, DescriptionCategoryID: category, TypeID: typeID,
		ShopCurrency: currency, Contract: decodeAttrMaps(attrs),
	}, nil
}

// List returns one page of the current shop.
func (uc *ListingUsecase) List(ctx context.Context, f ListingFilter) (*ListingPage, error) {
	if _, err := uc.shops.Get(ctx, f.ShopID); err != nil {
		return nil, err
	}
	f.Page, f.PageSize = pageBounds(f.Page, f.PageSize)
	f.Mode = strings.TrimSpace(f.Mode)
	if f.Mode == "" {
		f.Mode = "offer"
	}
	if f.Mode == "multi" {
		f.Offers = splitOffers(f.Query)
	}
	rows, total, err := uc.repo.ListSKUs(ctx, f)
	if err != nil {
		return nil, err
	}
	out := make([]ListingRow, 0, len(rows))
	for _, row := range rows {
		out = append(out, row.Row())
	}
	return &ListingPage{Rows: out, Total: total, Page: f.Page, PageSize: f.PageSize}, nil
}

// Detail loads one SKU, its versions, media and the attribute contract.
func (uc *ListingUsecase) Detail(ctx context.Context, shopID, id int) (*ListingDetail, error) {
	sku, err := uc.repo.GetSKU(ctx, shopID, id)
	if err != nil {
		return nil, err
	}
	revs, err := uc.repo.ListRevisions(ctx, shopID, id)
	if err != nil {
		return nil, err
	}
	media, err := uc.repo.ListMedia(ctx, id)
	if err != nil {
		return nil, err
	}
	stocks, err := uc.repo.ListStocks(ctx, shopID, id)
	if err != nil {
		return nil, err
	}
	current := revisionOrEmpty(revs, sku.CurrentVersion)
	expect, err := uc.expectSKU(ctx, sku)
	if err != nil {
		return nil, err
	}
	issues := current.Issues
	if len(current.ItemJSON) > 0 {
		issues = ValidateItem(current.ItemJSON, expect)
	}
	if issues == nil {
		issues = []string{}
	}
	return &ListingDetail{
		SKU:       sku.Row(),
		ItemJSON:  rawOr(current.ItemJSON, []byte("{}")),
		CodexJSON: rawOr(current.CodexItem, []byte("{}")),
		Version:   sku.CurrentVersion,
		Source:    current.Source,
		Issues:    issues,
		Contract:  rawOr(expectContract(expect), []byte("[]")),
		Revisions: revisionViews(revs),
		Media:     mediaViews(media, sku.TaskID),
		Stocks:    stockViews(stocks),
		Storage:   uc.storageView(),
	}, nil
}

// SaveVersion stores an edited object as the next version and revalidates it.
func (uc *ListingUsecase) SaveVersion(ctx context.Context, shopID, id, expected int, item json.RawMessage) (*ListingDetail, error) {
	sku, err := uc.repo.GetSKU(ctx, shopID, id)
	if err != nil {
		return nil, err
	}
	if sku.PublishStatus == PublishSubmitting {
		return nil, bad("SUBMITTING", "这个 SKU 正在提交，请等待结果后再修改。", nil)
	}
	if expected > 0 && expected != sku.CurrentVersion {
		return nil, versionConflict()
	}
	expect, err := uc.expectSKU(ctx, sku)
	if err != nil {
		return nil, err
	}
	item = bytesTrimJSON(item)
	issues := ValidateItem(item, expect)
	prev, err := uc.repo.GetRevision(ctx, shopID, id, sku.CurrentVersion)
	if err != nil {
		return nil, err
	}
	codex := item
	if prev != nil && len(prev.CodexItem) > 0 {
		codex = prev.CodexItem
	}
	sku.CurrentVersion++
	applyProjection(sku, item, issues)
	if _, err := uc.repo.AddRevision(ctx, sku, &ListingRevision{
		SKUID: id, ShopID: shopID, Version: sku.CurrentVersion, Source: "manual",
		CodexItem: codex, ItemJSON: item, Issues: issues, Valid: len(issues) == 0,
	}); err != nil {
		return nil, err
	}
	return uc.Detail(ctx, shopID, id)
}

// Generate asks Codex for a full import object and stores it as a new version.
func (uc *ListingUsecase) Generate(ctx context.Context, shopID, id int) (*ListingDetail, error) {
	sku, err := uc.repo.GetSKU(ctx, shopID, id)
	if err != nil {
		return nil, err
	}
	if sku.PublishStatus == PublishSubmitting {
		return nil, bad("SUBMITTING", "这个 SKU 正在提交，请等待结果后再生成。", nil)
	}
	if uc.runner == nil {
		return nil, bad("NO_CODEX", "Codex 运行器未配置。", nil)
	}
	rev, err := uc.repo.GetRevision(ctx, shopID, id, sku.CurrentVersion)
	if err != nil {
		return nil, err
	}
	current := json.RawMessage(`{}`)
	if rev != nil && len(rev.ItemJSON) > 0 {
		current = rev.ItemJSON
	}
	var obj map[string]any
	_ = json.Unmarshal(current, &obj)
	expect, err := uc.expectSKU(ctx, sku)
	if err != nil {
		return nil, err
	}
	facts := &ListingFacts{
		OfferID: sku.OfferID, Specs: sku.VariantSpecs, Title: firstText(asString(obj["name"]), sku.TitleRU),
		Currency: firstText(asString(obj["currency_code"]), expect.ShopCurrency), Price: asString(obj["price"]),
		PrimaryImage: asString(obj["primary_image"]), DescriptionCategoryID: expect.DescriptionCategoryID,
		TypeID: expect.TypeID, Attributes: expect.Contract,
	}
	if imgs, ok := obj["images"].([]any); ok {
		for _, img := range imgs {
			if s := strings.TrimSpace(asString(img)); s != "" {
				facts.Images = append(facts.Images, s)
			}
		}
	}
	rawFacts, _ := json.MarshalIndent(facts, "", "  ")
	prompt := "请为这个可售 SKU 生成一个完整的 POST /v3/product/import items[] 对象，放在 ozon-item 代码块里。\n" +
		"字段名和类型必须符合 Ozon 契约。不要输出外层 items。不要改 offer_id、description_category_id、type_id。\n" +
		"主图必须使用下面已经确认的 HTTPS 地址；如果还不是 HTTPS，原样保留并让校验失败。\n\n" + string(rawFacts)
	result, err := uc.runner.Run(ctx, RunRequest{
		Stage: StageListing, SessionID: sku.SessionID, Prompt: prompt, MasterSKU: sku.MasterSKU,
		Listing: facts, Attributes: expect.Contract,
	})
	if err != nil {
		return nil, bad("CODEX_FAILED", "生成商品参数失败："+err.Error(), nil)
	}
	item, err := ParseItemObject(result.AssistantText())
	if err != nil {
		return nil, bad("CODEX_OUTPUT", err.Error(), nil)
	}
	item = forceIdentity(item, sku.OfferID, expect.DescriptionCategoryID, expect.TypeID)
	issues := ValidateItem(item, expect)
	sku.CurrentVersion++
	sku.CodexAttempted = true
	applyProjection(sku, item, issues)
	if _, err := uc.repo.AddRevision(ctx, sku, &ListingRevision{
		SKUID: id, ShopID: shopID, Version: sku.CurrentVersion, Source: "codex",
		CodexItem: item, ItemJSON: append(json.RawMessage(nil), item...), Issues: issues, Valid: len(issues) == 0,
	}); err != nil {
		return nil, err
	}
	return uc.Detail(ctx, shopID, id)
}

// UploadMedia sends local files to the configured store and writes the public URLs into a new version.
func (uc *ListingUsecase) UploadMedia(ctx context.Context, shopID, id int) (*ListingDetail, error) {
	if !uc.store.Configured() {
		return nil, storageBlocked(uc.store)
	}
	sku, err := uc.repo.GetSKU(ctx, shopID, id)
	if err != nil {
		return nil, err
	}
	if sku.PublishStatus == PublishSubmitting {
		return nil, bad("SUBMITTING", "这个 SKU 正在提交，请等待结果后再上传媒体。", nil)
	}
	media, err := uc.repo.ListMedia(ctx, id)
	if err != nil {
		return nil, err
	}
	if len(media) == 0 {
		return nil, bad("NO_MEDIA", "没有待上传的本地媒体。可以直接把公网 HTTPS 地址写进商品参数。", nil)
	}
	rev, err := uc.repo.GetRevision(ctx, shopID, id, sku.CurrentVersion)
	if err != nil {
		return nil, err
	}
	item := json.RawMessage(`{}`)
	codex := item
	if rev != nil {
		item = append(json.RawMessage(nil), rev.ItemJSON...)
		codex = rev.CodexItem
	}
	var obj map[string]any
	if err := json.Unmarshal(item, &obj); err != nil || obj == nil {
		obj = map[string]any{}
	}
	var failed []string
	for i := range media {
		row := media[i]
		if IsPublicHTTPS(row.PublicURL) && row.Status == "uploaded" {
			assignMedia(obj, row.Role, row.PublicURL)
			continue
		}
		if strings.TrimSpace(row.LocalPath) == "" {
			continue
		}
		key := fmt.Sprintf("shop/%d/%s/%s", shopID, sku.OfferID, filepath.Base(row.LocalPath))
		objUploaded, upErr := uc.store.Upload(ctx, row.LocalPath, key)
		row.Attempts++
		row.StorageKey = key
		if upErr != nil {
			row.Status = "failed"
			row.Error = upErr.Error()
			failed = append(failed, row.Role+"："+upErr.Error())
		} else {
			row.Status = "uploaded"
			row.PublicURL = objUploaded.PublicURL
			row.PublicKey = objUploaded.PublicKey
			row.Error = ""
			assignMedia(obj, row.Role, objUploaded.PublicURL)
		}
		if _, err := uc.repo.SaveMedia(ctx, &row); err != nil {
			return nil, err
		}
	}
	raw, _ := json.Marshal(obj)
	expect, err := uc.expectSKU(ctx, sku)
	if err != nil {
		return nil, err
	}
	issues := ValidateItem(raw, expect)
	if len(failed) > 0 {
		issues = append(issues, failed...)
	}
	sku.CurrentVersion++
	applyProjection(sku, raw, issues)
	if IsPublicHTTPS(asString(obj["primary_image"])) {
		sku.PreviewPath = ""
	}
	if _, err := uc.repo.AddRevision(ctx, sku, &ListingRevision{
		SKUID: id, ShopID: shopID, Version: sku.CurrentVersion, Source: "media",
		CodexItem: rawOr(codex, raw), ItemJSON: raw, Issues: issues, Valid: len(issues) == 0 && len(failed) == 0,
	}); err != nil {
		return nil, err
	}
	if len(failed) > 0 {
		detail, derr := uc.Detail(ctx, shopID, id)
		if derr != nil {
			return nil, derr
		}
		return detail, &Error{Status: 502, Reason: "UPLOAD_FAILED", Message: "有媒体上传失败，可重试。 " + strings.Join(failed, "；")}
	}
	return uc.Detail(ctx, shopID, id)
}

func (uc *ListingUsecase) storageView() StorageView {
	name := "none"
	msg := storage.ErrUnconfigured.Error()
	ok := false
	if uc.store != nil {
		name = uc.store.Name()
		msg = uc.store.Message()
		ok = uc.store.Configured()
	}
	return StorageView{Provider: name, Configured: ok, Message: msg}
}

// TestUpload sends one local file and deletes the remote object afterwards.
func (uc *ListingUsecase) TestUpload(ctx context.Context, localPath string) (storage.Object, error) {
	if !uc.store.Configured() {
		return storage.Object{}, storageBlocked(uc.store)
	}
	key := "healthcheck/" + filepath.Base(localPath)
	obj, err := uc.store.Upload(ctx, localPath, key)
	if err != nil {
		return storage.Object{}, &Error{Status: 502, Reason: "UPLOAD_FAILED", Message: "测试上传失败：" + err.Error()}
	}
	if err := uc.store.Delete(ctx, obj.Key); err != nil {
		uc.log.Errorf("delete healthcheck: %v", err)
	}
	return obj, nil
}

func storageBlocked(store storage.Store) *Error {
	msg := storage.ErrUnconfigured.Error()
	if store != nil && strings.TrimSpace(store.Message()) != "" {
		msg = store.Message()
	}
	return &Error{Status: 400, Reason: "STORAGE_UNCONFIGURED", Message: msg}
}

func versionConflict() *Error {
	return &Error{Status: 409, Reason: "VERSION_CONFLICT", Message: "版本已变化，请刷新后再试。"}
}

func applyProjection(sku *ListingSKU, item json.RawMessage, issues []string) {
	var obj map[string]any
	_ = json.Unmarshal(item, &obj)
	if name := strings.TrimSpace(asString(obj["name"])); name != "" {
		sku.TitleRU = name
	}
	sku.Price = asString(obj["price"])
	if currency := strings.ToUpper(strings.TrimSpace(asString(obj["currency_code"]))); currency != "" {
		sku.CurrencyCode = currency
	}
	if image := strings.TrimSpace(asString(obj["primary_image"])); image != "" {
		sku.PrimaryImage = image
	}
	sku.PublishStatus = StatusAfterEdit(issues)
	sku.LastError = strings.Join(issues, "；")
}

func assignMedia(obj map[string]any, role, publicURL string) {
	switch role {
	case "primary", "":
		obj["primary_image"] = publicURL
	case "video":
		obj["videos"] = appendStringList(obj["videos"], publicURL)
	default:
		obj["images"] = appendStringList(obj["images"], publicURL)
	}
}

func appendStringList(raw any, value string) []string {
	var out []string
	if list, ok := raw.([]any); ok {
		for _, item := range list {
			if s := strings.TrimSpace(asString(item)); s != "" && s != value {
				out = append(out, s)
			}
		}
	}
	return append(out, value)
}

func forceIdentity(item json.RawMessage, offer string, category, typeID int64) json.RawMessage {
	var obj map[string]any
	dec := json.NewDecoder(strings.NewReader(string(item)))
	dec.UseNumber()
	if err := dec.Decode(&obj); err != nil || obj == nil {
		return item
	}
	obj["offer_id"] = offer
	obj["description_category_id"] = category
	obj["type_id"] = typeID
	raw, err := json.Marshal(obj)
	if err != nil {
		return item
	}
	return raw
}

func expectContract(expect ItemExpect) json.RawMessage {
	raw, err := json.Marshal(expect.Contract)
	if err != nil || len(raw) == 0 {
		return []byte("[]")
	}
	return raw
}

func bytesTrimJSON(raw json.RawMessage) json.RawMessage {
	text := strings.TrimSpace(string(raw))
	if text == "" {
		return json.RawMessage(`{}`)
	}
	return json.RawMessage(text)
}

func rawOr(raw json.RawMessage, fallback []byte) json.RawMessage {
	if len(strings.TrimSpace(string(raw))) == 0 {
		return json.RawMessage(append([]byte(nil), fallback...))
	}
	return raw
}

func splitOffers(raw string) []string {
	parts := strings.FieldsFunc(raw, func(r rune) bool {
		return unicode.IsSpace(r) || r == ',' || r == '，' || r == ';' || r == '；'
	})
	seen := map[string]struct{}{}
	var out []string
	for _, part := range parts {
		if part == "" {
			continue
		}
		if _, ok := seen[part]; ok {
			continue
		}
		seen[part] = struct{}{}
		out = append(out, part)
	}
	return out
}

func (uc *ListingUsecase) schedule(fn func()) {
	if uc.sched == nil {
		go fn()
		return
	}
	uc.sched.Go(fn)
}

func (uc *ListingUsecase) lock(key string) func() {
	uc.mu.Lock()
	m := uc.busy[key]
	if m == nil {
		m = &sync.Mutex{}
		uc.busy[key] = m
	}
	uc.mu.Unlock()
	m.Lock()
	return m.Unlock
}

func revisionOrEmpty(revs []ListingRevision, version int) ListingRevision {
	for _, rev := range revs {
		if rev.Version == version {
			return rev
		}
	}
	if len(revs) > 0 {
		return revs[len(revs)-1]
	}
	return ListingRevision{}
}
