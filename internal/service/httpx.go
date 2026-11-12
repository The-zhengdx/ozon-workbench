package service

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-kratos/kratos/v2/log"
	khttp "github.com/go-kratos/kratos/v2/transport/http"

	"ozon-workbench/internal/biz"
)

func writeErr(ctx khttp.Context, logger *log.Helper, err error) error {
	var be *biz.Error
	if errors.As(err, &be) {
		body := map[string]any{
			"code":    be.Status,
			"reason":  be.Reason,
			"message": be.Message,
		}
		if len(be.Fields) > 0 {
			body["fields"] = be.Fields
		}
		if be.Snapshot != nil {
			body["snapshot"] = be.Snapshot
		}
		return ctx.JSON(be.Status, body)
	}
	if logger != nil {
		logger.Errorf("request failed: %v", err)
	}
	return ctx.JSON(http.StatusInternalServerError, map[string]any{
		"code":    500,
		"reason":  "INTERNAL",
		"message": "服务器内部错误",
	})
}

func badJSON() *biz.Error {
	return &biz.Error{Status: 400, Reason: "BAD_JSON", Message: "请求体不是合法的 JSON。"}
}

func pathID(ctx khttp.Context) (int, error) {
	raw := ctx.Vars().Get("id")
	id, err := strconv.Atoi(raw)
	if err != nil || id <= 0 {
		return 0, &biz.Error{Status: 400, Reason: "BAD_ID", Message: "店铺 ID 不正确。"}
	}
	return id, nil
}

func headerShopID(ctx khttp.Context) (int, error) {
	raw := strings.TrimSpace(ctx.Request().Header.Get("X-Shop-Id"))
	if raw == "" {
		return 0, &biz.Error{Status: 400, Reason: "SHOP_REQUIRED", Message: "请先在顶部选择店铺。"}
	}
	id, err := strconv.Atoi(raw)
	if err != nil || id <= 0 {
		return 0, &biz.Error{Status: 400, Reason: "BAD_SHOP", Message: "X-Shop-Id 不正确。"}
	}
	return id, nil
}

func queryInt64(ctx khttp.Context, name string) (int64, error) {
	raw := strings.TrimSpace(ctx.Query().Get(name))
	if raw == "" {
		return 0, &biz.Error{Status: 400, Reason: "BAD_QUERY", Message: "缺少参数 " + name + "。"}
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n <= 0 {
		return 0, &biz.Error{Status: 400, Reason: "BAD_QUERY", Message: "参数 " + name + " 不正确。"}
	}
	return n, nil
}

func queryInt(ctx khttp.Context, name string) int {
	n, _ := strconv.Atoi(ctx.Query().Get(name))
	return n
}
