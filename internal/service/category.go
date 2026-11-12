package service

import (
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-kratos/kratos/v2/log"
	khttp "github.com/go-kratos/kratos/v2/transport/http"

	"ozon-workbench/internal/biz"
)

// CategoryService is the HTTP adapter for the category tree, attributes and prompts.
type CategoryService struct {
	uc  *biz.CategoryUsecase
	log *log.Helper
}

func NewCategoryService(uc *biz.CategoryUsecase, logger log.Logger) *CategoryService {
	return &CategoryService{uc: uc, log: log.NewHelper(logger)}
}

func (s *CategoryService) Register(r *khttp.Router) {
	r.GET("/categories/tree", s.tree)
	r.POST("/categories/sync", s.syncTree)
	r.GET("/categories/search", s.search)
	r.POST("/categories/import", s.importTree)
	r.GET("/categories/attributes", s.attributes)
	r.POST("/categories/attributes/sync", s.syncAttributes)
	r.POST("/categories/attributes/import", s.importAttributes)
	r.GET("/categories/attribute-values", s.values)
	r.POST("/categories/attribute-values/search", s.searchValues)
	r.GET("/category-prompts", s.getPrompt)
	r.PUT("/category-prompts", s.savePrompt)
}

func (s *CategoryService) tree(ctx khttp.Context) error {
	view, err := s.uc.Tree(ctx, ctx.Query().Get("language"))
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	return ctx.JSON(http.StatusOK, view)
}

func (s *CategoryService) syncTree(ctx khttp.Context) error {
	shopID, err := headerShopID(ctx)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	var body struct {
		Language string `json:"language"`
	}
	if err := ctx.Bind(&body); err != nil {
		return writeErr(ctx, s.log, badJSON())
	}
	view, err := s.uc.SyncTree(ctx, shopID, body.Language)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	return ctx.JSON(http.StatusOK, view)
}

func (s *CategoryService) search(ctx khttp.Context) error {
	nodes, err := s.uc.Search(ctx, ctx.Query().Get("language"), ctx.Query().Get("q"))
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	return ctx.JSON(http.StatusOK, map[string]any{"nodes": nodes})
}

func (s *CategoryService) importTree(ctx khttp.Context) error {
	var body struct {
		Language string          `json:"language"`
		Payload  json.RawMessage `json:"payload"`
	}
	if err := ctx.Bind(&body); err != nil {
		return writeErr(ctx, s.log, badJSON())
	}
	view, err := s.uc.ImportTree(ctx, body.Language, body.Payload)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	return ctx.JSON(http.StatusOK, view)
}

type attributeBody struct {
	DescriptionCategoryID int64           `json:"description_category_id"`
	TypeID                int64           `json:"type_id"`
	Language              string          `json:"language"`
	Payload               json.RawMessage `json:"payload"`
}

func (s *CategoryService) attributes(ctx khttp.Context) error {
	categoryID, typeID, err := attributeQuery(ctx)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	view, err := s.uc.Attributes(ctx, categoryID, typeID, ctx.Query().Get("language"))
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	return ctx.JSON(http.StatusOK, view)
}

func (s *CategoryService) syncAttributes(ctx khttp.Context) error {
	shopID, err := headerShopID(ctx)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	var body attributeBody
	if err := ctx.Bind(&body); err != nil {
		return writeErr(ctx, s.log, badJSON())
	}
	view, err := s.uc.SyncAttributes(ctx, shopID, body.DescriptionCategoryID, body.TypeID, body.Language)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	return ctx.JSON(http.StatusOK, view)
}

func (s *CategoryService) importAttributes(ctx khttp.Context) error {
	var body attributeBody
	if err := ctx.Bind(&body); err != nil {
		return writeErr(ctx, s.log, badJSON())
	}
	view, err := s.uc.ImportAttributes(ctx, body.DescriptionCategoryID, body.TypeID, body.Language, body.Payload)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	return ctx.JSON(http.StatusOK, view)
}

func (s *CategoryService) values(ctx khttp.Context) error {
	shopID, err := headerShopID(ctx)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	categoryID, typeID, err := attributeQuery(ctx)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	attributeID, err := queryInt64(ctx, "attribute_id")
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	last, _ := strconv.ParseInt(ctx.Query().Get("last_value_id"), 10, 64)
	refresh := truthyQuery(ctx.Query().Get("refresh"))
	// A later page always hits Ozon; the cache stores the accumulated series.
	if last > 0 {
		refresh = true
	}
	view, err := s.uc.AttributeValues(ctx, shopID, categoryID, typeID, attributeID, last, queryInt(ctx, "limit"), ctx.Query().Get("language"), refresh)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	return ctx.JSON(http.StatusOK, view)
}

func (s *CategoryService) searchValues(ctx khttp.Context) error {
	shopID, err := headerShopID(ctx)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	var body struct {
		DescriptionCategoryID int64  `json:"description_category_id"`
		TypeID                int64  `json:"type_id"`
		AttributeID           int64  `json:"attribute_id"`
		Language              string `json:"language"`
		Limit                 int    `json:"limit"`
		Value                 string `json:"value"`
		Refresh               bool   `json:"refresh"`
	}
	if err := ctx.Bind(&body); err != nil {
		return writeErr(ctx, s.log, badJSON())
	}
	view, err := s.uc.SearchAttributeValues(ctx, shopID, body.DescriptionCategoryID, body.TypeID, body.AttributeID, body.Value, body.Limit, body.Language, body.Refresh)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	return ctx.JSON(http.StatusOK, view)
}

func (s *CategoryService) getPrompt(ctx khttp.Context) error {
	categoryID, typeID, err := attributeQuery(ctx)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	view, err := s.uc.GetPrompt(ctx, categoryID, typeID)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	return ctx.JSON(http.StatusOK, view)
}

func (s *CategoryService) savePrompt(ctx khttp.Context) error {
	var body struct {
		DescriptionCategoryID int64  `json:"description_category_id"`
		TypeID                int64  `json:"type_id"`
		CodexPrompt           string `json:"codex_prompt"`
	}
	if err := ctx.Bind(&body); err != nil {
		return writeErr(ctx, s.log, badJSON())
	}
	view, err := s.uc.SavePrompt(ctx, body.DescriptionCategoryID, body.TypeID, body.CodexPrompt)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	return ctx.JSON(http.StatusOK, view)
}

func attributeQuery(ctx khttp.Context) (int64, int64, error) {
	categoryID, err := queryInt64(ctx, "description_category_id")
	if err != nil {
		return 0, 0, err
	}
	typeID, err := queryInt64(ctx, "type_id")
	if err != nil {
		return 0, 0, err
	}
	return categoryID, typeID, nil
}

func truthyQuery(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}
