export type ShopCreateBody = {
  name: string;
  client_id: string;
  api_key: string;
};

export function shopCreateBody(input: ShopCreateBody): ShopCreateBody {
  return {
    name: input.name.trim(),
    client_id: input.client_id.trim(),
    api_key: input.api_key.trim(),
  };
}

export type ShopEnableBody = { enable: boolean };

export function shopEnableBody(enable: boolean): ShopEnableBody {
  return { enable };
}

export type SettingsPatchBody = {
  max_active_tasks: number;
  prompt_fallback: string;
  generic_prompt: string;
  max_infra_retries: number;
  codex_binary: string;
};

export function settingsPatchBody(input: SettingsPatchBody): SettingsPatchBody {
  return {
    max_active_tasks: input.max_active_tasks,
    prompt_fallback: input.prompt_fallback,
    generic_prompt: input.generic_prompt,
    max_infra_retries: input.max_infra_retries,
    codex_binary: input.codex_binary,
  };
}

export type PricingRuleBody = {
  name: string;
  enabled: boolean;
  parameters_json: {
    price_expression: string;
    old_price_expression: string;
    variables: Record<string, string>;
    round_unit: string;
  };
};

export function pricingRuleBody(input: PricingRuleBody): PricingRuleBody {
  return {
    name: input.name,
    enabled: input.enabled,
    parameters_json: input.parameters_json,
  };
}

export type PromptBody = { prompt: string; enabled: boolean };

export function promptBody(prompt: string, enabled: boolean): PromptBody {
  return { prompt, enabled };
}

export type ProductPatchBody = { name: string; price: string };

export function productPatchBody(name: string, price: string): ProductPatchBody {
  return { name, price };
}

export type PublishBody = { shop_id: number; ids: number[] };

export function publishBody(shopId: number, ids: number[]): PublishBody {
  return { shop_id: shopId, ids };
}

export type ReviewBody = { kind: string; decision: string; note: string };

export function reviewBody(kind: string, decision: string): ReviewBody {
  return { kind, decision, note: "" };
}

export type MessageBody = { content: string };

export function messageBody(content: string): MessageBody {
  return { content };
}

export type TreeSyncBody = { shop_id: number; language: string };

export function treeSyncBody(shopId: number, language: string): TreeSyncBody {
  return { shop_id: shopId, language };
}

export const taskFormFields = [
  "shop_id",
  "description_category_id",
  "type_id",
  "base_sku",
  "origin_link",
  "copy_auto_approve",
  "visual_plan_auto_approve",
  "visual_result_auto_approve",
] as const;
