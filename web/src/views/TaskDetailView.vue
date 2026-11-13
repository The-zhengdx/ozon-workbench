<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from "vue";
import { useRoute } from "vue-router";

import { http, type Task } from "../api/client";
import { asList } from "../api/lists";
import { messageBody, reviewBody } from "../api/payloads";

const route = useRoute();
const task = ref<Task | null>(null);
const messages = ref<any[]>([]);
const messageRows = computed(() => asList(messages.value));
const content = ref("");
const error = ref("");
let timer = 0;

async function load() {
  const id = route.params.id;
  task.value = (await http.get<Task>(`/api/processing-tasks/${id}`)).data;
  messages.value = asList((await http.get(`/api/processing-tasks/${id}/messages`)).data);
}

onMounted(() => {
  load().catch((err: Error) => (error.value = err.message));
  timer = window.setInterval(() => load().catch(() => undefined), 3000);
});
onUnmounted(() => window.clearInterval(timer));

async function runAction(work: () => Promise<void>) {
  error.value = "";
  try {
    await work();
    await load();
  } catch (err) {
    error.value = err instanceof Error ? err.message : "操作失败";
  }
}

async function send() {
  if (!content.value.trim()) {
    error.value = "补充内容不能为空";
    return;
  }
  const body = content.value;
  await runAction(async () => {
    await http.post(`/api/processing-tasks/${route.params.id}/messages`, messageBody(body));
    content.value = "";
  });
}

async function review(kind: string, decision: string) {
  await runAction(async () => {
    await http.post(`/api/processing-tasks/${route.params.id}/review`, reviewBody(kind, decision));
  });
}

async function cancel() {
  await runAction(async () => {
    await http.post(`/api/processing-tasks/${route.params.id}/cancel`);
  });
}

async function retry() {
  await runAction(async () => {
    await http.post(`/api/processing-tasks/${route.params.id}/retry`);
  });
}
</script>

<template>
  <nav class="crumbs" aria-label="面包屑">
    <router-link to="/tasks">商品加工</router-link>
    <span>任务 {{ route.params.id }}</span>
  </nav>
  <p v-if="error" class="error">{{ error }}</p>
  <div v-if="task" class="stack">
    <div class="panel block between">
      <div>
        <h3>{{ task.base_sku }}</h3>
        <p class="muted">状态 {{ task.status }} · 阶段 {{ task.stage }} · {{ task.holds_slot ? "占用名额" : "不占名额" }}</p>
        <p v-if="task.status === 'running'" class="note">可以追加要求。消息已保存，等本轮结束后才会发送。</p>
        <p v-if="task.last_error" class="error">{{ task.last_error }}</p>
      </div>
      <div class="miniActions">
        <router-link class="ghost" to="/tasks">返回任务列表</router-link>
        <button class="ghost" type="button" :disabled="task.status === 'failed' || task.status === 'completed' || task.status === 'cancelled'" @click="cancel">取消</button>
        <button class="ghost" type="button" :disabled="task.status !== 'failed' && task.status !== 'cancelled'" @click="retry">重新排队</button>
      </div>
    </div>
    <div class="panel block">
      <article v-for="message in messageRows" :key="message.id" class="message">
        <header><b>{{ message.role }} · {{ message.kind }}</b><span>{{ message.delivery_status }}</span></header>
        <pre>{{ message.content }}</pre>
      </article>
      <p v-if="!messageRows.length" class="empty">这一轮还没有业务消息</p>
    </div>
    <form class="panel block stack" @submit.prevent="send">
      <textarea v-model="content" class="prompt" rows="4" placeholder="补充要求"></textarea>
      <div class="miniActions">
        <button class="primary" type="submit" :disabled="!content.trim()">保存到队列</button>
        <button class="ghost" type="button" @click="review('copy', 'approve')">通过文案</button>
        <button class="ghost" type="button" @click="review('visual_plan', 'approve')">通过视觉方案</button>
        <button class="ghost" type="button" @click="review('visual_result', 'approve')">通过视觉成品</button>
        <button class="ghost" type="button" @click="review('copy', 'reject')">退回</button>
      </div>
    </form>
  </div>
</template>
