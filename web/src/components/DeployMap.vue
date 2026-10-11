<script setup lang="ts">
import { computed } from "vue";
import type { Cluster } from "../api/types";
import { deployPieces, installedSummary, memberKind, roleText } from "../lib/deploy";

const props = defineProps<{ cluster: Cluster }>();

const members = computed(() =>
  [...props.cluster.members].sort((a, b) => {
    const aNginx = memberKind(props.cluster.layout, a.role) === "nginx" ? 1 : 0;
    const bNginx = memberKind(props.cluster.layout, b.role) === "nginx" ? 1 : 0;
    if (aNginx !== bNginx) return aNginx - bNginx;
    return b.priority - a.priority;
  }),
);

const vipNodeId = computed(() => members.value.find((member) => memberKind(props.cluster.layout, member.role) !== "nginx")?.nodeId ?? "");
</script>

<template>
  <div class="deploy-map">
    <p class="muted">入口由 Keepalived 守 VIP，分发由 LVS 选 nginx，代理由 nginx 转后端。</p>
    <p v-if="members.length === 0" class="muted">还没有成员。</p>
    <article v-for="member in members" :key="member.nodeId">
      <header>
        <strong>{{ member.nodeName }}</strong>
        <span>{{ member.address }}</span>
        <span class="tag">{{ roleText(cluster.layout, member.role) }}</span>
        <span v-if="member.nodeId === vipNodeId" class="tag">平时拿着 VIP</span>
        <span v-else-if="memberKind(cluster.layout, member.role) !== 'nginx'" class="tag">备机</span>
      </header>
      <p class="muted">部署 {{ installedSummary(cluster.layout, member.role) }}</p>
      <ul>
        <li v-for="piece in deployPieces(cluster.layout, member.role)" :key="piece.path">
          <span class="layer">{{ piece.layer }}</span>
          <strong>{{ piece.app }}</strong>
          <code>{{ piece.path }}</code>
          <span class="muted">{{ piece.note }}</span>
        </li>
      </ul>
    </article>
  </div>
</template>
