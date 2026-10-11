export class ApiError extends Error {
  status: number;
  code: string;

  constructor(status: number, code: string, message: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

export async function api<T>(path: string, init: RequestInit = {}): Promise<T> {
  const headers = new Headers(init.headers);
  if (init.body) headers.set("Content-Type", "application/json");
  const token = localStorage.getItem("rocketlb.token");
  if (token) headers.set("Authorization", `Bearer ${token}`);
  const res = await fetch(path, { ...init, headers });
  if (res.status === 401 && !path.endsWith("/login")) {
    localStorage.removeItem("rocketlb.token");
    if (!location.pathname.startsWith("/login")) location.assign("/login");
  }
  const text = await res.text();
  const body = text ? JSON.parse(text) : {};
  if (!res.ok) {
    const message = body?.error?.message || "请求失败";
    throw new ApiError(res.status, body?.error?.code || "error", message);
  }
  return body.data as T;
}
