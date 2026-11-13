import { createRouter, createWebHistory } from "vue-router";

import CategoriesView from "../views/CategoriesView.vue";
import ImportsView from "../views/ImportsView.vue";
import OverviewView from "../views/OverviewView.vue";
import ProductDetailView from "../views/ProductDetailView.vue";
import ProductsView from "../views/ProductsView.vue";
import SettingsView from "../views/SettingsView.vue";
import ShopsView from "../views/ShopsView.vue";
import TaskDetailView from "../views/TaskDetailView.vue";
import TasksView from "../views/TasksView.vue";

export const router = createRouter({
  history: createWebHistory(),
  routes: [
    { path: "/", component: OverviewView, meta: { title: "概览", desc: "当前店铺、活跃名额和排队情况。" } },
    { path: "/tasks", component: TasksView, meta: { title: "商品加工", desc: "上传素材并排队。店铺使用顶部的全局选择，不会写进每个任务的表单。" } },
    { path: "/tasks/:id", component: TaskDetailView, meta: { title: "加工任务", desc: "每轮结束后显示结构化回复。运行中可以追加要求，本轮不会被打断。" } },
    { path: "/products", component: ProductsView, meta: { title: "商品管理", desc: "每一行是一个可独立销售的 SKU 草稿。刊登由人工发起。" } },
    { path: "/products/:id", component: ProductDetailView, meta: { title: "SKU 详情", desc: "修改后会生成新的 API JSON 版本。已导入过的旧版本仍然保留。" } },
    { path: "/categories", component: CategoriesView, meta: { title: "类目与提示词", desc: "每个类目类型只保存一段完整提示词。属性和字典按需加载。" } },
    { path: "/imports", component: ImportsView, meta: { title: "刊登记录", desc: "导入受理、逐 SKU 结果和平台审核是三件不同的事。" } },
    { path: "/shops", component: ShopsView, meta: { title: "店铺管理", desc: "店铺表只有 9 个字段。API Key 明文保存在本地数据库，页面上只显示末四位。" } },
    { path: "/settings", component: SettingsView, meta: { title: "系统设置", desc: "并发名额、提示词回退、OSS 和定价公式都在这里配置。没有填写的公式不会被自动补上。" } },
  ],
});

router.afterEach((to) => {
  const title = typeof to.meta.title === "string" ? to.meta.title : "Ozon 工作台";
  document.title = `${title} · Ozon 工作台`;
});
