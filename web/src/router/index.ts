import { createRouter, createWebHistory } from "vue-router";

import CategoriesView from "../views/CategoriesView.vue";
import ShopsView from "../views/ShopsView.vue";
import SoonView from "../views/SoonView.vue";
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
      component: SoonView,
      props: { phase: "后续阶段", detail: "按可售 SKU 刊登、批次和库存不在本次范围内。" },
      meta: { title: "上架商品管理", desc: "每行对应一个可独立销售的卖家 SKU。" },
    },
  ],
});

router.afterEach((to) => {
  const title = typeof to.meta.title === "string" ? to.meta.title : "Ozon 工作台";
  document.title = `${title} · Ozon 工作台`;
});
