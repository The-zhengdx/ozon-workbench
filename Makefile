.PHONY: test api web build tidy

GO ?= go

# 本机 Go 常装在 /usr/local/go/bin。Windows 的 PATH 用分号；再拼一段冒号路径会让
# PowerShell、cmd 和 Git Bash 都找不到 go。Windows 直接用 PATH 里的 go。
ifneq ($(OS),Windows_NT)
export PATH := /usr/local/go/bin:$(PATH)
endif

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
