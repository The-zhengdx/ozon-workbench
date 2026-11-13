<script setup lang="ts">
import { onMounted, ref } from "vue";

import { http, type Settings } from "../api/client";
import { useShopStore } from "../stores/shop";

const shop = useShopStore();
const settings = ref<Settings | null>(null);
const error = ref("");

onMounted(async () => {
  try {
    settings.value = (await http.get<Settings>("/api/system/settings")).data;
  } catch (err) {
    error.value = err instanceof Error ? err.message : "加载失败";
  }
});
</script>

<template>
  <p v-if="error" class="error">{{ error }}</p>
  <div class="stack">
    <div class="panel block">
      <h3>当前店铺</h3>
      <p v-if="shop.current">{{ shop.current.name }} · {{ shop.current.currency_code }} · VAT {{ shop.current.vat }}</p>
      <p v-else class="muted">还没有选择店铺。新的加工和刊登都会被拒绝。</p>
    </div>
    <div class="panel block" v-if="settings">
      <h3>加工名额</h3>
      <p>上限 {{ settings.max_active_tasks }}，占用 {{ settings.active_count }}，排队 {{ settings.queued_count }}。</p>
      <p class="muted">等待补充或审核的任务继续占用名额。</p>
    </div>
  </div>
</template>
