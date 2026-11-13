<script setup lang="ts">
import { theme } from "ant-design-vue";
import { computed, onMounted } from "vue";
import { useRoute } from "vue-router";

import { useShopStore } from "./stores/shop";

const shop = useShopStore();
const route = useRoute();

const themeConfig = {
  algorithm: theme.darkAlgorithm,
  token: {
    colorPrimary: "#61e0d1",
    colorInfo: "#61e0d1",
    colorSuccess: "#8be3ae",
    colorWarning: "#f7c67a",
    colorError: "#ffa4aa",
    colorBgBase: "#0a1016",
    colorTextBase: "#e8f0f5",
    colorBorder: "#2a3a46",
    borderRadius: 7,
    fontFamily: 'Inter, "Microsoft YaHei UI", "PingFang SC", system-ui, sans-serif',
  },
};

const nav = [
  { to: "/", icon: "◈", label: "概览", short: "概览" },
  { to: "/tasks", icon: "≋", label: "商品加工", short: "加工" },
  { to: "/products", icon: "▤", label: "商品管理", short: "商品" },
  { to: "/categories", icon: "⌘", label: "类目与提示词", short: "类目" },
  { to: "/imports", icon: "⇪", label: "刊登记录", short: "刊登" },
  { to: "/shops", icon: "▦", label: "店铺管理", short: "店铺" },
  { to: "/settings", icon: "⚙", label: "系统设置", short: "设置" },
];

const title = computed(() => (typeof route.meta.title === "string" ? route.meta.title : ""));
const desc = computed(() => (typeof route.meta.desc === "string" ? route.meta.desc : ""));

onMounted(() => {
  void shop.load().catch(() => undefined);
});

function onSwitch(value: unknown) {
  shop.select(Number(value));
}

function isActive(to: string) {
  if (to === "/") return route.path === "/";
  return route.path === to || route.path.startsWith(`${to}/`);
}
</script>

<template>
  <a-config-provider :theme="themeConfig">
    <div class="shell">
      <aside>
        <div class="brand">
          <div class="mark">OZ</div>
          <div>
            <b>Ozon 工作台</b>
            <small>本地加工与刊登</small>
          </div>
        </div>
        <nav class="nav" aria-label="主导航">
          <router-link v-for="item in nav" :key="item.to" :to="item.to" :class="{ active: isActive(item.to) }" active-class="">
            <span class="ico">{{ item.icon }}</span>
            <span class="deskName">{{ item.label }}</span>
            <span class="mobileName">{{ item.short }}</span>
          </router-link>
        </nav>
        <div class="railfoot"><span class="dot"></span>单机 PostgreSQL</div>
      </aside>
      <main class="min-w-0">
        <header class="top">
          <div class="topLeft">
            <label for="shopSelect" class="muted">当前店铺</label>
            <a-select
              id="shopSelect"
              :value="shop.currentShopId ?? undefined"
              style="min-width: 180px; max-width: 280px"
              placeholder="请先新增店铺"
              :disabled="!shop.shops.length"
              @change="onSwitch"
            >
              <a-select-option v-for="item in shop.shops" :key="item.id" :value="item.id">
                {{ item.name }} · {{ item.currency_code }}{{ item.enable ? "" : "（停用）" }}
              </a-select-option>
            </a-select>
            <span v-if="shop.current && !shop.current.enable" class="status bad">停用店铺只能查看历史</span>
          </div>
        </header>
        <section class="content">
          <div class="head">
            <div>
              <h1>{{ title }}</h1>
              <p>{{ desc }}</p>
            </div>
          </div>
          <router-view />
        </section>
      </main>
    </div>
  </a-config-provider>
</template>
