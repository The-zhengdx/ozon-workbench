<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";

import { http, type Product } from "../api/client";
import { asList } from "../api/lists";
import { useShopStore } from "../stores/shop";

const shop = useShopStore();
const rows = ref<Product[]>([]);
const products = computed(() => asList(rows.value));
const picked = ref<number[]>([]);
const status = ref("");
const offer = ref("");
const error = ref("");
const message = ref("");

async function load() {
  if (!shop.currentShopId) return;
  rows.value = asList((await http.get<Product[] | null>("/api/products", {
    params: { shop_id: shop.currentShopId, publish_status: status.value, offer_id: offer.value },
  })).data);
}

onMounted(() => load().catch((err: Error) => (error.value = err.message)));
watch(() => shop.currentShopId, () => load().catch(() => undefined));

function toggle(id: number) {
  picked.value = picked.value.includes(id) ? picked.value.filter((item) => item !== id) : [...picked.value, id];
}

async function publish() {
  error.value = "";
  message.value = "";
  try {
    const { data } = await http.post("/api/products/publish", { shop_id: shop.currentShopId, ids: picked.value });
    const batches = asList(data);
    message.value = `已提交 ${batches.length} 个批次。导入受理不等于已上架。`;
    picked.value = [];
    await load();
  } catch (err) {
    error.value = err instanceof Error ? err.message : "刊登失败";
  }
}

function unsynced(row: Product) {
  return row.last_imported_revision != null && row.last_imported_revision !== row.api_json_revision;
}
</script>

<template>
  <p v-if="error" class="error">{{ error }}</p>
  <p v-if="message" class="success">{{ message }}</p>
  <div class="tools">
    <input v-model="offer" placeholder="offer_id" @change="load" />
    <select v-model="status" @change="load">
      <option value="">全部内部状态</option>
      <option value="draft">draft</option>
      <option value="processing">processing</option>
      <option value="imported">imported</option>
      <option value="failed">failed</option>
    </select>
    <button class="primary" type="button" :disabled="!picked.length" @click="publish">刊登所选</button>
  </div>
  <div class="panel tablewrap">
    <table>
      <thead>
        <tr><th></th><th>SKU</th><th>标题</th><th>成本 / 售价 / 划线价</th><th>校验</th><th>内部刊登</th><th>平台审核</th><th>可售</th><th></th></tr>
      </thead>
      <tbody>
        <tr v-for="row in products" :key="row.id">
          <td><input type="checkbox" :checked="picked.includes(row.id)" @change="toggle(row.id)" /></td>
          <td class="mono">{{ row.offer_id || "—" }}<div class="sub">{{ row.base_sku }}</div></td>
          <td>{{ row.name }}<div v-if="unsynced(row)" class="status warm">有未同步修改</div></td>
          <td class="money">{{ row.cost || "—" }} / {{ row.price || "—" }} / {{ row.old_price || "—" }}</td>
          <td>{{ row.validation_status }}</td>
          <td>{{ row.publish_status }}</td>
          <td>{{ row.ozon_moderation_status || "—" }}</td>
          <td>{{ row.ozon_sale_status || "—" }}</td>
          <td><router-link :to="`/products/${row.id}`">JSON</router-link></td>
        </tr>
        <tr v-if="!products.length"><td colspan="9" class="empty">当前店铺没有 SKU</td></tr>
      </tbody>
    </table>
  </div>
</template>
