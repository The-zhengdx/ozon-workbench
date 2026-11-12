package main

import (
	"flag"
	"os"

	"github.com/go-kratos/kratos/v2"
	"github.com/go-kratos/kratos/v2/log"
	khttp "github.com/go-kratos/kratos/v2/transport/http"

	"ozon-workbench/internal/conf"
)

func newApp(logger log.Logger, hs *khttp.Server) *kratos.App {
	return kratos.New(
		kratos.Name("ozon-workbench"),
		kratos.Logger(logger),
		kratos.Server(hs),
	)
}

func main() {
	confPath := flag.String("conf", "configs/config.yaml", "配置文件路径")
	flag.Parse()
	cfg, err := conf.Load(*confPath)
	if err != nil {
		panic(err)
	}
	logger := log.With(log.NewStdLogger(os.Stdout),
		"ts", log.DefaultTimestamp,
		"caller", log.DefaultCaller,
		"service", "ozon-workbench",
	)
	app, cleanup, err := initApp(cfg, logger)
	if err != nil {
		panic(err)
	}
	defer cleanup()
	if err := app.Run(); err != nil {
		panic(err)
	}
}
