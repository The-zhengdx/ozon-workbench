<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";

import { apiError } from "../api/http";
import {
  fetchAttributes,
  fetchPrompt,
  fetchTree,
  importAttributes,
  importTree,
  savePrompt,
  syncAttributes,
  syncTree,
} from "../api/workbench";
import type { AttributeRecord, AttributeSnapshot, CategoryTree, TreeNode } from "../api/types";
import CategoryTreeView from "../components/CategoryTree.vue";
import DictionaryDialog from "../components/DictionaryDialog.vue";
import { useToastStore } from "../stores/toast";

const toast = useToastStore();
const languages = [
  { value: "ZH_HANS", label: "简体中文" },
  { value: "RU", label: "Русский" },
  { value: "EN", label: "English" },
  { value: "TR", label: "Türkçe" },
  { value: "DEFAULT", label: "DEFAULT" },
];

const language = ref("ZH_HANS");
const tree = ref<CategoryTree | null>(null);
const loadingTree = ref(false);
const syncing = ref(false);
const query = ref("");
const expanded = ref(new Set<number>());
const selected = ref<TreeNode | null>(null);
const attrs = ref<AttributeSnapshot | null>(null);
const attrState = ref<"loading" | "ready" | "empty" | "missing" | "error">("missing");
const attrError = ref("");
const attrQuery = ref("");
const attrMode = ref("all");
const attrGroup = ref("");
const promptText = ref("");
const promptSaved = ref(false);
const promptAt = ref<string | null>(null);
const savingPrompt = ref(false);
const helpOpen = ref(false);
const dictOpen = ref(false);
const dictAttr = ref<AttributeRecord | null>(null);
const treeInput = ref<HTMLInputElement | null>(null);
const attrInput = ref<HTMLInputElement | null>(null);
let epoch = 0;

const selectedKey = computed(() =>
  selected.value ? `${selected.value.description_category_id}:${selected.value.type_id}` : "",
);

const groups = computed(() => {
  const names = new Set<string>();
  for (const attr of attrs.value?.attributes ?? []) {
    if (attr.group_name) names.add(String(attr.group_name));
  }
  return [...names];
});

const visibleAttrs = computed(() => {
  const term = attrQuery.value.trim().toLowerCase();
  return (attrs.value?.attributes ?? []).filter((attr) => {
    if (attrMode.value === "required" && !attr.is_required) return false;
    if (attrMode.value === "optional" && attr.is_required) return false;
    if (attrGroup.value && String(attr.group_name || "") !== attrGroup.value) return false;
    if (!term) return true;
    return [attr.name, attr.id, attr.group_name].join(" ").toLowerCase().includes(term);
  });
});

function leafKey(node: TreeNode) {
  return `${node.description_category_id}:${node.type_id}`;
}

function findLeaf(nodes: TreeNode[], key: string): TreeNode | null {
  for (const node of nodes) {
    if (node.is_leaf && leafKey(node) === key) return node;
    const child = findLeaf(node.children, key);
    if (child) return child;
  }
  return null;
}

function firstLeaf(nodes: TreeNode[]): TreeNode | null {
  for (const node of nodes) {
    if (node.is_leaf && !node.disabled && !node.ancestor_disabled) return node;
    const child = firstLeaf(node.children);
    if (child) return child;
  }
  return findAnyLeaf(nodes);
}

function findAnyLeaf(nodes: TreeNode[]): TreeNode | null {
  for (const node of nodes) {
    if (node.is_leaf) return node;
    const child = findAnyLeaf(node.children);
    if (child) return child;
  }
  return null;
}

function reveal(node: TreeNode) {
  const next = new Set(expanded.value);
  const walk = (nodes: TreeNode[], stack: number[]): boolean => {
    for (const current of nodes) {
      if (current.id === node.id) {
        stack.forEach((id) => next.add(id));
        return true;
      }
      if (walk(current.children, [...stack, current.id])) return true;
    }
    return false;
  };
  if (tree.value) walk(tree.value.roots, []);
  expanded.value = next;
}

function placeholder(node: TreeNode) {
  return `从 1688 素材提取${node.name}规格，按 {{master_sku}} 识别变体，核对必填属性并生成俄文标题与视觉方案。`;
}

async function loadTree() {
  loadingTree.value = true;
  try {
    tree.value = await fetchTree(language.value);
    const remembered = sessionStorage.getItem("ozon.selectedType") || "";
    const current = selected.value ? leafKey(selected.value) : remembered;
    const next = (current && findLeaf(tree.value.roots, current)) || firstLeaf(tree.value.roots);
    if (next) await choose(next);
    else selected.value = null;
  } catch (error) {
    toast.show(apiError(error).message);
  } finally {
    loadingTree.value = false;
  }
}

async function choose(node: TreeNode) {
  const token = ++epoch;
  selected.value = node;
  sessionStorage.setItem("ozon.selectedType", leafKey(node));
  reveal(node);
  attrQuery.value = "";
  attrMode.value = "all";
  attrGroup.value = "";
  attrError.value = "";
  attrs.value = null;
  attrState.value = "loading";
  promptText.value = "";
  promptSaved.value = false;
  promptAt.value = null;
  try {
    const prompt = await fetchPrompt(node.description_category_id, node.type_id);
    if (token !== epoch) return;
    promptSaved.value = prompt.saved;
    promptAt.value = prompt.updated_at;
    promptText.value = prompt.saved ? prompt.codex_prompt : placeholder(node);
  } catch (error) {
    if (token !== epoch) return;
    promptText.value = placeholder(node);
    toast.show(apiError(error).message);
  }
  try {
    const snapshot = await fetchAttributes(node.description_category_id, node.type_id, language.value);
    if (token !== epoch) return;
    attrs.value = snapshot;
    attrState.value = snapshot.total === 0 ? "empty" : "ready";
  } catch (error) {
    if (token !== epoch) return;
    const parsed = apiError(error);
    if (parsed.status === 404) {
      attrState.value = "missing";
      attrError.value = parsed.message;
      return;
    }
    attrState.value = "error";
    attrError.value = parsed.message;
  }
}

function toggle(id: number) {
  const next = new Set(expanded.value);
  if (next.has(id)) next.delete(id);
  else next.add(id);
  expanded.value = next;
}

async function syncOfficial() {
  syncing.value = true;
  try {
    tree.value = await syncTree(language.value);
    toast.show(`已同步 ${tree.value.type_count} 个商品类型`);
    const current = selected.value ? leafKey(selected.value) : "";
    const next = (current && findLeaf(tree.value.roots, current)) || firstLeaf(tree.value.roots);
    if (next) await choose(next);
  } catch (error) {
    const parsed = apiError(error);
    if (parsed.snapshot && "roots" in parsed.snapshot) tree.value = parsed.snapshot;
    toast.show(parsed.message);
  } finally {
    syncing.value = false;
  }
}

async function refreshAttributes() {
  if (!selected.value) return;
  const token = ++epoch;
  const node = selected.value;
  attrError.value = "";
  try {
    const snapshot = await syncAttributes(node.description_category_id, node.type_id, language.value);
    if (token !== epoch) return;
    attrs.value = snapshot;
    attrState.value = snapshot.total === 0 ? "empty" : "ready";
    toast.show(`已同步 ${snapshot.total} 项属性`);
  } catch (error) {
    if (token !== epoch) return;
    const parsed = apiError(error);
    attrError.value = parsed.message;
    if (parsed.snapshot && "attributes" in parsed.snapshot) {
      attrs.value = parsed.snapshot;
      attrState.value = parsed.snapshot.total === 0 ? "empty" : "ready";
    } else if (!attrs.value) {
      attrState.value = "error";
    }
    toast.show(parsed.message);
  }
}

async function persistPrompt() {
  if (!selected.value) return;
  savingPrompt.value = true;
  try {
    const saved = await savePrompt(selected.value.description_category_id, selected.value.type_id, promptText.value);
    promptSaved.value = saved.saved;
    promptAt.value = saved.updated_at;
    promptText.value = saved.codex_prompt;
    toast.show(`已保存「${selected.value.name}」的提示词`);
  } catch (error) {
    toast.show(apiError(error).message);
  } finally {
    savingPrompt.value = false;
  }
}

async function onTreeFile(event: Event) {
  const file = (event.target as HTMLInputElement).files?.[0];
  (event.target as HTMLInputElement).value = "";
  if (!file) return;
  try {
    const payload = JSON.parse((await file.text()).replace(/^\uFEFF/, ""));
    tree.value = await importTree(language.value, payload);
    toast.show("类目快照已导入");
    const next = firstLeaf(tree.value.roots);
    if (next) await choose(next);
  } catch (error) {
    toast.show(error instanceof SyntaxError ? "JSON 无法解析。" : apiError(error).message);
  }
}

async function onAttrFile(event: Event) {
  const file = (event.target as HTMLInputElement).files?.[0];
  (event.target as HTMLInputElement).value = "";
  if (!file || !selected.value) return;
  const token = ++epoch;
  const node = selected.value;
  try {
    const payload = JSON.parse((await file.text()).replace(/^\uFEFF/, ""));
    const snapshot = await importAttributes(node.description_category_id, node.type_id, language.value, payload);
    if (token !== epoch) return;
    attrs.value = snapshot;
    attrState.value = snapshot.total === 0 ? "empty" : "ready";
    attrError.value = "";
    toast.show("属性快照已导入");
  } catch (error) {
    if (token !== epoch) return;
    toast.show(error instanceof SyntaxError ? "JSON 无法解析。" : apiError(error).message);
  }
}

function openDict(attr: AttributeRecord) {
  dictAttr.value = attr;
  dictOpen.value = true;
}

function raw(attr: AttributeRecord) {
  return JSON.stringify(attr, null, 2);
}

const treeCommand = `$client = Read-Host 'Client ID'
$secret = Read-Host 'API Key' -AsSecureString
$headers = @{'Client-Id'=$client; 'Api-Key'=([System.Net.NetworkCredential]::new('', $secret).Password)}
$base = 'https://api-seller.ozon.ru/v1/description-category'
$dest = [Environment]::GetFolderPath('MyDocuments')
try {
  $tree = Invoke-RestMethod -Method Post -Uri "$base/tree" -Headers $headers -ContentType 'application/json' -Body '{"language":"ZH_HANS"}'
  $tree | ConvertTo-Json -Depth 100 | Set-Content -Encoding UTF8 (Join-Path $dest 'ozon-tree.json')
} finally { $headers.Clear(); $secret.Dispose() }`;

const attrCommand = computed(() => {
  const node = selected.value;
  return `$cid = [long]${node?.description_category_id ?? 0}
$tid = [long]${node?.type_id ?? 0}
$body = @{description_category_id=$cid;type_id=$tid;language='ZH_HANS'} | ConvertTo-Json
$res = Invoke-RestMethod -Method Post -Uri "$base/attribute" -Headers $headers -ContentType 'application/json' -Body $body
@{description_category_id=$cid;type_id=$tid;result=$res.result} | ConvertTo-Json -Depth 100 | Set-Content -Encoding UTF8 (Join-Path $dest "ozon-attrs-$cid-$tid.json")`;
});

onMounted(() => {
  void loadTree();
});

watch(language, () => {
  query.value = "";
  void loadTree();
});
</script>

<template>
  <div class="catalogLayout">
    <section class="panel block">
      <div class="between">
        <h3>Ozon 类目树</h3>
        <span class="sub">{{ tree?.type_count ?? 0 }} 个类型</span>
      </div>
      <div class="sub">
        {{ tree?.source_label || "尚未同步" }}
        <template v-if="tree?.snapshot_at"> · {{ new Date(tree.snapshot_at).toLocaleString("zh-CN", { hour12: false }) }}</template>
        <template v-if="tree"> · {{ tree.node_count }} 个节点</template>
      </div>
      <div v-if="tree?.last_error" class="error" style="margin-top: 10px">同步失败，已保留最近快照。{{ tree.last_error }}</div>
      <label class="field" style="margin-top: 12px">
        语言
        <a-select v-model:value="language" :options="languages" />
      </label>
      <label class="field" style="margin-top: 10px">
        搜索类目或类型
        <a-input v-model:value="query" allow-clear placeholder="名称、路径关键词或 ID" />
      </label>
      <div v-if="loadingTree && !tree" class="empty">正在读取类目树…</div>
      <div v-else-if="tree && !tree.roots.length" class="empty">这个语言还没有类目快照。</div>
      <CategoryTreeView
        v-else-if="tree"
        :nodes="tree.roots"
        :query="query"
        :expanded="expanded"
        :selected-key="selectedKey"
        show-empty
        @toggle="toggle"
        @choose="choose"
      />
      <div class="miniActions">
        <a-button :loading="syncing" @click="syncOfficial">{{ syncing ? "读取中…" : "同步官方全树" }}</a-button>
        <a-button @click="treeInput?.click()">导入类目 JSON</a-button>
        <a-button type="link" @click="helpOpen = true">获取官方快照</a-button>
      </div>
      <input ref="treeInput" type="file" accept=".json,application/json" hidden @change="onTreeFile" />
    </section>

    <section class="panel block stack">
      <template v-if="selected">
        <div>
          <div class="sub">{{ selected.path.join(" / ") }}</div>
          <h2>{{ selected.name }}</h2>
          <div class="meta">
            <span>类目 {{ selected.description_category_id }}</span>
            <span>类型 {{ selected.type_id }}</span>
            <span>层级 {{ selected.level }}</span>
            <span v-if="selected.disabled || selected.ancestor_disabled" class="status fuchsia">已停用</span>
            <span v-else class="status ok">可用于新任务</span>
          </div>
        </div>
        <label class="field">
          Codex 提示词
          <textarea v-model="promptText" class="prompt" spellcheck="false" />
        </label>
        <div class="between">
          <div class="meta">
            <span v-if="promptText.includes('{{master_sku}}')" class="status ready">含 &#123;&#123;master_sku&#125;&#125;</span>
            <span v-else class="status warm">未包含 &#123;&#123;master_sku&#125;&#125;</span>
            <span v-if="promptSaved && promptAt">已保存 {{ new Date(promptAt).toLocaleString("zh-CN", { hour12: false }) }}</span>
            <span v-else>尚未保存</span>
          </div>
          <a-button type="primary" :loading="savingPrompt" @click="persistPrompt">保存当前类型提示词</a-button>
        </div>
        <div class="between">
          <h3>全部类目属性</h3>
          <div class="miniActions">
            <a-button @click="refreshAttributes">同步属性</a-button>
            <a-button @click="attrInput?.click()">导入属性 JSON</a-button>
          </div>
        </div>
        <div v-if="attrs" class="sub">
          共 {{ attrs.total }} 项 · 必填 {{ attrs.required_count }} · 选填 {{ attrs.optional_count }} · 当前显示 {{ visibleAttrs.length }} 项
          · {{ attrs.source_label }} v{{ attrs.version }}
          · {{ new Date(attrs.synced_at).toLocaleString("zh-CN", { hour12: false }) }}
        </div>
        <div class="filters">
          <a-input v-model:value="attrQuery" class="grow" allow-clear placeholder="搜索属性名称、ID 或分组" />
          <a-select v-model:value="attrMode" style="width: 120px" :options="[{value:'all',label:'全部属性'},{value:'required',label:'仅必填'},{value:'optional',label:'仅选填'}]" />
          <a-select v-model:value="attrGroup" style="width: 150px" :options="[{value:'',label:'全部分组'}, ...groups.map(name => ({value:name,label:name}))]" />
        </div>
        <div v-if="attrError" class="error">{{ attrError }}</div>
        <div v-if="attrState === 'loading'" class="empty"><a-spin /> 正在读取属性…</div>
        <div v-else-if="attrState === 'missing'" class="note">{{ attrError || "本类型还没有属性快照。可同步属性，或导入该类目与类型的官方属性响应。" }}</div>
        <div v-else-if="attrState === 'error' && !attrs" class="note">属性加载失败。可重试同步，已有快照会被保留。</div>
        <div v-else-if="attrState === 'empty'" class="note">Ozon 返回了空的属性列表。这和「尚未加载」不同，快照已记录。</div>
        <div v-else-if="attrs" class="tablewrap attrTable">
          <table>
            <thead>
              <tr><th>属性 / ID</th><th>约束</th><th>类型 / 字典</th><th>说明</th></tr>
            </thead>
            <tbody>
              <tr v-for="attr in visibleAttrs" :key="String(attr.id)">
                <td>
                  {{ attr.name }}
                  <div class="sub">{{ attr.id }} {{ attr.group_name || "" }}</div>
                </td>
                <td>{{ attr.is_required ? "必填" : "选填" }} · {{ attr.is_collection ? "多值" : "单值" }}</td>
                <td>
                  {{ attr.type || "—" }}
                  <div class="sub">
                    <template v-if="Number(attr.dictionary_id) > 0">
                      字典 {{ attr.dictionary_id }}
                      <a-button type="link" size="small" @click="openDict(attr)">查看</a-button>
                    </template>
                    <template v-else>自由填写</template>
                  </div>
                </td>
                <td>
                  {{ attr.description || "—" }}
                  <details>
                    <summary>完整定义</summary>
                    <pre>{{ raw(attr) }}</pre>
                  </details>
                </td>
              </tr>
              <tr v-if="!visibleAttrs.length">
                <td colspan="4" class="empty">没有符合筛选条件的属性。</td>
              </tr>
            </tbody>
          </table>
        </div>
      </template>
      <div v-else class="empty">选择一个末级商品类型后，可以编辑提示词并查看全部属性。</div>
      <input ref="attrInput" type="file" accept=".json,application/json" hidden @change="onAttrFile" />
    </section>
  </div>

  <DictionaryDialog
    v-model:open="dictOpen"
    :attribute="dictAttr"
    :category-id="selected?.description_category_id || 0"
    :type-id="selected?.type_id || 0"
    :language="language"
  />

  <a-modal v-model:open="helpOpen" title="获取官方快照" :footer="null" width="760px">
    <div class="stack">
      <p>在 Windows PowerShell 分别运行下列命令，使用自己的店铺凭证。文件保存在「文档」。先导入全树，再选择类型并导入对应属性。浏览器直连 Ozon 会受跨域限制，本地后端同步不需要这一步。</p>
      <details open>
        <summary>① 下载完整类目树</summary>
        <pre>{{ treeCommand }}</pre>
      </details>
      <details>
        <summary>② 下载一个类型的全部属性</summary>
        <div class="sub" v-if="selected">当前选择 {{ selected.path.join(" / ") }} · {{ selected.description_category_id }} / {{ selected.type_id }}</div>
        <pre>{{ attrCommand }}</pre>
      </details>
    </div>
  </a-modal>
</template>
