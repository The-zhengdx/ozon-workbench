import assert from "node:assert/strict";

import {
  messageBody,
  pricingRuleBody,
  productPatchBody,
  promptBody,
  publishBody,
  reviewBody,
  settingsPatchBody,
  shopCreateBody,
  shopEnableBody,
  taskFormFields,
  storageBody,
  treeSyncBody,
} from "./payloads.ts";

const shop = shopCreateBody({ name: " 俄罗斯帽子店 ", client_id: " 6134344 ", api_key: " secret " });
assert.deepEqual(shop, { name: "俄罗斯帽子店", client_id: "6134344", api_key: "secret" });
assert.deepEqual(Object.keys(shop), ["name", "client_id", "api_key"]);

assert.deepEqual(shopEnableBody(false), { enable: false });
assert.deepEqual(Object.keys(settingsPatchBody({
  max_active_tasks: 3,
  prompt_fallback: "block",
  generic_prompt: "",
  max_infra_retries: 0,
  codex_binary: "codex",
})), ["max_active_tasks", "prompt_fallback", "generic_prompt", "max_infra_retries", "codex_binary"]);
assert.deepEqual(Object.keys(pricingRuleBody({
  name: "默认",
  enabled: true,
  parameters_json: { price_expression: "cost", old_price_expression: "", variables: {}, round_unit: "" },
})), ["name", "enabled", "parameters_json"]);
assert.deepEqual(promptBody("完整提示词", true), { prompt: "完整提示词", enabled: true });
assert.deepEqual(productPatchBody("帽子", "10"), { name: "帽子", price: "10" });
assert.deepEqual(publishBody(1, [2]), { shop_id: 1, ids: [2] });
assert.deepEqual(reviewBody("copy", "approve"), { kind: "copy", decision: "approve", note: "" });
assert.deepEqual(messageBody("补充"), { content: "补充" });
assert.deepEqual(treeSyncBody(1, "ZH_HANS"), { shop_id: 1, language: "ZH_HANS" });
assert.deepEqual(storageBody({
  endpoint: " https://s3.example.com ",
  region: " ru-1 ",
  bucket: " goods ",
  access_key_id: " AKID ",
  secret_access_key: "plain-secret",
  path_style: true,
  public_base_url: " https://cdn.example.com ",
  key_prefix: " ozon ",
}), {
  endpoint: "https://s3.example.com",
  region: "ru-1",
  bucket: "goods",
  access_key_id: "AKID",
  secret_access_key: "plain-secret",
  path_style: true,
  public_base_url: "https://cdn.example.com",
  key_prefix: "ozon",
});
assert.deepEqual(taskFormFields, [
  "shop_id",
  "description_category_id",
  "type_id",
  "base_sku",
  "origin_link",
  "copy_auto_approve",
  "visual_plan_auto_approve",
  "visual_result_auto_approve",
]);

console.log("payload keys ok");
