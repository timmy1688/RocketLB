<script setup lang="ts">
import { onMounted, ref, watch } from "vue";
import { RouterLink, useRoute } from "vue-router";
import { NAlert, NButton, NSpin, NTabPane, NTabs } from "naive-ui";
import { api } from "../../api/client";
import type { Bundle, Cluster, Revision } from "../../api/types";
import { layoutText } from "../../lib/deploy";
import PageHeader from "../../components/PageHeader.vue";
import ListenerPanel from "./ListenerPanel.vue";
import UpstreamPanel from "./UpstreamPanel.vue";
import MemberPanel from "./MemberPanel.vue";
import CertPanel from "./CertPanel.vue";
import PublishPanel from "./PublishPanel.vue";

const route = useRoute();
const cluster = ref<Cluster | null>(null);
const tab = ref("listeners");
const publishState = ref<"same" | "dirty" | "none" | "">("");

function memberLine(item: Cluster) {
  if (item.layout === "split") {
    const lvs = item.members.filter((member) => member.role === "lvs").length;
    const nginx = item.members.filter((member) => member.role === "nginx").length;
    return `转发 ${lvs} · nginx ${nginx}`;
  }
  return `${item.members.length} 台一起接流量`;
}

async function refreshPublish() {
  if (!cluster.value) return;
  const id = cluster.value.id;
  const [bundle, revisions] = await Promise.all([
    api<Bundle>(`/api/v1/clusters/${id}/preview`).catch(() => null),
    api<Revision[]>(`/api/v1/clusters/${id}/revisions`).catch(() => []),
  ]);
  const live = revisions.find((row) => row.status === "published");
  if (!live) publishState.value = "none";
  else if (bundle && bundle.hash === live.hash) publishState.value = "same";
  else publishState.value = "dirty";
}

async function load() {
  cluster.value = null;
  publishState.value = "";
  cluster.value = await api<Cluster>(`/api/v1/clusters/${route.params.id}`);
  await refreshPublish();
}

onMounted(load);
watch(() => route.params.id, load);
watch(tab, (name) => {
  if (name === "listeners" || name === "groups") void refreshPublish();
});
</script>

<template>
  <n-spin :show="!cluster">
    <template v-if="cluster">
      <p class="crumb"><RouterLink to="/clusters">实例</RouterLink> / {{ cluster.name }}</p>
      <PageHeader :title="cluster.name">
        <n-button v-if="publishState === 'dirty' || publishState === 'none'" type="primary" @click="tab = 'changes'">
          {{ publishState === "none" ? "去安装并发布" : "去发布" }}
        </n-button>
      </PageHeader>
      <div class="instance-facts">
        <div>
          <span>VIP</span>
          <strong class="vip">{{ cluster.vip }}/{{ cluster.prefix }}</strong>
        </div>
        <div>
          <span>部署</span>
          <strong>{{ layoutText(cluster.layout) }}</strong>
          <p class="fact-sub">{{ memberLine(cluster) }}</p>
        </div>
        <div>
          <span>网卡</span>
          <strong>{{ cluster.interface }}</strong>
        </div>
        <div>
          <span>线上</span>
          <strong v-if="publishState === 'same'">已是最新</strong>
          <strong v-else-if="publishState === 'dirty'">有改动未发布</strong>
          <strong v-else-if="publishState === 'none'">还没发布</strong>
          <strong v-else>…</strong>
        </div>
      </div>
      <n-alert v-if="publishState === 'dirty'" type="warning" style="margin-bottom: 16px">
        监听或服务器组已经改过，节点上的转发还是上一版。发布之后才会生效。
      </n-alert>
      <n-alert v-else-if="publishState === 'none'" type="info" style="margin-bottom: 16px">
        这个实例还没有成功发布。第一次要先安装软件，再把配置写到节点。
      </n-alert>
      <n-tabs v-model:value="tab" type="line" animated>
        <n-tab-pane name="listeners" tab="监听">
          <ListenerPanel :cluster-id="cluster.id" @changed="refreshPublish" @goto="tab = $event" />
        </n-tab-pane>
        <n-tab-pane name="groups" tab="服务器组">
          <UpstreamPanel :cluster-id="cluster.id" @changed="refreshPublish" />
        </n-tab-pane>
        <n-tab-pane name="nodes" tab="节点">
          <MemberPanel :cluster="cluster" :published="publishState === 'same' || publishState === 'dirty'" @saved="load" />
        </n-tab-pane>
        <n-tab-pane name="certs" tab="证书">
          <CertPanel :cluster-id="cluster.id" />
        </n-tab-pane>
        <n-tab-pane name="changes" tab="变更与发布">
          <PublishPanel :cluster-id="cluster.id" :members="cluster.members" :active="tab === 'changes'" />
        </n-tab-pane>
      </n-tabs>
    </template>
  </n-spin>
</template>
