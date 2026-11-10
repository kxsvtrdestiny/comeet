import { useAuth } from "../stores/auth";
export interface Envelope<T> {
  code: string;
  message: string;
  data: T;
  requestId: string;
}
export interface Health {
  database: string;
  schemaVersion: string;
  mediaConfigured: boolean;
  aiConfigured: boolean;
  stage: number;
}
export class ApiError extends Error {
  constructor(
    public code: string,
    message: string,
    public status: number,
  ) {
    super(message);
  }
}
export async function request<T>(
  path: string,
  options: RequestInit = {},
): Promise<T> {
  const headers = new Headers(options.headers);
  if (options.body) headers.set("Content-Type", "application/json");
  const token = useAuth.getState().session?.token;
  if (token) headers.set("Authorization", `Bearer ${token}`);
  let response: Response;
  try {
    const timeout = AbortSignal.timeout(10000);
    const signal = options.signal
      ? AbortSignal.any([options.signal, timeout])
      : timeout;
    response = await fetch("/api/v1" + path, { ...options, headers, signal });
  } catch (e) {
    if ((e as Error).name === "AbortError") throw e;
    throw new ApiError(
      "NETWORK_ERROR",
      "无法连接服务，请检查后端是否运行后重试",
      0,
    );
  }
  let body: Envelope<T>;
  try {
    body = await response.json();
  } catch {
    throw new ApiError(
      "INVALID_RESPONSE",
      "服务返回无效响应，请稍后重试",
      response.status,
    );
  }
  if (!response.ok || body.code !== "OK") {
    if (
      response.status === 401 &&
      useAuth.getState().session?.token === token &&
      !path.startsWith("/auth/login") &&
      !path.startsWith("/auth/register")
    )
      useAuth
        .getState()
        .clear(
          body.code === "SESSION_EXPIRED"
            ? "登录已过期或已撤销，请重新登录"
            : "登录已失效，请重新登录",
        );
    throw new ApiError(body.code, body.message, response.status);
  }
  return body.data;
}
export function post<T>(path: string, body?: unknown) {
  return request<T>(path, {
    method: "POST",
    body: body === undefined ? undefined : JSON.stringify(body),
  });
}
export function getHealth(signal: AbortSignal) {
  return request<Health>("/health/ready", { signal });
}
