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
  useDialog,
  useMessage,
  type DataTableColumns,
} from "naive-ui";
import { api, ApiError } from "../../api/client";
import type { StreamProxy, Upstream } from "../../api/types";

const props = defineProps<{ clusterId: string }>();
const message = useMessage();
const dialog = useDialog();
const rows = ref<StreamProxy[]>([]);
const upstreams = ref<Upstream[]>([]);
const open = ref(false);
const saving = ref(false);
const editing = ref<string | null>(null);
const form = ref(empty());

function empty() {
  return { name: "", protocol: "tcp" as "tcp" | "udp", listenPort: 3306, upstreamId: "" };
}

async function load() {
  const [streamRows, upstreamRows] = await Promise.all([
    api<StreamProxy[]>(`/api/v1/clusters/${props.clusterId}/streams`),
    api<Upstream[]>(`/api/v1/clusters/${props.clusterId}/upstreams`),
  ]);
  rows.value = streamRows;
  upstreams.value = upstreamRows.filter((item) => item.layer === "stream");
}

function showError(err: unknown) {
  message.error(err instanceof ApiError ? err.message : "请求失败");
}

function create() {
  editing.value = null;
  form.value = empty();
  open.value = true;
}

function edit(row: StreamProxy) {
  editing.value = row.id;
  form.value = { name: row.name, protocol: row.protocol, listenPort: row.listenPort, upstreamId: row.upstreamId };
  open.value = true;
}

async function save() {
  saving.value = true;
  try {
    if (editing.value) await api(`/api/v1/streams/${editing.value}`, { method: "PUT", body: JSON.stringify(form.value) });
    else await api(`/api/v1/clusters/${props.clusterId}/streams`, { method: "POST", body: JSON.stringify(form.value) });
    open.value = false;
    await load();
  } catch (err) {
    showError(err);
  } finally {
    saving.value = false;
  }
}

function remove(row: StreamProxy) {
  dialog.warning({
    title: "删除四层代理",
    content: `删除 ${row.name}？`,
    positiveText: "删除",
    negativeText: "取消",
    onPositiveClick: async () => {
      try {
        await api(`/api/v1/streams/${row.id}`, { method: "DELETE" });
        await load();
      } catch (err) {
        showError(err);
      }
    },
  });
}

const columns: DataTableColumns<StreamProxy> = [
  { title: "名称", key: "name" },
  { title: "协议", key: "protocol" },
  { title: "监听", key: "listenPort" },
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
    <div class="actions"><n-button type="primary" @click="create">添加四层代理</n-button></div>
    <n-data-table :columns="columns" :data="rows" :bordered="false" :scroll-x="640" :row-key="(row: StreamProxy) => row.id" />
    <n-modal v-model:show="open" preset="card" :title="editing ? '编辑四层代理' : '添加四层代理'" style="width: 520px">
      <n-form>
        <n-form-item label="名称"><n-input v-model:value="form.name" /></n-form-item>
        <n-form-item label="协议">
          <n-select
            v-model:value="form.protocol"
            :options="[
              { label: 'TCP', value: 'tcp' },
              { label: 'UDP', value: 'udp' },
            ]"
          />
        </n-form-item>
        <n-form-item label="监听端口"><n-input-number v-model:value="form.listenPort" :min="1" :max="65535" /></n-form-item>
        <n-form-item label="上游">
          <n-select v-model:value="form.upstreamId" :options="upstreams.map((item) => ({ label: item.name, value: item.id }))" />
        </n-form-item>
      </n-form>
      <template #footer>
        <n-button type="primary" :loading="saving" @click="save">保存</n-button>
      </template>
    </n-modal>
  </div>
</template>
