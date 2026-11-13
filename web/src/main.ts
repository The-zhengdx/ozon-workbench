import { createPinia } from "pinia";
import { createApp } from "vue";
import Antd from "ant-design-vue";
import "ant-design-vue/dist/reset.css";

import App from "./App.vue";
import { router } from "./router";
import "./styles/workbench.css";

const app = createApp(App);
app.config.errorHandler = (err, _instance, info) => {
  console.error(info, err);
};
app.use(createPinia());
app.use(router);
app.use(Antd);
app.mount("#app");
