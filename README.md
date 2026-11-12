# Ozon 本地多店铺智能电商工作台

本地单机运行的 Ozon 卖家工作台。这一阶段交付店铺凭证和完整类目契约：

- 新增、编辑、移除和切换店铺。API Key 用密码框输入，编辑时留空沿用原值。
- 后端用 AES-256-GCM 加密保存密钥。接口只返回「已配置 / 待配置」，不回显密钥。
- 刊登币种默认未知，不预填 RUB 或 CNY。
- 顶部店铺切换器把当前店铺放在 `X-Shop-Id` 请求头里。
- 测试连接会调用 Ozon `POST /v2/warehouse/list`，按 `cursor` / `has_next` 翻页并缓存仓库。
- 同步 `POST /v1/description-category/tree` 的整棵树，保留层级、停用标记和末级类型。失败时保留上一份快照。
- 选中末级类型后读取全部必填和选填属性，字典型属性可以分页或搜索。
- 每个类目和末级类型可以单独保存 Codex 提示词，占位符 `{{master_sku}}` 原样保存。

没有登录和权限系统。素材加工、Codex 会话和上架不在这一阶段。

## 技术栈

后端是 Go 1.23、Kratos v2 模块化单体（Service → Biz → Data）、Google Wire、Ent 和 PostgreSQL。前端是 Vue 3、Vite、Pinia、Ant Design Vue 和 Tailwind。开发时 Vite 把 `/api` 代理到后端；`make build` 之后单个二进制也可以托管构建好的页面。

默认 `OZON_MOCK=true`。Ozon 客户端仍按正式文档组请求（`Client-Id`、`Api-Key`、路径和 JSON），只是由进程内传输层返回示例数据，不访问 `api-seller.ozon.ru`。把 `OZON_MOCK=false` 后才会打真实接口。模拟环境下，Client ID 或 API Key 填 `invalid` 会返回 Ozon 风格的 403 文案。

## 本地运行

需要 Go 1.23+、Node.js 22+ 和 PostgreSQL 15+。

```bash
cp .env.example .env
# 启动数据库。没有 Docker 时，也可以本机安装 PostgreSQL 并创建同名库和用户。
docker compose up -d postgres

export OZON_CREDENTIAL_KEY=dev-only-local-key-change-me-please
export DATABASE_URL=postgres://ozon:ozon@127.0.0.1:5432/ozon_workbench?sslmode=disable

make test
make api          # http://127.0.0.1:18081
# 另一个终端
cd web && npm install && npm run dev   # http://127.0.0.1:43123
```

加密密钥只从环境变量 `OZON_CREDENTIAL_KEY` 读取。它可以是任意口令（内部做 SHA-256），也可以是 64 位十六进制或 32 字节标准 Base64。仓库里的示例口令只给本机开发用，换钥匙之后旧的 API Key 无法解密，需要重新填写。

`make build` 会构建前端并打出 `bin/ozon-workbench`。`docker compose --profile app up --build` 会同时启动数据库和这个二进制。

服务启动时用 Ent 自动建表。`migrations/001_init.sql` 是同一套表结构，便于审阅。

## 目录

```text
api/workbench/v1/     Proto 契约
cmd/server/           入口、Wire
configs/config.yaml
internal/biz          店铺与类目用例
internal/data         Ent、加密、Ozon 客户端
internal/service      HTTP 适配
internal/server       Kratos HTTP 与静态页
web/                  Vue 工作台
```

Proto 描述了 HTTP 路径和字段。运行中的路由由 `internal/service` 按这些路径实现，避免本地还要安装一套 protoc 插件。

## 测试

```bash
make test
```

`internal/biz` 覆盖建店校验、Client ID 唯一、空密钥沿用、币种保持为空、连接失败回显，以及类目展开、同步失败保留快照、同类目两个类型的属性和提示词互不覆盖。`internal/data` 用 `httptest` 检查 Ozon 客户端的请求头、分页、429 重试和属性接口正文。

## 这一阶段的取舍

- 类目树按语言保存一份，不按店铺拆分。Ozon 的描述类目是平台目录；当前店铺只在主动同步时提供凭证。
- 提示词用稳定的 `description_category_id + type_id` 做唯一键。重新同步会重写本地节点 ID，如果绑在本地主键上，提示词会丢。
- 属性快照另外放在 `attribute_contracts`，叶子节点的 `attributes_schema` 同时记下当前语言的最新快照。字典缓存额外带上属性 ID，否则不同字典会互相覆盖。
- 删除店铺是归档，可以恢复，和原型里的「移除」一致。
- 只有修改了 Client ID 或 API Key 才会重新测试连接；只改名称不会打 Ozon。
- 离线示例树在第一次打开中文类目时写入，来源显示为「离线示例」。同步成功后替换为「Ozon 官方同步」。
