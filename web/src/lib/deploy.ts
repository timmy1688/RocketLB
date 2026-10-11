// 一键安装走 Ubuntu 官方源，版本跟着系统走。下面这组是已在实验室验证过的 Ubuntu 20.04 版本。
export function usedBy(
  clusters: { id: string; name: string; members?: { nodeId: string }[] }[],
  nodeId: string,
  exceptId = "",
) {
  const hit = clusters.find(
    (cluster) => cluster.id !== exceptId && cluster.members?.some((member) => member.nodeId === nodeId),
  );
  return hit?.name ?? "";
}

export function layoutText(layout: string) {
  return layout === "split" ? "四台分开" : "两台一起";
}

export const softwareNote =
  "nginx、Keepalived、ipvsadm 都用 Ubuntu 系统源，安装前把软件源换成阿里云镜像。20.04、22.04、24.04 是同一套命令。nginx 会装上 libnginx-mod-stream，四层才可用。版本跟着系统走：20.04 和 22.04 是 nginx 1.18.0，24.04 是 nginx 1.24.0。LVS 是内核 IPVS。";

export interface DeployPiece {
  layer: string;
  app: string;
  path: string;
  note: string;
}

export function memberKind(layout: string, role: string) {
  if (layout === "split") return role === "nginx" ? "nginx" : "lvs";
  return "both";
}

export function vipDuty(layout: string, role: string, holdsVip: boolean) {
  if (memberKind(layout, role) === "nginx") return "—";
  return holdsVip ? "平时拿着 VIP" : "备机";
}

export function roleText(layout: string, role: string) {
  const kind = memberKind(layout, role);
  if (kind === "nginx") return "nginx";
  if (kind === "both") return "转发并代理";
  return "转发";
}

export function installedSummary(layout: string, role: string) {
  const kind = memberKind(layout, role);
  if (kind === "nginx") return "nginx、libnginx-mod-stream";
  if (kind === "both") return "Keepalived、ipvsadm、nginx、libnginx-mod-stream";
  return "Keepalived、ipvsadm";
}

export function deployPieces(layout: string, role: string): DeployPiece[] {
  const kind = memberKind(layout, role);
  if (kind === "nginx") {
    return [
      { layer: "代理", app: "nginx", path: "/etc/nginx/nginx.conf", note: "七层和四层都在这台转到后端" },
      { layer: "接收", app: "VIP/32", path: "/usr/libexec/rocketlb/lvs-real", note: "回环上放 VIP，用来收 LVS 转来的包" },
    ];
  }
  const pieces: DeployPiece[] = [
    { layer: "入口", app: "Keepalived", path: "/etc/keepalived/keepalived.conf", note: "和另一台抢 VIP，检查本机 LVS 还能不能转发" },
    { layer: "分发", app: "LVS", path: "/usr/libexec/rocketlb/lvs-prep", note: "拿到 VIP 时按端口把连接分给 nginx" },
  ];
  if (kind === "both") {
    pieces.push({ layer: "代理", app: "nginx", path: "/etc/nginx/nginx.conf", note: "这台同时也是一台 nginx，回环上有 VIP/32" });
  }
  return pieces;
}

export function deployLine(layout: string, role: string) {
  return `${roleText(layout, role)} · ${installedSummary(layout, role)}`;
}
