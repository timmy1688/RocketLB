<script setup lang="ts">
import { h, onMounted, ref } from "vue";
import {
  NButton,
  NDataTable,
  NForm,
  NFormItem,
  NInput,
  NModal,
  NSpace,
  useDialog,
  useMessage,
  type DataTableColumns,
} from "naive-ui";
import { api, ApiError } from "../../api/client";
import type { Certificate } from "../../api/types";
import { formatTime } from "../../lib/format";

const props = defineProps<{ clusterId: string }>();
const message = useMessage();
const dialog = useDialog();
const rows = ref<Certificate[]>([]);
const open = ref(false);
const saving = ref(false);
const form = ref({ name: "", certificate: "", privateKey: "" });

async function load() {
  rows.value = await api<Certificate[]>(`/api/v1/clusters/${props.clusterId}/certificates`);
}

function showError(err: unknown) {
  message.error(err instanceof ApiError ? err.message : "请求失败");
}

async function save() {
  saving.value = true;
  try {
    await api(`/api/v1/clusters/${props.clusterId}/certificates`, {
      method: "POST",
      body: JSON.stringify(form.value),
    });
    open.value = false;
    form.value = { name: "", certificate: "", privateKey: "" };
    await load();
  } catch (err) {
    showError(err);
  } finally {
    saving.value = false;
  }
}

function remove(row: Certificate) {
  dialog.warning({
    title: "删除证书",
    content: `删除 ${row.name}？私钥不会再显示。`,
    positiveText: "删除",
    negativeText: "取消",
    onPositiveClick: async () => {
      try {
        await api(`/api/v1/certificates/${row.id}`, { method: "DELETE" });
        await load();
      } catch (err) {
        showError(err);
      }
    },
  });
}

const columns: DataTableColumns<Certificate> = [
  { title: "名称", key: "name" },
  { title: "主题", key: "subject" },
  { title: "域名", key: "dnsNames" },
  { title: "到期", key: "notAfter", render: (row) => formatTime(row.notAfter) },
  {
    title: "",
    key: "actions",
    render: (row) => h(NSpace, {}, () => [h(NButton, { size: "small", quaternary: true, onClick: () => remove(row) }, () => "删除")]),
  },
];

onMounted(load);
</script>

<template>
  <div class="stack">
    <div class="panel-bar">
      <p class="muted">HTTPS 监听要选一张证书。上传后私钥不再显示。</p>
      <n-button type="primary" @click="open = true">上传证书</n-button>
    </div>
    <n-data-table :columns="columns" :data="rows" :bordered="false" :scroll-x="720" :row-key="(row: Certificate) => row.id" />
    <n-modal v-model:show="open" preset="card" title="上传证书" style="width: 640px">
      <n-form>
        <n-form-item label="名称"><n-input v-model:value="form.name" /></n-form-item>
        <n-form-item label="证书 PEM">
          <n-input v-model:value="form.certificate" type="textarea" :autosize="{ minRows: 5, maxRows: 10 }" />
        </n-form-item>
        <n-form-item label="私钥 PEM">
          <n-input v-model:value="form.privateKey" type="textarea" :autosize="{ minRows: 5, maxRows: 10 }" />
        </n-form-item>
      </n-form>
      <template #footer>
        <n-button type="primary" :loading="saving" @click="save">保存</n-button>
      </template>
    </n-modal>
  </div>
</template>
