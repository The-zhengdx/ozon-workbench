<script setup lang="ts">
import { reactive, ref } from "vue";

import { apiError } from "../api/http";
import { archiveShop, createShop, restoreShop, testShop, updateShop } from "../api/workbench";
import type { Shop } from "../api/types";
import { useShopStore } from "../stores/shop";
import { useToastStore } from "../stores/toast";

const shop = useShopStore();
const toast = useToastStore();

const open = ref(false);
const saving = ref(false);
const editing = ref<Shop | null>(null);
const fields = ref<Record<string, string>>({});
const form = reactive({ name: "", client_id: "", api_key: "" });

function startCreate() {
  editing.value = null;
  form.name = "";
  form.client_id = "";
  form.api_key = "";
  fields.value = {};
  open.value = true;
}

function startEdit(row: Shop) {
  editing.value = row;
  form.name = row.name;
  form.client_id = row.client_id;
  form.api_key = "";
  fields.value = {};
  open.value = true;
}

async function save() {
  saving.value = true;
  fields.value = {};
  try {
    const body = { name: form.name, client_id: form.client_id, api_key: form.api_key };
    const result = editing.value ? await updateShop(editing.value.id, body) : await createShop(body);
    shop.replace(result.shop);
    if (!editing.value || !shop.currentShopId) shop.applyCurrent(result.shop.id);
    open.value = false;
    toast.show(result.connection ? result.connection.message : "店铺资料已保存");
  } catch (error) {
    const parsed = apiError(error);
    fields.value = parsed.fields ?? {};
    if (!parsed.fields || !Object.keys(parsed.fields).length) toast.show(parsed.message);
    return Promise.reject(parsed.message);
  } finally {
    saving.value = false;
  }
}

async function test(row: Shop) {
  try {
    const result = await testShop(row.id);
    shop.replace(result.shop);
    toast.show(result.connection?.message || "连接完成");
  } catch (error) {
    toast.show(apiError(error).message);
  }
}

async function useShop(row: Shop) {
  await shop.select(row.id);
  toast.show(`已切换到「${row.name}」`);
}

async function archive(row: Shop) {
  try {
    const updated = await archiveShop(row.id);
    shop.replace(updated);
    toast.show(`已从切换器移除「${row.name}」`);
  } catch (error) {
    toast.show(apiError(error).message);
  }
}

async function restore(row: Shop) {
  try {
    const updated = await restoreShop(row.id);
    shop.replace(updated);
    toast.show(`已恢复「${row.name}」`);
  } catch (error) {
    toast.show(apiError(error).message);
  }
}

function when(value: string | null) {
  if (!value) return "";
  return new Date(value).toLocaleString("zh-CN", { hour12: false });
}
</script>

<template>
  <section class="panel block">
    <div class="between">
      <h3>店铺凭证</h3>
      <a-button type="primary" @click="startCreate">新增店铺</a-button>
    </div>
    <div v-if="shop.loading" class="empty">正在读取店铺…</div>
    <div v-else-if="shop.error" class="error" style="margin-top: 14px">{{ shop.error }}</div>
    <div v-else-if="!shop.shops.length" class="empty">还没有店铺。新增时只需名称、Client ID 和 API Key，币种保持未知。</div>
    <div v-else>
      <div v-for="row in shop.shops" :key="row.id" class="listitem between" style="align-items: flex-start">
        <div class="min-w-0">
          <b>{{ row.name }}</b>
          <div class="sub">
            Client ID {{ row.client_id }} · API Key {{ row.api_key_configured ? "已配置" : "待配置" }}
            · 币种 {{ row.currency_code || "未知" }}
            <template v-if="row.last_tested_at"> · 最近测试 {{ when(row.last_tested_at) }}</template>
          </div>
          <div v-if="row.last_test_message" class="sub" :class="{ error: row.last_test_ok === false }" style="margin-top: 8px">
            {{ row.last_test_message }}
          </div>
          <div v-if="row.warehouses.length" class="chips">
            <span v-for="warehouse in row.warehouses" :key="warehouse.warehouse_id" class="chip">
              {{ warehouse.name }} · {{ warehouse.warehouse_id }}
            </span>
          </div>
          <div v-else class="sub">仓库尚未同步。</div>
        </div>
        <div class="miniActions">
          <a-button size="small" @click="startEdit(row)">编辑凭证</a-button>
          <a-button size="small" @click="test(row)">测试连接</a-button>
          <span v-if="!row.archived && shop.currentShopId === row.id" class="status ready">当前店铺</span>
          <a-button v-else-if="!row.archived" size="small" @click="useShop(row)">切换</a-button>
          <a-button v-if="row.archived" size="small" @click="restore(row)">恢复店铺</a-button>
          <a-popconfirm v-else title="从切换器移除该店铺？可以稍后恢复。" ok-text="移除" cancel-text="取消" @confirm="archive(row)">
            <a-button size="small">移除</a-button>
          </a-popconfirm>
        </div>
      </div>
    </div>
  </section>

  <a-modal
    v-model:open="open"
    :title="editing ? '编辑店铺凭证' : '新增店铺'"
    :confirm-loading="saving"
    ok-text="保存店铺"
    cancel-text="取消"
    @ok="save"
  >
    <div class="stack">
      <label class="field">
        店铺名称
        <a-input v-model:value="form.name" :status="fields.name ? 'error' : ''" />
        <span v-if="fields.name" class="error">{{ fields.name }}</span>
      </label>
      <label class="field">
        Client ID
        <a-input v-model:value="form.client_id" autocomplete="off" :status="fields.client_id ? 'error' : ''" />
        <span v-if="fields.client_id" class="error">{{ fields.client_id }}</span>
      </label>
      <label class="field">
        API Key
        <a-input-password
          v-model:value="form.api_key"
          autocomplete="new-password"
          :placeholder="editing?.api_key_configured ? '留空沿用已有密钥' : '填写店铺 API Key'"
          :status="fields.api_key ? 'error' : ''"
        />
        <span v-if="fields.api_key" class="error">{{ fields.api_key }}</span>
      </label>
      <div class="sub">密钥只在后端加密保存。列表和后续任务只显示「已配置 / 待配置」，编辑时不会回显原值。新增店铺不选择币种，系统也不会预填 RUB 或 CNY。</div>
      <div class="sub">模拟模式下，API Key 填 <span class="mono">invalid</span> 会返回 Ozon 的鉴权错误，其它非空密钥可以同步仓库。</div>
    </div>
  </a-modal>
</template>
