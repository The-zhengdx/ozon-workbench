-- S3 兼容存储的明文配置。密钥按使用者的要求保存在数据库里，不加密。
-- 不修改已经执行过的 0001 / 0002。
BEGIN;

INSERT INTO ozon_app_config(key, value_json)
VALUES (
  's3',
  '{"endpoint":"","region":"","bucket":"","access_key_id":"","secret_access_key":"","path_style":true,"public_base_url":"","key_prefix":""}'::jsonb
)
ON CONFLICT (key) DO NOTHING;

COMMIT;
