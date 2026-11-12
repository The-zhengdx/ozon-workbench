import { defineStore } from "pinia";

import { currentShopId } from "../api/http";
import { fetchMeta, listShops, setDefaultShop } from "../api/workbench";
import type { Shop } from "../api/types";

const STORAGE_KEY = "ozon.currentShopId";

export const useShopStore = defineStore("shop", {
  state: () => ({
    shops: [] as Shop[],
    currentShopId: null as number | null,
    mock: true,
    loading: false,
    error: "",
  }),
  getters: {
    activeShops(state): Shop[] {
      return state.shops.filter((shop) => !shop.archived);
    },
    current(state): Shop | undefined {
      return state.shops.find((shop) => shop.id === state.currentShopId && !shop.archived);
    },
  },
  actions: {
    async load() {
      this.loading = true;
      this.error = "";
      try {
        const [shops, meta] = await Promise.all([listShops(), fetchMeta()]);
        this.shops = shops;
        this.mock = meta.mock;
        this.ensureCurrent();
      } catch (error) {
        this.error = error instanceof Error ? error.message : "店铺列表加载失败";
      } finally {
        this.loading = false;
      }
    },
    ensureCurrent() {
      const saved = Number(localStorage.getItem(STORAGE_KEY) || 0);
      const active = this.activeShops;
      const chosen =
        active.find((shop) => shop.id === saved) ??
        active.find((shop) => shop.is_default) ??
        active[0];
      this.applyCurrent(chosen?.id ?? null);
    },
    applyCurrent(id: number | null) {
      this.currentShopId = id;
      currentShopId.value = id;
      if (id) localStorage.setItem(STORAGE_KEY, String(id));
      else localStorage.removeItem(STORAGE_KEY);
    },
    async select(id: number) {
      const shop = this.shops.find((item) => item.id === id && !item.archived);
      if (!shop) return;
      this.applyCurrent(id);
      try {
        const updated = await setDefaultShop(id);
        this.shops = this.shops.map((item) => ({ ...item, is_default: item.id === updated.id }));
      } catch {
        // The header still carries the selection even if persisting the default fails.
      }
    },
    replace(shop: Shop) {
      const index = this.shops.findIndex((item) => item.id === shop.id);
      if (index >= 0) this.shops.splice(index, 1, shop);
      else this.shops.push(shop);
      if (!this.current && !shop.archived) this.applyCurrent(shop.id);
      if (this.currentShopId === shop.id && shop.archived) this.ensureCurrent();
    },
  },
});
