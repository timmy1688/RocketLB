import { defineStore } from "pinia";
import { api } from "../api/client";
import type { User } from "../api/types";

export const useSession = defineStore("session", {
  state: () => ({
    token: localStorage.getItem("rocketlb.token") || "",
    user: null as User | null,
  }),
  actions: {
    async login(username: string, password: string) {
      const result = await api<{ token: string; user: User }>("/api/v1/login", {
        method: "POST",
        body: JSON.stringify({ username, password }),
      });
      this.token = result.token;
      this.user = result.user;
      localStorage.setItem("rocketlb.token", result.token);
    },
    async fetchMe() {
      if (!this.token) return;
      this.user = await api<User>("/api/v1/me");
    },
    logout() {
      this.token = "";
      this.user = null;
      localStorage.removeItem("rocketlb.token");
    },
  },
});
