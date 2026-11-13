-- Ozon 本地 AI 商品加工与自动刊登系统 - 全新库参考 DDL V1.0
-- PostgreSQL 16+
-- 注意：此文件只用于空库首次建表。已有 V2/V3/V4 表时必须单独编写迁移。
-- 用户确认 ozon_shops 仅 9 字段；其它设计以 01_项目需求规格说明书.md 为准。
BEGIN;

CREATE OR REPLACE FUNCTION app_touch_updated_at()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  NEW.updated_at = NOW();
  RETURN NEW;
END;
$$;

CREATE TABLE ozon_shops (
  id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  name VARCHAR(128) NOT NULL CHECK (BTRIM(name) <> ''),
  client_id VARCHAR(64) NOT NULL UNIQUE CHECK (BTRIM(client_id) <> ''),
  api_key TEXT NOT NULL CHECK (BTRIM(api_key) <> ''),
  currency_code VARCHAR(3) NOT NULL DEFAULT 'CNY',
  vat NUMERIC(5,3) NOT NULL DEFAULT 0.1,
  enable BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT ck_ozon_shops_currency CHECK (currency_code ~ '^[A-Z]{3}$')
);
CREATE TRIGGER trg_ozon_shops_updated_at BEFORE UPDATE ON ozon_shops
  FOR EACH ROW EXECUTE FUNCTION app_touch_updated_at();

CREATE TABLE ozon_system_settings (
  id SMALLINT PRIMARY KEY DEFAULT 1 CHECK (id=1),
  max_active_tasks INTEGER NOT NULL DEFAULT 3 CHECK (max_active_tasks>0),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
INSERT INTO ozon_system_settings(id,max_active_tasks) VALUES (1,3);

CREATE TABLE ozon_category_tree_cache (
  language VARCHAR(16) PRIMARY KEY,
  tree_json JSONB NOT NULL CHECK (jsonb_typeof(tree_json) IN ('array','object')),
  synced_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE ozon_category_ai_rules (
  id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  description_category_id BIGINT NOT NULL CHECK (description_category_id>0),
  type_id BIGINT NOT NULL CHECK (type_id>0),
  prompt TEXT NOT NULL CHECK (BTRIM(prompt) <> ''),
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  version INTEGER NOT NULL DEFAULT 1 CHECK (version>0),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT uq_ozon_category_ai_rules_type UNIQUE(description_category_id,type_id)
);
CREATE TRIGGER trg_ozon_category_ai_rules_updated_at BEFORE UPDATE ON ozon_category_ai_rules
  FOR EACH ROW EXECUTE FUNCTION app_touch_updated_at();

CREATE TABLE ozon_category_attribute_cache (
  shop_id BIGINT NOT NULL REFERENCES ozon_shops(id) ON DELETE RESTRICT,
  description_category_id BIGINT NOT NULL,
  type_id BIGINT NOT NULL,
  language VARCHAR(16) NOT NULL DEFAULT 'RU',
  attributes_json JSONB NOT NULL CHECK (jsonb_typeof(attributes_json)='array'),
  fetched_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  expires_at TIMESTAMPTZ,
  PRIMARY KEY(shop_id,description_category_id,type_id,language)
);

CREATE TABLE ozon_attribute_dictionary_values (
  shop_id BIGINT NOT NULL REFERENCES ozon_shops(id) ON DELETE RESTRICT,
  description_category_id BIGINT NOT NULL,
  type_id BIGINT NOT NULL,
  attribute_id BIGINT NOT NULL,
  language VARCHAR(16) NOT NULL DEFAULT 'RU',
  value_id BIGINT NOT NULL,
  value_text TEXT NOT NULL,
  info TEXT,
  picture TEXT,
  source VARCHAR(12) NOT NULL DEFAULT 'search' CHECK (source IN ('search','list')),
  fetched_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY(shop_id,description_category_id,type_id,attribute_id,language,value_id)
);
CREATE INDEX idx_ozon_dictionary_text ON ozon_attribute_dictionary_values
  (shop_id,description_category_id,type_id,attribute_id,language,LOWER(value_text));

CREATE TABLE ozon_attribute_dictionary_sync (
  shop_id BIGINT NOT NULL REFERENCES ozon_shops(id) ON DELETE RESTRICT,
  description_category_id BIGINT NOT NULL,
  type_id BIGINT NOT NULL,
  attribute_id BIGINT NOT NULL,
  language VARCHAR(16) NOT NULL DEFAULT 'RU',
  list_status VARCHAR(12) NOT NULL DEFAULT 'partial' CHECK (list_status IN ('partial','complete')),
  last_value_id BIGINT NOT NULL DEFAULT 0,
  synced_at TIMESTAMPTZ,
  expires_at TIMESTAMPTZ,
  PRIMARY KEY(shop_id,description_category_id,type_id,attribute_id,language)
);

-- 尚未确认真实定价公式；仅保存参数，公式由 Go 定价服务使用受控白名单解释。
CREATE TABLE ozon_pricing_rules (
  id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  name VARCHAR(128) NOT NULL,
  enabled BOOLEAN NOT NULL DEFAULT TRUE,
  parameters_json JSONB NOT NULL DEFAULT '{}'::jsonb
    CHECK (jsonb_typeof(parameters_json)='object'),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE TRIGGER trg_ozon_pricing_rules_updated_at BEFORE UPDATE ON ozon_pricing_rules
  FOR EACH ROW EXECUTE FUNCTION app_touch_updated_at();


-- SKU 主表：沿用此前已确认的 V2.0 54 字段。
-- Ozon AI 自动刊登系统：SKU 级商品主表 V2.0
-- PostgreSQL 16+
-- 设计边界：仅创建 ozon_products，不修改其他业务表。
-- 本文件适用于首次建表；已有 ozon_products 时请另做迁移脚本。
-- 外键引用在本合并脚本的下方统一补齐。
-- 业务原则：Codex 输出结构化数据；后端标准化并计算价格、构建 api_item_json。



CREATE TABLE ozon_products (
    -- A. 标识与货源（7）
    id                          bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    shop_id                     bigint NOT NULL,
    generation_task_id          bigint,
    base_sku                    varchar(128) NOT NULL,
    offer_id                    varchar(128),               -- 草稿可空，同店铺非空时唯一
    variant_label               varchar(255),
    origin_link                 text,                       -- 1688 货源详情页，允许为空

    -- B. 类目与基础信息（5）
    description_category_id     bigint NOT NULL,
    type_id                     bigint NOT NULL,
    new_description_category_id bigint,                     -- 变更已有商品类目时才填；创建时 NULL
    name                        varchar(500),
    barcode                     varchar(128),              -- 平台条形码，可在首轮导入成功后补齐

    -- C. 采购成本、定价（9）
    cost                        numeric(18,2),             -- Codex 提取：单件进货价 + 国内运费到货代
    cost_currency_code          varchar(3) NOT NULL DEFAULT 'CNY',
    cost_source_json            jsonb NOT NULL DEFAULT '{}'::jsonb,  -- 提取证据、单价/运费拆分及不确定性
    price                       numeric(18,2),             -- 后端计算
    old_price                   numeric(18,2),             -- 后端划线价规则计算
    currency_code               varchar(3),                -- 来自店铺配置
    vat                         varchar(16),               -- 来自店铺税务配置；不硬编码 0.1
    pricing_rule_id             bigint,
    pricing_snapshot            jsonb NOT NULL DEFAULT '{}'::jsonb,  -- 当次计算汇率、规则版本等

    -- D. 媒体、包装与类目属性（14）
    primary_image               text,
    images                      jsonb NOT NULL DEFAULT '[]'::jsonb,
    color_image                 text,
    pdf_list                    jsonb NOT NULL DEFAULT '[]'::jsonb,
    depth                       integer,
    height                      integer,
    width                       integer,
    dimension_unit              varchar(8) NOT NULL DEFAULT 'mm',
    weight                      integer,
    weight_unit                 varchar(8) NOT NULL DEFAULT 'g',
    promotions                  jsonb NOT NULL DEFAULT '[]'::jsonb,
    attributes                  jsonb NOT NULL DEFAULT '[]'::jsonb,
    complex_attributes          jsonb NOT NULL DEFAULT '[]'::jsonb,
    ai_structured_json          jsonb,                       -- Codex 原始结构化输出，不等于 API JSON

    -- E. API 请求对象与校验（5）
    api_item_json               jsonb,                       -- 单条 items[n]，不含外层 {"items": [...]}
    api_json_revision           integer NOT NULL DEFAULT 0,
    last_imported_revision      integer,                    -- 最后被 Ozon 导入成功的 JSON 版本
    validation_status           varchar(20) NOT NULL DEFAULT 'unchecked',
    validation_errors           jsonb NOT NULL DEFAULT '[]'::jsonb,

    -- F. 内部审核 / API 导入 / 平台状态（12）
    text_review_status          varchar(20) NOT NULL DEFAULT 'pending',
    image_review_status         varchar(20) NOT NULL DEFAULT 'pending',
    publish_status              varchar(24) NOT NULL DEFAULT 'draft',
    ozon_moderation_status      varchar(64),                -- 原样保存平台状态码，不写死枚举
    ozon_sale_status            varchar(64),                -- 原样保存平台状态码，不写死枚举
    ozon_product_id             bigint,
    ozon_sku                    bigint,
    ozon_status_raw             jsonb NOT NULL DEFAULT '{}'::jsonb,
    last_error_json             jsonb NOT NULL DEFAULT '{}'::jsonb,
    last_submitted_at           timestamptz,
    last_imported_at            timestamptz,
    last_platform_synced_at     timestamptz,

    -- G. 时间（2）
    created_at                  timestamptz NOT NULL DEFAULT now(),
    updated_at                  timestamptz NOT NULL DEFAULT now(),

    -- 业务与类型约束
    CONSTRAINT uq_ozon_products_shop_offer UNIQUE (shop_id, offer_id),
    CONSTRAINT ck_ozon_products_base_sku CHECK (btrim(base_sku) <> ''),
    CONSTRAINT ck_ozon_products_offer CHECK (offer_id IS NULL OR btrim(offer_id) <> ''),
    CONSTRAINT ck_ozon_products_category CHECK (
        description_category_id > 0 AND type_id > 0 AND
        (new_description_category_id IS NULL OR new_description_category_id > 0)
    ),
    CONSTRAINT ck_ozon_products_money CHECK (
        (cost IS NULL OR cost >= 0) AND
        (price IS NULL OR price > 0) AND
        (old_price IS NULL OR old_price >= 0)
    ),
    CONSTRAINT ck_ozon_products_dimensions CHECK (
        (depth IS NULL OR depth > 0) AND
        (height IS NULL OR height > 0) AND
        (width IS NULL OR width > 0) AND
        (weight IS NULL OR weight > 0)
    ),
    CONSTRAINT ck_ozon_products_currency CHECK (
        cost_currency_code ~ '^[A-Z]{3}$' AND
        (currency_code IS NULL OR currency_code ~ '^[A-Z]{3}$')
    ),
    CONSTRAINT ck_ozon_products_json_types CHECK (
        jsonb_typeof(cost_source_json) = 'object' AND
        jsonb_typeof(pricing_snapshot) = 'object' AND
        jsonb_typeof(images) = 'array' AND
        jsonb_typeof(pdf_list) = 'array' AND
        jsonb_typeof(promotions) = 'array' AND
        jsonb_typeof(attributes) = 'array' AND
        jsonb_typeof(complex_attributes) = 'array' AND
        jsonb_typeof(validation_errors) = 'array' AND
        jsonb_typeof(ozon_status_raw) = 'object' AND
        jsonb_typeof(last_error_json) = 'object' AND
        (ai_structured_json IS NULL OR jsonb_typeof(ai_structured_json) = 'object') AND
        (api_item_json IS NULL OR jsonb_typeof(api_item_json) = 'object')
    ),
    CONSTRAINT ck_ozon_products_api_item CHECK (
        api_item_json IS NULL OR
        (
            offer_id IS NOT NULL AND
            jsonb_typeof(api_item_json) = 'object' AND
            api_item_json ? 'offer_id' AND
            api_item_json->>'offer_id' = offer_id AND
            NOT (api_item_json ? 'items')
        )
    ),
    CONSTRAINT ck_ozon_products_revision CHECK (
        api_json_revision >= 0 AND
        (last_imported_revision IS NULL OR
            (last_imported_revision >= 0 AND last_imported_revision <= api_json_revision))
    ),
    CONSTRAINT ck_ozon_products_validation_status CHECK (
        validation_status IN ('unchecked', 'valid', 'invalid')
    ),
    CONSTRAINT ck_ozon_products_text_review_status CHECK (
        text_review_status IN ('pending', 'approved', 'rejected')
    ),
    CONSTRAINT ck_ozon_products_image_review_status CHECK (
        image_review_status IN ('pending', 'approved', 'rejected')
    ),
    CONSTRAINT ck_ozon_products_publish_status CHECK (
        publish_status IN ('draft','ready','submitting','processing','imported','failed')
    ),
    CONSTRAINT ck_ozon_products_platform_ids CHECK (
        (ozon_product_id IS NULL OR ozon_product_id > 0) AND
        (ozon_sku IS NULL OR ozon_sku > 0)
    )
);

-- 单店铺按状态 + 最近修改时间筛选
CREATE INDEX idx_ozon_products_shop_status
    ON ozon_products (shop_id, publish_status, updated_at DESC);
-- 按主 SKU 归组
CREATE INDEX idx_ozon_products_shop_base_sku
    ON ozon_products (shop_id, base_sku);
-- 根据任务查找生成的 SKU
CREATE INDEX idx_ozon_products_generation_task
    ON ozon_products (generation_task_id);
-- 类目筛选与特征模板匹配
CREATE INDEX idx_ozon_products_shop_category_type
    ON ozon_products (shop_id, description_category_id, type_id);
-- 店铺内按审核状态筛选
CREATE INDEX idx_ozon_products_shop_reviews
    ON ozon_products (shop_id, text_review_status, image_review_status);
-- 可提交候选
CREATE INDEX idx_ozon_products_ready
    ON ozon_products (shop_id, updated_at DESC)
    WHERE publish_status = 'ready';
-- 已导入但本地 JSON 发生变化的 SKU
CREATE INDEX idx_ozon_products_unsynced
    ON ozon_products (shop_id, updated_at DESC)
    WHERE last_imported_revision IS DISTINCT FROM api_json_revision;

-- PostgreSQL 不自动更新 updated_at，使用表级触发器
CREATE OR REPLACE FUNCTION ozon_products_touch_updated_at()
RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
    NEW.updated_at = now();
    RETURN NEW;
END;
$$;

CREATE TRIGGER trg_ozon_products_touch_updated_at
BEFORE UPDATE ON ozon_products
FOR EACH ROW EXECUTE FUNCTION ozon_products_touch_updated_at();


-- 本整合脚本中的 shop / processing task / pricing rule 外键在下方统一添加。



-- 注意：任务必须在商品前的业务流程创建，但外键在两表建好后再补。
CREATE TABLE ozon_processing_tasks (
  id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  shop_id BIGINT NOT NULL REFERENCES ozon_shops(id) ON DELETE RESTRICT,
  description_category_id BIGINT NOT NULL,
  type_id BIGINT NOT NULL,
  base_sku VARCHAR(128) NOT NULL,
  origin_link TEXT,
  category_rule_id BIGINT REFERENCES ozon_category_ai_rules(id) ON DELETE SET NULL,
  category_rule_version INTEGER,
  prompt_snapshot TEXT NOT NULL,
  codex_session_id TEXT UNIQUE,
  status VARCHAR(24) NOT NULL DEFAULT 'queued',
  stage VARCHAR(24) NOT NULL DEFAULT 'extract',
  requirement_revision BIGINT NOT NULL DEFAULT 0,
  applied_requirement_revision BIGINT NOT NULL DEFAULT 0,
  copy_auto_approve BOOLEAN NOT NULL DEFAULT FALSE,
  visual_plan_auto_approve BOOLEAN NOT NULL DEFAULT FALSE,
  visual_result_auto_approve BOOLEAN NOT NULL DEFAULT FALSE,
  copy_approved BOOLEAN NOT NULL DEFAULT FALSE,
  visual_plan_approved BOOLEAN NOT NULL DEFAULT FALSE,
  visual_result_approved BOOLEAN NOT NULL DEFAULT FALSE,
  last_reply_json JSONB,
  last_error TEXT,
  admitted_at TIMESTAMPTZ,
  finished_at TIMESTAMPTZ,
  cancel_requested BOOLEAN NOT NULL DEFAULT FALSE,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  CONSTRAINT ck_processing_status CHECK(status IN (
    'queued','dispatching','running','waiting_input','waiting_review',
    'uploading_media','finalizing','completed','failed','cancelled')),
  CONSTRAINT ck_processing_stage CHECK(stage IN ('extract','copy','visual','delivery')),
  CONSTRAINT ck_processing_revisions CHECK (
    requirement_revision>=0 AND applied_requirement_revision>=0 AND
    applied_requirement_revision<=requirement_revision)
);
CREATE INDEX idx_processing_fifo ON ozon_processing_tasks(created_at,id) WHERE status='queued';
CREATE INDEX idx_processing_active ON ozon_processing_tasks(id)
  WHERE status IN ('dispatching','running','waiting_input','waiting_review','uploading_media','finalizing');
CREATE INDEX idx_processing_shop_status ON ozon_processing_tasks(shop_id,status,created_at DESC);
CREATE TRIGGER trg_ozon_processing_tasks_updated_at BEFORE UPDATE ON ozon_processing_tasks
  FOR EACH ROW EXECUTE FUNCTION app_touch_updated_at();

ALTER TABLE ozon_products ADD CONSTRAINT fk_ozon_products_shop
  FOREIGN KEY(shop_id) REFERENCES ozon_shops(id) ON DELETE RESTRICT;
ALTER TABLE ozon_products ADD CONSTRAINT fk_ozon_products_generation_task
  FOREIGN KEY(generation_task_id) REFERENCES ozon_processing_tasks(id) ON DELETE RESTRICT;
ALTER TABLE ozon_products ADD CONSTRAINT fk_ozon_products_pricing_rule
  FOREIGN KEY(pricing_rule_id) REFERENCES ozon_pricing_rules(id) ON DELETE RESTRICT;

CREATE TABLE ozon_processing_turns (
  id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  task_id BIGINT NOT NULL REFERENCES ozon_processing_tasks(id) ON DELETE RESTRICT,
  seq INTEGER NOT NULL CHECK(seq>0),
  run_status VARCHAR(24) NOT NULL DEFAULT 'starting' CHECK (
    run_status IN ('starting','running','succeeded','failed','uncertain')),
  input_max_requirement_revision BIGINT NOT NULL DEFAULT 0,
  output_file TEXT,
  exit_code INTEGER,
  reply_json JSONB,
  error_message TEXT,
  started_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  finished_at TIMESTAMPTZ,
  UNIQUE(task_id,seq)
);
CREATE UNIQUE INDEX uq_processing_one_active_turn ON ozon_processing_turns(task_id)
  WHERE run_status IN ('starting','running');

CREATE TABLE ozon_processing_messages (
  id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  task_id BIGINT NOT NULL REFERENCES ozon_processing_tasks(id) ON DELETE RESTRICT,
  turn_id BIGINT REFERENCES ozon_processing_turns(id) ON DELETE SET NULL,
  role VARCHAR(20) NOT NULL CHECK (role IN ('user','assistant','system')),
  kind VARCHAR(24) NOT NULL CHECK (kind IN (
    'user_requirement','user_input','review_decision','media_uploaded','codex_reply','system_note')),
  content TEXT NOT NULL,
  payload JSONB,
  requirement_revision BIGINT CHECK(requirement_revision IS NULL OR requirement_revision>0),
  delivery_status VARCHAR(20) NOT NULL DEFAULT 'pending' CHECK (
    delivery_status IN ('pending','sent','accepted','uncertain','not_applicable')),
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  processed_at TIMESTAMPTZ
);
CREATE INDEX idx_processing_messages_pending ON ozon_processing_messages(task_id,id)
  WHERE delivery_status='pending' AND role IN ('user','system');

CREATE TABLE ozon_media_assets (
  id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  task_id BIGINT NOT NULL REFERENCES ozon_processing_tasks(id) ON DELETE RESTRICT,
  asset_id VARCHAR(128) NOT NULL,
  sku_ref VARCHAR(128) NOT NULL,
  role VARCHAR(32) NOT NULL,
  local_path TEXT NOT NULL,
  object_key TEXT,
  public_url TEXT,
  mime_type VARCHAR(128),
  upload_status VARCHAR(24) NOT NULL DEFAULT 'pending' CHECK (
    upload_status IN ('pending','uploading','uploaded','failed','superseded')),
  error_message TEXT,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE(task_id,asset_id)
);
CREATE INDEX idx_media_task_status ON ozon_media_assets(task_id,upload_status);
CREATE TRIGGER trg_ozon_media_assets_updated_at BEFORE UPDATE ON ozon_media_assets
  FOR EACH ROW EXECUTE FUNCTION app_touch_updated_at();

-- 独立的交付幂等映射：同一任务内 sku_ref 不得重复交付产品。
CREATE TABLE ozon_processing_deliveries (
  task_id BIGINT NOT NULL REFERENCES ozon_processing_tasks(id) ON DELETE RESTRICT,
  sku_ref VARCHAR(128) NOT NULL,
  product_id BIGINT NOT NULL UNIQUE REFERENCES ozon_products(id) ON DELETE RESTRICT,
  delivered_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  PRIMARY KEY(task_id,sku_ref)
);

CREATE TABLE ozon_import_batches (
  id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  shop_id BIGINT NOT NULL REFERENCES ozon_shops(id) ON DELETE RESTRICT,
  ozon_task_id BIGINT,
  status VARCHAR(24) NOT NULL DEFAULT 'created' CHECK (
    status IN ('created','submitting','processing','completed','partial_failed','failed')),
  request_json JSONB NOT NULL CHECK(jsonb_typeof(request_json)='object'),
  response_json JSONB,
  last_error JSONB,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  submitted_at TIMESTAMPTZ,
  finished_at TIMESTAMPTZ
);
CREATE UNIQUE INDEX uq_ozon_import_task ON ozon_import_batches(shop_id,ozon_task_id)
  WHERE ozon_task_id IS NOT NULL;
CREATE INDEX idx_ozon_import_batches_shop ON ozon_import_batches(shop_id,created_at DESC);

CREATE TABLE ozon_import_batch_items (
  id BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
  batch_id BIGINT NOT NULL REFERENCES ozon_import_batches(id) ON DELETE RESTRICT,
  product_id BIGINT NOT NULL REFERENCES ozon_products(id) ON DELETE RESTRICT,
  offer_id VARCHAR(128) NOT NULL,
  submitted_revision INTEGER NOT NULL CHECK(submitted_revision>=0),
  request_item_json JSONB NOT NULL CHECK(jsonb_typeof(request_item_json)='object'),
  status VARCHAR(24) NOT NULL DEFAULT 'created' CHECK (
    status IN ('created','submitted','processing','imported','failed')),
  ozon_product_id BIGINT,
  error_json JSONB,
  created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE(batch_id,product_id)
);
CREATE INDEX idx_ozon_import_item_product ON ozon_import_batch_items(product_id,created_at DESC);
CREATE TRIGGER trg_ozon_import_batch_items_updated_at BEFORE UPDATE ON ozon_import_batch_items
  FOR EACH ROW EXECUTE FUNCTION app_touch_updated_at();

COMMIT;

-- 工程注意：任务并发上限不是单个表约束，必须由事务性调度器控制。
-- 工程注意：商品字段必须与 api_item_json 同事务更新；校验与业务状态机在 Go 实现。
-- 工程注意：明文 api_key 仅符合用户确认的本地方案，不得出现在日志和 Codex 提示词中。
