export function formatTime(value?: string) {
  if (!value) return "—";
  const date = new Date(value);
  if (Number.isNaN(date.getTime())) return value;
  return date.toLocaleString("zh-CN", { hour12: false });
}

export function statusText(status: string) {
  if (status === "online") return "在线";
  if (status === "offline") return "离线";
  return "未知";
}

export function jobText(status: string) {
  if (status === "succeeded") return "成功";
  if (status === "failed") return "失败";
  if (status === "running") return "进行中";
  return status;
}

export function methodText(method: string) {
  if (method === "least_conn") return "最少连接";
  if (method === "ip_hash" || method === "hash") return "源地址哈希";
  return "轮询";
}

export function actionText(action: string) {
  if (action === "publish") return "发布";
  if (action === "rollback") return "回滚";
  if (action === "provision") return "安装";
  return action;
}
