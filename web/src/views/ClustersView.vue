<script setup lang="ts">
import { computed, h, onMounted, ref } from "vue";
import { RouterLink, useRouter } from "vue-router";
import {
  NButton,
  NDataTable,
  NForm,
  NFormItem,
  NInput,
  NModal,
  NRadio,
  NRadioGroup,
  NSelect,
  NSwitch,
  useMessage,
  type DataTableColumns,
} from "naive-ui";
import { api, ApiError } from "../api/client";
import { roleText, softwareNote, usedBy } from "../lib/deploy";
import type { Cluster, LbNode } from "../api/types";
import PageHeader from "../components/PageHeader.vue";

const router = useRouter();
const message = useMessage();
const rows = ref<Cluster[]>([]);
const nodes = ref<LbNode[]>([]);
const open = ref(false);
const saving = ref(false);
const selected = ref<Record<string, boolean>>({});
const holder = ref("");
const role = ref<Record<string, "lvs" | "nginx">>({});
const layout = ref<"combined" | "split">("combined");
const form = ref({
  name: "",
  vip: "",
  interface: "ens33",
});

async function load() {
  rows.value = await api<Cluster[]>("/api/v1/clusters");
  nodes.value = await api<LbNode[]>("/api/v1/nodes");
}

function showCreate() {
  form.value = { name: "", vip: "", interface: "ens33" };
  layout.value = "combined";
  selected.value = {};
  holder.value = "";
  role.value = {};
  nodes.value.forEach((node) => {
    selected.value[node.id] = false;
    role.value[node.id] = "lvs";
  });
  open.value = true;
}

function isForward(id: string) {
  return layout.value === "combined" || role.value[id] === "lvs";
}

const holderId = computed(() => {
  const forwards = nodes.value.filter((node) => selected.value[node.id] && !usedBy(rows.value, node.id) && isForward(node.id));
  if (forwards.some((node) => node.id === holder.value)) return holder.value;
  return forwards[0]?.id ?? "";
});

const orderedNodes = computed(() =>
  [...nodes.value].sort(
    (a, b) => Number(!!usedBy(rows.value, a.id)) - Number(!!usedBy(rows.value, b.id)) || a.name.localeCompare(b.name),
  ),
);
const freeCount = computed(() => nodes.value.filter((node) => !usedBy(rows.value, node.id)).length);

function priorities() {
  let backup = 140;
  const out: Record<string, number> = {};
  for (const node of nodes.value.filter((item) => selected.value[item.id])) {
    if (!isForward(node.id)) {
      out[node.id] = 100;
      continue;
    }
    if (node.id === holderId.value) out[node.id] = 150;
    else {
      out[node.id] = backup;
      backup -= 10;
    }
  }
  return out;
}

async function save() {
  const rank = priorities();
  const members = nodes.value
    .filter((node) => selected.value[node.id] && !usedBy(rows.value, node.id))
    .map((node) => ({
      nodeId: node.id,
      priority: rank[node.id] || 100,
      role: layout.value === "split" ? role.value[node.id] : "both",
    }));
  saving.value = true;
  try {
    const created = await api<Cluster>("/api/v1/clusters", {
      method: "POST",
      body: JSON.stringify({ ...form.value, layout: layout.value, members }),
    });
    open.value = false;
    router.push(`/clusters/${created.id}`);
  } catch (err) {
    message.error(err instanceof ApiError ? err.message : "创建失败");
  } finally {
    saving.value = false;
  }
}

const columns: DataTableColumns<Cluster> = [
  {
    title: "实例",
    key: "name",
    render: (row) => h(RouterLink, { to: `/clusters/${row.id}`, class: "name-link" }, () => row.name),
  },
  { title: "VIP", key: "vip", render: (row) => h("span", { class: "vip" }, `${row.vip}/${row.prefix}`) },
  {
    title: "部署",
    key: "layout",
    render: (row) => (row.layout === "split" ? "四台分开" : "两台一起"),
  },
  {
    title: "节点",
    key: "members",
    render: (row) =>
      row.members?.length
        ? row.members.map((member) => `${member.nodeName} ${roleText(row.layout, member.role)}`).join("，")
        : "还没有节点",
  },
  {
    title: "",
    key: "actions",
    render: (row) => h(NButton, { size: "small", onClick: () => router.push(`/clusters/${row.id}`) }, () => "管理"),
  },
];

onMounted(load);
</script>

<template>
  <PageHeader title="负载均衡实例" description="一个实例对外是一个 VIP。点进实例后配置监听和服务器组，再发布到节点。">
    <n-button type="primary" @click="showCreate">创建实例</n-button>
  </PageHeader>
  <n-data-table :columns="columns" :data="rows" :bordered="false" :scroll-x="980" :row-key="(row: Cluster) => row.id" />
  <n-modal v-model:show="open" preset="card" title="创建实例" style="width: 720px">
    <n-form>
      <n-form-item label="名称"><n-input v-model:value="form.name" /></n-form-item>
      <n-form-item label="VIP"><n-input v-model:value="form.vip" placeholder="10.0.0.10" /></n-form-item>
      <n-form-item label="网卡"><n-input v-model:value="form.interface" placeholder="ens33" /></n-form-item>
      <n-form-item label="部署">
        <n-radio-group v-model:value="layout">
          <n-radio value="combined">两台一起</n-radio>
          <n-radio value="split">四台分开</n-radio>
        </n-radio-group>
      </n-form-item>
      <p v-if="layout === 'combined'" class="muted">转发和 nginx 装在同一台，至少选两台。</p>
      <p v-else class="muted">选两台转发、两台 nginx。勾选的那台平时拿着 VIP，另一台转发等着接。</p>
      <p class="muted">一台机器只能属于一个实例。已经在用的机器不能再选。</p>
      <p class="muted">{{ softwareNote }}</p>
      <n-form-item label="成员">
        <div style="width: 100%">
          <p v-if="nodes.length === 0" class="muted">还没有节点。先到「节点」把机器加进来。</p>
          <p v-else-if="freeCount === 0" class="muted">现有机器都已经属于别的实例。要做新实例，先添加新机器。</p>
          <div class="member-list">
            <div v-for="node in orderedNodes" :key="node.id" class="member-row" :class="{ split: layout === 'split', taken: usedBy(rows, node.id) }">
              <n-switch v-model:value="selected[node.id]" :disabled="!!usedBy(rows, node.id)" />
              <div>
                <div>{{ node.name }}</div>
                <div v-if="usedBy(rows, node.id)" class="muted">已用于 {{ usedBy(rows, node.id) }}</div>
                <div v-else class="muted">{{ node.address }}</div>
              </div>
              <label v-if="!usedBy(rows, node.id) && layout === 'split'" class="role">
                角色
                <n-select
                  v-model:value="role[node.id]"
                  :options="[
                    { label: '转发', value: 'lvs' },
                    { label: 'nginx', value: 'nginx' },
                  ]"
                />
              </label>
              <label v-if="!usedBy(rows, node.id) && selected[node.id] && (layout === 'combined' || role[node.id] === 'lvs')" class="prio">
                <n-radio :checked="holderId === node.id" @update:checked="(on: boolean) => on && (holder = node.id)">平时拿着 VIP</n-radio>
              </label>
            </div>
          </div>
        </div>
      </n-form-item>
    </n-form>
    <template #footer>
      <n-button type="primary" :loading="saving" @click="save">创建</n-button>
    </template>
  </n-modal>
</template>
