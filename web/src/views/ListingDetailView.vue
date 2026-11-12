<script setup lang="ts">
import { computed, ref, watch } from "vue";
import { useRoute, useRouter } from "vue-router";

import { apiError } from "../api/http";
import { generateListing, getListing, publishListings, refreshListingPlatform, saveListingVersion, updateListingStock, uploadListingMedia } from "../api/workbench";
import type { AttributeRecord, ListingDetail } from "../api/types";
import { useShopStore } from "../stores/shop";
import { useToastStore } from "../stores/toast";

const route = useRoute();
const router = useRouter();
const shop = useShopStore();
const toast = useToastStore();

const detail = ref<ListingDetail | null>(null);
const working = ref<Record<string, any>>({});
const jsonText = ref("");
const jsonError = ref("");
const busy = ref(false);
const stockQty = ref(1);
const warehouseId = ref<number | undefined>(undefined);
let syncing = false;

const id = computed(() => Number(route.params.id));
const contract = computed(() => detail.value?.contract ?? []);
const singleBody = computed(() => JSON.stringify({ items: [working.value] }, null, 2));

function loadFrom(item: Record<string, unknown>) {
  syncing = true;
  working.value = JSON.parse(JSON.stringify(item || {}));
  jsonText.value = JSON.stringify(working.value, null, 2);
  jsonError.value = "";
  syncing = false;
}

async function load() {
  if (!id.value) return;
  try {
    detail.value = await getListing(id.value);
    loadFrom(detail.value.item_json || {});
    const warehouses = shop.current?.warehouses ?? [];
    if (!warehouseId.value && warehouses[0]) warehouseId.value = warehouses[0].warehouse_id;
  } catch (error) {
    toast.show(apiError(error).message);
  }
}

watch(working, () => {
  if (syncing) return;
  jsonText.value = JSON.stringify(working.value, null, 2);
  jsonError.value = "";
}, { deep: true });

watch(() => [id.value, shop.currentShopId], () => void load());

function onJson(text: string) {
  jsonText.value = text;
  try {
    const parsed = JSON.parse(text);
    if (!parsed || typeof parsed !== "object" || Array.isArray(parsed)) {
      jsonError.value = "商品参数必须是 JSON 对象";
      return;
    }
    syncing = true;
    working.value = parsed;
    syncing = false;
    jsonError.value = "";
  } catch {
    jsonError.value = "JSON 还不能解析，表单暂不覆盖";
  }
}

function valuesOf(attr: AttributeRecord) {
  const list = Array.isArray(working.value.attributes) ? working.value.attributes : [];
  const found = list.find((row: any) => Number(row?.id) === Number(attr.id));
  const values = Array.isArray(found?.values) ? found.values : [];
  return values as Record<string, unknown>[];
}

function ensureAttr(attr: AttributeRecord) {
  if (!Array.isArray(working.value.attributes)) working.value.attributes = [];
  let found = working.value.attributes.find((row: any) => Number(row?.id) === Number(attr.id));
  if (!found) {
    found = { complex_id: 0, id: attr.id, values: [] };
    working.value.attributes.push(found);
  }
  if (!Array.isArray(found.values)) found.values = [];
  return found;
}

function textValue(attr: AttributeRecord) {
  const values = valuesOf(attr);
  if (attr.is_collection) return values.map((row) => String(row.value ?? "")).filter(Boolean).join("\n");
  return String(values[0]?.value ?? "");
}

function dictValue(attr: AttributeRecord) {
  const values = valuesOf(attr);
  return values[0]?.dictionary_value_id ? String(values[0].dictionary_value_id) : "";
}

function setText(attr: AttributeRecord, raw: string) {
  const found = ensureAttr(attr);
  if (attr.is_collection) {
    found.values = raw.split(/\n+/).map((line) => line.trim()).filter(Boolean).map((value) => ({ value }));
    return;
  }
  found.values = [{ ...(found.values[0] || {}), value: raw }];
}

function setDict(attr: AttributeRecord, raw: string) {
  const found = ensureAttr(attr);
  const id = Number(raw);
  found.values = [{ dictionary_value_id: Number.isFinite(id) ? id : 0, value: String(found.values?.[0]?.value ?? "") }];
}

function imagesText() {
  const images = working.value.images;
  return Array.isArray(images) ? images.join("\n") : "";
}

function setImages(raw: string) {
  working.value.images = raw.split(/\n+/).map((line) => line.trim()).filter(Boolean);
}

async function save() {
  if (!detail.value || jsonError.value) return;
  busy.value = true;
  try {
    detail.value = await saveListingVersion(detail.value.sku.id, { version: detail.value.version, item_json: working.value });
    loadFrom(detail.value.item_json);
    toast.show(`已保存为 v${detail.value.version}`);
  } catch (error) {
    toast.show(apiError(error).message);
  } finally {
    busy.value = false;
  }
}

async function generate() {
  if (!detail.value) return;
  busy.value = true;
  try {
    if (JSON.stringify(working.value) !== JSON.stringify(detail.value.item_json)) await save();
    detail.value = await generateListing(detail.value.sku.id);
    loadFrom(detail.value.item_json);
    toast.show(detail.value.issues.length ? "已生成草稿，仍有缺项" : "已生成可上架版本");
  } catch (error) {
    toast.show(apiError(error).message);
  } finally {
    busy.value = false;
  }
}

async function upload() {
  if (!detail.value) return;
  busy.value = true;
  try {
    detail.value = await uploadListingMedia(detail.value.sku.id);
    loadFrom(detail.value.item_json);
    toast.show("媒体已换成公网地址");
  } catch (error) {
    toast.show(apiError(error).message);
    await load();
  } finally {
    busy.value = false;
  }
}

async function publish() {
  if (!detail.value?.sku.eligible) return;
  busy.value = true;
  try {
    const plan = await publishListings({
      items: [{ id: detail.value.sku.id, version: detail.value.version }],
      idempotency_key: crypto.randomUUID(),
      execute: true,
    });
    const batch = plan.batch;
    toast.show(batch ? `已提交，导入 ${batch.imported}，失败 ${batch.failed}，待核对 ${batch.unknown}` : "没有可提交的版本");
    await load();
  } catch (error) {
    toast.show(apiError(error).message);
  } finally {
    busy.value = false;
  }
}

async function refreshPlatform() {
  if (!detail.value) return;
  busy.value = true;
  try {
    detail.value = await refreshListingPlatform(detail.value.sku.id);
    toast.show(`平台状态：${detail.value.sku.platform_status}`);
  } catch (error) {
    toast.show(apiError(error).message);
  } finally {
    busy.value = false;
  }
}

async function pushStock() {
  if (!detail.value || !warehouseId.value) return;
  busy.value = true;
  try {
    await updateListingStock(detail.value.sku.id, { warehouse_id: warehouseId.value, stock: stockQty.value });
    toast.show("库存已更新");
    await load();
  } catch (error) {
    toast.show(apiError(error).message);
    await load();
  } finally {
    busy.value = false;
  }
}

async function copy(text: string) {
  await navigator.clipboard.writeText(text);
  toast.show("已复制");
}

void load();
</script>

<template>
  <div v-if="detail" class="stack">
    <div class="between">
      <button class="ghost" type="button" @click="router.push('/listings')">返回列表</button>
      <div class="miniActions">
        <span class="status" :class="detail.sku.publish_status === '待上架' ? 'ready' : detail.sku.publish_status === '已导入' ? 'ok' : detail.sku.publish_status === '导入失败' ? 'bad' : 'warm'">{{ detail.sku.publish_status }}</span>
        <span class="status">{{ detail.sku.platform_status }}</span>
        <span class="sub">v{{ detail.version }} · {{ detail.source || "draft" }}</span>
      </div>
    </div>
    <div v-if="!detail.storage.configured" class="note">{{ detail.storage.message }}</div>
    <div v-if="detail.issues.length" class="error">
      <b>校验未通过</b>
      <div v-for="issue in detail.issues" :key="issue">{{ issue }}</div>
    </div>
    <div v-if="detail.sku.platform_reason || detail.sku.last_error" class="error">
      <b>驳回与失败原因</b>
      <div v-if="detail.sku.last_error">导入：{{ detail.sku.last_error }}</div>
      <div v-if="detail.sku.platform_reason">平台：{{ detail.sku.platform_reason }}</div>
    </div>
    <div class="detail-grid">
      <div class="panel block stack">
        <div class="between">
          <h3>{{ detail.sku.offer_id }}</h3>
          <span class="sub">主 SKU {{ detail.sku.master_sku }} · {{ detail.sku.variant_specs }}</span>
        </div>
        <div class="form-grid">
          <label class="field">俄文标题<input v-model="working.name" /></label>
          <label class="field">售价<input v-model="working.price" /></label>
          <label class="field">币种<input v-model="working.currency_code" maxlength="3" placeholder="如 CNY" /></label>
          <label class="field">主图 HTTPS<input v-model="working.primary_image" /></label>
          <label class="field">长<input v-model="working.depth" /></label>
          <label class="field">宽<input v-model="working.width" /></label>
          <label class="field">高<input v-model="working.height" /></label>
          <label class="field">尺寸单位<input v-model="working.dimension_unit" placeholder="mm" /></label>
          <label class="field">重量<input v-model="working.weight" /></label>
          <label class="field">重量单位<input v-model="working.weight_unit" placeholder="g" /></label>
          <label class="field wide">附加图片，每行一个 HTTPS
            <textarea class="prompt" :value="imagesText()" @input="setImages(($event.target as HTMLTextAreaElement).value)"></textarea>
          </label>
        </div>
        <h3>属性契约</h3>
        <div v-for="attr in contract" :key="attr.id" class="field">
          <span>{{ attr.name }} <span class="sub">#{{ attr.id }}{{ attr.is_required ? " · 必填" : " · 选填" }}{{ attr.dictionary_id ? " · 字典" : "" }}</span></span>
          <input v-if="attr.dictionary_id" :value="dictValue(attr)" placeholder="字典值 ID" @input="setDict(attr, ($event.target as HTMLInputElement).value)" />
          <textarea v-else-if="attr.is_collection" class="prompt" :value="textValue(attr)" @input="setText(attr, ($event.target as HTMLTextAreaElement).value)"></textarea>
          <input v-else :value="textValue(attr)" @input="setText(attr, ($event.target as HTMLInputElement).value)" />
        </div>
        <div v-if="detail.media.length" class="visual-grid">
          <figure v-for="media in detail.media" :key="media.id">
            <img v-if="media.preview" class="thumb-img" :src="media.preview" alt="" />
            <figcaption class="sub">{{ media.role }} · {{ media.status }}<span v-if="media.error"> · {{ media.error }}</span></figcaption>
          </figure>
        </div>
        <div class="miniActions">
          <button class="primary" type="button" :disabled="busy || !!jsonError" @click="save">保存为新版本</button>
          <button class="ghost" type="button" :disabled="busy" @click="generate">生成商品参数</button>
          <button class="ghost" type="button" :disabled="busy" @click="upload">确认并上传媒体</button>
          <button class="ghost" type="button" :disabled="busy || !detail.sku.eligible" @click="publish">{{ detail.sku.publish_status === "已导入" && detail.sku.current_version !== detail.sku.last_imported_version ? "提交更新" : "上架这一条" }}</button>
          <button class="ghost" type="button" :disabled="busy" @click="refreshPlatform">回读平台状态</button>
        </div>
        <div class="panel block stack">
          <h3>分仓库存</h3>
          <p class="sub">使用当前的 POST /v2/products/stocks。同一商品同一仓库至少间隔 30 秒。平台若返回 TOO_MANY_REQUESTS，原因会显示在下面。</p>
          <div class="form-grid">
            <label class="field">仓库
              <select v-model.number="warehouseId">
                <option v-for="item in shop.current?.warehouses ?? []" :key="item.warehouse_id" :value="item.warehouse_id">{{ item.name }} · {{ item.warehouse_id }}</option>
              </select>
            </label>
            <label class="field">数量<input v-model.number="stockQty" type="number" min="0" /></label>
          </div>
          <button class="ghost" type="button" :disabled="busy || !warehouseId" @click="pushStock">更新这个仓库的库存</button>
          <div v-for="row in detail.stocks" :key="row.warehouse_id" class="sub">
            仓库 {{ row.warehouse_id }} · 目标 {{ row.target_qty }} · {{ row.sync_status || "未同步" }} {{ row.sync_message }}
          </div>
        </div>
      </div>
      <div class="panel block stack">
        <div class="between">
          <h3>JSON 预览</h3>
          <div class="miniActions">
            <button class="ghost" type="button" @click="copy(jsonText)">复制 SKU 对象</button>
            <button class="ghost" type="button" @click="copy(singleBody)">复制单条请求体</button>
          </div>
        </div>
        <p class="sub">{{ shop.current?.name }} · {{ detail.sku.offer_id }} · v{{ detail.version }} · {{ detail.issues.length ? "校验未通过" : "校验通过" }}</p>
        <textarea class="prompt json-editor" :value="jsonText" spellcheck="false" @input="onJson(($event.target as HTMLTextAreaElement).value)"></textarea>
        <p v-if="jsonError" class="error">{{ jsonError }}</p>
        <h3>版本</h3>
        <div v-for="rev in detail.revisions" :key="rev.version" class="sub">v{{ rev.version }} · {{ rev.source }} · {{ rev.valid ? "通过" : "未通过" }}</div>
      </div>
    </div>
  </div>
  <div v-else class="empty">正在读取 SKU…</div>
</template>
