//go:build wireinject

package main

import (
	"github.com/go-kratos/kratos/v2"
	"github.com/go-kratos/kratos/v2/log"
	"github.com/google/wire"

	"ozon-workbench/internal/biz"
	"ozon-workbench/internal/conf"
	"ozon-workbench/internal/data"
	"ozon-workbench/internal/server"
	"ozon-workbench/internal/service"
)

func initApp(*conf.Runtime, log.Logger) (*kratos.App, func(), error) {
	panic(wire.Build(
		data.ProviderSet,
		biz.ProviderSet,
		service.ProviderSet,
		server.ProviderSet,
		newApp,
	))
}
