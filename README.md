# Ozon 本地多店铺智能电商工作台

本地单机运行的 Ozon 卖家工作台。当前交付店铺凭证、完整类目契约，以及素材加工工作台：

- 新增、编辑、移除和切换店铺。API Key 用密码框输入，编辑时留空沿用原值。
- 后端用 AES-256-GCM 加密保存密钥。接口只返回「已配置 / 待配置」，不回显密钥。
- 刊登币种默认未知，不预填 RUB 或 CNY。
- 顶部店铺切换器把当前店铺放在 `X-Shop-Id` 请求头里。
- 测试连接会调用 Ozon `POST /v2/warehouse/list`，按 `cursor` / `has_next` 翻页并缓存仓库。
- 同步 `POST /v1/description-category/tree` 的整棵树，保留层级、停用标记和末级类型。失败时保留上一份快照。
- 选中末级类型后读取全部必填和选填属性，字典型属性可以分页或搜索。
- 每个类目和末级类型可以单独保存 Codex 提示词，占位符 `{{master_sku}}` 原样保存。
- 素材加工任务绑定创建时的店铺（`X-Shop-Id`）和已保存提示词的末级类型。上传 `.zip` 后由后端解压到 `media.root`，成功或失败都会删除压缩包。
- 任务状态为 `RUNNING`、`NEED_INPUT`、`PENDING_TEXT_REVIEW`、`PENDING_VISUAL_REVIEW`、`COMPLETED`。解压或 Codex 失败记为 `FAILED`。
- 每个任务有独立的「文案自动通过」和「视觉方案自动通过」。正在等待的环节被打开时只推进一次，并用任务版本防止并发重复推进。
- 同一 Codex 会话保留完整时间线，可追问。写入前会脱敏密钥和鉴权头。

没有登录和权限系统。按可售 SKU 刊登（模块 4）还不在这一阶段。

## 技术栈

后端是 Go 1.23、Kratos v2 模块化单体（Service → Biz → Data）、Google Wire、Ent 和 PostgreSQL。前端是 Vue 3、Vite、Pinia、Ant Design Vue 和 Tailwind。开发时 Vite 把 `/api` 代理到后端；`make build` 之后单个二进制也可以托管构建好的页面。

默认 `OZON_MOCK=true`。Ozon 客户端仍按正式文档组请求（`Client-Id`、`Api-Key`、路径和 JSON），只是由进程内传输层返回示例数据，不访问 `api-seller.ozon.ru`。把 `OZON_MOCK=false` 后才会打真实接口。模拟环境下，Client ID 或 API Key 填 `invalid` 会返回 Ozon 风格的 403 文案。

默认 `CODEX_MOCK=true`。模拟运行器不调用本机 `codex`，但会走同一套状态机：先进入待补充数据，补充后生成 `主SKU-1`、`主SKU-2` 文案，再进入视觉方案，并在同一会话里回答追问。把 `CODEX_MOCK=false` 后，按 `configs/config.yaml` 的命令模板执行非交互命令：

```bash
codex exec --json --skip-git-repo-check --sandbox workspace-write -C <素材目录> -o <最后一条回复> -
codex exec resume <SESSION_ID> --json --skip-git-repo-check --sandbox workspace-write -C <素材目录> -o <最后一条回复> -
```

提示词从 stdin 送入（参数里的 `-`）。会话号取自 JSONL 事件 `thread.started.thread_id`。命令模板可以用 `{{work_dir}}`、`{{session_id}}`、`{{last_message}}` 改写。

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

服务启动时用 Ent 自动建表。`migrations/001_init.sql` 与 `migrations/002_workbench_tasks.sql` 是同一套表结构，便于审阅。

素材目录默认是 `data/media`（可用 `MEDIA_ROOT` 覆盖）。每个任务解压到 `data/media/tasks/<uuid>/`，路径记在 `zip_local_path`。页面通过 `/media/tasks/{id}/...` 预览图片。

新建任务前，先在「类目与提示词」里给目标末级类型保存含 `{{master_sku}}` 的提示词。主 SKU 必填，1688 链接可选，两个自动审核开关默认关闭。解压失败的任务会留在列表里，状态为失败，并写明原因。

## 目录

```text
api/workbench/v1/     Proto 契约
cmd/server/           入口、Wire
configs/config.yaml
internal/biz          店铺、类目、任务状态机与 Codex 编排
internal/data         Ent、加密、Ozon 客户端
internal/media        zip 安全解压
internal/service      HTTP 适配
internal/server       Kratos HTTP、静态页与 /media
web/                  Vue 工作台
```

Proto 描述了 HTTP 路径和字段。运行中的路由由 `internal/service` 按这些路径实现，避免本地还要安装一套 protoc 插件。

## 测试

```bash
make test
```

`internal/biz` 覆盖建店校验、Client ID 唯一、空密钥沿用、币种保持为空、连接失败回显，以及类目展开、同步失败保留快照、同类目两个类型的属性和提示词互不覆盖。素材加工覆盖提示词渲染、子 SKU 解析、审核开关只推进当前环节、并发版本冲突、会话追问互不串线，以及时间线写入前的密钥脱敏。`internal/media` 覆盖 zip 路径穿越、绝对路径、文件数和体积上限、GBK 文件名、UTF-8 中文名，以及成功和失败后都删除压缩包。`internal/data` 用 `httptest` 检查 Ozon 客户端的请求头、分页、429 重试和属性接口正文。真实 Codex CLI 不在 CI 里执行；`codex_cli_test` 用假二进制检查 `exec` / `exec resume` 参数、stdin 提示词和 JSONL 解析。

## 这一阶段的取舍

- 类目树按语言保存一份，不按店铺拆分。Ozon 的描述类目是平台目录；当前店铺只在主动同步时提供凭证。
- 提示词用稳定的 `description_category_id + type_id` 做唯一键。重新同步会重写本地节点 ID，如果绑在本地主键上，提示词会丢。
- 属性快照另外放在 `attribute_contracts`，叶子节点的 `attributes_schema` 同时记下当前语言的最新快照。字典缓存额外带上属性 ID，否则不同字典会互相覆盖。
- 删除店铺是归档，可以恢复，和原型里的「移除」一致。
- 只有修改了 Client ID 或 API Key 才会重新测试连接；只改名称不会打 Ozon。
- 离线示例树在第一次打开中文类目时写入，来源显示为「离线示例」。同步成功后替换为「Ozon 官方同步」。
- 加工任务固定创建时的店铺、末级类型、提示词和属性快照。提示词按 `description_category_id + type_id` 读取，不随本地节点 ID 变化。
- `{{master_sku}}` 会替换进提示词；没有这个占位符时，主 SKU 会补在开头。属性契约有 `{{attributes}}` 时替换它，否则附在文末。
- 模拟运行器第一次总会提出待补充字段（必填属性，没有必填属性时是「品牌」），方便把状态机走完。正式 CLI 则解析模型输出里的 `ozon-signal` 代码块。
- 文案或视觉开关在对应环节打开时只自动通过这一次。更后面的环节要等到达时再读最新开关。已经通过的审核结果保留。
- 追问写入同一 `codex_session_id`，不改审核状态。另一任务的会话分开存放。
- 完成时把子 SKU、文案和视觉方案写到任务目录的 `approved_plan.json`。按可售 SKU 落刊登记录属于模块 4，这一阶段没有做。
- 解压会拒绝绝对路径、`..`、符号链接，并限制解压后的总体积和文件数。未设置 UTF-8 标记且文件名不是合法 UTF-8 时，按 GBK 解码，以兼容 Windows 中文压缩包。
