import { defineStore } from "pinia";

import { http, type Shop } from "../api/client";
import { asList } from "../api/lists";

const key = "ozon.currentShopId";

export const useShopStore = defineStore("shop", {
  state: () => ({
    shops: [] as Shop[],
    currentShopId: null as number | null,
    loaded: false,
  }),
  getters: {
    list: (state) => asList(state.shops),
    current: (state) => asList(state.shops).find((shop) => shop.id === state.currentShopId) ?? null,
    enabled: (state) => asList(state.shops).filter((shop) => shop.enable),
  },
  actions: {
    async load() {
      const { data } = await http.get<Shop[] | null>("/api/shops");
      const shops = asList(data);
      this.shops = shops;
      const saved = Number(localStorage.getItem(key) || "");
      const found = shops.find((shop) => shop.id === saved);
      this.currentShopId = found?.id ?? shops.find((shop) => shop.enable)?.id ?? shops[0]?.id ?? null;
      this.loaded = true;
    },
    select(id: number) {
      this.currentShopId = id;
      localStorage.setItem(key, String(id));
    },
  },
});
