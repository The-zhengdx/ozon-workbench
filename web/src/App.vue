<script setup lang="ts">
import { theme } from "ant-design-vue";
import { computed, onMounted } from "vue";
import { useRoute } from "vue-router";

import { useShopStore } from "./stores/shop";
import { useToastStore } from "./stores/toast";

const shop = useShopStore();
const toast = useToastStore();
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
  { to: "/shops", icon: "▦", label: "店铺管理", short: "店铺" },
  { to: "/categories", icon: "⌘", label: "类目与提示词", short: "类目" },
  { to: "/tasks", icon: "≋", label: "素材加工工作台", short: "素材" },
  { to: "/listings", icon: "▤", label: "上架商品管理", short: "上架" },
];

const title = computed(() => (typeof route.meta.title === "string" ? route.meta.title : ""));
const desc = computed(() => (typeof route.meta.desc === "string" ? route.meta.desc : ""));

onMounted(() => {
  void shop.load();
});

function onSwitch(value: number) {
  void shop.select(value);
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
            <small>卖家运营中心</small>
          </div>
        </div>
        <nav class="nav" aria-label="主导航">
          <router-link v-for="item in nav" :key="item.to" :to="item.to" active-class="active">
            <span class="ico">{{ item.icon }}</span>
            <span class="deskName">{{ item.label }}</span>
            <span class="mobileName">{{ item.short }}</span>
          </router-link>
        </nav>
        <div class="railfoot"><span class="dot"></span>本地工作台</div>
      </aside>
      <main class="min-w-0">
        <header class="top">
          <div class="topLeft">
            <label for="shopSelect" class="muted">当前店铺</label>
            <a-select
              id="shopSelect"
              :value="shop.currentShopId ?? undefined"
              style="min-width: 180px; max-width: 260px"
              placeholder="请先新增店铺"
              :disabled="!shop.activeShops.length"
              @change="onSwitch"
            >
              <a-select-option v-for="item in shop.activeShops" :key="item.id" :value="item.id">
                {{ item.name }}{{ item.currency_code ? ` · ${item.currency_code}` : "" }}
              </a-select-option>
            </a-select>
            <span v-if="shop.current && !shop.current.currency_code" class="status warm currency-pill">币种未核实</span>
          </div>
          <div class="demo">
            <span v-if="shop.mock" class="status warm">模拟模式 · 仓库是内置示例</span>
            <span v-else class="status ready">真实 Ozon · {{ shop.proxyLabel }}</span>
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
    <div v-if="toast.text" class="toast" role="status">{{ toast.text }}</div>
  </a-config-provider>
</template>
