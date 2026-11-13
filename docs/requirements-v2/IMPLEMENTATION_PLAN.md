# Ozon V2 实施计划

权威需求：`01_项目需求规格说明书.md`。协议：`02_codex_reply_v1_2.schema.json`。空库参考：`03_数据库参考DDL.sql`。本文件记录实现前已经确认的冲突、阶段划分和待配置项。业务范围不另做设计。

## 已确认的冲突与不可擅自补齐项

1. **V1 数据模型不能原位升级。** 现有 Ent 表（店铺加密密钥、listing、workbench task）与严格 9 字段 `ozon_shops`、`ozon_products` 54 字段、加工任务表不是同一套结构。`0001_v2_init.sql` 只创建新的 `ozon_*` 表，不 DROP、不覆盖 V1 表。已有 V1 库可以共存，但 V2 进程只读写 V2 表。
2. **`ozon_system_settings` 只有 `max_active_tasks`。** 系统设置页还要 OSS、Codex、通用提示词、审核默认值和定价规则。这些不写入店铺表，也不加列到该 3 字段表。`0002_app_support.sql` 增加 `ozon_app_config`、幂等键、任务只读类目 Token。
3. **定价公式、汇率、利润率没有定稿。** 只实现白名单算术表达式。未保存表达式和变量时，草稿可以入库，但 `price`/`old_price` 保持空、`validation_status=invalid`，刊登预检拒绝。代码里没有默认倍率或汇率。
4. **默认类目提示词正文没有提供。** 不预置任何类目规则文案。未配置类目提示词时默认阻断创建任务；只有操作者写入通用提示词并把回退策略改成 `generic` 才降级。
5. **OSS 厂商与公共域名没有提供。** 默认不上传。对象存储只走 S3 兼容接口，配置在系统设置里明文保存。未配置可长期访问的 HTTPS 基址时，刊登预检失败。
6. **视觉生成工具未指定。** 只保留生成器接口。未配置时不报“已生成”。`media_ready` 必须指向任务目录里真实存在的文件。
7. **Ozon 视频属性结构未指定。** 不发明顶层 `video` 字段，不写死属性 ID。视频只允许出现在已通过类目属性校验的 `complex_attributes` 中，且 URL 必须来自已上传素材。
8. **账户币种/VAT 的官方核验接口未在需求中钉死。** 店铺默认 `CNY`/`0.1` 只作为本地默认。真实客户端在刊登前调用可配置的账户核验；核验失败则阻断，不用猜测结果冒充成功。Mock 只用于测试和 `OZON_MOCK=true`。
9. **本机没有 Codex CLI，无法现场核对 `exec resume --output-schema`。** 调用参数按 `04_接口与协议示例.md` 用 argv 数组组装，失败即失败，不降级成自然语言解析。
10. **前端框架需求写“未指定”。** 继续使用仓库已有的 Vue 3 + Vite + Pinia + Ant Design Vue，并保留现有深色视觉样式。
11. **PR #4 尚未合并。** Windows 上 `codex.cmd` 经 `cmd.exe` 启动、测试用当前测试二进制冒充 CLI、Makefile 不在 Windows 把冒号路径拼进 `PATH`，这些做法直接做进 V2。

## 技术决策

- Go 1.23，标准库 `net/http`，PostgreSQL 16+（`pgx`）。不再使用 Kratos / Ent / MCP / Codex App Server / SSE。
- 业务规则集中在 `internal/app`，HTTP 只做编解码。调度、交付、刊登都走数据库事务。单元测试使用带事务回滚的内存库；`OZON_TEST_DATABASE_URL` 存在时再跑迁移集成测试。
- Codex 每轮 `exec` 或 `exec resume <session>`，带 `--output-schema` 与 `-o`。`--json` 只用于提取 `thread.started.thread_id`。
- 类目属性接口同时给页面和 Codex `curl` 使用。Codex 使用任务级只读 Token，后端用任务冻结的店铺凭证回源。
- 副作用幂等：OSS 对象键 `tasks/{task}/{sku_ref}/{asset_id}`，交付唯一 `(task_id, sku_ref)` 与 `(shop_id, offer_id)`，导入批次按 Ozon `task_id` 去重。
- 进程启动巡检：`dispatching` 且没有轮次则退回 `queued`；已启动但进程不在的轮次标 `uncertain`，不自动开第二轮；已写入的交付不重复插入。

## 目录

```text
cmd/server                 # 加载 .env、迁移、HTTP、静态前端
migrations                 # 0001 空库参考 DDL，0002 工程辅助表
internal/app               # 店铺、类目、任务状态机、交付、刊登
internal/codexcli          # exec/resume、Windows .cmd、Schema
internal/domain            # 表对应的结构
internal/httpapi           # REST
internal/ozonapi           # 官方 API 与 Mock
internal/pricing           # 可配置表达式，无默认系数
internal/store             # 内存库与 PostgreSQL
internal/storage           # none / mock / s3
web                        # Vue 3 管理台
```

## 阶段

| 阶段 | 内容 | 数据与接口 | 验收 |
|---|---|---|---|
| 1 | 迁移、9 字段店铺、全局店铺校验、系统设置、类目树、单提示词、属性/字典懒加载 | `ozon_shops`、`ozon_system_settings`、类目缓存与规则；`/api/shops`、`/api/categories/*`、`/api/category-rules/*` | AT-01～04、AT-11～12 |
| 2 | Codex CLI、任务/轮次/消息、FIFO 名额、追加要求、审核、重启巡检 | 加工三表；`/api/processing-tasks*`、`/api/system/settings` | AT-05～10、AT-23～24 |
| 3 | 媒体校验、OSS、定价、事务草稿 | `ozon_media_assets`、`ozon_products`、`ozon_processing_deliveries`、`ozon_pricing_rules` | AT-13～17、AT-25 |
| 4 | 商品管理、批量导入、结果与平台状态、页面 | `ozon_import_batches`、`ozon_import_batch_items`；`/api/products*`、`/api/import-batches*` | AT-18～22 |

每个阶段都带单元测试。需要 PostgreSQL 的迁移测试在没有 `OZON_TEST_DATABASE_URL` 时跳过，不把跳过当成通过场景。

## 风险

- `codex exec resume` 是否接受 `--output-schema` 取决于本机 CLI 版本。未验证前不会改协议。
- 内存库覆盖状态机；PostgreSQL 语句需要在真实空库再跑一次迁移。
- 单用户本地部署仍把 API Key 明文放在 `ozon_shops.api_key`。日志、提示词和列表接口只显示脱敏值。
- 等待审核的任务继续占名额。`max_active_tasks` 很小且都停在审核时，队列不会前进，这是需求规定的行为。

## 待配置（实现不填默认业务值）

- 定价表达式、变量、取整单位
- 通用提示词正文；默认回退策略是阻断
- OSS Provider、Endpoint、Bucket、密钥、长期公开基址
- 图像/视频生成器命令
- Codex 可执行文件是否在 PATH，以及 resume 是否支持 `--output-schema`
- 类目缓存 TTL（不配置则不自动过期，只能手动刷新）
- Ozon 账户真实币种、VAT、视频属性、每日限额
- 审核开关默认关闭，可在系统设置里改
