package httpapi

import (
	"bytes"
	"context"
	"embed"
	"encoding/json"
	"io"
	"io/fs"
	"net/http"
	"strconv"
	"strings"

	"ozon-workbench/internal/app"
	"ozon-workbench/internal/domain"
)

//go:embed all:web
var webFS embed.FS

type Server struct {
	Engine *app.Engine
	mux    *http.ServeMux
}

func New(engine *app.Engine) *Server {
	s := &Server{Engine: engine, mux: http.NewServeMux()}
	s.routes()
	return s
}

func (s *Server) Handler() http.Handler { return s.mux }

func (s *Server) routes() {
	s.mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
	})
	s.mux.HandleFunc("GET /api/shops", s.listShops)
	s.mux.HandleFunc("POST /api/shops", s.createShop)
	s.mux.HandleFunc("PATCH /api/shops/{id}", s.patchShop)
	s.mux.HandleFunc("GET /api/categories/tree", s.getTree)
	s.mux.HandleFunc("POST /api/categories/tree/sync", s.syncTree)
	s.mux.HandleFunc("GET /api/categories/{category_id}/types/{type_id}/attributes", s.attributes)
	s.mux.HandleFunc("GET /api/categories/{category_id}/types/{type_id}/attributes/{attribute_id}/values", s.values)
	s.mux.HandleFunc("GET /api/category-rules/{category_id}/{type_id}", s.getRule)
	s.mux.HandleFunc("PUT /api/category-rules/{category_id}/{type_id}", s.putRule)
	s.mux.HandleFunc("GET /api/system/settings", s.getSettings)
	s.mux.HandleFunc("PATCH /api/system/settings", s.patchSettings)
	s.mux.HandleFunc("POST /api/pricing-rules", s.createPricing)
	s.mux.HandleFunc("POST /api/processing-tasks", s.createTask)
	s.mux.HandleFunc("GET /api/processing-tasks", s.listTasks)
	s.mux.HandleFunc("GET /api/processing-tasks/{id}", s.getTask)
	s.mux.HandleFunc("GET /api/processing-tasks/{id}/messages", s.messages)
	s.mux.HandleFunc("POST /api/processing-tasks/{id}/messages", s.addMessage)
	s.mux.HandleFunc("POST /api/processing-tasks/{id}/review", s.review)
	s.mux.HandleFunc("POST /api/processing-tasks/{id}/cancel", s.cancel)
	s.mux.HandleFunc("POST /api/processing-tasks/{id}/retry", s.retry)
	s.mux.HandleFunc("GET /api/products", s.listProducts)
	s.mux.HandleFunc("GET /api/products/{id}", s.getProduct)
	s.mux.HandleFunc("PATCH /api/products/{id}", s.patchProduct)
	s.mux.HandleFunc("POST /api/products/validate", s.validateProducts)
	s.mux.HandleFunc("POST /api/products/publish", s.publish)
	s.mux.HandleFunc("GET /api/import-batches", s.listBatches)
	s.mux.HandleFunc("GET /api/import-batches/{id}/items", s.batchItems)
	s.mux.HandleFunc("POST /api/import-batches/{id}/refresh", s.refreshBatch)
	sub, _ := fs.Sub(webFS, "web")
	s.mux.Handle("/", http.FileServer(http.FS(sub)))
}

func (s *Server) listShops(w http.ResponseWriter, r *http.Request) {
	shops, err := s.Engine.ListShops(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	for i := range shops {
		shops[i].APIKey = app.MaskKey(shops[i].APIKey)
	}
	writeJSON(w, http.StatusOK, shops)
}

func (s *Server) createShop(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name, ClientID, APIKey, CurrencyCode, VAT string
		Enable                                    *bool
	}
	if !decode(w, r, &body) {
		return
	}
	var currency, vat *string
	if body.CurrencyCode != "" {
		currency = &body.CurrencyCode
	}
	if body.VAT != "" {
		vat = &body.VAT
	}
	shop, err := s.Engine.CreateShop(r.Context(), app.ShopInput{
		Name: body.Name, ClientID: body.ClientID, APIKey: body.APIKey, CurrencyCode: currency, VAT: vat, Enable: body.Enable,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	shop.APIKey = app.MaskKey(shop.APIKey)
	writeJSON(w, http.StatusCreated, shop)
}

func (s *Server) patchShop(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name, ClientID, APIKey, CurrencyCode, VAT *string
		Enable                                    *bool
	}
	if !decode(w, r, &body) {
		return
	}
	in := app.ShopInput{Enable: body.Enable, CurrencyCode: body.CurrencyCode, VAT: body.VAT}
	if body.Name != nil {
		in.Name = *body.Name
	}
	if body.ClientID != nil {
		in.ClientID = *body.ClientID
	}
	if body.APIKey != nil {
		in.APIKey = *body.APIKey
	}
	shop, err := s.Engine.UpdateShop(r.Context(), pathID(r, "id"), in)
	if err != nil {
		writeErr(w, err)
		return
	}
	shop.APIKey = app.MaskKey(shop.APIKey)
	writeJSON(w, http.StatusOK, shop)
}

func (s *Server) getTree(w http.ResponseWriter, r *http.Request) {
	if q := r.URL.Query().Get("q"); q != "" || r.URL.Query().Has("q") {
		hits, err := s.Engine.SearchTree(r.Context(), r.URL.Query().Get("language"), q)
		if err != nil {
			writeErr(w, err)
			return
		}
		writeJSON(w, http.StatusOK, hits)
		return
	}
	view, err := s.Engine.GetTree(r.Context(), r.URL.Query().Get("language"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) syncTree(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ShopID   int64  `json:"shop_id"`
		Language string `json:"language"`
	}
	if !decode(w, r, &body) {
		return
	}
	view, err := s.Engine.SyncTree(r.Context(), body.ShopID, body.Language)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) attributes(w http.ResponseWriter, r *http.Request) {
	raw, err := s.Engine.Attributes(r.Context(), shopQuery(r), pathID(r, "category_id"), pathID(r, "type_id"), r.URL.Query().Get("language"), bearer(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	body := bytes.TrimSpace(raw)
	if len(body) == 0 || bytes.Equal(body, []byte("null")) {
		body = []byte("[]")
	}
	_, _ = w.Write(body)
}

func (s *Server) values(w http.ResponseWriter, r *http.Request) {
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	size, _ := strconv.Atoi(r.URL.Query().Get("page_size"))
	out, err := s.Engine.Dictionary(r.Context(), shopQuery(r), pathID(r, "category_id"), pathID(r, "type_id"), pathID(r, "attribute_id"), r.URL.Query().Get("language"), r.URL.Query().Get("keyword"), page, size, bearer(r))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) getRule(w http.ResponseWriter, r *http.Request) {
	rule, err := s.Engine.GetRule(r.Context(), pathID(r, "category_id"), pathID(r, "type_id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rule)
}

func (s *Server) putRule(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Prompt  string `json:"prompt"`
		Enabled *bool  `json:"enabled"`
	}
	if !decode(w, r, &body) {
		return
	}
	enabled := true
	if body.Enabled != nil {
		enabled = *body.Enabled
	}
	rule, err := s.Engine.SaveRule(r.Context(), pathID(r, "category_id"), pathID(r, "type_id"), body.Prompt, enabled)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rule)
}

func (s *Server) getSettings(w http.ResponseWriter, r *http.Request) {
	settings, err := s.Engine.GetSettings(r.Context())
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s *Server) patchSettings(w http.ResponseWriter, r *http.Request) {
	var body struct {
		MaxActiveTasks      *int                `json:"max_active_tasks"`
		PromptFallback      *string             `json:"prompt_fallback"`
		GenericPrompt       *string             `json:"generic_prompt"`
		ReviewDefaults      *app.ReviewDefaults `json:"review_defaults"`
		MaxInfraRetries     *int                `json:"max_infra_retries"`
		CodexBinary         *string             `json:"codex_binary"`
		ActivePricingRuleID *int64              `json:"active_pricing_rule_id"`
	}
	if !decode(w, r, &body) {
		return
	}
	settings, err := s.Engine.PatchSettings(r.Context(), app.SettingsPatch{
		MaxActiveTasks: body.MaxActiveTasks, PromptFallback: body.PromptFallback, GenericPrompt: body.GenericPrompt,
		ReviewDefaults: body.ReviewDefaults, MaxInfraRetries: body.MaxInfraRetries, CodexBinary: body.CodexBinary,
		ActivePricingRuleID: body.ActivePricingRuleID,
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s *Server) createPricing(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name       string          `json:"name"`
		Enabled    bool            `json:"enabled"`
		Parameters json.RawMessage `json:"parameters_json"`
	}
	if !decode(w, r, &body) {
		return
	}
	rule, err := s.Engine.SavePricingRule(r.Context(), body.Name, body.Enabled, body.Parameters)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, rule)
}

func (s *Server) createTask(w http.ResponseWriter, r *http.Request) {
	in, files, err := readTask(r)
	if err != nil {
		writeErr(w, err)
		return
	}
	in.IdemKey = r.Header.Get("Idempotency-Key")
	in.Files = files
	view, err := s.Engine.CreateTask(r.Context(), in)
	if err != nil {
		writeErr(w, err)
		return
	}
	go s.Engine.Pump(context.Background())
	writeJSON(w, http.StatusCreated, view)
}

func (s *Server) listTasks(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	rows, err := s.Engine.ListTasks(r.Context(), domain.TaskFilter{
		ShopID: int64Query(q.Get("shop_id")), Status: q.Get("status"), BaseSKU: q.Get("base_sku"),
		Limit: intQuery(q.Get("limit"), 50), Offset: intQuery(q.Get("offset"), 0),
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

func (s *Server) getTask(w http.ResponseWriter, r *http.Request) {
	view, err := s.Engine.GetTask(r.Context(), pathID(r, "id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view)
}

func (s *Server) messages(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Engine.Messages(r.Context(), pathID(r, "id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

func (s *Server) addMessage(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Content string `json:"content"`
	}
	if !decode(w, r, &body) {
		return
	}
	msg, err := s.Engine.AddMessage(r.Context(), pathID(r, "id"), body.Content, r.Header.Get("Idempotency-Key"))
	if err != nil {
		writeErr(w, err)
		return
	}
	go s.Engine.Pump(context.Background())
	writeJSON(w, http.StatusCreated, msg)
}

func (s *Server) review(w http.ResponseWriter, r *http.Request) {
	var body app.ReviewInput
	if !decode(w, r, &body) {
		return
	}
	if err := s.Engine.Review(r.Context(), pathID(r, "id"), body); err != nil {
		writeErr(w, err)
		return
	}
	go s.Engine.Pump(context.Background())
	writeJSON(w, http.StatusOK, map[string]string{"status": "queued"})
}

func (s *Server) cancel(w http.ResponseWriter, r *http.Request) {
	if err := s.Engine.CancelTask(r.Context(), pathID(r, "id")); err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "cancel_requested"})
}

func (s *Server) retry(w http.ResponseWriter, r *http.Request) {
	if err := s.Engine.RetryTask(r.Context(), pathID(r, "id")); err != nil {
		writeErr(w, err)
		return
	}
	go s.Engine.Pump(context.Background())
	writeJSON(w, http.StatusOK, map[string]string{"status": "queued"})
}

func (s *Server) listProducts(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	rows, err := s.Engine.ListProducts(r.Context(), domain.ProductFilter{
		ShopID: int64Query(q.Get("shop_id")), OfferID: q.Get("offer_id"), BaseSKU: q.Get("base_sku"),
		PublishStatus: q.Get("publish_status"), CategoryID: int64Query(q.Get("description_category_id")),
		TypeID: int64Query(q.Get("type_id")), ValidationStatus: q.Get("validation_status"),
		TextReviewStatus: q.Get("text_review_status"), Limit: intQuery(q.Get("limit"), 50), Offset: intQuery(q.Get("offset"), 0),
	})
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

func (s *Server) getProduct(w http.ResponseWriter, r *http.Request) {
	row, err := s.Engine.GetProduct(r.Context(), pathID(r, "id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, row)
}

func (s *Server) patchProduct(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Name         *string          `json:"name"`
		Cost         *string          `json:"cost"`
		Price        *string          `json:"price"`
		OldPrice     *string          `json:"old_price"`
		PrimaryImage *string          `json:"primary_image"`
		Images       []string         `json:"images"`
		APIItem      *json.RawMessage `json:"api_item_json"`
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 2<<20))
	if err != nil {
		writeErr(w, err)
		return
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"code": "BAD_JSON", "message": "请求不是 JSON"})
		return
	}
	patch := app.ProductPatch{Name: body.Name, Cost: body.Cost, Price: body.Price, OldPrice: body.OldPrice, PrimaryImage: body.PrimaryImage, APIItem: body.APIItem}
	if strings.Contains(string(raw), `"images"`) {
		patch.HasImages = true
		patch.Images = body.Images
	}
	row, err := s.Engine.UpdateProduct(r.Context(), pathID(r, "id"), patch)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, row)
}

func (s *Server) validateProducts(w http.ResponseWriter, r *http.Request) {
	var body struct {
		IDs []int64 `json:"ids"`
	}
	if !decode(w, r, &body) {
		return
	}
	rows, err := s.Engine.ValidateProducts(r.Context(), body.IDs)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

func (s *Server) publish(w http.ResponseWriter, r *http.Request) {
	var body struct {
		ShopID int64   `json:"shop_id"`
		IDs    []int64 `json:"ids"`
	}
	if !decode(w, r, &body) {
		return
	}
	rows, err := s.Engine.Publish(r.Context(), body.ShopID, body.IDs)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

func (s *Server) listBatches(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Engine.ListBatches(r.Context(), int64Query(r.URL.Query().Get("shop_id")))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

func (s *Server) batchItems(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Engine.ListBatchItems(r.Context(), pathID(r, "id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, rows)
}

func (s *Server) refreshBatch(w http.ResponseWriter, r *http.Request) {
	row, err := s.Engine.RefreshBatch(r.Context(), pathID(r, "id"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, row)
}

func readTask(r *http.Request) (app.NewTask, []app.Upload, error) {
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		if err := r.ParseMultipartForm(64 << 20); err != nil {
			return app.NewTask{}, nil, err
		}
		in := app.NewTask{
			ShopID: int64Query(r.FormValue("shop_id")), CategoryID: int64Query(r.FormValue("description_category_id")),
			TypeID: int64Query(r.FormValue("type_id")), BaseSKU: r.FormValue("base_sku"), OriginLink: r.FormValue("origin_link"),
		}
		in.CopyAutoApprove = boolForm(r, "copy_auto_approve")
		in.VisualPlanAutoApprove = boolForm(r, "visual_plan_auto_approve")
		in.VisualResultAutoApprove = boolForm(r, "visual_result_auto_approve")
		var files []app.Upload
		if r.MultipartForm != nil {
			for _, headers := range r.MultipartForm.File {
				for _, header := range headers {
					file, err := header.Open()
					if err != nil {
						return in, nil, err
					}
					data, err := io.ReadAll(io.LimitReader(file, 32<<20))
					file.Close()
					if err != nil {
						return in, nil, err
					}
					files = append(files, app.Upload{Name: header.Filename, Data: data})
				}
			}
		}
		return in, files, nil
	}
	var body struct {
		ShopID                  int64  `json:"shop_id"`
		CategoryID              int64  `json:"description_category_id"`
		TypeID                  int64  `json:"type_id"`
		BaseSKU                 string `json:"base_sku"`
		OriginLink              string `json:"origin_link"`
		CopyAutoApprove         *bool  `json:"copy_auto_approve"`
		VisualPlanAutoApprove   *bool  `json:"visual_plan_auto_approve"`
		VisualResultAutoApprove *bool  `json:"visual_result_auto_approve"`
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	if err != nil {
		return app.NewTask{}, nil, err
	}
	if err := json.Unmarshal(raw, &body); err != nil {
		return app.NewTask{}, nil, err
	}
	return app.NewTask{
		ShopID: body.ShopID, CategoryID: body.CategoryID, TypeID: body.TypeID, BaseSKU: body.BaseSKU, OriginLink: body.OriginLink,
		CopyAutoApprove: body.CopyAutoApprove, VisualPlanAutoApprove: body.VisualPlanAutoApprove, VisualResultAutoApprove: body.VisualResultAutoApprove,
	}, nil, nil
}

func boolForm(r *http.Request, key string) *bool {
	if r.FormValue(key) == "" {
		return nil
	}
	v := r.FormValue(key) == "true" || r.FormValue(key) == "1"
	return &v
}

func decode(w http.ResponseWriter, r *http.Request, dest any) bool {
	dec := json.NewDecoder(io.LimitReader(r.Body, 2<<20))
	if err := dec.Decode(dest); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"code": "BAD_JSON", "message": "请求不是 JSON"})
		return false
	}
	return true
}

func writeErr(w http.ResponseWriter, err error) {
	if e, ok := err.(*app.Error); ok {
		writeJSON(w, e.Status, map[string]any{"code": e.Code, "message": e.Message, "details": e.Details})
		return
	}
	writeJSON(w, http.StatusInternalServerError, map[string]any{"code": "INTERNAL", "message": err.Error()})
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(normalizeEmptyLists(body))
}

func pathID(r *http.Request, name string) int64 {
	n, _ := strconv.ParseInt(r.PathValue(name), 10, 64)
	return n
}

func int64Query(v string) int64 {
	n, _ := strconv.ParseInt(v, 10, 64)
	return n
}

func intQuery(v string, fallback int) int {
	if v == "" {
		return fallback
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return fallback
	}
	return n
}

func shopQuery(r *http.Request) int64 {
	if v := r.URL.Query().Get("shop_id"); v != "" {
		return int64Query(v)
	}
	return int64Query(r.Header.Get("X-Shop-Id"))
}

func bearer(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if strings.HasPrefix(strings.ToLower(h), "bearer ") {
		return strings.TrimSpace(h[7:])
	}
	return ""
}
