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
export async function getHealth(signal: AbortSignal): Promise<Health> {
  const response = await fetch("/api/v1/health/ready", { signal });
  const body: Envelope<Health | null> = await response.json();
  if (!response.ok || body.code !== "OK" || !body.data)
    throw new Error(body.message || "健康检查失败");
  return body.data;
}
