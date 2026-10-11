<script setup lang="ts">
import { h, onMounted, ref } from "vue";
import {
  NButton,
  NDataTable,
  NForm,
  NFormItem,
  NInput,
  NInputNumber,
  NModal,
  NSelect,
  NSpace,
  NSwitch,
  useDialog,
  useMessage,
  type DataTableColumns,
} from "naive-ui";
import { api, ApiError } from "../../api/client";
import type { Server, Upstream } from "../../api/types";
import { methodText } from "../../lib/format";

const props = defineProps<{ clusterId: string }>();
const emit = defineEmits<{ changed: [] }>();
const message = useMessage();
const dialog = useDialog();
const rows = ref<Upstream[]>([]);
const open = ref(false);
const saving = ref(false);
const editing = ref<string | null>(null);
const form = ref(empty());

function emptyServer(): Server {
  return { address: "", port: 80, weight: 1, maxFails: 3, failTimeout: 10, backup: false, down: false };
}

function empty() {
  return { name: "", layer: "http" as "http" | "stream", method: "round_robin", servers: [emptyServer()] };
}

const methods = [
  { label: "轮询", value: "round_robin" },
  { label: "最少连接", value: "least_conn" },
  { label: "源地址哈希", value: "hash" },
  { label: "源地址哈希（七层 ip_hash）", value: "ip_hash" },
];

async function load() {
  rows.value = await api<Upstream[]>(`/api/v1/clusters/${props.clusterId}/upstreams`);
}

function showError(err: unknown) {
  message.error(err instanceof ApiError ? err.message : "请求失败");
}

function create() {
  editing.value = null;
  form.value = empty();
  open.value = true;
}

function edit(row: Upstream) {
  editing.value = row.id;
  form.value = {
    name: row.name,
    layer: row.layer,
    method: row.method,
    servers: row.servers.map((server) => ({ ...server })),
  };
  open.value = true;
}

async function save() {
  saving.value = true;
  const body = {
    name: form.value.name,
    layer: form.value.layer,
    method: form.value.method,
    servers: form.value.servers,
  };
  try {
    if (editing.value) {
      await api(`/api/v1/upstreams/${editing.value}`, { method: "PUT", body: JSON.stringify(body) });
    } else {
      await api(`/api/v1/clusters/${props.clusterId}/upstreams`, { method: "POST", body: JSON.stringify(body) });
    }
    open.value = false;
    await load();
    emit("changed");
  } catch (err) {
    showError(err);
  } finally {
    saving.value = false;
  }
}

function remove(row: Upstream) {
  dialog.warning({
    title: "删除上游",
    content: `删除 ${row.name}？`,
    positiveText: "删除",
    negativeText: "取消",
    onPositiveClick: async () => {
      try {
        await api(`/api/v1/upstreams/${row.id}`, { method: "DELETE" });
        await load();
        emit("changed");
      } catch (err) {
        showError(err);
      }
    },
  });
}

const columns: DataTableColumns<Upstream> = [
  { title: "服务器组", key: "name" },
  { title: "协议", key: "layer", render: (row) => (row.layer === "http" ? "七层 HTTP" : "四层 TCP/UDP") },
  { title: "调度", key: "method", render: (row) => methodText(row.method) },
  {
    title: "后端服务器",
    key: "servers",
    render: (row) =>
      h(
        "div",
        { class: "backend-list" },
        row.servers.map((item) =>
          h("div", { key: `${item.address}:${item.port}` }, [
            h("span", `${item.address}:${item.port}`),
            h("span", { class: "muted" }, `权重 ${item.weight}`),
            item.backup ? h("span", { class: "tag" }, "备用") : null,
            item.down ? h("span", { class: "tag warn" }, "已下线") : null,
          ]),
        ),
      ),
  },
  {
    title: "",
    key: "actions",
    render: (row) =>
      h(NSpace, {}, () => [
        h(NButton, { size: "small", onClick: () => edit(row) }, () => "编辑"),
        h(NButton, { size: "small", quaternary: true, onClick: () => remove(row) }, () => "删除"),
      ]),
  },
];

onMounted(load);
</script>

<template>
  <div class="stack">
    <div class="panel-bar">
      <p class="muted">服务器组是一组后端。监听把流量转到这里，调度算法和健康检查都配在组上。</p>
      <n-button type="primary" @click="create">添加服务器组</n-button>
    </div>
    <n-data-table :columns="columns" :data="rows" :bordered="false" :scroll-x="860" :row-key="(row: Upstream) => row.id" />
    <n-modal v-model:show="open" preset="card" :title="editing ? '编辑服务器组' : '添加服务器组'" style="width: 760px">
      <n-form>
        <n-form-item label="名称"><n-input v-model:value="form.name" placeholder="web" /></n-form-item>
        <n-form-item label="用于">
          <n-select
            v-model:value="form.layer"
            :options="[
              { label: '七层监听（HTTP / HTTPS）', value: 'http' },
              { label: '四层监听（TCP / UDP）', value: 'stream' },
            ]"
          />
        </n-form-item>
        <n-form-item label="调度算法"><n-select v-model:value="form.method" :options="methods" /></n-form-item>
        <n-form-item label="后端服务器">
          <div style="width: 100%">
            <div v-for="(server, index) in form.servers" :key="index" class="server-card">
              <label class="mini">地址<n-input v-model:value="server.address" placeholder="10.1.0.2" /></label>
              <label class="mini">端口<n-input-number v-model:value="server.port" :min="1" :max="65535" /></label>
              <label class="mini">权重<n-input-number v-model:value="server.weight" :min="1" :max="100" /></label>
              <n-button quaternary :disabled="form.servers.length === 1" @click="form.servers.splice(index, 1)">删</n-button>
              <div class="server-meta">
                <label class="mini">失败次数<n-input-number v-model:value="server.maxFails" :min="0" :max="100" /></label>
                <label class="mini">失败超时（秒）<n-input-number v-model:value="server.failTimeout" :min="0" :max="3600" /></label>
                <n-switch v-model:value="server.backup"><template #checked>备用</template><template #unchecked>主用</template></n-switch>
                <n-switch v-model:value="server.down"><template #checked>已下线</template><template #unchecked>在线</template></n-switch>
              </div>
            </div>
            <n-button @click="form.servers.push(emptyServer())">添加后端服务器</n-button>
          </div>
        </n-form-item>
      </n-form>
      <template #footer>
        <n-button type="primary" :loading="saving" @click="save">保存</n-button>
      </template>
    </n-modal>
  </div>
</template>
