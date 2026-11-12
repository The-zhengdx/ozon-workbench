<script lang="ts">
export default { name: "CategoryTree" };
</script>
<script setup lang="ts">
import { computed } from "vue";

import type { TreeNode } from "../api/types";

const props = defineProps<{
  nodes: TreeNode[];
  query: string;
  expanded: Set<number>;
  selectedKey: string;
  showEmpty?: boolean;
}>();

const emit = defineEmits<{
  toggle: [id: number];
  choose: [node: TreeNode];
}>();

const term = computed(() => props.query.trim().toLowerCase());

function blob(node: TreeNode) {
  return [...node.path, node.name, String(node.description_category_id), node.type_id ? String(node.type_id) : ""]
    .join(" ")
    .toLowerCase();
}

const matchMemo = computed(() => {
  const memo = new Map<number, boolean>();
  const visit = (node: TreeNode): boolean => {
    if (memo.has(node.id)) return memo.get(node.id)!;
    const self = blob(node).includes(term.value);
    const child = node.children.some(visit);
    const ok = !term.value || self || child;
    memo.set(node.id, ok);
    return ok;
  };
  props.nodes.forEach(visit);
  return memo;
});

function shown(node: TreeNode) {
  return matchMemo.value.get(node.id) !== false;
}

function opened(node: TreeNode) {
  return !!term.value || props.expanded.has(node.id);
}

function leafKey(node: TreeNode) {
  return `${node.description_category_id}:${node.type_id}`;
}

function blocked(node: TreeNode) {
  return node.disabled || node.ancestor_disabled;
}
</script>

<template>
  <ul class="catTree">
    <template v-for="node in nodes" :key="node.id">
      <li v-if="shown(node)">
        <button
          v-if="node.children.length"
          type="button"
          :aria-expanded="opened(node)"
          :class="{ off: blocked(node) }"
          @click="emit('toggle', node.id)"
        >
          {{ opened(node) ? "▾" : "▸" }} {{ node.name }}<template v-if="blocked(node)"> · 停用</template>
        </button>
        <button
          v-else
          type="button"
          :class="{ selected: leafKey(node) === selectedKey, off: blocked(node) }"
          :aria-pressed="leafKey(node) === selectedKey"
          @click="emit('choose', node)"
        >
          {{ node.name }}<template v-if="blocked(node)"> · 停用</template>
        </button>
        <CategoryTree
          v-if="node.children.length && opened(node)"
          :nodes="node.children"
          :query="query"
          :expanded="expanded"
          :selected-key="selectedKey"
          @toggle="emit('toggle', $event)"
          @choose="emit('choose', $event)"
        />
      </li>
    </template>
    <li v-if="showEmpty && term && !nodes.some(shown)" class="muted">未找到匹配节点。试试商品类型 ID。</li>
  </ul>
</template>
