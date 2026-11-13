.PHONY: test api web build tidy

GO ?= go

# Windows 的 PATH 用分号。再拼一段冒号路径会让 PowerShell 和 cmd 找不到 go。
ifneq ($(OS),Windows_NT)
export PATH := /usr/local/go/bin:$(PATH)
endif

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

tidy:
	$(GO) mod tidy

api:
	$(GO) run ./cmd/server

web:
	cd web && npm run dev

ifeq ($(OS),Windows_NT)
build:
	cd web && npm run build
	if exist internal\httpapi\web rmdir /S /Q internal\httpapi\web
	mkdir internal\httpapi\web
	xcopy /E /I /Y web\dist internal\httpapi\web
	$(GO) build -o bin/ozon-workbench.exe ./cmd/server
else
build:
	cd web && npm run build
	rm -rf internal/httpapi/web
	mkdir -p internal/httpapi/web
	cp -a web/dist/. internal/httpapi/web/
	$(GO) build -o bin/ozon-workbench ./cmd/server
endif
