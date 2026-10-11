<script setup lang="ts">
import { h, onMounted, ref } from "vue";
import { RouterLink } from "vue-router";
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
  useDialog,
  useMessage,
  type DataTableColumns,
} from "naive-ui";
import { api, ApiError } from "../api/client";
import type { Cluster, LbNode } from "../api/types";
import PageHeader from "../components/PageHeader.vue";
import { installedSummary, roleText } from "../lib/deploy";
import { formatTime, statusText } from "../lib/format";

const message = useMessage();
const dialog = useDialog();
const rows = ref<LbNode[]>([]);
const clusters = ref<Cluster[]>([]);
const open = ref(false);
const saving = ref(false);
const editing = ref<string | null>(null);
const probing = ref("");
const form = ref(emptyForm());

function emptyForm() {
  return {
    name: "",
    address: "",
    sshPort: 22,
    sshUser: "root",
    authType: "password" as "password" | "key",
    secret: "",
  };
}

async function load() {
  const [nodeRows, clusterRows] = await Promise.all([api<LbNode[]>("/api/v1/nodes"), api<Cluster[]>("/api/v1/clusters")]);
  rows.value = nodeRows;
  clusters.value = clusterRows;
}

function placement(row: LbNode) {
  for (const cluster of clusters.value) {
    const member = cluster.members.find((item) => item.nodeId === row.id);
    if (member) return { cluster, member };
  }
  return null;
}

function showError(err: unknown) {
  message.error(err instanceof ApiError ? err.message : "请求失败");
}

function create() {
  editing.value = null;
  form.value = emptyForm();
  open.value = true;
}

function edit(row: LbNode) {
  editing.value = row.id;
  form.value = {
    name: row.name,
    address: row.address,
    sshPort: row.sshPort,
    sshUser: row.sshUser,
    authType: row.authType,
    secret: "",
  };
  open.value = true;
}

async function save() {
  saving.value = true;
  try {
    const body = { ...form.value };
    if (editing.value) {
      await api(`/api/v1/nodes/${editing.value}`, { method: "PUT", body: JSON.stringify(body) });
    } else {
      await api("/api/v1/nodes", { method: "POST", body: JSON.stringify(body) });
    }
    open.value = false;
    await load();
  } catch (err) {
    showError(err);
  } finally {
    saving.value = false;
  }
}

async function probe(row: LbNode) {
  probing.value = row.id;
  try {
    const updated = await api<LbNode>(`/api/v1/nodes/${row.id}/probe`, { method: "POST" });
    if (updated.status === "online") message.success(updated.os || "探测成功");
    else message.warning(updated.lastError || "探测失败");
    await load();
  } catch (err) {
    showError(err);
  } finally {
    probing.value = "";
  }
}

function remove(row: LbNode) {
  dialog.warning({
    title: "删除节点",
    content: `删除 ${row.name}？`,
    positiveText: "删除",
    negativeText: "取消",
    onPositiveClick: async () => {
      try {
        await api(`/api/v1/nodes/${row.id}`, { method: "DELETE" });
        await load();
      } catch (err) {
        showError(err);
      }
    },
  });
}

const columns: DataTableColumns<LbNode> = [
  {
    title: "状态",
    key: "status",
    render: (row) => h("span", [h("i", { class: `dot ${row.status}` }), statusText(row.status)]),
  },
  { title: "名称", key: "name" },
  { title: "地址", key: "address" },
  { title: "SSH", key: "ssh", render: (row) => `${row.sshUser}@${row.address}:${row.sshPort}` },
  { title: "系统", key: "os" },
  {
    title: "角色",
    key: "role",
    render: (row) => {
      const where = placement(row);
      if (!where) return "还没加入集群";
      return roleText(where.cluster.layout, where.member.role);
    },
  },
  {
    title: "部署",
    key: "deploy",
    render: (row) => {
      const where = placement(row);
      if (!where) return "—";
      return h("span", [
        installedSummary(where.cluster.layout, where.member.role),
        " · ",
        h(RouterLink, { to: `/clusters/${where.cluster.id}` }, () => where.cluster.name),
      ]);
    },
  },
  { title: "最近探测", key: "lastProbeAt", render: (row) => formatTime(row.lastProbeAt) },
  {
    title: "",
    key: "actions",
    render: (row) =>
      h(NSpace, {}, () => [
        h(NButton, { size: "small", loading: probing.value === row.id, onClick: () => probe(row) }, () => "探测"),
        h(NButton, { size: "small", onClick: () => edit(row) }, () => "编辑"),
        h(NButton, { size: "small", quaternary: true, onClick: () => remove(row) }, () => "删除"),
      ]),
  },
];

onMounted(load);
</script>

<template>
  <PageHeader title="节点" description="先把机器加进来并探测在线。一台机器只能加入一个实例。">
    <n-button type="primary" @click="create">添加节点</n-button>
  </PageHeader>
  <n-data-table :columns="columns" :data="rows" :bordered="false" :scroll-x="1360" :row-key="(row: LbNode) => row.id" />
  <n-modal v-model:show="open" preset="card" :title="editing ? '编辑节点' : '添加节点'" style="width: 520px">
    <n-form>
      <n-form-item label="名称"><n-input v-model:value="form.name" /></n-form-item>
      <n-form-item label="地址"><n-input v-model:value="form.address" placeholder="10.0.0.11" /></n-form-item>
      <n-form-item label="SSH 端口"><n-input-number v-model:value="form.sshPort" :min="1" :max="65535" /></n-form-item>
      <n-form-item label="SSH 用户"><n-input v-model:value="form.sshUser" /></n-form-item>
      <n-form-item label="认证">
        <n-select
          v-model:value="form.authType"
          :options="[
            { label: '密码', value: 'password' },
            { label: '私钥', value: 'key' },
          ]"
        />
      </n-form-item>
      <n-form-item :label="form.authType === 'key' ? '私钥' : '密码'">
        <n-input
          v-model:value="form.secret"
          :type="form.authType === 'key' ? 'textarea' : 'password'"
          :placeholder="editing ? '留空则不修改' : ''"
          :autosize="{ minRows: 4, maxRows: 8 }"
        />
      </n-form-item>
    </n-form>
    <template #footer>
      <n-button type="primary" :loading="saving" @click="save">保存</n-button>
    </template>
  </n-modal>
</template>
