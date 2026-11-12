package service

import (
	"net/http"

	"github.com/go-kratos/kratos/v2/log"
	khttp "github.com/go-kratos/kratos/v2/transport/http"

	"ozon-workbench/internal/biz"
)

// ShopService is the HTTP adapter for shop credentials.
type ShopService struct {
	uc  *biz.ShopUsecase
	log *log.Helper
}

func NewShopService(uc *biz.ShopUsecase, logger log.Logger) *ShopService {
	return &ShopService{uc: uc, log: log.NewHelper(logger)}
}

func (s *ShopService) Register(r *khttp.Router) {
	r.GET("/shops", s.list)
	r.POST("/shops", s.create)
	r.GET("/shops/{id}", s.get)
	r.PUT("/shops/{id}", s.update)
	r.DELETE("/shops/{id}", s.archive)
	r.POST("/shops/{id}/restore", s.restore)
	r.POST("/shops/{id}/default", s.setDefault)
	r.POST("/shops/{id}/connection-test", s.test)
}

type shopBody struct {
	Name     string `json:"name"`
	ClientID string `json:"client_id"`
	APIKey   string `json:"api_key"`
}

func (s *ShopService) list(ctx khttp.Context) error {
	rows, err := s.uc.List(ctx)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	return ctx.JSON(http.StatusOK, map[string]any{"shops": rows})
}

func (s *ShopService) get(ctx khttp.Context) error {
	id, err := pathID(ctx)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	row, err := s.uc.Get(ctx, id)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	return ctx.JSON(http.StatusOK, row)
}

func (s *ShopService) create(ctx khttp.Context) error {
	var body shopBody
	if err := ctx.Bind(&body); err != nil {
		return writeErr(ctx, s.log, badJSON())
	}
	row, err := s.uc.Create(ctx, body.Name, body.ClientID, body.APIKey)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	return ctx.JSON(http.StatusCreated, row)
}

func (s *ShopService) update(ctx khttp.Context) error {
	id, err := pathID(ctx)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	var body shopBody
	if err := ctx.Bind(&body); err != nil {
		return writeErr(ctx, s.log, badJSON())
	}
	row, err := s.uc.Update(ctx, id, body.Name, body.ClientID, body.APIKey)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	return ctx.JSON(http.StatusOK, row)
}

func (s *ShopService) archive(ctx khttp.Context) error {
	id, err := pathID(ctx)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	row, err := s.uc.Archive(ctx, id)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	return ctx.JSON(http.StatusOK, row)
}

func (s *ShopService) restore(ctx khttp.Context) error {
	id, err := pathID(ctx)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	row, err := s.uc.Restore(ctx, id)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	return ctx.JSON(http.StatusOK, row)
}

func (s *ShopService) setDefault(ctx khttp.Context) error {
	id, err := pathID(ctx)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	row, err := s.uc.SetDefault(ctx, id)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	return ctx.JSON(http.StatusOK, row)
}

func (s *ShopService) test(ctx khttp.Context) error {
	id, err := pathID(ctx)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	row, err := s.uc.TestConnection(ctx, id)
	if err != nil {
		return writeErr(ctx, s.log, err)
	}
	return ctx.JSON(http.StatusOK, row)
}
