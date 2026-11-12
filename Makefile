.PHONY: test api web build tidy

GO ?= go
export PATH := /usr/local/go/bin:$(PATH)

# 未导出密钥时使用仅供本地开发的口令。生产环境请换成自己的值。
CREDENTIAL_KEY ?= dev-only-local-key-change-me-please

test:
	$(GO) test ./...

tidy:
	$(GO) mod tidy

api:
	OZON_CREDENTIAL_KEY=$(CREDENTIAL_KEY) $(GO) run ./cmd/server -conf configs/config.yaml

web:
	cd web && npm run dev

build:
	cd web && npm run build
	rm -rf internal/server/webdist
	mkdir -p internal/server/webdist
	cp -a web/dist/. internal/server/webdist/
	OZON_CREDENTIAL_KEY=$(CREDENTIAL_KEY) $(GO) build -o bin/ozon-workbench ./cmd/server
