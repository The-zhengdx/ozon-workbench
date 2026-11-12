import { defineStore } from "pinia";

export const useToastStore = defineStore("toast", {
  state: () => ({ text: "" }),
  actions: {
    show(text: string) {
      this.text = text;
      window.setTimeout(() => {
        if (this.text === text) this.text = "";
      }, 3400);
    },
  },
});
