<script setup lang="ts">
import { computed, ref, watch } from "vue";

import { http } from "../api/client";
import { asList } from "../api/lists";
import { promptBody } from "../api/payloads";
import CategoryTree, { type CategoryNode } from "../components/CategoryTree.vue";
import { useShopStore } from "../stores/shop";

type Attr = { id: number; name: string; type: string; is_required: boolean; dictionary_id: number };
type AttrMeta = { synced?: boolean; fetched_at?: string; language_used?: string; fallback?: boolean; attributes?: Attr[] };

const shop = useShopStore();
const language = ref("ZH_HANS");
const selected = ref<CategoryNode | null>(null);
const attrs = ref<Attr[]>([]);
const meta = ref<AttrMeta>({});
const prompt = ref("");
const version = ref(0);
const enabled = ref(true);
const message = ref("");
const error = ref("");
const shopId = computed(() => shop.currentShopId);
const attributes = computed(() => asList(attrs.value));

async function open(node: CategoryNode) {
  if (!node.leaf || !node.description_category_id || !node.type_id) return;
  selected.value = node;
  error.value = "";
  message.value = "";
  try {
    await Promise.all([loadAttributes(), loadPrompt()]);
  } catch (err) {
    error.value = err instanceof Error ? err.message : "类目加载失败";
  }
}

async function loadAttributes() {
  if (!selected.value?.description_category_id || !selected.value.type_id) return;
  const { data } = await http.get<AttrMeta>(
    `/api/categories/${selected.value.description_category_id}/types/${selected.value.type_id}/attributes`,
    { params: { local: 1, shop_id: shopId.value || undefined, language: language.value } },
  );
  meta.value = data || {};
  attrs.value = asList(data?.attributes);
}

async function loadPrompt() {
  if (!selected.value?.description_category_id || !selected.value.type_id) return;
  try {
    const rule = (await http.get(`/api/category-rules/${selected.value.description_category_id}/${selected.value.type_id}`)).data;
    prompt.value = rule.prompt;
    version.value = rule.version;
    enabled.value = rule.enabled;
  } catch {
    prompt.value = "";
    version.value = 0;
    enabled.value = true;
  }
}

async function syncAttributes() {
  if (!selected.value?.description_category_id || !selected.value.type_id) return;
  error.value = "";
  if (!shopId.value) {
    error.value = "先选择一个启用中的店铺";
    return;
  }
  try {
    const { data } = await http.post<AttrMeta>(
      `/api/categories/${selected.value.description_category_id}/types/${selected.value.type_id}/attributes/sync`,
      { shop_id: shopId.value, language: language.value },
    );
    meta.value = data || {};
    attrs.value = asList(data?.attributes);
    message.value = "节点属性已按当前语言保存";
  } catch (err) {
    error.value = err instanceof Error ? err.message : "属性同步失败";
  }
}

async function save() {
  if (!selected.value?.description_category_id || !selected.value.type_id) return;
  error.value = "";
  try {
    const rule = (await http.put(`/api/category-rules/${selected.value.description_category_id}/${selected.value.type_id}`, promptBody(prompt.value, enabled.value))).data;
    version.value = rule.version;
    message.value = `已保存，版本 ${rule.version}`;
  } catch (err) {
    error.value = err instanceof Error ? err.message : "提示词保存失败";
  }
}

async function copyJSON() {
  try {
    await navigator.clipboard.writeText(JSON.stringify(attributes.value, null, 2));
    message.value = "已复制属性 JSON";
  } catch {
    error.value = "复制失败";
  }
}

async function values(attr: Attr) {
  if (!selected.value?.description_category_id || !selected.value.type_id) return;
  const keyword = window.prompt(`查询属性 ${attr.name} 的字典值`) ?? "";
  error.value = "";
  try {
    const { data } = await http.get(
      `/api/categories/${selected.value.description_category_id}/types/${selected.value.type_id}/attributes/${attr.id}/values`,
      { params: { shop_id: shopId.value, language: language.value, keyword, page: 1, page_size: 50 } },
    );
    window.alert(JSON.stringify(data, null, 2));
  } catch (err) {
    error.value = err instanceof Error ? err.message : "字典查询失败";
  }
}

watch(language, () => {
  if (selected.value) loadAttributes().catch((err: Error) => (error.value = err.message));
});

function syncedText() {
  if (!meta.value.synced) return "未同步";
  return meta.value.fetched_at ? `上次同步 ${meta.value.fetched_at}` : "已同步";
}
</script>

<template>
  <p v-if="error" class="error">{{ error }}</p>
  <p v-if="message" class="success">{{ message }}</p>
  <div class="catalogLayout">
    <div class="panel block">
      <CategoryTree v-model="language" :shop-id="shopId || undefined" @select="open" />
    </div>
    <div class="stack" v-if="selected">
      <div class="panel block stack">
        <h3>{{ selected.category_name }} / {{ selected.type_name }}</h3>
        <p class="mono">description_category_id {{ selected.description_category_id }} · type_id {{ selected.type_id }}</p>
        <div class="between">
          <span>{{ syncedText() }}<template v-if="meta.fallback"> · 显示的是 {{ meta.language_used }}</template></span>
          <button class="primary" type="button" @click="syncAttributes">同步节点属性</button>
        </div>
      </div>
      <form class="panel block stack" @submit.prevent="save">
        <div class="between"><h3>类目提示词</h3><span class="status">版本 {{ version || "未保存" }}</span></div>
        <textarea v-model="prompt" class="prompt" rows="8" placeholder="这一段同时包含文案、属性、成本和视觉要求。系统不会替你写默认内容。"></textarea>
        <label class="switch-line"><input v-model="enabled" type="checkbox" /> 启用</label>
        <button class="primary" type="submit">保存</button>
      </form>
      <div class="panel block stack">
        <div class="between">
          <h3>属性</h3>
          <button class="ghost" type="button" @click="copyJSON">复制属性 JSON</button>
        </div>
        <div class="tablewrap">
          <table>
            <thead><tr><th>属性</th><th>类型</th><th>必填</th><th>字典</th><th></th></tr></thead>
            <tbody>
              <tr v-for="attr in attributes" :key="attr.id">
                <td>{{ attr.name }} <span class="mono">{{ attr.id }}</span></td>
                <td>{{ attr.type || "—" }}</td>
                <td>{{ attr.is_required ? "是" : "否" }}</td>
                <td>{{ attr.dictionary_id || "—" }}</td>
                <td><button v-if="attr.dictionary_id" class="linkish" type="button" @click="values(attr)">查字典</button></td>
              </tr>
            </tbody>
          </table>
          <p v-if="!attributes.length" class="empty">{{ meta.synced ? "这个类型没有属性" : "还没有同步这个节点的属性" }}</p>
        </div>
      </div>
    </div>
    <div v-else class="panel block"><p class="empty">在左侧选择一个类型</p></div>
  </div>
</template>
