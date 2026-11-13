<script setup lang="ts">
import { onMounted, ref } from "vue";
import { useRoute } from "vue-router";

import { http, type Product } from "../api/client";

const route = useRoute();
const row = ref<Product | null>(null);
const name = ref("");
const price = ref("");
const error = ref("");

async function load() {
  row.value = (await http.get<Product>(`/api/products/${route.params.id}`)).data;
  name.value = row.value.name || "";
  price.value = row.value.price || "";
}

onMounted(() => load().catch((err: Error) => (error.value = err.message)));

async function save() {
  error.value = "";
  try {
    row.value = (await http.patch(`/api/products/${route.params.id}`, { name: name.value, price: price.value })).data;
  } catch (err) {
    error.value = err instanceof Error ? err.message : "保存失败";
  }
}

const unsynced = () => row.value && row.value.last_imported_revision != null && row.value.last_imported_revision !== row.value.api_json_revision;
</script>

<template>
  <p v-if="error" class="error">{{ error }}</p>
  <div v-if="row" class="detail-grid">
    <form class="panel block stack" @submit.prevent="save">
      <p v-if="unsynced()" class="note">有未同步修改。上次导入成功的版本仍然保留。</p>
      <label class="field">俄文标题<input v-model="name" /></label>
      <label class="field">售价<input v-model="price" /></label>
      <p class="muted">内部状态 {{ row.publish_status }} · 平台审核 {{ row.ozon_moderation_status || "未回读" }} · 可售 {{ row.ozon_sale_status || "未回读" }}</p>
      <button class="primary" type="submit">保存并递增 JSON 版本</button>
    </form>
    <pre class="code">{{ JSON.stringify(row.api_item_json, null, 2) }}</pre>
  </div>
</template>
