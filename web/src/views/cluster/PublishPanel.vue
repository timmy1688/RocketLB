<script setup lang="ts">
import { computed, onMounted, ref, watch } from "vue";
import { NAlert, NButton, useDialog, useMessage } from "naive-ui";
import { api, ApiError } from "../../api/client";
import type { Bundle, ConfigFile, Job, Member, Revision, RevisionDetail } from "../../api/types";
import { actionText, formatTime, jobText } from "../../lib/format";

const props = defineProps<{ clusterId: string; members: Member[]; active?: boolean }>();
const message = useMessage();
const dialog = useDialog();
const bundle = ref<Bundle | null>(null);
const jobs = ref<Job[]>([]);
const revisions = ref<Revision[]>([]);
const liveFiles = ref<ConfigFile[]>([]);
const busy = ref("");
const ready = ref(false);
const fileKey = ref("");

function showError(err: unknown) {
  message.error(err instanceof ApiError ? err.message : "请求失败");
}

const live = computed(() => revisions.value.find((row) => row.status === "published") ?? null);

const viewingFiles = computed(() => bundle.value?.files ?? []);

const selectedFile = computed(() => shown.value.find((file) => file.id === fileKey.value) ?? shown.value[0] ?? null);

const sameAsLive = computed(() => Boolean(bundle.value && live.value && bundle.value.hash === live.value.hash));

function revisionText(status: string) {
  if (status === "published") return "已发布";
  if (status === "failed") return "失败";
  if (status === "publishing") return "发布中";
  return status;
}

function nodeName(file: ConfigFile) {
  if (!file.nodeId) return "";
  return props.members.find((item) => item.nodeId === file.nodeId)?.nodeName ?? file.nodeId;
}

function fileTitle(path: string) {
  if (path.endsWith("/keepalived.conf")) return "转发规则";
  if (path.endsWith("/lvs-prep")) return "转发脚本";
  if (path.endsWith("/sites.conf")) return "网站";
  if (path.endsWith("/nginx.conf")) return "nginx";
  if (path.endsWith("/http-upstreams.conf")) return "服务器组";
  if (path.endsWith("/streams.conf")) return "四层";
  if (path.endsWith("/stream-upstreams.conf")) return "四层服务器组";
  if (path.endsWith("/check-lvs")) return "健康检查";
  if (path.endsWith("/lvs-real")) return "nginx 收包";
  if (path.endsWith(".crt")) return "证书 " + path.split("/").pop();
  return path.split("/").pop() ?? path;
}

function fileRank(path: string) {
  const order = ["/keepalived.conf", "/lvs-prep", "/sites.conf", "/nginx.conf", "/http-upstreams.conf", "/streams.conf", "/stream-upstreams.conf", "/check-lvs", "/lvs-real", ".crt"];
  const index = order.findIndex((item) => path.endsWith(item));
  return index === -1 ? order.length : index;
}

const shown = computed(() => {
  const groups = new Map<string, { path: string; content: string; nodes: string[] }>();
  for (const file of viewingFiles.value) {
    if (file.path.endsWith(".key")) continue;
    const id = `${file.path}\n${file.content}`;
    const group = groups.get(id) ?? { path: file.path, content: file.content, nodes: [] };
    const name = nodeName(file);
    if (name) group.nodes.push(name);
    groups.set(id, group);
  }
  return [...groups.entries()]
    .map(([id, group]) => ({
      id,
      title: fileTitle(group.path),
      path: group.path,
      content: group.content,
      where: group.nodes.length ? group.nodes.join("、") : "相关节点",
      rank: fileRank(group.path),
    }))
    .sort((a, b) => a.rank - b.rank || a.where.localeCompare(b.where) || a.path.localeCompare(b.path));
});

function readForwards(files: ConfigFile[]) {
  const file = files.find((item) => item.path.endsWith("/keepalived.conf"));
  if (!file) return [];
  return file.content
    .split("virtual_server ")
    .slice(1)
    .map((block) => {
      const head = block.match(/^(\S+)\s+(\d+)/);
      const proto = block.match(/protocol\s+(\S+)/)?.[1] ?? "TCP";
      const reals = [...block.matchAll(/real_server\s+(\S+)\s+\d+/g)].map((item) => {
        const member = props.members.find((row) => row.address === item[1]);
        return member?.nodeName ?? item[1];
      });
      return { port: head?.[2] ?? "", proto, reals: [...new Set(reals)] };
    })
    .filter((item) => item.port);
}

const forwards = computed(() => readForwards(viewingFiles.value));

const changeLines = computed(() => {
  if (!bundle.value || !live.value || sameAsLive.value) return [];
  const next = readForwards(bundle.value.files);
  const prev = readForwards(liveFiles.value);
  const key = (item: { port: string; proto: string }) => `${item.port}/${item.proto}`;
  const prevKeys = new Set(prev.map(key));
  const nextKeys = new Set(next.map(key));
  const lines: string[] = [];
  for (const item of next) {
    if (!prevKeys.has(key(item))) lines.push(`开始转发 ${item.port} ${item.proto}，到 ${item.reals.join("、") || "nginx"}`);
  }
  for (const item of prev) {
    if (!nextKeys.has(key(item))) lines.push(`停止转发 ${item.port} ${item.proto}`);
  }
  if (lines.length === 0) lines.push("转发端口没变。网站或服务器组有修改，发布后才会到节点上。");
  return lines;
});

const headline = computed(() => {
  if (jobs.value[0]?.status === "running") return "正在写到节点";
  if (!live.value) return "还没装到机器上";
  if (sameAsLive.value) return "线上已是最新";
  return "有改动还没发布";
});

const detailText = computed(() => {
  if (!ready.value || !bundle.value) return "正在读取当前配置。";
  if (jobs.value[0]?.status === "running") return "写完之前先别关页面。有一台失败会把这次写入还原。";
  if (!live.value) return "右边这个按钮会先装软件，再把转发规则写到节点。以后改监听，再回来点发布。";
  if (sameAsLive.value) return `上次发布 ${formatTime(live.value.createdAt)}。现在不用点任何按钮。`;
  return "节点上还是上一版。看下面的变化，确认后点右边的发布。";
});

const step = computed((): 1 | 2 | 3 => {
  if (!live.value) return 3;
  if (!sameAsLive.value) return 2;
  return 1;
});

function preferForward() {
  const chosen = shown.value.find((file) => file.path.endsWith("/keepalived.conf")) ?? shown.value[0];
  fileKey.value = chosen?.id ?? "";
}

async function load() {
  const [jobRows, revisionRows] = await Promise.all([
    api<Job[]>(`/api/v1/clusters/${props.clusterId}/jobs`),
    api<Revision[]>(`/api/v1/clusters/${props.clusterId}/revisions`),
  ]);
  jobs.value = jobRows;
  revisions.value = revisionRows;
  ready.value = true;
  const published = revisionRows.find((row) => row.status === "published");
  if (!published) {
    liveFiles.value = [];
    return;
  }
  const row = await api<RevisionDetail>(`/api/v1/revisions/${published.id}`);
  liveFiles.value = row.files;
}

async function preview() {
  busy.value = "preview";
  try {
    bundle.value = await api<Bundle>(`/api/v1/clusters/${props.clusterId}/preview`);
    preferForward();
  } catch (err) {
    showError(err);
  } finally {
    busy.value = "";
  }
}

async function wait(job: Job) {
  let current = job;
  while (current.status === "running") {
    await new Promise((resolve) => setTimeout(resolve, 1000));
    current = await api<Job>(`/api/v1/jobs/${job.id}`);
    jobs.value = [current, ...jobs.value.filter((item) => item.id !== current.id)];
  }
  if (current.status === "succeeded") message.success(current.action === "provision" ? "已安装并发布" : "已发布到节点");
  else message.error(current.log.split("\n").filter(Boolean).slice(-1)[0] || "任务失败");
  await Promise.all([load(), preview()]);
}

function publish() {
  dialog.warning({
    title: "发布到节点",
    content: changeLines.value.join("\n") || "会把当前监听和服务器组写到全部节点。有一台失败会还原。",
    positiveText: "发布",
    negativeText: "取消",
    onPositiveClick: async () => {
      try {
        const job = await api<Job>(`/api/v1/clusters/${props.clusterId}/publish`, { method: "POST", body: "{}" });
        jobs.value = [job, ...jobs.value];
        await wait(job);
      } catch (err) {
        showError(err);
      }
    },
  });
}

function provision() {
  dialog.warning({
    title: "安装并发布",
    content: "在每台成员上安装对应的软件，然后把当前配置写上去。已经装过的会跳过。",
    positiveText: "安装并发布",
    negativeText: "取消",
    onPositiveClick: async () => {
      try {
        const job = await api<Job>(`/api/v1/clusters/${props.clusterId}/provision`, { method: "POST", body: "{}" });
        jobs.value = [job, ...jobs.value];
        await wait(job);
      } catch (err) {
        showError(err);
      }
    },
  });
}

function rollback(row: Revision) {
  dialog.warning({
    title: "恢复以前的发布",
    content: `节点会回到 ${formatTime(row.createdAt)} 那一版。`,
    positiveText: "回滚",
    negativeText: "取消",
    onPositiveClick: async () => {
      try {
        const job = await api<Job>(`/api/v1/clusters/${props.clusterId}/revisions/${row.id}/rollback`, {
          method: "POST",
          body: "{}",
        });
        await wait(job);
      } catch (err) {
        showError(err);
      }
    },
  });
}

onMounted(() => {
  void load();
  void preview();
});
watch(
  () => props.active,
  (on) => {
    if (on) {
      void load();
      void preview();
    }
  },
);
</script>

<template>
  <div class="stack">
    <n-alert v-if="members.length < 2" type="warning">至少两个节点才能发布。</n-alert>
    <section class="publish-hero">
      <div>
        <h2>{{ ready && bundle ? headline : "正在读取…" }}</h2>
        <p class="muted">{{ detailText }}</p>
      </div>
      <div class="publish-actions">
        <n-button
          v-if="ready && !live"
          type="primary"
          size="large"
          :disabled="members.length < 2 || !bundle || jobs[0]?.status === 'running'"
          @click="provision"
        >
          安装并发布
        </n-button>
        <n-button
          v-else-if="ready && bundle && !sameAsLive"
          type="primary"
          size="large"
          :disabled="members.length < 2 || !bundle || jobs[0]?.status === 'running'"
          @click="publish"
        >
          发布到节点
        </n-button>
        <span v-else-if="ready && bundle" class="tag">不用操作</span>
      </div>
    </section>
    <ol v-if="ready && bundle" class="publish-steps">
      <li :class="{ now: step === 1 }"><span>1</span>在「监听」或「服务器组」里改好并保存</li>
      <li :class="{ now: step === 2 }"><span>2</span>回到这一页，看有没有变化</li>
      <li :class="{ now: step >= 2 }"><span>3</span>{{ live ? "点「发布到节点」" : "点「安装并发布」" }}</li>
    </ol>
    <ul v-if="changeLines.length" class="change-list">
      <li v-for="line in changeLines" :key="line">{{ line }}</li>
    </ul>
    <div>
      <p class="section-label">{{ sameAsLive ? "正在转发" : "发布后会转发" }}</p>
      <ul v-if="forwards.length" class="forward-list">
        <li v-for="item in forwards" :key="item.port + item.proto">
          <strong>{{ item.port }}</strong>
          <span>{{ item.proto }}</span>
          <span class="muted">转到 {{ item.reals.join("、") }}</span>
        </li>
      </ul>
      <p v-else-if="bundle" class="muted">还没有监听。先到「监听」里添加端口。</p>
    </div>
    <details v-if="jobs[0]" class="fold" :open="jobs[0].status !== 'succeeded'">
      <summary>最近一次任务 · {{ actionText(jobs[0].action) }} · {{ jobText(jobs[0].status) }} · {{ formatTime(jobs[0].createdAt) }}</summary>
      <div class="log">{{ jobs[0].log || "等待输出" }}</div>
    </details>
    <details class="fold">
      <summary>以前的发布（{{ revisions.length }}）</summary>
      <p v-if="revisions.length === 0" class="muted">还没有。第一次点上面的「安装并发布」。</p>
      <div v-for="row in revisions" :key="row.id" class="rev-line">
        <span v-if="live && row.id === live.id" class="tag">当前</span>
        <span>{{ revisionText(row.status) }}</span>
        <span class="muted">{{ formatTime(row.createdAt) }} · {{ row.note || "发布" }}</span>
        <n-button v-if="row.status === 'published' && live && row.id !== live.id" size="small" @click="rollback(row)">恢复这一版</n-button>
      </div>
    </details>
    <details class="fold">
      <summary>查看配置文件</summary>
      <div v-if="shown.length" class="file-board">
        <div class="file-list">
          <button
            v-for="file in shown"
            :key="file.id"
            type="button"
            :class="{ on: selectedFile && selectedFile.id === file.id }"
            @click="fileKey = file.id"
          >
            <strong>{{ file.title }}</strong>
            <span>{{ file.where }}</span>
          </button>
        </div>
        <div v-if="selectedFile" class="file-view">
          <div class="file-path">
            <strong>{{ selectedFile.title }}</strong>
            <span>{{ selectedFile.path }} · {{ selectedFile.where }}</span>
          </div>
          <pre class="code">{{ selectedFile.content }}</pre>
        </div>
      </div>
    </details>
    <details v-if="live" class="fold">
      <summary>重新安装软件</summary>
      <p class="muted">机器已经装过就不用点。只有新机器或软件缺失时才用。</p>
      <n-button :disabled="members.length < 2 || jobs[0]?.status === 'running'" @click="provision">安装并发布</n-button>
    </details>
  </div>
</template>
