# Ozon 本地多店铺智能电商工作台

本地单机运行的 Ozon 卖家工作台。从店铺凭证、类目契约、素材加工，一直到按可售 SKU 上架：

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
- 任务完成后，每个可售 SKU 各有一行刊登记录。媒体换成公网 HTTPS 之后，Codex 生成 `POST /v3/product/import` 的一个 `items[]` 元素，校验通过才进入待上架。
- 单条和批量上架走同一个接口。超过 100 条会拆成多个请求，但属于同一批次。结果按 `offer_id` 回写。
- 库存走当前的 `POST /v2/products/stocks`（`/v1/product/import/stocks` 已弃用）。

没有登录和权限系统。

## 技术栈

后端是 Go 1.23、Kratos v2 模块化单体（Service → Biz → Data）、Google Wire、Ent 和 PostgreSQL。前端是 Vue 3、Vite、Pinia、Ant Design Vue 和 Tailwind。开发时 Vite 把 `/api` 代理到后端；`make build` 之后单个二进制也可以托管构建好的页面。

默认 `OZON_MOCK=true`（`configs/config.yaml` 的 `ozon.mock`，以及 `docker compose` 在变量未设置时的默认值）。Ozon 客户端仍按正式文档组请求（`Client-Id`、`Api-Key`、路径和 JSON），只是由进程内传输层返回示例仓库，不访问 `api-seller.ozon.ru`。示例仓库是「莫斯科一号仓 / 20605650762000」等三条，页面会标成「模拟」。把进程环境里的 `OZON_MOCK=false` 后才会打真实接口。只写在 `.env` 里、没有 `export` 到 server 进程，不会生效。模拟环境下，Client ID 或 API Key 填 `invalid` 会返回 Ozon 风格的 403 文案。真实模式下，启动时会删掉以前存进店铺里的这三条示例仓库；连接失败不会拿它们冒充同步结果，上次真正成功的仓库会留下来并标出同步时间。

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

服务启动时用 Ent 自动建表。`migrations/001_init.sql` 到 `migrations/004_listings.sql` 是同一套表结构，便于审阅。

素材目录默认是 `data/media`（可用 `MEDIA_ROOT` 覆盖）。每个任务解压到 `data/media/tasks/<uuid>/`，路径记在 `zip_local_path`。页面通过 `/media/tasks/{id}/...` 预览图片。

新建任务前，先在「类目与提示词」里给目标末级类型保存含 `{{master_sku}}` 的提示词。主 SKU 必填，1688 链接可选，两个自动审核开关默认关闭。解压失败的任务会留在列表里，状态为失败，并写明原因。

## 目录

```text
api/workbench/v1/     Proto 契约
cmd/server/           入口、Wire
configs/config.yaml
internal/biz          店铺、类目、任务状态机、Codex 编排、上架
internal/data         Ent、加密、Ozon 客户端
internal/storage      图床（none / Yandex Disk / mock）
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

`internal/biz` 覆盖建店校验、Client ID 唯一、空密钥沿用、币种保持为空、连接失败回显、失败时保留上次真实仓库并清掉示例仓库，以及类目展开、同步失败保留快照、同类目两个类型的属性和提示词互不覆盖。素材加工覆盖提示词渲染、子 SKU 解析、审核开关只推进当前环节、并发版本冲突、会话追问互不串线，以及时间线写入前的密钥脱敏。上架覆盖属性契约校验、单条与 101 条分块、导入结果按版本回写、迟到结果不覆盖新版本、重复操作号，以及未配置图床时拒绝上传。`internal/storage` 用 `httptest` 检查 Yandex Disk 的上传、发布和直链，并拒绝把分享页当成图片地址。`internal/media` 覆盖 zip 路径穿越、绝对路径、文件数和体积上限、GBK 文件名、UTF-8 中文名，以及成功和失败后都删除压缩包。`internal/data` 用 `httptest` 检查 Ozon 客户端的请求头、分页、429 重试、传输错误重试、代理模式和属性接口正文。真实 Codex CLI 不在 CI 里执行；`codex_cli_test` 用假二进制检查 `exec` / `exec resume` 参数、stdin 提示词和 JSONL 解析。

## 从素材到上架

1. 在「店铺管理」新建店铺。模拟模式下 Client ID 和 API Key 不要填 `invalid`。币种保持空白。
2. 在「类目与提示词」给目标末级类型保存含 `{{master_sku}}` 的提示词。
3. 在「素材加工」上传 zip，填写主 SKU。模拟运行器会先要求补充字段，补充后生成 `主SKU-1`、`主SKU-2`，再经过文案和视觉审核。两个自动通过开关可以一次走完。
4. 任务变为已完成后，上架列表出现每个可售 SKU 一行，状态一般是「待完善」：还没有公网图片、价格或币种。
5. 打开详情。表单和右侧 JSON 改的是同一份 `item_json`。保存会生成新版本并按属性契约校验。
6. 「生成商品参数」用当前的 Codex 运行器（默认模拟）写出完整的 `items[]` 元素，原始输出记在 `codex_item_json`，有效对象记在 `item_json`。
7. 主图必须是公网 `https://` 地址。本地路径、`http://`，以及 `yadi.sk` / `disk.yandex.ru` 分享页都不能过校验。
8. 勾选一行或多行，预检后提交。请求体是 `{"items":[...]}`，单次最多 100 个，101 条会变成两个请求、同一个批次。页面按 `offer_id` 显示导入成功、失败原因或「待核对」。
9. 导入成功后可以按仓库更新库存。平台驳回原因出现在平台状态下面。

店铺切换会清空勾选。已经发出的批次继续用创建时的店铺凭证。

## 图床

Ozon 商品导入不接收本地文件，只接收它自己能下载的 HTTPS 地址。图床是可替换的：实现 `storage.Store`（上传、删除、健康检查、刷新直链），再在 `storage.Open` 里加一个 provider 名字即可。对象存储（S3 / R2）以后可以按同样的接口接上。

`configs/config.yaml` 的默认值是空 provider，令牌也是空的。这样即使还没有 Yandex 账号，工作台也能启动。需要公网地址的「确认并上传」会停住，并说明要去配置图床。详情里如果已经有别的 HTTPS 地址，仍然可以保存和上架。

```yaml
storage:
  provider: ""          # 空或 none。准备好以后改成 yandex
  yandex:
    token: ""           # 不要写在这里
    root: ozon-workbench
    api_base: https://cloud-api.yandex.net
```

环境变量盖过配置文件：

| 变量 | 作用 |
|---|---|
| `STORAGE_PROVIDER` | `none`（默认）、`yandex`，或仅本地演练用的 `mock` |
| `YANDEX_DISK_TOKEN` | OAuth 令牌。只放在进程环境里，不要提交 |
| `YANDEX_DISK_ROOT` | Disk 上的目录，默认 `ozon-workbench` |
| `YANDEX_DISK_API` | 默认 `https://cloud-api.yandex.net` |

以后要接 Yandex Disk：

1. 在 [Yandex OAuth](https://oauth.yandex.com/) 注册应用，勾选 Disk 的读写和发布权限，拿到令牌。
2. 启动前执行 `export STORAGE_PROVIDER=yandex` 和 `export YANDEX_DISK_TOKEN=...`。`config.yaml` 里的 token 保持空白。
3. 重启工作台。设置页会显示已配置，可以用「测试上传」放一个小文件再删掉。
4. 在 SKU 详情点「确认并上传媒体」。程序会建目录、上传、发布，再向 `GET /v1/disk/public/resources/download` 要直链。

直链的限制需要知道：

- `public_url`（`yadi.sk` 或 `disk.yandex.ru`）是分享页，返回 HTML，不是图片。校验会拒绝把它交给 Ozon。
- 下载接口返回的 `href` 才是直接地址。Yandex 文档写明这个地址有效期有限，而且下载时经常 `302` 到 `*.storage.yandex.net`。
- Ozon 是异步拉图的。直链过期、跳转不被跟随，或者跳到需要 Cookie 的地址，导入结果里会出现图片下载失败。提交前会尽量刷新一次直链，但这不能变成永久地址。
- 正式长期上架更适合 S3、R2 这类稳定的公网对象地址。Yandex Disk 适合先把链路跑通。

`STORAGE_PROVIDER=mock` 只给本地和测试用，返回 `https://mock-storage.local/...`。Ozon 真实环境下载不到。

## 上架接口

- 导入：`POST /v3/product/import`，单次最多 100 个商品。任务号用 `POST /v1/product/import/info` 按 `offer_id` 回读。传输超时且不知道平台是否受理时，本地状态是「待核对」，不会自动再发一单。
- 限流：沿用进程里的每秒上限，并尊重 `429` 的 `Retry-After` / `Item-Retry-After`。
- 商品状态：`POST /v3/product/info/list`。审核中、不可售、可售和驳回文案（`status_failed`、`status_description`、`status_tooltip`）分开显示。新版本可以是「待上架」，同时线上版本仍是「可售」。
- 库存：`POST /v2/products/stocks` 仍然是当前的 FBS / rFBS 更新方法。2025-06 去掉了 `quant_size`，本程序不再发送它。`/v1/product/import/stocks` 已弃用。一次最多 100 对商品-仓库，大约每分钟 80 次；同一对更新过频会返回 `TOO_MANY_REQUESTS` / `Stock is updated too frequently`。本地另外保证同一对至少间隔 30 秒，平台若要求更长，错误会原样显示。

## 连接 Ozon 失败 / EOF

测试连接调用的是当前的 `POST /v2/warehouse/list`（`/v1/warehouse/list` 已弃用）。请求体是 `{"limit":200}`，下一页才带 `cursor`。头是 `Content-Type: application/json`、`Client-Id`（去掉首尾空白后的十进制卖家 ID）、`Api-Key`。凭证不对时，Ozon 返回 JSON，例如 HTTP 400 `Invalid Api-Key` 或 `Client-Id header value should be positive integer`。那是接口错误，界面会显示 `Ozon HTTP …`。

`Post "https://api-seller.ozon.ru/v2/warehouse/list": EOF` 不是这种错误。连接在收到 HTTP 响应之前被关掉了。在本机用 curl 能复现一条会成功的路径：ALPN 为 `http/1.1`，请求发出后对端发起 TLS 重协商（schannel 日志里是 `remote party requests renegotiation`，而且会出现两次），协商完成后才是 HTTP 400 JSON。Go 的 `crypto/tls` 默认 `RenegotiateNever`，遇到 HelloRequest 会关掉连接，`net/http` 就报 EOF。重协商只存在于 TLS 1.2，和 HTTP/2 不兼容。客户端因此强制 HTTP/1.1，并设置 `RenegotiateFreelyAsClient`（只允许一次的话，第二次重协商仍会失败）。`api-seller.ozon.ru` 自己优先协商 TLS 1.3，不会重协商；会重协商的是部分 TLS 1.2 路径，包括 TUN / fake-ip 代理。

Go 也不看代理软件的规则。`ozon.proxy` 决定这个进程怎么走：

| 值 | 行为 |
|---|---|
| 空或 `direct` | 直连，忽略 `HTTP_PROXY` / `HTTPS_PROXY` / `ALL_PROXY` |
| `env` | 使用这些环境变量，以及 `NO_PROXY` |
| `http://127.0.0.1:7890` 或 `socks5://127.0.0.1:7891` | 固定走这个代理 |

环境变量 `OZON_PROXY` 优先于 `configs/config.yaml`。页面顶部在真实模式下会显示当前是直连还是哪一个代理。TUN / fake-ip 模式在流量进进程之前就拦截了：DNS 若得到 `198.18.0.0/15` 或 `2001:2::`，规则里把含 `ozon` 的域名设为 DIRECT 也仍然经过代理出站。那种情况下 curl 和本程序走的是同一条 TLS 路径。

在 Windows 上可以这样对比（Client ID 用 `1`、Key 用 `dummy` 即可，成功的标志是 JSON，不是 200）：

```powershell
nslookup api-seller.ozon.ru
echo %HTTP_PROXY% %HTTPS_PROXY% %ALL_PROXY% %NO_PROXY%

curl.exe -sv --ssl-no-revoke -X POST "https://api-seller.ozon.ru/v2/warehouse/list" -H "Content-Type: application/json" -H "Client-Id: 1" -H "Api-Key: dummy" --data "{\"limit\":200}"

curl.exe -sv --noproxy "*" --ssl-no-revoke -X POST "https://api-seller.ozon.ru/v2/warehouse/list" -H "Content-Type: application/json" -H "Client-Id: 1" -H "Api-Key: dummy" --data "{\"limit\":200}"

curl.exe -sv -x http://127.0.0.1:7890 --ssl-no-revoke -X POST "https://api-seller.ozon.ru/v2/warehouse/list" -H "Content-Type: application/json" -H "Client-Id: 1" -H "Api-Key: dummy" --data "{\"limit\":200}"
```

把 `7890` 换成自己的本地代理端口。`--noproxy` 躲不开 TUN。若 curl 已经能拿到 JSON，而旧版程序仍是 EOF，更新到这个版本后再测一次。若 curl 直连也是 connection reset / EOF，而带 `-x` 能拿到 JSON，把 `ozon.proxy` 设成同一个 URL，并让代理规则把 `ozon.ru` 转到可用节点，不要设成 DIRECT。

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
- 完成时把子 SKU、文案和视觉方案写到任务目录的 `approved_plan.json`，并按可售 SKU 落下刊登行。重跑同一个卖家货号不会另建一行。
- 解压会拒绝绝对路径、`..`、符号链接，并限制解压后的总体积和文件数。未设置 UTF-8 标记且文件名不是合法 UTF-8 时，按 GBK 解码，以兼容 Windows 中文压缩包。
