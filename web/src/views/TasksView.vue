<script setup lang="ts">
import { computed, onMounted, onUnmounted, reactive, ref, watch } from "vue";

import { apiError } from "../api/http";
import {
  approveTask,
  createTask,
  fetchPrompt,
  fetchTree,
  followUpTask,
  getTask,
  listTaskEvents,
  listTasks,
  submitTaskInput,
  updateTaskPolicy,
} from "../api/workbench";
import type { TaskEvent, TaskStatus, TreeNode, WorkbenchTask } from "../api/types";
import { useShopStore } from "../stores/shop";
import { useToastStore } from "../stores/toast";

const shop = useShopStore();
const toast = useToastStore();

const rows = ref<WorkbenchTask[]>([]);
const loading = ref(false);
const statusFilter = ref<"" | TaskStatus>("");
const busy = ref<Record<number, boolean>>({});
const creating = ref(false);
const uploading = ref(false);
const progress = ref(0);
const fields = ref<Record<string, string>>({});
const promptState = ref("");
const leaves = ref<{ key: string; label: string; cid: number; tid: number; disabled: boolean }[]>([]);
const file = ref<File | null>(null);
const form = reactive({
  master_sku: "",
  supplier_url: "",
  typeKey: "",
  auto_text_approve: false,
  auto_visual_approve: false,
});

const drawerOpen = ref(false);
const detail = ref<WorkbenchTask | null>(null);
const events = ref<TaskEvent[]>([]);
const hasMore = ref(false);
const answers = reactive<Record<string, string>>({});
const followUp = ref("");
const acting = ref(false);

const filters: { value: "" | TaskStatus; label: string }[] = [
  { value: "", label: "全部" },
  { value: "RUNNING", label: "运行中" },
  { value: "NEED_INPUT", label: "待补充" },
  { value: "PENDING_TEXT_REVIEW", label: "待文案" },
  { value: "PENDING_VISUAL_REVIEW", label: "待视觉" },
  { value: "COMPLETED", label: "已完成" },
  { value: "FAILED", label: "失败" },
];

const statusText: Record<string, string> = {
  RUNNING: "运行中",
  NEED_INPUT: "待补充数据",
  PENDING_TEXT_REVIEW: "待文案审核",
  PENDING_VISUAL_REVIEW: "待视觉方案审核",
  COMPLETED: "已完成",
  FAILED: "失败",
};

const statusClass: Record<string, string> = {
  RUNNING: "ready",
  NEED_INPUT: "warm",
  PENDING_TEXT_REVIEW: "warm",
  PENDING_VISUAL_REVIEW: "fuchsia",
  COMPLETED: "ok",
  FAILED: "bad",
};

const roleText: Record<string, string> = {
  user: "用户",
  assistant: "AI",
  tool_call: "工具调用",
  tool_result: "工具结果",
  system: "工作台",
  prompt: "业务提示词",
  review: "审核",
};

const typeOptions = computed(() => leaves.value.filter((leaf) => !leaf.disabled).map((leaf) => ({ value: leaf.key, label: leaf.label })));
const visualItems = computed(() => detail.value?.visual_plan?.items ?? []);

let timer = 0;

function flatten(nodes: TreeNode[], out: typeof leaves.value = []) {
  for (const node of nodes) {
    if (node.is_leaf) {
      out.push({
        key: `${node.description_category_id}:${node.type_id}`,
        label: node.path.join(" / "),
        cid: node.description_category_id,
        tid: node.type_id,
        disabled: node.disabled || node.ancestor_disabled,
      });
    }
    if (node.children?.length) flatten(node.children, out);
  }
  return out;
}

async function load(quiet = false) {
  if (!shop.currentShopId) {
    rows.value = [];
    return;
  }
  if (!quiet) loading.value = true;
  try {
    const page = await listTasks(statusFilter.value);
    rows.value = page.tasks ?? [];
    if (detail.value) {
      const fresh = await getTask(detail.value.id);
      const previous = events.value.at(-1)?.seq ?? 0;
      detail.value = fresh;
      if (previous === 0) {
        const timeline = await listTaskEvents(fresh.id, { limit: 200 });
        events.value = timeline.events ?? [];
        hasMore.value = timeline.has_more;
      } else {
        const timeline = await listTaskEvents(fresh.id, { after_seq: previous, limit: 200 });
        const known = new Set(events.value.map((item) => item.seq));
        for (const item of timeline.events ?? []) {
          if (!known.has(item.seq)) events.value.push(item);
        }
      }
    }
  } catch (error) {
    if (!quiet) toast.show(apiError(error).message);
  } finally {
    loading.value = false;
  }
}

function startPolling() {
  window.clearInterval(timer);
  timer = window.setInterval(() => {
    const running = rows.value.some((row) => row.status === "RUNNING") || detail.value?.status === "RUNNING";
    if (running) void load(true);
  }, 1500);
}

async function openCreate() {
  if (!shop.current) {
    toast.show("请先新增或选择店铺。");
    return;
  }
  fields.value = {};
  promptState.value = "";
  file.value = null;
  progress.value = 0;
  form.master_sku = "";
  form.supplier_url = "";
  form.auto_text_approve = false;
  form.auto_visual_approve = false;
  creating.value = true;
  try {
    const tree = await fetchTree("ZH_HANS");
    leaves.value = flatten(tree.roots ?? []);
    const first = leaves.value.find((leaf) => !leaf.disabled);
    form.typeKey = first?.key ?? "";
    if (first) await checkPrompt();
  } catch (error) {
    toast.show(apiError(error).message);
  }
}

async function checkPrompt() {
  const leaf = leaves.value.find((item) => item.key === form.typeKey);
  promptState.value = "";
  if (!leaf) return;
  try {
    const prompt = await fetchPrompt(leaf.cid, leaf.tid);
    promptState.value = prompt.saved ? "已保存这个类型的 Codex 提示词。" : "这个类型还没有保存提示词。请先到类目页保存。";
    if (!prompt.saved) fields.value.type_id = "请先为这个末级类型保存 Codex 提示词。";
    else delete fields.value.type_id;
  } catch (error) {
    promptState.value = apiError(error).message;
  }
}

function onFile(event: Event) {
  const input = event.target as HTMLInputElement;
  file.value = input.files?.[0] ?? null;
}

async function submitCreate() {
  fields.value = {};
  const leaf = leaves.value.find((item) => item.key === form.typeKey);
  if (!form.master_sku.trim()) fields.value.master_sku = "请填写主 SKU。";
  if (!leaf || leaf.disabled) fields.value.type_id = "请选择可用的末级类型。";
  if (!file.value) fields.value.file = "请上传 .zip 素材包。";
  if (Object.keys(fields.value).length) return Promise.reject(new Error("invalid"));
  const body = new FormData();
  body.set("master_sku", form.master_sku.trim());
  body.set("supplier_url", form.supplier_url.trim());
  body.set("description_category_id", String(leaf!.cid));
  body.set("type_id", String(leaf!.tid));
  body.set("language", "ZH_HANS");
  body.set("auto_text_approve", form.auto_text_approve ? "true" : "false");
  body.set("auto_visual_approve", form.auto_visual_approve ? "true" : "false");
  body.set("file", file.value!);
  uploading.value = true;
  progress.value = 0;
  try {
    const created = await createTask(body, (percent) => {
      progress.value = percent;
    });
    creating.value = false;
    await load(true);
    if (created.status === "FAILED") toast.show(created.error_message || "解压失败");
    else toast.show(created.status === "NEED_INPUT" ? "任务已创建，需要补充数据。" : "任务已创建。");
    await openDetail(created.id);
  } catch (error) {
    const parsed = apiError(error);
    fields.value = parsed.fields ?? {};
    toast.show(parsed.message);
    return Promise.reject(parsed.message);
  } finally {
    uploading.value = false;
  }
}

async function onPolicy(row: WorkbenchTask, which: "text" | "visual", checked: boolean) {
  busy.value = { ...busy.value, [row.id]: true };
  try {
    const updated = await updateTaskPolicy(row.id, {
      revision: row.revision,
      auto_text_approve: which === "text" ? checked : row.auto_text_approve,
      auto_visual_approve: which === "visual" ? checked : row.auto_visual_approve,
    });
    replaceRow(updated);
    if (detail.value?.id === updated.id) await openDetail(updated.id);
  } catch (error) {
    const parsed = apiError(error);
    toast.show(parsed.message);
    await load(true);
  } finally {
    busy.value = { ...busy.value, [row.id]: false };
  }
}

function replaceRow(updated: WorkbenchTask) {
  const index = rows.value.findIndex((row) => row.id === updated.id);
  if (index >= 0) rows.value.splice(index, 1, { ...rows.value[index], ...updated });
  else rows.value.unshift(updated);
}

async function openDetail(id: number) {
  try {
    detail.value = await getTask(id);
    const timeline = await listTaskEvents(id, { limit: 200 });
    events.value = timeline.events ?? [];
    hasMore.value = timeline.has_more;
    for (const key of Object.keys(answers)) delete answers[key];
    for (const field of detail.value.missing_fields ?? []) answers[field.key] = "";
    followUp.value = "";
    drawerOpen.value = true;
  } catch (error) {
    toast.show(apiError(error).message);
  }
}

async function loadEarlier() {
  if (!detail.value || !events.value.length) return;
  const timeline = await listTaskEvents(detail.value.id, { before_seq: events.value[0].seq, limit: 100 });
  const known = new Set(events.value.map((item) => item.seq));
  events.value = [...(timeline.events ?? []).filter((item) => !known.has(item.seq)), ...events.value];
  hasMore.value = timeline.has_more;
}

async function sendInput() {
  if (!detail.value) return;
  acting.value = true;
  try {
    const updated = await submitTaskInput(detail.value.id, { revision: detail.value.revision, fields: { ...answers } });
    replaceRow(updated);
    await openDetail(updated.id);
    toast.show("已提交补充数据，任务继续运行。");
  } catch (error) {
    const parsed = apiError(error);
    fields.value = parsed.fields ?? {};
    toast.show(parsed.message);
    await load(true);
  } finally {
    acting.value = false;
  }
}

async function approve(stage: "text" | "visual") {
  if (!detail.value) return;
  acting.value = true;
  try {
    const updated = await approveTask(detail.value.id, { revision: detail.value.revision, stage });
    replaceRow(updated);
    await openDetail(updated.id);
    toast.show(stage === "text" ? "文案审核已通过。" : "视觉方案审核已通过。");
  } catch (error) {
    toast.show(apiError(error).message);
    await load(true);
  } finally {
    acting.value = false;
  }
}

async function sendFollowUp() {
  if (!detail.value || !followUp.value.trim()) return;
  acting.value = true;
  try {
    const updated = await followUpTask(detail.value.id, { revision: detail.value.revision, content: followUp.value.trim() });
    followUp.value = "";
    replaceRow(updated);
    await openDetail(updated.id);
  } catch (error) {
    toast.show(apiError(error).message);
    await load(true);
  } finally {
    acting.value = false;
  }
}

function when(value: string) {
  return new Date(value).toLocaleString("zh-CN", { hour12: false });
}

function roleClass(role: string) {
  if (role === "assistant") return "ai";
  if (role === "user" || role === "prompt") return "user";
  return "sys";
}

watch(() => shop.currentShopId, () => {
  drawerOpen.value = false;
  detail.value = null;
  void load();
});

watch(statusFilter, () => {
  void load();
});

onMounted(() => {
  void load();
  startPolling();
});

onUnmounted(() => window.clearInterval(timer));
</script>

<template>
  <section class="panel">
    <div class="block between">
      <div>
        <h3>加工任务</h3>
        <div class="sub">{{ shop.codexMock ? "Codex 使用内置模拟运行器。" : "Codex 调用本机 CLI。" }} 文案和视觉开关只作用于当前任务，打开正在等待的环节会通过一次。</div>
      </div>
      <a-button type="primary" @click="openCreate">上传 1688 素材包</a-button>
    </div>
    <div class="block" style="padding-top: 0">
      <div class="filters">
        <button v-for="item in filters" :key="item.label" type="button" :class="{ on: statusFilter === item.value }" @click="statusFilter = item.value">
          {{ item.label }}
        </button>
      </div>
    </div>
    <div v-if="!shop.current" class="empty">请先在顶部选择店铺。任务会绑定创建时的店铺。</div>
    <div v-else-if="loading && !rows.length" class="empty">正在读取任务…</div>
    <template v-else>
      <div class="tablewrap">
        <table>
          <thead>
            <tr>
              <th>任务 / 主 SKU</th>
              <th>素材 / 类型</th>
              <th>任务状态</th>
              <th>文案自动通过</th>
              <th>视觉方案自动通过</th>
              <th>操作</th>
            </tr>
          </thead>
          <tbody>
            <tr v-for="row in rows" :key="row.id">
              <td>
                <span class="mono">#{{ row.id }}</span>
                <div class="sub">主 SKU {{ row.master_sku }}</div>
              </td>
              <td>
                {{ row.zip_filename || "素材包" }}
                <div class="sub">{{ row.type_path?.join(" / ") || row.type_name }}</div>
              </td>
              <td>
                <span class="status" :class="statusClass[row.status]">{{ statusText[row.status] || row.status }}</span>
                <div v-if="row.error_message" class="sub">{{ row.error_message }}</div>
              </td>
              <td>
                <a-switch
                  :checked="row.auto_text_approve"
                  checked-children="自动"
                  un-checked-children="人工"
                  :loading="busy[row.id]"
                  @change="(checked) => onPolicy(row, 'text', checked === true)"
                />
              </td>
              <td>
                <a-switch
                  :checked="row.auto_visual_approve"
                  checked-children="自动"
                  un-checked-children="人工"
                  :loading="busy[row.id]"
                  @change="(checked) => onPolicy(row, 'visual', checked === true)"
                />
              </td>
              <td><a-button type="link" size="small" @click="openDetail(row.id)">完整会话</a-button></td>
            </tr>
          </tbody>
        </table>
      </div>
      <div v-if="!rows.length" class="empty">当前店铺没有{{ statusFilter ? "符合筛选的" : "" }}加工任务。</div>
    </template>
  </section>

  <a-modal v-model:open="creating" title="新建素材加工任务" :confirm-loading="uploading" ok-text="创建任务" cancel-text="取消" @ok="submitCreate">
    <div class="stack">
      <div class="form-grid">
        <label class="field">
          主 SKU
          <a-input v-model:value="form.master_sku" placeholder="例如 30" :status="fields.master_sku ? 'error' : ''" />
          <span v-if="fields.master_sku" class="error">{{ fields.master_sku }}</span>
        </label>
        <label class="field">
          目标类型（简体中文类目树）
          <a-select v-model:value="form.typeKey" show-search option-filter-prop="label" :options="typeOptions" placeholder="选择末级类型" @change="checkPrompt" />
          <span v-if="fields.type_id" class="error">{{ fields.type_id }}</span>
        </label>
        <label class="field wide">
          1688 供应商链接（选填）
          <a-input v-model:value="form.supplier_url" placeholder="https://detail.1688.com/offer/…" :status="fields.supplier_url ? 'error' : ''" />
          <span v-if="fields.supplier_url" class="error">{{ fields.supplier_url }}</span>
        </label>
        <label class="field wide">
          本地 ZIP 素材包
          <input class="file" type="file" accept=".zip,application/zip" @change="onFile" />
          <span v-if="fields.file" class="error">{{ fields.file }}</span>
        </label>
      </div>
      <label class="switch-line"><a-switch v-model:checked="form.auto_text_approve" /> 文案审核自动通过</label>
      <label class="switch-line"><a-switch v-model:checked="form.auto_visual_approve" /> 视觉方案审核自动通过</label>
      <div v-if="uploading" class="bar"><div class="bar-in" :style="{ width: progress + '%' }"></div></div>
      <div v-if="uploading" class="sub">正在上传并解压 {{ progress }}%。上传完成后压缩包会被删除，只保留解压目录。</div>
      <div class="note">{{ promptState || "可在任务列表随时调整审核方式。新任务默认两段都由人工审核。" }} <router-link to="/categories">去类目页保存提示词</router-link></div>
    </div>
  </a-modal>

  <a-drawer v-model:open="drawerOpen" :title="detail ? `完整 AI 会话 · #${detail.id}` : '完整 AI 会话'" width="min(760px, 96vw)" placement="right">
    <div v-if="detail" class="stack">
      <div class="meta">
        <span class="status" :class="statusClass[detail.status]">{{ statusText[detail.status] || detail.status }}</span>
        <b>{{ detail.type_name }}</b>
        <span>主 SKU {{ detail.master_sku }}</span>
        <span>文案{{ detail.auto_text_approve ? "自动" : "人工" }} / 视觉方案{{ detail.auto_visual_approve ? "自动" : "人工" }}</span>
      </div>
      <div class="sub">会话 {{ detail.codex_session_id || "尚未建立" }} · 共 {{ events.length }} 条已加载 · 按发生顺序</div>
      <div v-if="detail.error_message" class="error">{{ detail.error_message }}</div>
      <div v-if="detail.sub_skus?.length" class="chips">
        <span v-for="sku in detail.sub_skus" :key="sku.offer_id" class="chip">{{ sku.offer_id }}<template v-if="sku.specs"> · {{ sku.specs }}</template></span>
      </div>
      <div v-if="detail.text_draft?.title_ru" class="note">俄文标题：{{ detail.text_draft.title_ru }}</div>
      <div v-if="visualItems.length" class="visual-grid">
        <figure v-for="(item, index) in visualItems" :key="item.offer_id || index">
          <img v-if="item.primary_image" class="thumb-img" :src="`/media/tasks/${detail.id}/${item.primary_image}`" :alt="item.offer_id || '预览'" />
          <figcaption class="sub">{{ item.offer_id }} {{ item.notes }}</figcaption>
        </figure>
      </div>

      <div v-if="detail.status === 'NEED_INPUT'" class="stack">
        <h3>补充缺失数据</h3>
        <label v-for="field in detail.missing_fields" :key="field.key" class="field">
          {{ field.label }}<template v-if="field.required">（必填）</template>
          <a-input v-model:value="answers[field.key]" :placeholder="field.hint || ''" :status="fields[field.key] ? 'error' : ''" />
          <span v-if="field.hint" class="sub">{{ field.hint }}</span>
          <span v-if="fields[field.key]" class="error">{{ fields[field.key] }}</span>
        </label>
        <div><a-button type="primary" :loading="acting" @click="sendInput">提交并继续</a-button></div>
      </div>

      <div v-if="hasMore"><a-button size="small" @click="loadEarlier">加载更早的记录</a-button></div>
      <ol class="chatlog">
        <li v-for="item in events" :key="item.seq" class="message" :class="roleClass(item.role)">
          <header>
            <b>{{ roleText[item.role] || item.role }}</b>
            <span>{{ item.tool_call_id ? item.tool_call_id + " · " : "" }}{{ item.stage }} · {{ when(item.created_at) }} · #{{ item.seq }}</span>
          </header>
          <pre>{{ item.content }}</pre>
          <div v-if="item.resource_ref" class="sub">附件 <a :href="`/media/tasks/${detail.id}/${item.resource_ref}`" target="_blank" rel="noopener">{{ item.resource_ref }}</a></div>
        </li>
      </ol>

      <label class="field">
        在此会话内追问
        <a-textarea v-model:value="followUp" :rows="3" placeholder="例如：请列出当前货号和审核状态。" />
      </label>
      <div class="row">
        <a-button v-if="detail.status === 'PENDING_TEXT_REVIEW'" :loading="acting" @click="approve('text')">人工通过文案审核</a-button>
        <a-button v-if="detail.status === 'PENDING_VISUAL_REVIEW'" :loading="acting" @click="approve('visual')">人工通过视觉审核</a-button>
        <a-button type="primary" :disabled="!followUp.trim() || detail.status === 'RUNNING' || !detail.codex_session_id" :loading="acting" @click="sendFollowUp">发送追问</a-button>
      </div>
    </div>
  </a-drawer>
</template>
