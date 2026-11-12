import { defineConfig } from "vite";
import vue from "@vitejs/plugin-vue";

export default defineConfig({
  plugins: [vue()],
  server: {
    host: "0.0.0.0",
    port: 43123,
    proxy: {
      "/api": "http://127.0.0.1:18081",
      "/healthz": "http://127.0.0.1:18081",
      "/media": "http://127.0.0.1:18081",
    },
  },
  preview: {
    host: "0.0.0.0",
    port: 43123,
  },
});
