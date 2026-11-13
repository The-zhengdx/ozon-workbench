# Ozon 本地加工与刊登工作台

面向本地单机使用的 Ozon 商品加工台。Go 后端、PostgreSQL、Vue 3 管理页，Codex CLI 按轮次执行。需求以 `docs/requirements-v2/` 为准。

不使用 MCP，不推送 Codex 实时事件。API Key 按确认方案明文存在 `ozon_shops.api_key`，列表接口只返回脱敏值，也不会写进提示词。

## Windows PowerShell

本机需要 Go 1.23、Node.js、以及已经在跑的 PostgreSQL 16+。不需要 Docker，也不需要先安装 make 以外的 Unix 工具；`make` 在 PowerShell 里可以直接用。

```powershell
copy .env.example .env
# 编辑 DATABASE_URL，指向你的 Postgres

cd web
npm install
cd ..

make test
make api
```

另开一个终端：

```powershell
make web
```

浏览器打开 `http://127.0.0.1:43123`。接口代理到 `http://127.0.0.1:18081`。

后端启动时会从当前目录向上查找 `.env` 并加载尚未设置的变量。Postgres 可以是空库，程序只执行 `migrations/0001_v2_init.sql`、`migrations/0002_app_support.sql` 和 `migrations/0003_s3_settings.sql`。以前的 V1 表不会被删除。

`make build` 会构建前端并生成 `bin/ozon-workbench.exe`。

Codex 装在 npm 里时，Windows 上通常是 `codex.cmd`。把 `.env` 的 `CODEX_BIN` 留成 `codex` 即可，程序会自己找到 `.cmd`，并通过 `cmd.exe` 启动。不要把参数拼进一条 shell 字符串。

## 默认不会替你填的配置

- 定价表达式、变量和取整单位。没配好时草稿可以生成，但售价为空，刊登预检会拒绝。
- 类目提示词正文。没有专属提示词时默认阻断创建任务。只有你写了通用提示词，并把回退改成「使用通用提示词」才会降级。
- 对象存储。默认不上传。在系统设置里填写 S3 Endpoint、Region、Bucket、密钥、Path-style、公开基址和可选前缀。密钥明文存在数据库。没有可用的 HTTPS 公开基址时，素材上传会停住。
- 图像和视频生成器。没有配置时，系统不会宣称已经生成文件。
- Ozon 账户币种。店铺默认 CNY / 0.1 只是本地默认。真实刊登前核验失败就会停住。

`OZON_MOCK=true` 时不访问 `api-seller.ozon.ru`。

## 验收

`go test ./...` 覆盖 AT-01 到 AT-25 的服务级场景：店铺 9 字段、类目懒加载、FIFO 名额、排队消息、非法 JSON、草稿事务、冲突回滚、拆批刊登、平台状态和重启巡检。设置 `OZON_TEST_DATABASE_URL` 后会额外对真实 PostgreSQL 执行迁移。

## 目录

- `cmd/server`：HTTP 服务
- `internal/app`：任务状态机、交付和刊登
- `internal/codexcli`：`exec` / `exec resume`，以及 Windows `.cmd`
- `internal/store`：PostgreSQL 与测试用内存库
- `migrations`：空库脚本。`001`–`004` 是 V1 历史，V2 进程不执行
- `web`：Vue 3 + Vite + Pinia + Ant Design Vue
