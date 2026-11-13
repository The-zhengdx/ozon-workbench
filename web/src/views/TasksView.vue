<script setup lang="ts">
import { computed, onMounted, reactive, ref } from "vue";
import { useRouter } from "vue-router";

import { http, type Task } from "../api/client";
import { asList } from "../api/lists";
import { useShopStore } from "../stores/shop";

const shop = useShopStore();
const router = useRouter();
const tasks = ref<Task[]>([]);
const rows = computed(() => asList(tasks.value));
const error = ref("");
const form = reactive({
  description_category_id: "",
  type_id: "",
  base_sku: "",
  origin_link: "",
  copy_auto_approve: false,
  visual_plan_auto_approve: false,
  visual_result_auto_approve: false,
});

async function load() {
  tasks.value = asList((await http.get<Task[] | null>("/api/processing-tasks", { params: { shop_id: shop.currentShopId || undefined } })).data);
}

onMounted(() => load().catch((err: Error) => (error.value = err.message)));

async function create() {
  error.value = "";
  if (!shop.current?.enable) {
    error.value = shop.current ? "当前店铺已停用，不能创建加工任务" : "先在顶部选择店铺";
    return;
  }
  const body = new FormData();
  body.set("shop_id", String(shop.current.id));
  body.set("description_category_id", form.description_category_id);
  body.set("type_id", form.type_id);
  body.set("base_sku", form.base_sku);
  body.set("origin_link", form.origin_link);
  body.set("copy_auto_approve", String(form.copy_auto_approve));
  body.set("visual_plan_auto_approve", String(form.visual_plan_auto_approve));
  body.set("visual_result_auto_approve", String(form.visual_result_auto_approve));
  const input = document.getElementById("files") as HTMLInputElement | null;
  for (const file of input?.files ?? []) body.append("files", file);
  try {
    const { data } = await http.post<Task>("/api/processing-tasks", body);
    await router.push(`/tasks/${data.id}`);
  } catch (err) {
    error.value = err instanceof Error ? err.message : "创建失败";
  }
}
</script>

<template>
  <p v-if="error" class="error">{{ error }}</p>
  <form class="panel block form-grid" @submit.prevent="create">
    <p class="wide muted">店铺：{{ shop.current ? `${shop.current.name}（只读）` : "未选择" }}</p>
    <label class="field">类目 ID<input v-model="form.description_category_id" required /></label>
    <label class="field">类型 ID<input v-model="form.type_id" required /></label>
    <label class="field">主 SKU<input v-model="form.base_sku" required /></label>
    <label class="field">货源链接<input v-model="form.origin_link" /></label>
    <label class="field wide">素材<input id="files" type="file" multiple /></label>
    <label class="switch-line"><input v-model="form.copy_auto_approve" type="checkbox" /> 文案自动审核</label>
    <label class="switch-line"><input v-model="form.visual_plan_auto_approve" type="checkbox" /> 视觉方案自动审核</label>
    <label class="switch-line"><input v-model="form.visual_result_auto_approve" type="checkbox" /> 视觉成品自动审核</label>
    <div class="wide"><button class="primary" type="submit">创建并排队</button></div>
  </form>
  <div class="panel tablewrap" style="margin-top: 14px">
    <table>
      <thead><tr><th>ID</th><th>主 SKU</th><th>类目</th><th>状态</th><th>名额</th><th></th></tr></thead>
      <tbody>
        <tr v-for="task in rows" :key="task.id">
          <td class="mono">{{ task.id }}</td>
          <td>{{ task.base_sku }}</td>
          <td class="mono">{{ task.description_category_id }}/{{ task.type_id }}</td>
          <td><span class="status">{{ task.status }}</span></td>
          <td>{{ task.holds_slot ? "占用" : task.queue_position ? `排队第 ${task.queue_position}` : "—" }}</td>
          <td><router-link :to="`/tasks/${task.id}`">查看</router-link></td>
        </tr>
      </tbody>
    </table>
  </div>
</template>
