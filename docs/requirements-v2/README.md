# Ozon 项目最终需求包

**阅读顺序**

1. `01_项目需求规格说明书.md`：完整权威需求（业务、架构、数据流、数据库、前端、状态机、验收、待定项）。
2. `02_codex_reply_v1_2.schema.json`：固定 Codex 业务回复 Schema；在 `codex exec` 和 `resume` 时传入。
3. `03_数据库参考DDL.sql`：空库 PostgreSQL 16+ 参考 DDL；**不是已有库迁移脚本**。
4. `04_接口与协议示例.md`：CLI/REST/业务协议示例。
5. `examples/`：六种 Codex 回复/系统事件参考数据。

**不可偏离的最终决策**：本地 Go + PostgreSQL、全局店铺、9 字段店铺表、一个类目一个完整提示词、Codex CLI 非交互逐轮执行、不使用 MCP、不需要实时 Codex 事件、任务中消息排队下一轮处理、最大活跃任务数排队、类目属性/字典懒加载、OSS 上传 URL 回传、每 SKU 交付生成 draft、运营手动批量刊登 Ozon。

**待定**：真实定价公式、具体生成工具、OSS Provider、前端框架、Ozon 账户币种及视频支持规则。不得由实现 AI 擅自补成硬编码常量。
