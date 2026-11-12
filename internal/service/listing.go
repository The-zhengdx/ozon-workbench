package service

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"github.com/go-kratos/kratos/v2/log"
	khttp "github.com/go-kratos/kratos/v2/transport/http"

	"ozon-workbench/internal/biz"
)

// ListingService is the HTTP adapter for SKU listing, publish batches and media storage.
type ListingService struct {
	uc  *biz.ListingUsecase
	log *log.Helper
}

func NewListingService(uc *biz.ListingUsecase, logger log.Logger) *ListingService {
	return &ListingService{uc: uc, log: log.NewHelper(logger)}
}

// Store exposes the media provider for the process metadata endpoint.
func (s *ListingService) Store() interface {
	Name() string
	Configured() bool
} {
	if s == nil || s.uc == nil {
		return nil
	}
	return s.uc.Store()
}

func (s *ListingService) Register(r *khttp.Router) {
	r.GET("/storage", s.storage)
	r.POST("/storage/test", s.storageTest)
	r.GET("/listings", s.list)
	r.POST("/listings/publish", s.publish)
	r.GET("/listings/batches/{id}", s.batch)
	r.GET("/listings/{id}", s.detail)
	r.POST("/listings/{id}/versions", s.save)
	r.POST("/listings/{id}/generate", s.generate)
	r.POST("/listings/{id}/media", s.media)
	r.POST("/listings/{id}/stocks", s.stock)
	r.POST("/listings/{id}/platform", s.platform)
}

func (s *ListingService) storage(ctx khttp.Context) error {
	view := s.uc.Store()
	body := map[string]any{"provider": "none", "configured": false, "message": ""}
	if view != nil {
		body["provider"] = view.Name()
		body["configured"] = view.Configured()
		body["message"] = view.Message()
	}
	return ctx.JSON(http.StatusOK, body)
}

func (s *ListingService) storageTest(ctx khttp.Context) error {
	file, err := os.CreateTemp("", "ozon-storage-*.txt")
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	path := file.Name()
	_, _ = file.WriteString("ozon-workbench storage check\n")
	file.Close()
	defer os.Remove(path)
	obj, err := s.uc.TestUpload(ctx, path)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	return ctx.JSON(http.StatusOK, map[string]any{
		"ok": true, "public_url": obj.PublicURL, "message": "测试文件已上传并删除。",
	})
}

func (s *ListingService) list(ctx khttp.Context) error {
	shopID, err := headerShopID(ctx)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	page, err := s.uc.List(ctx, biz.ListingFilter{
		ShopID: shopID, Mode: ctx.Query().Get("mode"), Query: ctx.Query().Get("q"),
		Publish: splitCSV(ctx.Query().Get("publish_status")), Platform: strings.TrimSpace(ctx.Query().Get("platform_status")),
		Page: queryInt(ctx, "page"), PageSize: queryInt(ctx, "page_size"),
	})
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	return ctx.JSON(http.StatusOK, page)
}

func (s *ListingService) detail(ctx khttp.Context) error {
	shopID, id, err := shopAndID(ctx)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	detail, err := s.uc.Detail(ctx, shopID, id)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	return ctx.JSON(http.StatusOK, detail)
}

func (s *ListingService) save(ctx khttp.Context) error {
	shopID, id, err := shopAndID(ctx)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	var body struct {
		Version  int             `json:"version"`
		ItemJSON json.RawMessage `json:"item_json"`
	}
	if err := ctx.Bind(&body); err != nil {
		return writeErr(ctx, s.log, badJSON())
	}
	detail, err := s.uc.SaveVersion(ctx, shopID, id, body.Version, body.ItemJSON)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	return ctx.JSON(http.StatusOK, detail)
}

func (s *ListingService) generate(ctx khttp.Context) error {
	shopID, id, err := shopAndID(ctx)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	detail, err := s.uc.Generate(ctx, shopID, id)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	return ctx.JSON(http.StatusOK, detail)
}

func (s *ListingService) media(ctx khttp.Context) error {
	shopID, id, err := shopAndID(ctx)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	detail, err := s.uc.UploadMedia(ctx, shopID, id)
	if err != nil {
		if detail != nil {
			_ = ctx.JSON(statusOf(err), map[string]any{"message": err.Error(), "detail": detail})
			return nil
		}
		return writeErr(ctx, s.log, err)
	}
	return ctx.JSON(http.StatusOK, detail)
}

func (s *ListingService) publish(ctx khttp.Context) error {
	shopID, err := headerShopID(ctx)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	var body struct {
		Items          []biz.PublishPick `json:"items"`
		IdempotencyKey string            `json:"idempotency_key"`
		OnlyValid      bool              `json:"only_valid"`
		Execute        bool              `json:"execute"`
	}
	if err := ctx.Bind(&body); err != nil {
		return writeErr(ctx, s.log, badJSON())
	}
	plan, err := s.uc.Publish(ctx, biz.PublishInput{
		ShopID: shopID, Items: body.Items, IdempotencyKey: body.IdempotencyKey,
		OnlyValid: body.OnlyValid, Execute: body.Execute,
	})
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	return ctx.JSON(http.StatusOK, plan)
}

func (s *ListingService) batch(ctx khttp.Context) error {
	shopID, id, err := shopAndID(ctx)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	view, err := s.uc.GetBatch(ctx, shopID, id)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	return ctx.JSON(http.StatusOK, view)
}

func (s *ListingService) stock(ctx khttp.Context) error {
	shopID, id, err := shopAndID(ctx)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	var body struct {
		WarehouseID int64 `json:"warehouse_id"`
		Stock       int   `json:"stock"`
	}
	if err := ctx.Bind(&body); err != nil {
		return writeErr(ctx, s.log, badJSON())
	}
	view, err := s.uc.UpdateStock(ctx, shopID, id, body.WarehouseID, body.Stock)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	return ctx.JSON(http.StatusOK, view)
}

func (s *ListingService) platform(ctx khttp.Context) error {
	shopID, id, err := shopAndID(ctx)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	detail, err := s.uc.RefreshPlatform(ctx, shopID, id)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	return ctx.JSON(http.StatusOK, detail)
}

func shopAndID(ctx khttp.Context) (int, int, error) {
	shopID, err := headerShopID(ctx)
	if err != nil {
		return 0, 0, err
	}
	id, err := pathID(ctx)
	if err != nil {
		return 0, 0, &biz.Error{Status: 400, Reason: "BAD_ID", Message: "记录 ID 不正确。"}
	}
	return shopID, id, nil
}

func splitCSV(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func statusOf(err error) int {
	if be, ok := err.(*biz.Error); ok && be.Status != 0 {
		return be.Status
	}
	return http.StatusBadGateway
}
