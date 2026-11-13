<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";

import { http, type Shop } from "../api/client";
import { asList } from "../api/lists";
import { shopCreateBody, shopEnableBody } from "../api/payloads";
import { useShopStore } from "../stores/shop";

const shop = useShopStore();
const shops = computed(() => asList(shop.list));
const form = reactive({ name: "", client_id: "", api_key: "" });
const error = ref("");

onMounted(() => shop.load().catch((err: Error) => (error.value = err.message)));

async function create() {
  error.value = "";
  try {
    await http.post("/api/shops", shopCreateBody(form));
    form.name = "";
    form.client_id = "";
    form.api_key = "";
    await shop.load();
  } catch (err) {
    error.value = err instanceof Error ? err.message : "保存失败";
  }
}

async function toggle(item: Shop) {
  await http.patch(`/api/shops/${item.id}`, shopEnableBody(!item.enable));
  await shop.load();
}
</script>

<template>
  <p v-if="error" class="error">{{ error }}</p>
  <div class="stack">
    <form class="panel block form-grid" @submit.prevent="create">
      <label class="field">名称<input v-model="form.name" required /></label>
      <label class="field">Client ID<input v-model="form.client_id" required /></label>
      <label class="field wide">API Key<input v-model="form.api_key" required type="password" /></label>
      <div class="wide"><button class="primary" type="submit">添加店铺</button></div>
      <p class="muted wide">币种默认 CNY，VAT 默认 0.1，启用默认打开。这些可以之后修改，不会增加第 10 个字段。</p>
    </form>
    <div class="panel tablewrap">
      <table>
        <thead>
          <tr><th>名称</th><th>Client ID</th><th>API Key</th><th>币种</th><th>VAT</th><th>状态</th><th></th></tr>
        </thead>
        <tbody>
          <tr v-for="item in shops" :key="item.id">
            <td>{{ item.name }}</td>
            <td class="mono">{{ item.client_id }}</td>
            <td class="mono">{{ item.api_key }}</td>
            <td>{{ item.currency_code }}</td>
            <td>{{ item.vat }}</td>
            <td><span class="status" :class="item.enable ? 'ok' : 'bad'">{{ item.enable ? "启用" : "停用" }}</span></td>
            <td><button class="ghost" type="button" @click="toggle(item)">{{ item.enable ? "停用" : "启用" }}</button></td>
          </tr>
          <tr v-if="!shops.length"><td colspan="7" class="empty">还没有店铺</td></tr>
        </tbody>
      </table>
    </div>
  </div>
</template>
