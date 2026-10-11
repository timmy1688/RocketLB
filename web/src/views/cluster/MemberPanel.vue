<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { NAlert, NButton, NForm, NFormItem, NInput, NRadio, NRadioGroup, NSelect, NSwitch, useMessage } from "naive-ui";
import { api, ApiError } from "../../api/client";
import { installedSummary, memberKind, roleText, softwareNote, usedBy, vipDuty } from "../../lib/deploy";
import type { Cluster, LbNode } from "../../api/types";

const props = defineProps<{ cluster: Cluster; published: boolean }>();
const emit = defineEmits<{ saved: [] }>();
const message = useMessage();
const nodes = ref<LbNode[]>([]);
const clusters = ref<Cluster[]>([]);
const saving = ref(false);
const editingOpen = ref(false);
const selected = ref<Record<string, boolean>>({});
const holder = ref("");
const role = ref<Record<string, "lvs" | "nginx">>({});
const layout = ref<"combined" | "split">("combined");
const form = ref({
  name: "",
  vip: "",
  prefix: 24,
  vrid: 51,
  interface: "eth0",
  authPass: "",
  advertInt: 1,
});

function fill() {
  form.value = {
    name: props.cluster.name,
    vip: props.cluster.vip,
    prefix: props.cluster.prefix,
    vrid: props.cluster.vrid,
    interface: props.cluster.interface,
    authPass: "",
    advertInt: props.cluster.advertInt,
  };
  selected.value = {};
  holder.value = "";
  role.value = {};
  layout.value = props.cluster.layout === "split" ? "split" : "combined";
  let best = -1;
  for (const node of nodes.value) {
    selected.value[node.id] = false;
    role.value[node.id] = "lvs";
  }
  for (const member of props.cluster.members) {
    selected.value[member.nodeId] = true;
    role.value[member.nodeId] = member.role === "nginx" ? "nginx" : "lvs";
    const forward = memberKind(props.cluster.layout, member.role) !== "nginx";
    if (forward && member.priority > best) {
      best = member.priority;
      holder.value = member.nodeId;
    }
  }
}

function isForward(id: string) {
  return layout.value === "combined" || role.value[id] === "lvs";
}

function taken(id: string) {
  return usedBy(clusters.value, id, props.cluster.id);
}

const orderedNodes = computed(() =>
  [...nodes.value].sort((a, b) => Number(!!taken(a.id)) - Number(!!taken(b.id)) || a.name.localeCompare(b.name)),
);

const holderId = computed(() => {
  const forwards = nodes.value.filter((node) => selected.value[node.id] && isForward(node.id) && !taken(node.id));
  if (forwards.some((node) => node.id === holder.value)) return holder.value;
  return forwards[0]?.id ?? "";
});

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

const vipHolderId = computed(() => {
  const forwards = props.cluster.members.filter((member) => memberKind(props.cluster.layout, member.role) !== "nginx");
  return forwards.reduce((best, member) => (member.priority > (best?.priority ?? -1) ? member : best), forwards[0])?.nodeId ?? "";
});

const picked = computed(() => nodes.value.filter((node) => selected.value[node.id]).length);
const lvsCount = computed(() => nodes.value.filter((node) => selected.value[node.id] && role.value[node.id] === "lvs").length);
const nginxCount = computed(() => nodes.value.filter((node) => selected.value[node.id] && role.value[node.id] === "nginx").length);

async function loadNodes() {
  const [nodeRows, clusterRows] = await Promise.all([api<LbNode[]>("/api/v1/nodes"), api<Cluster[]>("/api/v1/clusters")]);
  nodes.value = nodeRows;
  clusters.value = clusterRows;
  fill();
}

async function save() {
  const rank = priorities();
  const members = props.published
    ? props.cluster.members.map((member) => ({
        nodeId: member.nodeId,
        priority: member.priority,
        role: member.role,
      }))
    : nodes.value
        .filter((node) => selected.value[node.id] && !taken(node.id))
        .map((node) => ({
          nodeId: node.id,
          priority: rank[node.id] || 100,
          role: layout.value === "split" ? role.value[node.id] : "both",
        }));
  saving.value = true;
  try {
    await api(`/api/v1/clusters/${props.cluster.id}`, {
      method: "PUT",
      body: JSON.stringify({ ...form.value, layout: layout.value, members }),
    });
    message.success("已保存");
    editingOpen.value = false;
    emit("saved");
  } catch (err) {
    message.error(err instanceof ApiError ? err.message : "保存失败");
  } finally {
    saving.value = false;
  }
}

onMounted(loadNodes);
watch(() => props.cluster, fill);
</script>

<template>
  <div class="stack">
    <div class="panel-bar">
      <p v-if="published" class="muted">节点在创建时定好。实例已经上线，不能再增删机器、改角色或换谁拿 VIP。平时只改监听和服务器组。</p>
      <p v-else class="muted">这些机器组成这个实例。还没发布之前可以调整，已经属于别的实例的机器不能再选。发布之后就不能再改节点。</p>
      <n-button
        @click="
          fill();
          editingOpen = true;
        "
      >
        {{ published ? "改名称" : "修改实例" }}
      </n-button>
    </div>
    <div class="node-table">
      <div class="node-row head"><span>节点</span><span>地址</span><span>角色</span><span>VIP</span><span>部署</span></div>
      <div v-for="member in cluster.members" :key="member.nodeId" class="node-row">
        <strong>{{ member.nodeName }}</strong>
        <span>{{ member.address }}</span>
        <span class="tag">{{ roleText(cluster.layout, member.role) }}</span>
        <span>{{ vipDuty(cluster.layout, member.role, member.nodeId === vipHolderId) }}</span>
        <span class="muted">{{ installedSummary(cluster.layout, member.role) }}</span>
      </div>
      <p v-if="cluster.members.length === 0" class="muted">还没有节点。</p>
    </div>
    <n-modal v-model:show="editingOpen" preset="card" :title="published ? '改名称' : '修改实例'" style="width: 760px">
      <template v-if="published">
        <p class="muted" style="margin-bottom: 12px">名称只在控制面显示，不会改节点上的转发。</p>
        <n-form>
          <n-form-item label="名称"><n-input v-model:value="form.name" /></n-form-item>
          <n-button type="primary" :loading="saving" @click="save">保存</n-button>
        </n-form>
      </template>
      <template v-else>
      <n-alert v-if="layout === 'split' && (lvsCount < 2 || nginxCount < 2)" type="warning" style="margin-bottom: 12px">
        分开部署需要两台转发和两台 nginx。当前是 {{ lvsCount }} 台转发、{{ nginxCount }} 台 nginx。
      </n-alert>
      <n-alert v-else-if="picked < 2" type="warning" style="margin-bottom: 12px">发布和安装至少需要两个节点。</n-alert>
      <n-form class="field-grid">
      <n-form-item class="span-2" label="部署">
        <div>
          <n-radio-group v-model:value="layout">
            <n-radio value="combined">两台一起</n-radio>
            <n-radio value="split">四台分开</n-radio>
          </n-radio-group>
          <p v-if="layout === 'combined'" class="muted">转发和 nginx 装在同一台。至少两台，拿到 VIP 的那台把连接分给每一台。</p>
          <p v-else class="muted">两台只转发，两台只跑 nginx。勾选的那台平时拿着 VIP，另一台转发等着接。</p>
          <p class="muted">{{ softwareNote }}</p>
        </div>
      </n-form-item>
      <n-form-item label="名称"><n-input v-model:value="form.name" /></n-form-item>
      <n-form-item label="VIP"><n-input v-model:value="form.vip" /></n-form-item>
      <n-form-item label="网卡"><n-input v-model:value="form.interface" /></n-form-item>
      <n-form-item class="span-2" label="成员">
        <div class="member-list">
          <div v-for="node in orderedNodes" :key="node.id" class="member-row" :class="{ split: layout === 'split', taken: taken(node.id) }">
            <n-switch v-model:value="selected[node.id]" :disabled="!!taken(node.id)" />
            <div>
              <div>{{ node.name }}</div>
              <div v-if="taken(node.id)" class="muted">已用于 {{ taken(node.id) }}</div>
              <div v-else class="muted">{{ node.address }}</div>
              <div v-if="selected[node.id] && !taken(node.id)" class="muted">
                {{ roleText(layout, layout === "split" ? role[node.id] : "both") }} ·
                {{ installedSummary(layout, layout === "split" ? role[node.id] : "both") }}
              </div>
            </div>
            <label v-if="!taken(node.id) && layout === 'split'" class="role">
              角色
              <n-select
                v-model:value="role[node.id]"
                :options="[
                  { label: '转发', value: 'lvs' },
                  { label: 'nginx', value: 'nginx' },
                ]"
              />
            </label>
            <label v-if="!taken(node.id) && selected[node.id] && (layout === 'combined' || role[node.id] === 'lvs')" class="prio">
              <n-radio :checked="holderId === node.id" @update:checked="(on: boolean) => on && (holder = node.id)">平时拿着 VIP</n-radio>
            </label>
          </div>
        </div>
      </n-form-item>
      <div class="span-2">
        <n-button type="primary" :loading="saving" @click="save">保存</n-button>
      </div>
    </n-form>
      </template>
    </n-modal>
  </div>
</template>
