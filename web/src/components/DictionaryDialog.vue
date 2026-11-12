<script setup lang="ts">
import { ref, watch } from "vue";

import { apiError } from "../api/http";
import { fetchDictValues, searchDictValues } from "../api/workbench";
import type { AttributeRecord, DictPage } from "../api/types";

const props = defineProps<{
  open: boolean;
  attribute: AttributeRecord | null;
  categoryId: number;
  typeId: number;
  language: string;
}>();
const emit = defineEmits<{ "update:open": [boolean] }>();

const page = ref<DictPage | null>(null);
const query = ref("");
const loading = ref(false);
const error = ref("");

watch(
  () => props.open,
  (value) => {
    if (value) {
      page.value = null;
      error.value = "";
      void load(false);
    }
  },
);

async function load(refresh: boolean) {
  if (!props.attribute) return;
  query.value = "";
  loading.value = true;
  error.value = "";
  try {
    page.value = await fetchDictValues({
      description_category_id: props.categoryId,
      type_id: props.typeId,
      attribute_id: Number(props.attribute.id),
      language: props.language,
      limit: 100,
      refresh,
    });
  } catch (err) {
    error.value = apiError(err).message;
  } finally {
    loading.value = false;
  }
}

async function more() {
  if (!props.attribute || !page.value?.has_next) return;
  loading.value = true;
  error.value = "";
  try {
    page.value = await fetchDictValues({
      description_category_id: props.categoryId,
      type_id: props.typeId,
      attribute_id: Number(props.attribute.id),
      language: props.language,
      limit: 100,
      last_value_id: page.value.last_value_id,
      refresh: true,
    });
  } catch (err) {
    error.value = apiError(err).message;
  } finally {
    loading.value = false;
  }
}

async function search() {
  if (!props.attribute) return;
  loading.value = true;
  error.value = "";
  try {
    page.value = await searchDictValues({
      description_category_id: props.categoryId,
      type_id: props.typeId,
      attribute_id: Number(props.attribute.id),
      language: props.language,
      value: query.value,
      limit: 20,
      refresh: true,
    });
  } catch (err) {
    error.value = apiError(err).message;
  } finally {
    loading.value = false;
  }
}
</script>

<template>
  <a-modal :open="open" :title="attribute ? `字典 · ${attribute.name}` : '字典'" :footer="null" width="720px" @cancel="emit('update:open', false)">
    <div class="stack">
      <div class="sub">按属性分页调用 /v1/description-category/attribute/values，搜索走 /values/search。缓存按类目、类型、属性和语言保存，并记录版本与时间。</div>
      <div class="filters">
        <a-input v-model:value="query" class="grow" placeholder="搜索字典值，至少 2 个字符" @press-enter="search" />
        <a-button @click="search">搜索</a-button>
        <a-button @click="load(true)">重新拉取</a-button>
      </div>
      <div v-if="error" class="error">{{ error }}</div>
      <div v-if="loading && !page" class="empty">正在读取字典…</div>
      <div v-else-if="page" class="tablewrap">
        <div class="sub">
          {{ page.values.length }} 条
          <template v-if="page.from_cache"> · 来自缓存 v{{ page.version }}</template>
          <template v-else> · 版本 v{{ page.version }}</template>
          · {{ new Date(page.synced_at).toLocaleString("zh-CN", { hour12: false }) }}
        </div>
        <table>
          <thead>
            <tr><th>ID</th><th>值</th><th>说明</th></tr>
          </thead>
          <tbody>
            <tr v-for="item in page.values" :key="item.id">
              <td class="mono">{{ item.id }}</td>
              <td>{{ item.value }}</td>
              <td>{{ item.info || "—" }}</td>
            </tr>
            <tr v-if="!page.values.length"><td colspan="3" class="empty">没有匹配的字典值。</td></tr>
          </tbody>
        </table>
        <div style="margin-top: 10px">
          <a-button v-if="page.has_next && !query" :loading="loading" @click="more">加载更多</a-button>
        </div>
      </div>
    </div>
  </a-modal>
</template>
