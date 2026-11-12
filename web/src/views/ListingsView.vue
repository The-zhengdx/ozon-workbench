<script setup lang="ts">
import { computed, onUnmounted, reactive, ref, watch } from "vue";
import { useRouter } from "vue-router";

import { apiError } from "../api/http";
import { getPublishBatch, listListings, publishListings } from "../api/workbench";
import type { ListingRow, PublishPlan } from "../api/types";
import { useShopStore } from "../stores/shop";
import { useToastStore } from "../stores/toast";

const shop = useShopStore();
const toast = useToastStore();
const router = useRouter();

const publishStatuses = ["待完善", "待上架", "提交中", "待核对", "导入失败", "已导入"];
const platformStatuses = ["", "待回读", "审核中", "不可售", "可售"];

const rows = ref<ListingRow[]>([]);
const total = ref(0);
const page = ref(1);
const pageSize = ref(20);
const loading = ref(false);
const mode = ref<"offer" | "master" | "multi">("offer");
const query = ref("");
const publishOn = reactive(new Set<string>());
const platform = ref("");
const picked = ref(new Map<number, number>());
const plan = ref<PublishPlan | null>(null);
const planOpen = ref(false);
const publishing = ref(false);
const chunkIndex = ref(0);
let timer = 0;

const pages = computed(() => Math.max(1, Math.ceil(total.value / pageSize.value)));
const pickedCount = computed(() => picked.value.size);
const pageEligible = computed(() => rows.value.filter((row) => row.eligible));
const pageAllPicked = computed(() => pageEligible.value.length > 0 && pageEligible.value.every((row) => picked.value.has(row.id)));

const placeholder = computed(() => {
  if (mode.value === "multi") return "粘贴多个卖家 SKU，用逗号、空格或换行分隔";
  if (mode.value === "master") return "例如 30，查看同款所有变体";
  return "搜索卖家 SKU，保留前导零";
});

function statusClass(status: string) {
  if (status === "导入失败" || status === "不可售") return "bad";
  if (status === "已导入" || status === "可售") return "ok";
  if (status === "待上架") return "ready";
  if (status === "提交中" || status === "审核中" || status === "待完善" || status === "待核对") return "warm";
  return "";
}

function actionLabel(row: ListingRow) {
  if (row.publish_status === "导入失败") return "重试";
  if (row.publish_status === "已导入" && row.current_version !== row.last_imported_version) return "提交更新";
  return "上架";
}

async function load() {
  if (!shop.currentShopId) {
    rows.value = [];
    total.value = 0;
    return;
  }
  loading.value = true;
  try {
    const data = await listListings({
      mode: mode.value,
      q: query.value,
      publish_status: [...publishOn].join(","),
      platform_status: platform.value,
      page: page.value,
      page_size: pageSize.value,
    });
    rows.value = data.rows ?? [];
    total.value = data.total ?? 0;
  } catch (error) {
    toast.show(apiError(error).message);
  } finally {
    loading.value = false;
  }
}

function resetPick() {
  picked.value = new Map();
}

watch(() => shop.currentShopId, () => {
  page.value = 1;
  resetPick();
  void load();
});

watch([mode, platform, pageSize], () => {
  page.value = 1;
  resetPick();
  void load();
});

function togglePublish(status: string) {
  if (publishOn.has(status)) publishOn.delete(status);
  else publishOn.add(status);
  page.value = 1;
  resetPick();
  void load();
}

function search() {
  page.value = 1;
  resetPick();
  void load();
}

function clearFilters() {
  query.value = "";
  mode.value = "offer";
  platform.value = "";
  publishOn.clear();
  page.value = 1;
  resetPick();
  void load();
}

function toggleRow(row: ListingRow, on: boolean) {
  const next = new Map(picked.value);
  if (on) next.set(row.id, row.current_version);
  else next.delete(row.id);
  picked.value = next;
}

function togglePage(on: boolean) {
  const next = new Map(picked.value);
  for (const row of pageEligible.value) {
    if (on) next.set(row.id, row.current_version);
    else next.delete(row.id);
  }
  picked.value = next;
}

async function openPlan(items: { id: number; version: number }[]) {
  if (!items.length) return;
  publishing.value = true;
  try {
    plan.value = await publishListings({
      items,
      idempotency_key: crypto.randomUUID(),
      execute: false,
    });
    chunkIndex.value = 0;
    planOpen.value = true;
  } catch (error) {
    toast.show(apiError(error).message);
  } finally {
    publishing.value = false;
  }
}

function publishOne(row: ListingRow) {
  void openPlan([{ id: row.id, version: row.current_version }]);
}

function publishPicked() {
  void openPlan([...picked.value].map(([id, version]) => ({ id, version })));
}

async function execute(onlyValid: boolean) {
  if (!plan.value) return;
  const items = plan.value.ready.map((row) => ({ id: row.id, version: row.version }));
  if (!items.length) return;
  publishing.value = true;
  try {
    const result = await publishListings({
      items,
      idempotency_key: crypto.randomUUID(),
      only_valid: onlyValid,
      execute: true,
    });
    plan.value = result;
    if (result.need_confirm && !result.batch) {
      toast.show(`有 ${result.blocked.length} 条需要修改。可以只提交通过的 ${result.ready.length} 条。`);
      return;
    }
    if (result.batch) {
      toast.show(`批次 ${result.batch.id}：已导入 ${result.batch.imported}，失败 ${result.batch.failed}，待核对 ${result.batch.unknown}`);
      resetPick();
      await load();
      watchBatch(result.batch.id);
    }
  } catch (error) {
    toast.show(apiError(error).message);
  } finally {
    publishing.value = false;
  }
}

async function watchBatch(id: number) {
  window.clearInterval(timer);
  timer = window.setInterval(async () => {
    try {
      const batch = await getPublishBatch(id);
      if (plan.value?.batch?.id === id) {
        plan.value = { ...plan.value, batch };
      }
      if (batch.pending === 0) {
        window.clearInterval(timer);
        await load();
      }
    } catch {
      window.clearInterval(timer);
    }
  }, 2000);
}

async function copy(text: string) {
  await navigator.clipboard.writeText(text);
  toast.show("已复制");
}

const previewBody = computed(() => {
  const bodies = plan.value?.bodies ?? [];
  return JSON.stringify(bodies[chunkIndex.value] ?? { items: [] }, null, 2);
});

onUnmounted(() => window.clearInterval(timer));
void load();
</script>

<template>
  <div class="stack">
    <div v-if="!shop.storageConfigured" class="note">
      图床未配置。Ozon 只接受公网 HTTPS 图片和视频。可以先在详情里填写已经公开的地址，或到
      <router-link to="/settings">图床设置</router-link>
      填入 Yandex Disk 令牌后再上传本地文件。
    </div>
    <div class="panel block">
      <div class="tools">
        <div class="searchgrp">
          <select v-model="mode" aria-label="SKU 搜索方式">
            <option value="offer">卖家 SKU</option>
            <option value="master">主 SKU</option>
            <option value="multi">批量精确</option>
          </select>
          <input v-model="query" type="search" :placeholder="placeholder" aria-label="搜索 SKU" @keydown.enter="search" />
        </div>
        <button class="ghost" type="button" @click="search">搜索</button>
        <details class="pop">
          <summary class="ghost">发布状态<span v-if="publishOn.size"> · {{ publishOn.size }}</span></summary>
          <div class="popbox">
            <label v-for="status in publishStatuses" :key="status">
              <input type="checkbox" :checked="publishOn.has(status)" @change="togglePublish(status)" />
              {{ status }}
            </label>
          </div>
        </details>
        <select v-model="platform" aria-label="平台状态">
          <option v-for="status in platformStatuses" :key="status" :value="status">{{ status || "全部平台状态" }}</option>
        </select>
        <button class="ghost" type="button" @click="clearFilters">重置筛选</button>
        <button class="primary" type="button" :disabled="!pickedCount || publishing" @click="publishPicked">
          批量上架{{ pickedCount ? `（${pickedCount}）` : "" }}
        </button>
      </div>
      <div class="tablewrap">
        <table>
          <thead>
            <tr>
              <th><input type="checkbox" aria-label="选择本页可操作 SKU" :disabled="!pageEligible.length" :checked="pageAllPicked" @change="togglePage(($event.target as HTMLInputElement).checked)" /></th>
              <th>卖家 SKU</th>
              <th>商品 / 变体</th>
              <th>售价</th>
              <th>发布状态</th>
              <th>平台状态</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="row in rows" :key="row.id" :class="{ picked: picked.has(row.id) }">
              <td>
                <input type="checkbox" :aria-label="`选择 ${row.offer_id}`" :disabled="!row.eligible" :checked="picked.has(row.id)" @change="toggleRow(row, ($event.target as HTMLInputElement).checked)" />
              </td>
              <td>
                <span class="mono">{{ row.offer_id }}</span>
                <div class="sub">主 SKU {{ row.master_sku }}</div>
              </td>
              <td>
                <div class="product">
                  <img v-if="row.preview_url" class="thumb-img" :src="row.preview_url" alt="" />
                  <span v-else class="thumb-img placeholder"></span>
                  <div>
                    <div class="name">{{ row.title || "未命名" }}</div>
                    <div class="sub">{{ row.category_label || "未分类" }}<span v-if="row.variant_specs"> · {{ row.variant_specs }}</span></div>
                    <a v-if="row.supplier_url" :href="row.supplier_url" target="_blank" rel="noreferrer">1688 链接</a>
                  </div>
                </div>
              </td>
              <td class="money">{{ row.currency_code || "币种未填" }} {{ row.price || "—" }}</td>
              <td>
                <span class="status" :class="statusClass(row.publish_status)">{{ row.publish_status }}</span>
                <div class="sub">版本 v{{ row.current_version }}<span v-if="row.last_imported_version"> · 已导入 v{{ row.last_imported_version }}</span></div>
                <div v-if="row.last_error" class="sub reject" :title="row.last_error">{{ row.last_error }}</div>
              </td>
              <td>
                <span class="status" :class="statusClass(row.platform_status)">{{ row.platform_status }}</span>
                <div v-if="row.platform_reason" class="sub reject">{{ row.platform_reason }}</div>
                <div v-if="row.ozon_sku" class="sub">Ozon SKU {{ row.ozon_sku }}</div>
              </td>
              <td>
                <div class="miniActions">
                  <button class="linkish" type="button" @click="router.push(`/listings/${row.id}`)">详情</button>
                  <button class="ghost" type="button" :disabled="!row.eligible || publishing" @click="publishOne(row)">{{ actionLabel(row) }}</button>
                </div>
              </td>
            </tr>
          </tbody>
        </table>
        <div v-if="!loading && !rows.length" class="empty">没有符合条件的 SKU。<button class="linkish" type="button" @click="clearFilters">清除筛选</button></div>
      </div>
      <div class="foot">
        <span>共 {{ total }} 条 · 已选 {{ pickedCount }} 条</span>
        <div class="row">
          <select v-model.number="pageSize" aria-label="每页数量">
            <option :value="10">10 条 / 页</option>
            <option :value="20">20 条 / 页</option>
            <option :value="50">50 条 / 页</option>
            <option :value="100">100 条 / 页</option>
          </select>
          <button class="ghost" type="button" :disabled="page <= 1" @click="page--; load()">上一页</button>
          <span>{{ page }} / {{ pages }}</span>
          <button class="ghost" type="button" :disabled="page >= pages" @click="page++; load()">下一页</button>
        </div>
      </div>
    </div>

    <div v-if="planOpen && plan" class="modal-back" @click.self="planOpen = false">
      <div class="modal panel" role="dialog" aria-label="上架请求预览">
        <div class="between">
          <h3>上架请求预览</h3>
          <button class="ghost" type="button" @click="planOpen = false">关闭</button>
        </div>
        <div class="meta">
          <b>{{ shop.current?.name }}</b>
          <span>通过 {{ plan.ready.length }} 个</span>
          <span>需修改 {{ plan.blocked.length }} 个</span>
          <span>{{ plan.chunks }} 个请求</span>
        </div>
        <div v-if="plan.blocked.length" class="error">
          <b>需修改 {{ plan.blocked.length }} 项</b>
          <div v-for="row in plan.blocked" :key="row.id">{{ row.offer_id || row.id }} · {{ row.errors.join("；") }}</div>
        </div>
        <div v-if="plan.batch" class="success">
          批次 {{ plan.batch.id }} · 已导入 {{ plan.batch.imported }} · 失败 {{ plan.batch.failed }} · 处理中 {{ plan.batch.pending }} · 待核对 {{ plan.batch.unknown }}
          <div v-for="req in plan.batch.requests" :key="req.id" class="sub">
            第 {{ req.chunk + 1 }} 块 · task {{ req.task_id || "—" }} · {{ req.status }}
            <div v-for="item in req.items" :key="item.offer_id">
              {{ item.offer_id }} · {{ item.result }}
              <span v-if="item.error_detail"> · {{ item.error_code }} {{ item.error_detail }}</span>
            </div>
          </div>
        </div>
        <div v-if="plan.bodies.length" class="stack">
          <div class="between">
            <code>POST /v3/product/import</code>
            <button class="ghost" type="button" @click="copy(previewBody)">复制本次请求体</button>
          </div>
          <label v-if="plan.bodies.length > 1" class="field">请求分块
            <select v-model.number="chunkIndex">
              <option v-for="(_, index) in plan.bodies" :key="index" :value="index">第 {{ index + 1 }} 块</option>
            </select>
          </label>
          <pre>{{ previewBody }}</pre>
        </div>
        <div class="miniActions">
          <button v-if="plan.blocked.length && plan.ready.length" class="primary" type="button" :disabled="publishing" @click="execute(true)">仅提交通过的 {{ plan.ready.length }} 条</button>
          <button v-else class="primary" type="button" :disabled="publishing || !plan.ready.length" @click="execute(false)">提交 {{ plan.ready.length }} 条</button>
        </div>
      </div>
    </div>
  </div>
</template>
