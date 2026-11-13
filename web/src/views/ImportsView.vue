<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";

import { http } from "../api/client";
import { asList } from "../api/lists";
import { useShopStore } from "../stores/shop";

const shop = useShopStore();
const batches = ref<any[]>([]);
const batchRows = computed(() => asList(batches.value));
const items = ref<any[]>([]);
const itemRows = computed(() => asList(items.value));
const current = ref<number | null>(null);
const error = ref("");

async function load() {
  batches.value = asList((await http.get("/api/import-batches", { params: { shop_id: shop.currentShopId || undefined } })).data);
}

async function open(id: number) {
  current.value = id;
  items.value = asList((await http.get(`/api/import-batches/${id}/items`)).data);
}

async function refresh(id: number) {
  error.value = "";
  try {
    await http.post(`/api/import-batches/${id}/refresh`);
    await load();
    await open(id);
  } catch (err) {
    error.value = err instanceof Error ? err.message : "查询失败";
  }
}

onMounted(() => load().catch((err: Error) => (error.value = err.message)));
watch(() => shop.currentShopId, () => load().catch(() => undefined));
</script>

<template>
  <p v-if="error" class="error">{{ error }}</p>
  <div class="panel tablewrap">
    <table>
      <thead><tr><th>批次</th><th>Ozon task</th><th>状态</th><th></th></tr></thead>
      <tbody>
        <tr v-for="batch in batchRows" :key="batch.id">
          <td class="mono">{{ batch.id }}</td>
          <td class="mono">{{ batch.ozon_task_id || "—" }}</td>
          <td>{{ batch.status }}</td>
          <td class="miniActions">
            <button class="ghost" type="button" @click="open(batch.id)">明细</button>
            <button class="ghost" type="button" @click="refresh(batch.id)">查询结果</button>
          </td>
        </tr>
      </tbody>
    </table>
  </div>
  <div v-if="current" class="panel tablewrap" style="margin-top: 14px">
    <table>
      <thead><tr><th>SKU</th><th>提交版本</th><th>结果</th></tr></thead>
      <tbody>
        <tr v-for="item in itemRows" :key="item.id">
          <td class="mono">{{ item.offer_id }}</td>
          <td>{{ item.submitted_revision }}</td>
          <td>{{ item.status }}</td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
