<script setup lang="ts">
import { onErrorCaptured, ref, watch } from "vue";
import { useRoute } from "vue-router";

const route = useRoute();
const failed = ref(false);
const detail = ref("");

watch(
  () => route.fullPath,
  () => {
    failed.value = false;
    detail.value = "";
  },
);

onErrorCaptured((err) => {
  failed.value = true;
  detail.value = err instanceof Error ? err.message : "页面渲染失败";
  return false;
});
</script>

<template>
  <p v-if="failed" class="error">这一页没有显示出来（{{ detail }}）。侧栏和店铺选择仍然可用。</p>
  <slot v-else />
</template>
