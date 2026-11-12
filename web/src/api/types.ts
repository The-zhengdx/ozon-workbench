export interface Warehouse {
  warehouse_id: number;
  name: string;
  status?: string;
  is_rfbs: boolean;
  warehouse_type?: string;
}

export interface Shop {
  id: number;
  name: string;
  client_id: string;
  currency_code: string | null;
  api_key_configured: boolean;
  warehouses: Warehouse[];
  warehouses_synced_at: string | null;
  warehouses_mock: boolean;
  is_default: boolean;
  archived: boolean;
  last_tested_at: string | null;
  last_test_ok: boolean | null;
  last_test_message: string | null;
  created_at: string;
  updated_at: string;
}

export interface ConnectionResult {
  ok: boolean;
  message: string;
  warehouse_count: number;
}

export interface ShopWriteResult {
  shop: Shop;
  connection?: ConnectionResult;
}

export interface TreeNode {
  id: number;
  description_category_id: number;
  type_id: number;
  name: string;
  disabled: boolean;
  ancestor_disabled: boolean;
  is_leaf: boolean;
  level: number;
  path: string[];
  children: TreeNode[];
}

export interface CategoryTree {
  language: string;
  source: string;
  source_label: string;
  snapshot_at: string | null;
  type_count: number;
  node_count: number;
  last_error: string;
  last_error_at: string | null;
  shop_id: number | null;
  roots: TreeNode[];
}

export interface AttributeRecord {
  id: number;
  name: string;
  description?: string;
  type?: string;
  is_required?: boolean;
  is_collection?: boolean;
  dictionary_id?: number;
  group_name?: string;
  group_id?: number;
  [key: string]: unknown;
}

export interface AttributeSnapshot {
  description_category_id: number;
  type_id: number;
  language: string;
  version: number;
  source: string;
  source_label: string;
  synced_at: string;
  total: number;
  required_count: number;
  optional_count: number;
  attributes: AttributeRecord[];
}

export interface PromptRecord {
  description_category_id: number;
  type_id: number;
  codex_prompt: string;
  updated_at: string | null;
  saved: boolean;
}

export interface DictValue {
  id: number;
  value: string;
  info?: string;
  picture?: string;
}

export interface DictPage {
  description_category_id: number;
  type_id: number;
  attribute_id: number;
  language: string;
  version: number;
  synced_at: string;
  has_next: boolean;
  last_value_id: number;
  from_cache: boolean;
  values: DictValue[];
  query?: string;
}

export type TaskStatus =
  | "RUNNING"
  | "NEED_INPUT"
  | "PENDING_TEXT_REVIEW"
  | "PENDING_VISUAL_REVIEW"
  | "COMPLETED"
  | "FAILED";

export interface MissingField {
  key: string;
  label: string;
  required: boolean;
  hint?: string;
}

export interface SubSKU {
  offer_id: string;
  specs?: string;
}

export interface WorkbenchTask {
  id: number;
  shop_id: number;
  category_id: number;
  description_category_id: number;
  type_id: number;
  language: string;
  type_name: string;
  type_path: string[];
  master_sku: string;
  supplier_url: string;
  zip_filename: string;
  zip_local_path: string;
  auto_text_approve: boolean;
  auto_visual_approve: boolean;
  text_approved: boolean;
  text_approved_auto: boolean;
  visual_approved: boolean;
  visual_approved_auto: boolean;
  status: TaskStatus;
  codex_session_id: string;
  revision: number;
  missing_fields: MissingField[];
  sub_skus: SubSKU[];
  text_draft: {
    title_ru?: string;
    selling_points?: string[];
    attributes?: Record<string, unknown>;
    sub_skus?: SubSKU[];
  };
  visual_plan: {
    items?: { offer_id?: string; primary_image?: string; notes?: string }[];
  };
  error_message: string;
  created_at: string;
  updated_at: string;
  prompt_template?: string;
  rendered_prompt?: string;
  attributes?: unknown;
}

export interface TaskPage {
  tasks: WorkbenchTask[];
  total: number;
  page: number;
  page_size: number;
}

export interface TaskEvent {
  id: number;
  task_id: number;
  session_id: string;
  seq: number;
  role: string;
  stage: string;
  content: string;
  tool_call_id: string;
  resource_ref: string;
  created_at: string;
}

export interface EventPage {
  events: TaskEvent[];
  has_more: boolean;
}

export interface ApiFailure {
  message: string;
  fields?: Record<string, string>;
  snapshot?: CategoryTree | AttributeSnapshot;
  status?: number;
}
