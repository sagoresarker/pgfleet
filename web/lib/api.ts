// Typed client for the PgFleet control-plane API.

export type Role = "admin" | "operator" | "viewer";

export interface User {
  id: string;
  email: string;
  role: Role;
}

export interface Instance {
  id: string;
  name: string;
  status: "provisioning" | "running" | "stopped" | "error" | "destroying" | "restoring";
  repo_type: "s3" | "local";
  pg_version: string;
  host_port: number;
  stanza: string;
  role: "standalone" | "primary" | "replica";
  cluster_id?: string;
  last_error?: string;
}

export interface Backup {
  id: string;
  label: string;
  type: string;
  repo_size: number;
  logical_size: number;
  wal_start: string;
  wal_stop: string;
  error: boolean;
}

export interface Cluster {
  id: string;
  name: string;
  status: "provisioning" | "running" | "degraded" | "error" | "destroying";
  router_port: number;
  last_error?: string;
}

export interface MetricSample {
  metric: string;
  value: number;
  at: string;
}

export interface QueryStat {
  query: string;
  calls: number;
  total_time_ms: number;
  mean_time_ms: number;
  rows: number;
}

export interface HealthReport {
  instance_id: string;
  archiving_ok: boolean;
  has_backup: boolean;
  wal_bytes: number;
  drill_ran: boolean;
  drill_ok: boolean;
  issues: string[];
  checked_at: string;
}

export interface Alert {
  instance_id: string;
  message: string;
}

const TOKEN_KEY = "pgfleet.token";

export function getToken(): string | null {
  if (typeof window === "undefined") return null;
  return window.localStorage.getItem(TOKEN_KEY);
}

export function setToken(token: string | null) {
  if (typeof window === "undefined") return;
  if (token) window.localStorage.setItem(TOKEN_KEY, token);
  else window.localStorage.removeItem(TOKEN_KEY);
}

export class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.status = status;
  }
}

async function request<T>(method: string, path: string, body?: unknown): Promise<T> {
  const headers: Record<string, string> = {};
  const token = getToken();
  if (token) headers["Authorization"] = `Bearer ${token}`;
  if (body !== undefined) headers["Content-Type"] = "application/json";

  const res = await fetch(path, {
    method,
    headers,
    body: body !== undefined ? JSON.stringify(body) : undefined,
  });

  if (res.status === 401) {
    setToken(null);
    if (typeof window !== "undefined" && !path.includes("/auth/login")) {
      window.location.href = "/login";
    }
  }

  if (!res.ok) {
    let message = res.statusText;
    try {
      const data = await res.json();
      if (data?.error) message = data.error;
    } catch {
      /* ignore */
    }
    throw new ApiError(res.status, message);
  }

  if (res.status === 204 || res.status === 202) return undefined as T;
  return (await res.json()) as T;
}

export const api = {
  login: (email: string, password: string) =>
    request<{ token: string; user: User }>("POST", "/api/v1/auth/login", { email, password }),
  logout: () => request<void>("POST", "/api/v1/auth/logout"),

  listInstances: () => request<{ instances: Instance[] }>("GET", "/api/v1/instances"),
  getInstance: (id: string) => request<{ instance: Instance }>("GET", `/api/v1/instances/${id}`),
  createInstance: (input: { name: string; repo_type: string; password: string; pg_version?: string }) =>
    request<void>("POST", "/api/v1/instances", input),
  startInstance: (id: string) => request<void>("POST", `/api/v1/instances/${id}/start`),
  stopInstance: (id: string) => request<void>("POST", `/api/v1/instances/${id}/stop`),
  restartInstance: (id: string) => request<void>("POST", `/api/v1/instances/${id}/restart`),
  destroyInstance: (id: string, retain: boolean) =>
    request<void>("DELETE", `/api/v1/instances/${id}?retain_backups=${retain}`),
  connection: (id: string) => request<{ dsn: string }>("GET", `/api/v1/instances/${id}/connection`),

  listClusters: () => request<{ clusters: Cluster[] }>("GET", "/api/v1/clusters"),
  getCluster: (id: string) => request<{ cluster: Cluster; members: Instance[] }>("GET", `/api/v1/clusters/${id}`),
  createCluster: (input: { name: string; replicas: number; password: string; repo_type?: string; pg_version?: string }) =>
    request<void>("POST", "/api/v1/clusters", input),
  destroyCluster: (id: string) => request<void>("DELETE", `/api/v1/clusters/${id}?retain_backups=true`),
  clusterConnection: (id: string) => request<{ dsn: string }>("GET", `/api/v1/clusters/${id}/connection`),

  listBackups: (id: string) => request<{ backups: Backup[] }>("GET", `/api/v1/instances/${id}/backups`),
  createBackup: (id: string, type: string) => request<void>("POST", `/api/v1/instances/${id}/backups`, { type }),
  restore: (id: string, input: { type?: string; target?: string; set?: string }) =>
    request<void>("POST", `/api/v1/instances/${id}/restore`, input),

  latestMetrics: (id: string) => request<{ metrics: Record<string, MetricSample> }>("GET", `/api/v1/instances/${id}/metrics`),
  rangeMetrics: (id: string, metric: string, since: string) =>
    request<{ samples: MetricSample[] }>("GET", `/api/v1/instances/${id}/metrics/${metric}?since=${encodeURIComponent(since)}`),
  topQueries: (id: string) => request<{ queries: QueryStat[] }>("GET", `/api/v1/instances/${id}/queries`),

  health: () => request<{ reports: HealthReport[]; alerts: Alert[] }>("GET", "/api/v1/health"),

  listUsers: () => request<{ users: User[] }>("GET", "/api/v1/users"),
  createUser: (input: { email: string; password: string; role: string }) =>
    request<{ user: User }>("POST", "/api/v1/users", input),
  disableUser: (id: string) => request<void>("POST", `/api/v1/users/${id}/disable`),
  enableUser: (id: string) => request<void>("POST", `/api/v1/users/${id}/enable`),
};
