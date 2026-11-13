import { defineStore } from "pinia";

import { http, type Shop } from "../api/client";

const key = "ozon.currentShopId";

export const useShopStore = defineStore("shop", {
  state: () => ({
    shops: [] as Shop[],
    currentShopId: null as number | null,
    loaded: false,
  }),
  getters: {
    current: (state) => state.shops.find((shop) => shop.id === state.currentShopId) ?? null,
    enabled: (state) => state.shops.filter((shop) => shop.enable),
  },
  actions: {
    async load() {
      const { data } = await http.get<Shop[]>("/api/shops");
      this.shops = data;
      const saved = Number(localStorage.getItem(key) || "");
      const found = data.find((shop) => shop.id === saved);
      this.currentShopId = found?.id ?? data.find((shop) => shop.enable)?.id ?? data[0]?.id ?? null;
      this.loaded = true;
    },
    select(id: number) {
      this.currentShopId = id;
      localStorage.setItem(key, String(id));
    },
  },
});
