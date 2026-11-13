<script setup lang="ts">
import { computed, ref, watch } from "vue";

import { http } from "../api/client";
import { treeSyncBody } from "../api/payloads";

export type CategoryNode = {
  key: string;
  title: string;
  description_category_id?: number;
  category_name?: string;
  type_id?: number;
  type_name?: string;
  leaf?: boolean;
  children?: CategoryNode[];
};

const language = defineModel<string>({ default: "ZH_HANS" });
const props = defineProps<{ shopId?: number }>();
const emit = defineEmits<{ select: [node: CategoryNode] }>();

const nodes = ref<CategoryNode[]>([]);
const query = ref("");
const expandedKeys = ref<string[]>([]);
const selectedKeys = ref<string[]>([]);
const error = ref("");
let loadTicket = 0;
const fallback = ref(false);
const languageUsed = ref("");

type TreeRow = CategoryNode & { selectable: boolean; isLeaf: boolean; children?: TreeRow[] };

function toRows(list: CategoryNode[]): TreeRow[] {
  return list.map((node) => ({
    ...node,
    selectable: !!node.leaf,
    isLeaf: !!node.leaf,
    children: node.children?.length ? toRows(node.children) : undefined,
  }));
}

function filterRows(list: TreeRow[], needle: string): { rows: TreeRow[]; keys: string[] } {
  const keys: string[] = [];
  const walk = (items: TreeRow[]): TreeRow[] => {
    const out: TreeRow[] = [];
    for (const item of items) {
      const children = walk(item.children ?? []);
      const blob = `${item.title} ${item.category_name ?? ""} ${item.type_name ?? ""} ${item.description_category_id ?? ""} ${item.type_id ?? ""}`.toLowerCase();
      if (children.length || blob.includes(needle)) {
        if (children.length) keys.push(item.key);
        out.push({ ...item, children: children.length ? children : undefined });
      }
    }
    return out;
  };
  return { rows: walk(list), keys };
}

const visible = computed(() => {
  const rows = toRows(nodes.value);
  const needle = query.value.trim().toLowerCase();
  if (!needle) return { rows, keys: [] as string[] };
  return filterRows(rows, needle);
});

watch(
  () => query.value.trim(),
  (value) => {
    expandedKeys.value = value ? visible.value.keys : [];
  },
);

async function reload() {
  const ticket = ++loadTicket;
  try {
    const { data } = await http.get("/api/categories/tree", { params: { language: language.value } });
    if (ticket !== loadTicket) return;
    nodes.value = Array.isArray(data?.nodes) ? data.nodes : [];
    fallback.value = !!data?.fallback;
    languageUsed.value = data?.language_used || language.value;
    error.value = "";
  } catch (err) {
    if (ticket !== loadTicket) return;
    nodes.value = [];
    error.value = err instanceof Error ? err.message : "类目树还没有同步";
  }
}

async function sync() {
  error.value = "";
  if (!props.shopId) {
    error.value = "先选择一个启用中的店铺";
    return;
  }
  try {
    await http.post("/api/categories/tree/sync", treeSyncBody(props.shopId, language.value));
    await reload();
  } catch (err) {
    error.value = err instanceof Error ? err.message : "同步失败";
  }
}

function onSelect(keys: string[], info: { node?: CategoryNode & { payload?: CategoryNode; dataRef?: CategoryNode } }) {
  const node = info.node?.payload || info.node?.dataRef || info.node;
  if (!node?.leaf || !node.description_category_id || !node.type_id) return;
  selectedKeys.value = keys;
  emit("select", node);
}

watch(language, () => reload(), { immediate: true });
defineExpose({ reload });
</script>

<template>
  <div class="stack">
    <div class="tools">
      <select v-model="language">
        <option value="ZH_HANS">中文</option>
        <option value="EN">English</option>
        <option value="RU">Русский</option>
      </select>
      <input v-model="query" placeholder="搜索类目、类型或 ID" />
      <button class="primary" type="button" @click="sync">同步类目树</button>
    </div>
    <p v-if="error" class="error">{{ error }}</p>
    <p v-if="fallback" class="muted">当前语言还没有缓存，暂时显示 {{ languageUsed }}。</p>
    <div class="treePane">
      <a-tree
        v-if="visible.rows.length"
        v-model:expandedKeys="expandedKeys"
        v-model:selectedKeys="selectedKeys"
        block-node
        virtual
        :height="440"
        :tree-data="visible.rows.map((row) => ({ ...row, payload: row }))"
        @select="onSelect"
      />
      <p v-else class="empty">同步后按类目展开，只有类型叶子可以选中</p>
    </div>
  </div>
</template>
