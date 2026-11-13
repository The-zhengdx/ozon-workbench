<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";

import { http, type Settings } from "../api/client";
import { asList } from "../api/lists";
import { pricingRuleBody, settingsPatchBody, storageBody } from "../api/payloads";

const settings = ref<Settings | null>(null);
const rules = computed(() => asList(settings.value?.pricing_rules));
const error = ref("");
const message = ref("");
const form = reactive({
  max_active_tasks: 3,
  prompt_fallback: "block",
  generic_prompt: "",
  max_infra_retries: 0,
  codex_binary: "codex",
  price_expression: "",
  old_price_expression: "",
  variables: "",
  round_unit: "",
  rule_name: "",
});
const storage = reactive({
  endpoint: "",
  region: "",
  bucket: "",
  access_key_id: "",
  secret_access_key: "",
  path_style: true,
  public_base_url: "",
  key_prefix: "",
});

function normalize(data: Settings): Settings {
  return { ...data, pricing_rules: asList(data?.pricing_rules) };
}

async function load() {
  settings.value = normalize((await http.get<Settings>("/api/system/settings")).data);
  form.max_active_tasks = settings.value.max_active_tasks;
  form.prompt_fallback = settings.value.prompt_fallback;
  form.generic_prompt = settings.value.generic_prompt;
  form.max_infra_retries = settings.value.max_infra_retries;
  form.codex_binary = settings.value.codex_binary;
  const saved = settings.value.storage;
  storage.endpoint = saved?.endpoint || "";
  storage.region = saved?.region || "";
  storage.bucket = saved?.bucket || "";
  storage.access_key_id = saved?.access_key_id || "";
  storage.secret_access_key = saved?.secret_access_key || "";
  storage.path_style = saved?.path_style !== false;
  storage.public_base_url = saved?.public_base_url || "";
  storage.key_prefix = saved?.key_prefix || "";
}

onMounted(() => load().catch((err: Error) => (error.value = err.message)));

async function save() {
  error.value = "";
  message.value = "";
  try {
    settings.value = normalize((await http.patch("/api/system/settings", settingsPatchBody({
      max_active_tasks: Number(form.max_active_tasks),
      prompt_fallback: form.prompt_fallback,
      generic_prompt: form.generic_prompt,
      max_infra_retries: Number(form.max_infra_retries),
      codex_binary: form.codex_binary,
    }))).data);
    message.value = "已保存。正在运行的任务不会被强制停止。";
  } catch (err) {
    error.value = err instanceof Error ? err.message : "保存失败";
  }
}

async function saveStorage() {
  error.value = "";
  message.value = "";
  try {
    settings.value = normalize((await http.patch("/api/system/settings", { storage: storageBody(storage) })).data);
    message.value = "S3 配置已保存";
  } catch (err) {
    error.value = err instanceof Error ? err.message : "S3 保存失败";
  }
}

async function testStorage() {
  error.value = "";
  message.value = "";
  try {
    await http.post("/api/system/storage/test", storageBody(storage));
    message.value = "连接成功：测试对象已写入、公开地址可读，并已删除";
  } catch (err) {
    error.value = err instanceof Error ? err.message : "连接失败";
  }
}

async function addRule() {
  error.value = "";
  const variables = Object.fromEntries(
    form.variables.split("\n").map((line) => line.trim()).filter(Boolean).map((line) => {
      const [key, value] = line.split("=");
      return [key.trim(), (value || "").trim()];
    }),
  );
  try {
    await http.post("/api/pricing-rules", pricingRuleBody({
      name: form.rule_name,
      enabled: true,
      parameters_json: {
        price_expression: form.price_expression,
        old_price_expression: form.old_price_expression,
        variables,
        round_unit: form.round_unit,
      },
    }));
    message.value = "定价规则已保存。表达式或变量不完整时，系统不会计算价格。";
    await load();
  } catch (err) {
    error.value = err instanceof Error ? err.message : "规则保存失败";
  }
}
</script>

<template>
  <p v-if="error" class="error">{{ error }}</p>
  <p v-if="message" class="success">{{ message }}</p>
  <div class="stack" v-if="settings">
    <form class="panel block stack" @submit.prevent="save">
      <p>占用 {{ settings.active_count }} / {{ settings.max_active_tasks }}，排队 {{ settings.queued_count }}</p>
      <label class="field">最大活跃任务数<input v-model.number="form.max_active_tasks" type="number" min="1" /></label>
      <label class="field">没有类目提示词时
        <select v-model="form.prompt_fallback"><option value="block">阻断</option><option value="generic">使用通用提示词</option></select>
      </label>
      <label class="field">通用提示词<textarea v-model="form.generic_prompt" class="prompt" rows="5" placeholder="留空。不会预置类目规则。"></textarea></label>
      <p class="muted">可用占位符：<span class="mono" v-pre>{{main_sku}}</span>，创建任务时替换成用户填写的主 SKU。创建时会把通用提示词和类目提示词拼在一起再替换。未列出的 <span class="mono" v-pre>{{名称}}</span> 会原样保留。</p>
      <label class="field">Codex 可执行文件<input v-model="form.codex_binary" /></label>
      <label class="field">基础设施失败后的额外重试次数<input v-model.number="form.max_infra_retries" type="number" min="0" /></label>
      <button class="primary" type="submit">保存系统设置</button>
    </form>
    <form class="panel block stack" @submit.prevent="saveStorage">
      <h3>对象存储</h3>
      <p>{{ settings.oss_provider }} · {{ settings.oss_configured ? "已配置" : "未配置" }}</p>
      <p class="muted">{{ settings.oss_message }}</p>
      <p class="muted">只支持 S3 兼容接口。密钥按明文存在数据库里，默认全部留空，不读取环境变量。</p>
      <label class="field">Endpoint<input v-model="storage.endpoint" placeholder="https://s3.example.com" /></label>
      <label class="field">Region<input v-model="storage.region" placeholder="us-east-1" /></label>
      <label class="field">Bucket<input v-model="storage.bucket" /></label>
      <label class="field">Access Key ID<input v-model="storage.access_key_id" /></label>
      <label class="field">Secret Access Key<input v-model="storage.secret_access_key" type="text" autocomplete="off" /></label>
      <label class="switch-line"><input v-model="storage.path_style" type="checkbox" /> Path-style（关闭则使用 bucket 子域名）</label>
      <label class="field">公开基址 / 自定义域名<input v-model="storage.public_base_url" placeholder="https://cdn.example.com" /></label>
      <label class="field">对象键前缀<input v-model="storage.key_prefix" placeholder="可留空" /></label>
      <div class="row">
        <button class="primary" type="submit">保存对象存储</button>
        <button class="ghost" type="button" @click="testStorage">测试连接</button>
      </div>
    </form>
    <form class="panel block stack" @submit.prevent="addRule">
      <h3>定价公式</h3>
      <p class="muted">可用变量来自下面的清单，另外可以引用 cost 和 price。没有表达式时售价留空。</p>
      <label class="field">名称<input v-model="form.rule_name" required /></label>
      <label class="field">售价表达式<input v-model="form.price_expression" placeholder="cost * rate + fee" /></label>
      <label class="field">划线价表达式<input v-model="form.old_price_expression" placeholder="price * factor" /></label>
      <label class="field">变量，一行一个 key=value<textarea v-model="form.variables" class="prompt" rows="4"></textarea></label>
      <label class="field">取整单位<input v-model="form.round_unit" placeholder="0.01" /></label>
      <button class="primary" type="submit">保存规则</button>
      <ul>
        <li v-for="rule in rules" :key="rule.id">{{ rule.name }} · {{ rule.enabled ? "启用" : "停用" }}</li>
      </ul>
    </form>
  </div>
</template>
