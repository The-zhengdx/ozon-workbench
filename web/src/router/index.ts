import { createRouter, createWebHistory } from "vue-router";

import CategoriesView from "../views/CategoriesView.vue";
import ListingDetailView from "../views/ListingDetailView.vue";
import ListingsView from "../views/ListingsView.vue";
import SettingsView from "../views/SettingsView.vue";
import ShopsView from "../views/ShopsView.vue";
import TasksView from "../views/TasksView.vue";

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: "/", redirect: "/shops" },
    {
      path: "/shops",
      component: ShopsView,
      meta: { title: "店铺管理", desc: "维护店铺名称、Client ID 和 API Key。刊登币种保持未知，直到报价或发布预检。" },
    },
    {
      path: "/categories",
      component: CategoriesView,
      meta: { title: "类目与提示词", desc: "同步 Ozon 全量类目树，按末级类型查看全部属性并保存 Codex 提示词。" },
    },
    {
      path: "/tasks",
      component: TasksView,
      meta: { title: "素材加工工作台", desc: "上传 1688 素材包，按任务审核文案和视觉方案，并在同一 Codex 会话里追问。" },
    },
    {
      path: "/listings",
      component: ListingsView,
      meta: { title: "上架商品管理", desc: "每行对应一个可独立销售的卖家 SKU。单条和批量都提交选定版本。" },
    },
    {
      path: "/listings/:id",
      component: ListingDetailView,
      meta: { title: "SKU 详情", desc: "同一份商品参数驱动表单和 JSON。保存后生成新版本并重新校验。" },
    },
    {
      path: "/settings",
      component: SettingsView,
      meta: { title: "图床设置", desc: "Ozon 只接受公网 HTTPS 媒体地址。未配置时工作台照常运行，上架前需要先补上地址。" },
    },
  ],
});

router.afterEach((to) => {
  const title = typeof to.meta.title === "string" ? to.meta.title : "Ozon 工作台";
  document.title = `${title} · Ozon 工作台`;
});
