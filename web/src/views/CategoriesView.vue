<script setup lang="ts">
import { computed, ref } from "vue";

import { http } from "../api/client";
import { asList } from "../api/lists";
import { promptBody, treeSyncBody } from "../api/payloads";
import { useShopStore } from "../stores/shop";

type Hit = { description_category_id: number; category_name: string; type_id: number; type_name: string };
type Attr = { id: number; name: string; type: string; is_required: boolean; dictionary_id: number };

const shop = useShopStore();
const language = ref("ZH");
const query = ref("");
const hits = ref<Hit[]>([]);
const results = computed(() => asList(hits.value));
const selected = ref<Hit | null>(null);
const attrs = ref<Attr[]>([]);
const attributes = computed(() => asList(attrs.value));
const prompt = ref("");
const version = ref(0);
const enabled = ref(true);
const message = ref("");
const error = ref("");

const shopId = computed(() => shop.currentShopId);

async function sync() {
  error.value = "";
  if (!shopId.value) {
    error.value = "先选择一个启用中的店铺";
    return;
  }
  try {
    await http.post("/api/categories/tree/sync", treeSyncBody(shopId.value, language.value));
    await search();
    message.value = "类目树已更新";
  } catch (err) {
    error.value = err instanceof Error ? err.message : "同步失败";
  }
}

async function search() {
  const { data } = await http.get<Hit[] | null>("/api/categories/tree", { params: { language: language.value, q: query.value } });
  hits.value = asList(data);
}

async function open(hit: Hit) {
  selected.value = hit;
  error.value = "";
  const params = { shop_id: shopId.value, language: "RU" };
  attrs.value = asList((await http.get<Attr[] | null>(`/api/categories/${hit.description_category_id}/types/${hit.type_id}/attributes`, { params })).data);
  try {
    const rule = (await http.get(`/api/category-rules/${hit.description_category_id}/${hit.type_id}`)).data;
    prompt.value = rule.prompt;
    version.value = rule.version;
    enabled.value = rule.enabled;
  } catch {
    prompt.value = "";
    version.value = 0;
    enabled.value = true;
  }
}

async function save() {
  if (!selected.value) return;
  const rule = (await http.put(`/api/category-rules/${selected.value.description_category_id}/${selected.value.type_id}`, promptBody(prompt.value, enabled.value))).data;
  version.value = rule.version;
  message.value = `已保存，版本 ${rule.version}`;
}

async function values(attr: Attr) {
  if (!selected.value) return;
  const keyword = window.prompt(`查询属性 ${attr.name} 的字典值`) ?? "";
  const { data } = await http.get(`/api/categories/${selected.value.description_category_id}/types/${selected.value.type_id}/attributes/${attr.id}/values`, {
    params: { shop_id: shopId.value, language: "RU", keyword, page: 1, page_size: 50 },
  });
  window.alert(JSON.stringify(data, null, 2));
}
</script>

<template>
  <p v-if="error" class="error">{{ error }}</p>
  <p v-if="message" class="success">{{ message }}</p>
  <div class="tools">
    <select v-model="language"><option value="ZH">中文</option><option value="RU">俄文</option><option value="DEFAULT">DEFAULT</option></select>
    <input v-model="query" placeholder="类目名、类型名或 ID" />
    <button class="ghost" type="button" @click="search">搜索</button>
    <button class="primary" type="button" @click="sync">同步类目树</button>
  </div>
  <div class="catalogLayout">
    <div class="panel block">
      <button v-for="hit in results" :key="`${hit.description_category_id}-${hit.type_id}`" class="ghost" :class="{ on: selected?.type_id === hit.type_id }" type="button" @click="open(hit)">
        {{ hit.category_name }} / {{ hit.type_name }}
        <small class="mono">{{ hit.description_category_id }} / {{ hit.type_id }}</small>
      </button>
      <p v-if="!results.length" class="empty">同步或搜索后显示可售类型</p>
    </div>
    <div class="stack" v-if="selected">
      <div class="panel tablewrap">
        <table>
          <thead><tr><th>属性</th><th>必填</th><th>字典</th><th></th></tr></thead>
          <tbody>
            <tr v-for="attr in attributes" :key="attr.id">
              <td>{{ attr.name }} <span class="mono">{{ attr.id }}</span></td>
              <td>{{ attr.is_required ? "是" : "否" }}</td>
              <td>{{ attr.dictionary_id || "—" }}</td>
              <td><button v-if="attr.dictionary_id" class="linkish" type="button" @click="values(attr)">查字典</button></td>
            </tr>
          </tbody>
        </table>
      </div>
      <form class="panel block stack" @submit.prevent="save">
        <div class="between"><h3>完整提示词</h3><span class="status">版本 {{ version || "未保存" }}</span></div>
        <textarea v-model="prompt" class="prompt" rows="12" placeholder="这一段同时包含文案、属性、成本和视觉要求。系统不会替你写默认内容。"></textarea>
        <label class="switch-line"><input v-model="enabled" type="checkbox" /> 启用</label>
        <button class="primary" type="submit">保存</button>
      </form>
    </div>
  </div>
</template>
