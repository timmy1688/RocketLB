<script setup lang="ts">
import { ref } from "vue";
import { useRouter } from "vue-router";
import { NButton, NCard, NForm, NFormItem, NInput } from "naive-ui";
import { useSession } from "../stores/session";
import { ApiError } from "../api/client";

const router = useRouter();
const session = useSession();
const username = ref("");
const password = ref("");
const error = ref("");
const loading = ref(false);

async function submit() {
  error.value = "";
  loading.value = true;
  try {
    await session.login(username.value.trim(), password.value);
    router.push("/");
  } catch (err) {
    error.value = err instanceof ApiError ? err.message : "登录失败";
  } finally {
    loading.value = false;
  }
}
</script>

<template>
  <div class="login">
    <n-card class="login-card" title="RocketLB">
      <p class="muted" style="margin-top: 0">管理机房里的 nginx 与 keepalived。</p>
      <n-form @submit.prevent="submit">
        <n-form-item label="用户名">
          <n-input v-model:value="username" autocomplete="username" />
        </n-form-item>
        <n-form-item label="密码">
          <n-input v-model:value="password" type="password" show-password-on="click" autocomplete="current-password" />
        </n-form-item>
        <p v-if="error" style="color: #f07178">{{ error }}</p>
        <n-button type="primary" attr-type="submit" :loading="loading" @click="submit">登录</n-button>
      </n-form>
    </n-card>
  </div>
</template>
