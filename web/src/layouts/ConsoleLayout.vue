<script setup lang="ts">
import { onMounted, ref } from "vue";
import { useRoute, useRouter, RouterLink } from "vue-router";
import { NButton, NForm, NFormItem, NInput, NModal, useMessage } from "naive-ui";
import { api, ApiError } from "../api/client";
import type { User } from "../api/types";
import { useSession } from "../stores/session";

const route = useRoute();
const router = useRouter();
const session = useSession();
const items = [
  { to: "/", label: "总览" },
  { to: "/nodes", label: "节点" },
  { to: "/clusters", label: "实例" },
];

function active(path: string) {
  if (path === "/") return route.path === "/";
  return route.path === path || route.path.startsWith(path + "/");
}

function logout() {
  session.logout();
  router.push("/login");
}

onMounted(() => {
  session.fetchMe().catch(() => session.logout());
});

const ready = ref(true);
const message = useMessage();
const openPassword = ref(false);
const savingPassword = ref(false);
const currentPassword = ref("");
const nextPassword = ref("");

async function changePassword() {
  savingPassword.value = true;
  try {
    session.user = await api<User>("/api/v1/password", {
      method: "POST",
      body: JSON.stringify({ current: currentPassword.value, next: nextPassword.value }),
    });
    currentPassword.value = "";
    nextPassword.value = "";
    openPassword.value = false;
    message.success("密码已更新");
  } catch (err) {
    message.error(err instanceof ApiError ? err.message : "修改失败");
  } finally {
    savingPassword.value = false;
  }
}
</script>

<template>
  <div class="shell">
    <aside class="side">
      <div class="brand">
        <strong>RocketLB</strong>
        <span>高可用负载均衡</span>
      </div>
      <nav class="nav">
        <RouterLink v-for="item in items" :key="item.to" :to="item.to" :class="{ active: active(item.to) }">
          {{ item.label }}
        </RouterLink>
      </nav>
      <div class="user">
        <span>{{ session.user?.username || "已登录" }}</span>
        <n-button size="tiny" quaternary @click="openPassword = true">改密码</n-button>
        <n-button size="tiny" quaternary @click="logout">退出</n-button>
      </div>
    </aside>
    <main class="main">
      <router-view v-if="ready" />
    </main>
    <n-modal v-model:show="openPassword" preset="card" title="修改密码" style="width: 420px">
      <n-form>
        <n-form-item label="当前密码"><n-input v-model:value="currentPassword" type="password" show-password-on="click" /></n-form-item>
        <n-form-item label="新密码"><n-input v-model:value="nextPassword" type="password" show-password-on="click" placeholder="至少 8 位" /></n-form-item>
      </n-form>
      <template #footer>
        <n-button type="primary" :loading="savingPassword" @click="changePassword">保存</n-button>
      </template>
    </n-modal>
  </div>
</template>
