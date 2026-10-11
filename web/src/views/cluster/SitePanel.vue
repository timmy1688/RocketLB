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
import type { Certificate, Site, Upstream } from "../../api/types";

const props = defineProps<{ clusterId: string }>();
const message = useMessage();
const dialog = useDialog();
const rows = ref<Site[]>([]);
const upstreams = ref<Upstream[]>([]);
const certs = ref<Certificate[]>([]);
const open = ref(false);
const saving = ref(false);
const editing = ref<string | null>(null);
const form = ref(empty());

function empty() {
  return {
    name: "",
    listenPort: 80,
    serverName: "",
    tls: false,
    certificateId: "",
    upstreamId: "",
    webSocket: false,
  };
}

async function load() {
  const [siteRows, upstreamRows, certRows] = await Promise.all([
    api<Site[]>(`/api/v1/clusters/${props.clusterId}/sites`),
    api<Upstream[]>(`/api/v1/clusters/${props.clusterId}/upstreams`),
    api<Certificate[]>(`/api/v1/clusters/${props.clusterId}/certificates`),
  ]);
  rows.value = siteRows;
  upstreams.value = upstreamRows.filter((item) => item.layer === "http");
  certs.value = certRows;
}

function showError(err: unknown) {
  message.error(err instanceof ApiError ? err.message : "请求失败");
}

function create() {
  editing.value = null;
  form.value = empty();
  open.value = true;
}

function edit(row: Site) {
  editing.value = row.id;
  form.value = {
    name: row.name,
    listenPort: row.listenPort,
    serverName: row.serverName,
    tls: row.tls,
    certificateId: row.certificateId,
    upstreamId: row.upstreamId,
    webSocket: row.webSocket,
  };
  open.value = true;
}

async function save() {
  saving.value = true;
  const body = { ...form.value, certificateId: form.value.tls ? form.value.certificateId : "" };
  try {
    if (editing.value) await api(`/api/v1/sites/${editing.value}`, { method: "PUT", body: JSON.stringify(body) });
    else await api(`/api/v1/clusters/${props.clusterId}/sites`, { method: "POST", body: JSON.stringify(body) });
    open.value = false;
    await load();
  } catch (err) {
    showError(err);
  } finally {
    saving.value = false;
  }
}

function remove(row: Site) {
  dialog.warning({
    title: "删除站点",
    content: `删除 ${row.name}？`,
    positiveText: "删除",
    negativeText: "取消",
    onPositiveClick: async () => {
      try {
        await api(`/api/v1/sites/${row.id}`, { method: "DELETE" });
        await load();
      } catch (err) {
        showError(err);
      }
    },
  });
}

const columns: DataTableColumns<Site> = [
  { title: "名称", key: "name" },
  { title: "监听", key: "listenPort" },
  { title: "域名", key: "serverName" },
  { title: "TLS", key: "tls", render: (row) => (row.tls ? "是" : "否") },
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
    <div class="actions"><n-button type="primary" @click="create">添加站点</n-button></div>
    <n-data-table :columns="columns" :data="rows" :bordered="false" :scroll-x="720" :row-key="(row: Site) => row.id" />
    <n-modal v-model:show="open" preset="card" :title="editing ? '编辑站点' : '添加站点'" style="width: 560px">
      <n-form>
        <n-form-item label="名称"><n-input v-model:value="form.name" /></n-form-item>
        <n-form-item label="监听端口"><n-input-number v-model:value="form.listenPort" :min="1" :max="65535" /></n-form-item>
        <n-form-item label="域名"><n-input v-model:value="form.serverName" placeholder="app.example.com" /></n-form-item>
        <n-form-item label="上游">
          <n-select v-model:value="form.upstreamId" :options="upstreams.map((item) => ({ label: item.name, value: item.id }))" />
        </n-form-item>
        <n-form-item label="TLS"><n-switch v-model:value="form.tls" /></n-form-item>
        <n-form-item v-if="form.tls" label="证书">
          <n-select v-model:value="form.certificateId" :options="certs.map((item) => ({ label: item.name, value: item.id }))" />
        </n-form-item>
        <n-form-item label="WebSocket"><n-switch v-model:value="form.webSocket" /></n-form-item>
      </n-form>
      <template #footer>
        <n-button type="primary" :loading="saving" @click="save">保存</n-button>
      </template>
    </n-modal>
  </div>
</template>
