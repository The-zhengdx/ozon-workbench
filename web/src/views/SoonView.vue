<script setup lang="ts">
defineProps<{ phase: string; detail: string }>();
</script>

<template>
  <section class="panel block">
    <h3>{{ phase }}</h3>
    <p class="sub">{{ detail }}</p>
    <div class="note" style="margin-top: 14px">当前可用的是店铺凭证和类目提示词。顶部切换器会记住当前店铺，并通过 X-Shop-Id 传给后续会读取凭证的请求。</div>
  </section>
</template>
