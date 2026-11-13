import axios from "axios";

export const http = axios.create();

http.interceptors.response.use(
  (response) => response,
  (error) => {
    const message = error?.response?.data?.message || error.message || "请求失败";
    return Promise.reject(new Error(message));
  },
);

export type Shop = {
  id: number;
  name: string;
  client_id: string;
  api_key?: string;
  currency_code: string;
  vat: string;
  enable: boolean;
  created_at: string;
  updated_at: string;
};

export type Task = {
  id: number;
  shop_id: number;
  description_category_id: number;
  type_id: number;
  base_sku: string;
  origin_link?: string;
  status: string;
  stage: string;
  queue_position?: number;
  holds_slot: boolean;
  last_error?: string;
  created_at: string;
  admitted_at?: string;
  copy_auto_approve: boolean;
  visual_plan_auto_approve: boolean;
  visual_result_auto_approve: boolean;
};

export type Product = {
  id: number;
  shop_id: number;
  base_sku: string;
  offer_id?: string;
  name?: string;
  price?: string;
  old_price?: string;
  cost?: string;
  primary_image?: string;
  publish_status: string;
  validation_status: string;
  validation_errors: unknown;
  ozon_moderation_status?: string;
  ozon_sale_status?: string;
  api_item_json?: unknown;
  api_json_revision: number;
  last_imported_revision?: number;
  description_category_id: number;
  type_id: number;
  updated_at: string;
};

export type Settings = {
  max_active_tasks: number;
  active_count: number;
  queued_count: number;
  prompt_fallback: string;
  generic_prompt: string;
  review_defaults: { copy: boolean; visual_plan: boolean; visual_result: boolean };
  max_infra_retries: number;
  codex_binary: string;
  oss_provider: string;
  oss_configured: boolean;
  oss_message: string;
  storage?: {
    endpoint: string;
    region: string;
    bucket: string;
    access_key_id: string;
    secret_access_key: string;
    path_style: boolean;
    public_base_url: string;
    key_prefix: string;
  };
  pricing_rules: { id: number; name: string; enabled: boolean; parameters_json: unknown }[];
  active_pricing_rule_id?: number;
};
