<script setup lang="ts">
import { computed, onMounted, ref } from "vue";
import { RouterLink } from "vue-router";
import { NCard } from "naive-ui";
import { api } from "../api/client";
import type { Cluster, LbNode, Revision } from "../api/types";
import PageHeader from "../components/PageHeader.vue";
import { layoutText, roleText } from "../lib/deploy";
import { formatTime, statusText } from "../lib/format";
const nodes = ref<LbNode[]>([]);
const clusters = ref<Cluster[]>([]);
const live = ref<Record<string, Revision | null>>({});

function clusterReady(cluster: Cluster) {
  if (cluster.layout === "split") {
    const lvs = cluster.members.filter((member) => member.role === "lvs").length;
    const nginx = cluster.members.filter((member) => member.role === "nginx").length;
    return lvs >= 2 && nginx >= 2;
  }
  return cluster.members.length >= 2;
}

const checks = computed(() => [
  {
    ok: nodes.value.filter((node) => node.status === "online").length >= 2,
    title: "节点已经探测在线",
    detail: "至少两台 SSH 在线。四台分开时，四台都要在线。",
    to: "/nodes",
  },
  {
    ok: clusters.value.some(clusterReady),
    title: "集群成员齐了",
    detail: "两台一起至少两台。四台分开要两台转发、两台 nginx。",
    to: "/clusters",
  },
  {
    ok: Object.values(live.value).some((row) => row),
    title: "配置已经发到节点",
    detail: "一键安装或发布成功后，VIP 才会按控制台里的配置转发。",
    to: clusters.value[0] ? `/clusters/${clusters.value[0].id}` : "/clusters",
  },
]);

const done = computed(() => checks.value.filter((item) => item.ok).length);

onMounted(async () => {
  const [nodeRows, clusterRows] = await Promise.all([api<LbNode[]>("/api/v1/nodes"), api<Cluster[]>("/api/v1/clusters")]);
  nodes.value = nodeRows;
  clusters.value = clusterRows;
  const rows = await Promise.all(
    clusterRows.map(async (cluster) => {
      const revisions = await api<Revision[]>(`/api/v1/clusters/${cluster.id}/revisions`);
      return [cluster.id, revisions.find((row) => row.status === "published") ?? null] as const;
    }),
  );
  live.value = Object.fromEntries(rows);
});

function place(node: LbNode) {
  for (const cluster of clusters.value) {
    const member = cluster.members.find((item) => item.nodeId === node.id);
    if (member) return { cluster, member };
  }
  return null;
}

function memberLine(cluster: Cluster) {
  if (cluster.layout === "split") {
    const lvs = cluster.members.filter((member) => member.role === "lvs").length;
    const nginx = cluster.members.filter((member) => member.role === "nginx").length;
    return `${lvs} 台转发，${nginx} 台 nginx`;
  }
  return `${cluster.members.length} 台一起接流量`;
}
</script>

<template>
  <PageHeader title="总览" description="从实例进入监听和服务器组。节点只负责把这个 VIP 扛起来。" />
  <div v-if="done < checks.length" class="check-list" style="margin-bottom: 16px">
    <div v-for="item in checks" :key="item.title" class="check-item">
      <i class="dot" :class="item.ok ? 'online' : 'offline'" />
      <div>
        <strong>{{ item.ok ? "已完成" : "还没做" }} · {{ item.title }}</strong>
        <p class="muted">{{ item.detail }}</p>
        <RouterLink v-if="item.to && !item.ok" :to="item.to">去处理</RouterLink>
      </div>
    </div>
  </div>
  <div class="instance-grid">
    <RouterLink v-for="cluster in clusters" :key="cluster.id" :to="`/clusters/${cluster.id}`" class="instance-card">
      <div class="panel-bar">
        <strong>{{ cluster.name }}</strong>
        <span v-if="live[cluster.id]" class="tag">运行中</span>
        <span v-else class="tag warn">未发布</span>
      </div>
      <p class="vip">{{ cluster.vip }}</p>
      <p class="muted">{{ layoutText(cluster.layout) }} · {{ memberLine(cluster) }}</p>
      <p class="role-line">
        <span v-for="member in cluster.members" :key="member.nodeId" class="tag">
          {{ member.nodeName }} {{ roleText(cluster.layout, member.role) }}
        </span>
      </p>
      <p v-if="live[cluster.id]" class="muted">发布于 {{ formatTime(live[cluster.id]?.createdAt) }}</p>
    </RouterLink>
    <RouterLink v-if="clusters.length === 0" to="/clusters" class="instance-card">
      <strong>还没有实例</strong>
      <p class="muted">创建一个 VIP，再把监听挂上去。</p>
    </RouterLink>
  </div>
  <n-card v-if="nodes.length" title="节点" style="margin-top: 16px">
    <div class="node-table compact">
      <div class="node-row head"><span>节点</span><span>地址</span><span>角色</span><span>状态</span></div>
      <div v-for="node in nodes" :key="node.id" class="node-row">
        <strong>{{ node.name }}</strong>
        <span>{{ node.address }}</span>
        <span v-if="place(node)">{{ roleText(place(node)!.cluster.layout, place(node)!.member.role) }}</span>
        <span v-else class="muted">未加入实例</span>
        <span><i class="dot" :class="node.status" />{{ statusText(node.status) }}</span>
      </div>
    </div>
  </n-card>
</template>
