<script setup lang="ts">
import { computed, h, onMounted, ref } from "vue";
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
  NTag,
  useDialog,
  useMessage,
  type DataTableColumns,
} from "naive-ui";
import { api, ApiError } from "../../api/client";
import type { Certificate, Site, StreamProxy, Upstream } from "../../api/types";

const props = defineProps<{ clusterId: string }>();
const emit = defineEmits<{ changed: []; goto: [tab: string] }>();
const message = useMessage();
const dialog = useDialog();
const sites = ref<Site[]>([]);
const streams = ref<StreamProxy[]>([]);
const upstreams = ref<Upstream[]>([]);
const certs = ref<Certificate[]>([]);
const open = ref(false);
const saving = ref(false);
const editing = ref<{ kind: "site" | "stream"; id: string } | null>(null);
const proto = ref<"http" | "https" | "tcp" | "udp">("http");
const form = ref(emptyForm());

type ListenerRow = {
  key: string;
  kind: "site" | "stream";
  id: string;
  name: string;
  proto: "HTTP" | "HTTPS" | "TCP" | "UDP";
  port: number;
  target: string;
  upstreamId: string;
  note: string;
};

function emptyForm() {
  return { name: "", listenPort: 80, serverName: "", certificateId: "", upstreamId: "", webSocket: false };
}

const httpGroups = computed(() => upstreams.value.filter((item) => item.layer === "http"));
const streamGroups = computed(() => upstreams.value.filter((item) => item.layer === "stream"));
const groups = computed(() => (proto.value === "tcp" || proto.value === "udp" ? streamGroups.value : httpGroups.value));

const rows = computed<ListenerRow[]>(() => {
  const byId = new Map(upstreams.value.map((item) => [item.id, item]));
  const siteRows: ListenerRow[] = sites.value.map((site) => {
    const group = byId.get(site.upstreamId);
    return {
      key: `site-${site.id}`,
      kind: "site",
      id: site.id,
      name: site.name,
      proto: site.tls ? "HTTPS" : "HTTP",
      port: site.listenPort,
      target: site.serverName,
      upstreamId: site.upstreamId,
      note: describeGroup(group, site.webSocket ? "WebSocket" : ""),
    };
  });
  const streamRows: ListenerRow[] = streams.value.map((stream) => ({
    key: `stream-${stream.id}`,
    kind: "stream",
    id: stream.id,
    name: stream.name,
    proto: stream.protocol === "udp" ? "UDP" : "TCP",
    port: stream.listenPort,
    target: "四层转发",
    upstreamId: stream.upstreamId,
    note: describeGroup(byId.get(stream.upstreamId), ""),
  }));
  return [...siteRows, ...streamRows].sort((a, b) => a.port - b.port || a.proto.localeCompare(b.proto));
});

function describeGroup(group: Upstream | undefined, extra: string) {
  if (!group) return extra || "还没选服务器组";
  const servers = group.servers.map((server) => `${server.address}:${server.port}`).join("、");
  return [group.name, servers, extra].filter(Boolean).join(" · ");
}

async function load() {
  const [siteRows, streamRows, upstreamRows, certRows] = await Promise.all([
    api<Site[]>(`/api/v1/clusters/${props.clusterId}/sites`),
    api<StreamProxy[]>(`/api/v1/clusters/${props.clusterId}/streams`),
    api<Upstream[]>(`/api/v1/clusters/${props.clusterId}/upstreams`),
    api<Certificate[]>(`/api/v1/clusters/${props.clusterId}/certificates`),
  ]);
  sites.value = siteRows;
  streams.value = streamRows;
  upstreams.value = upstreamRows;
  certs.value = certRows;
}

function showError(err: unknown) {
  message.error(err instanceof ApiError ? err.message : "请求失败");
}

function pick(next: "http" | "https" | "tcp" | "udp") {
  if (editing.value) return;
  proto.value = next;
  form.value.listenPort = next === "https" ? 443 : next === "http" ? 80 : next === "udp" ? 53 : 3306;
  form.value.upstreamId = "";
}

function create() {
  editing.value = null;
  proto.value = "http";
  form.value = emptyForm();
  open.value = true;
}

function edit(row: ListenerRow) {
  if (row.kind === "site") {
    const site = sites.value.find((item) => item.id === row.id);
    if (!site) return;
    editing.value = { kind: "site", id: site.id };
    proto.value = site.tls ? "https" : "http";
    form.value = {
      name: site.name,
      listenPort: site.listenPort,
      serverName: site.serverName,
      certificateId: site.certificateId,
      upstreamId: site.upstreamId,
      webSocket: site.webSocket,
    };
  } else {
    const stream = streams.value.find((item) => item.id === row.id);
    if (!stream) return;
    editing.value = { kind: "stream", id: stream.id };
    proto.value = stream.protocol === "udp" ? "udp" : "tcp";
    form.value = { ...emptyForm(), name: stream.name, listenPort: stream.listenPort, upstreamId: stream.upstreamId };
  }
  open.value = true;
}

async function save() {
  saving.value = true;
  try {
    if (proto.value === "tcp" || proto.value === "udp") {
      const body = { name: form.value.name, protocol: proto.value, listenPort: form.value.listenPort, upstreamId: form.value.upstreamId };
      if (editing.value?.kind === "stream") await api(`/api/v1/streams/${editing.value.id}`, { method: "PUT", body: JSON.stringify(body) });
      else await api(`/api/v1/clusters/${props.clusterId}/streams`, { method: "POST", body: JSON.stringify(body) });
    } else {
      const body = {
        name: form.value.name,
        listenPort: form.value.listenPort,
        serverName: form.value.serverName,
        tls: proto.value === "https",
        certificateId: proto.value === "https" ? form.value.certificateId : "",
        upstreamId: form.value.upstreamId,
        webSocket: form.value.webSocket,
      };
      if (editing.value?.kind === "site") await api(`/api/v1/sites/${editing.value.id}`, { method: "PUT", body: JSON.stringify(body) });
      else await api(`/api/v1/clusters/${props.clusterId}/sites`, { method: "POST", body: JSON.stringify(body) });
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

function remove(row: ListenerRow) {
  dialog.warning({
    title: "删除监听",
    content: `删除 ${row.proto} :${row.port} ${row.name}？删除后要再发布，节点才会停掉这个端口。`,
    positiveText: "删除",
    negativeText: "取消",
    onPositiveClick: async () => {
      try {
        const path = row.kind === "site" ? `/api/v1/sites/${row.id}` : `/api/v1/streams/${row.id}`;
        await api(path, { method: "DELETE" });
        await load();
        emit("changed");
      } catch (err) {
        showError(err);
      }
    },
  });
}

const columns: DataTableColumns<ListenerRow> = [
  {
    title: "监听",
    key: "listen",
    render: (row) =>
      h("div", { class: "listen-cell" }, [
        h(NTag, { size: "small", bordered: false, type: row.proto === "HTTPS" ? "success" : row.proto === "UDP" ? "warning" : "info" }, () => row.proto),
        h("strong", `:${row.port}`),
      ]),
  },
  { title: "名称", key: "name" },
  { title: "域名 / 用途", key: "target" },
  { title: "转发到", key: "note" },
  {
    title: "",
    key: "actions",
    width: 140,
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
      <p class="muted">七层用域名区分。同一个 443 可以挂多个网站，直接用 IP 打开不会进其中任何一个。四层按端口独占。</p>
      <n-button type="primary" @click="create">添加监听</n-button>
    </div>
    <n-data-table :columns="columns" :data="rows" :bordered="false" :scroll-x="860" :row-key="(row: ListenerRow) => row.key" />
    <n-modal v-model:show="open" preset="card" :title="editing ? '编辑监听' : '添加监听'" style="width: 640px">
      <div class="proto-pick">
        <button type="button" :class="{ on: proto === 'http' }" :disabled="!!editing" @click="pick('http')">
          <strong>HTTP</strong>
          <span>七层网站</span>
        </button>
        <button type="button" :class="{ on: proto === 'https' }" :disabled="!!editing" @click="pick('https')">
          <strong>HTTPS</strong>
          <span>七层，带证书</span>
        </button>
        <button type="button" :class="{ on: proto === 'tcp' }" :disabled="!!editing" @click="pick('tcp')">
          <strong>TCP</strong>
          <span>四层，如数据库</span>
        </button>
        <button type="button" :class="{ on: proto === 'udp' }" :disabled="!!editing" @click="pick('udp')">
          <strong>UDP</strong>
          <span>四层报文</span>
        </button>
      </div>
      <n-form class="field-grid">
        <n-form-item label="名称"><n-input v-model:value="form.name" placeholder="www" /></n-form-item>
        <n-form-item label="端口"><n-input-number v-model:value="form.listenPort" :min="1" :max="65535" /></n-form-item>
        <n-form-item v-if="proto === 'http' || proto === 'https'" class="span-2" label="域名">
          <n-input v-model:value="form.serverName" placeholder="www.example.com" />
        </n-form-item>
        <n-form-item class="span-2" label="服务器组">
          <div>
            <n-select v-model:value="form.upstreamId" :options="groups.map((item) => ({ label: item.name, value: item.id }))" placeholder="选择要把流量转到的一组后端" />
            <p v-if="groups.length === 0" class="muted">
              还没有{{ proto === 'tcp' || proto === 'udp' ? '四层' : '七层' }}服务器组。
              <button type="button" class="text-btn" @click="emit('goto', 'groups'); open = false">去创建</button>
            </p>
          </div>
        </n-form-item>
        <n-form-item v-if="proto === 'https'" class="span-2" label="证书">
          <div>
            <n-select v-model:value="form.certificateId" :options="certs.map((item) => ({ label: item.name, value: item.id }))" placeholder="选择证书" />
            <p v-if="certs.length === 0" class="muted">
              还没有证书。
              <button type="button" class="text-btn" @click="emit('goto', 'certs'); open = false">去上传</button>
            </p>
          </div>
        </n-form-item>
        <n-form-item v-if="proto === 'http' || proto === 'https'" label="WebSocket">
          <n-switch v-model:value="form.webSocket" />
        </n-form-item>
      </n-form>
      <template #footer>
        <n-button type="primary" :loading="saving" @click="save">保存</n-button>
      </template>
    </n-modal>
  </div>
</template>
