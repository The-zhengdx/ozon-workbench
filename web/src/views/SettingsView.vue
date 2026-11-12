<script setup lang="ts">
import { onMounted, ref } from "vue";

import { apiError } from "../api/http";
import { fetchStorage, testStorage } from "../api/workbench";
import { useShopStore } from "../stores/shop";
import { useToastStore } from "../stores/toast";

const shop = useShopStore();
const toast = useToastStore();
const info = ref<{ provider: string; configured: boolean; message: string } | null>(null);
const busy = ref(false);

async function load() {
  try {
    info.value = await fetchStorage();
    shop.storageProvider = info.value.provider;
    shop.storageConfigured = info.value.configured;
  } catch (error) {
    toast.show(apiError(error).message);
  }
}

async function test() {
  busy.value = true;
  try {
    const result = await testStorage();
    toast.show(result.message || "测试上传成功");
  } catch (error) {
    toast.show(apiError(error).message);
  } finally {
    busy.value = false;
  }
}

onMounted(load);
</script>

<template>
  <div class="panel block stack">
    <div class="between">
      <h3>当前图床</h3>
      <span class="status" :class="info?.configured ? 'ok' : 'warm'">{{ info?.configured ? info?.provider : "未配置" }}</span>
    </div>
    <p>{{ info?.message || "正在读取配置…" }}</p>
    <p class="sub">默认 provider 为空。令牌只放在环境变量 YANDEX_DISK_TOKEN，不要写进仓库。改完配置后需要重启工作台进程。</p>
    <div class="miniActions">
      <button class="primary" type="button" :disabled="busy || !info?.configured" @click="test">测试上传</button>
    </div>
    <pre>storage:
  provider: ""          # 以后改成 yandex
  yandex:
    token: ""           # 留空，用 YANDEX_DISK_TOKEN
    root: ozon-workbench</pre>
  </div>
</template>
