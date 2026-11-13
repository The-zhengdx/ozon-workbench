-- V2 工程辅助表。不修改 ozon_shops 的 9 字段，也不给 ozon_system_settings 加业务列。
-- ozon_app_config：OSS、Codex、通用提示词、审核默认值等可配置项。
-- ozon_idempotency_keys：创建任务和发送消息的幂等键。
-- category_token：Codex curl 类目只读接口的任务级令牌，不包含店铺密钥。
BEGIN;

CREATE TABLE ozon_app_config (
  key TEXT PRIMARY KEY,
  value_json JSONB NOT NULL,
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE ozon_idempotency_keys (
  scope TEXT NOT NULL,
  idem_key TEXT NOT NULL,
  response_json JSONB NOT NULL,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY (scope, idem_key)
);

ALTER TABLE ozon_processing_tasks ADD COLUMN category_token TEXT;
CREATE UNIQUE INDEX uq_processing_category_token ON ozon_processing_tasks (category_token)
  WHERE category_token IS NOT NULL;

COMMIT;
