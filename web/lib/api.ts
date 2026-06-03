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
  parameters?: Record<string, string>;
  extensions?: string[];
  public?: boolean;
}

export interface AuditEntry {
  id: string;
  actor: string;
  action: string;
  target: string;
  metadata?: Record<string, string>;
  created_at: string;
}

export interface PoolStat {
  database: string;
  user: string;
  pool_mode: string;
  cl_active: number;
  cl_waiting: number;
  cl_idle: number;
  sv_active: number;
  sv_idle: number;
  sv_used: number;
  sv_tested: number;
  sv_login: number;
  maxwait: number;
  maxwait_us: number;
}

export interface PoolDbStat {
  database: string;
  total_xact_count: number;
  total_query_count: number;
  total_received: number;
  total_sent: number;
  avg_xact_count: number;
  avg_query_count: number;
  avg_query_time: number;
  avg_wait_time: number;
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
  annotations?: Record<string, string>;
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

export interface ActiveAlert {
  id: string;
  instance_id: string;
  kind: string;
  severity: "warning" | "critical";
  state: "firing" | "resolved";
  message: string;
  value?: number;
  threshold?: number;
  fired_at: string;
  resolved_at?: string;
}

export interface Hypertable {
  schema: string;
  name: string;
  num_chunks: number;
  size_bytes: number;
  compression_enabled: boolean;
}

export interface TimescaleJob {
  id: number;
  application: string;
  schedule_interval: string;
  next_start?: string;
  last_run_status: string;
}

export interface EventItem {
  id: string;
  instance_id?: string;
  cluster_id?: string;
  type: string;
  message: string;
  metadata?: Record<string, string>;
  created_at: string;
}

// A cataloged remote (migrate-in) dump. Field names mirror the password-free
// remoteDumpPayload returned by internal/api/remote.go. The source password is
// write-only and is NEVER present here.
export interface RemoteDump {
  id: string;
  object_key: string;
  source_host: string;
  source_db: string;
  server_major: number;
  size_bytes: number;
  created_at: string;
}

export type RemoteRestoreTarget = "instance" | "cluster";

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
  listAudit: (limit = 100) => request<{ entries: AuditEntry[] }>("GET", `/api/v1/audit?limit=${limit}`),
  login: (email: string, password: string) =>
    request<{ token: string; user: User }>("POST", "/api/v1/auth/login", { email, password }),
  // ssoLogin exchanges the proxy-verified identity (Authelia/OIDC forward-auth
  // header) for a PgFleet token. It only succeeds when the request arrives
  // through the IdP proxy; otherwise the server returns 401.
  ssoLogin: () => request<{ token: string; user: User }>("POST", "/api/v1/auth/sso"),
  logout: () => request<void>("POST", "/api/v1/auth/logout"),

  listInstances: () => request<{ instances: Instance[] }>("GET", "/api/v1/instances"),
  getInstance: (id: string) => request<{ instance: Instance }>("GET", `/api/v1/instances/${id}`),
  createInstance: (input: { name: string; repo_type: string; password: string; pg_version?: string; parameters?: Record<string, string>; extensions?: string[] }) =>
    request<void>("POST", "/api/v1/instances", input),
  cloneInstance: (id: string, input: { name: string; password: string }) =>
    request<void>("POST", `/api/v1/instances/${id}/clone`, input),
  setVisibility: (id: string, isPublic: boolean) =>
    request<void>("POST", `/api/v1/instances/${id}/visibility`, { public: isPublic }),
  downloadDump: async (id: string, name: string) => {
    const token = getToken();
    const res = await fetch(`/api/v1/instances/${id}/dump`, {
      headers: token ? { Authorization: `Bearer ${token}` } : {},
    });
    if (!res.ok) throw new Error((await res.text()) || "download failed");
    const blob = await res.blob();
    const url = URL.createObjectURL(blob);
    const a = document.createElement("a");
    a.href = url;
    a.download = `${name}.sql`;
    a.click();
    URL.revokeObjectURL(url);
  },
  startInstance: (id: string) => request<void>("POST", `/api/v1/instances/${id}/start`),
  stopInstance: (id: string) => request<void>("POST", `/api/v1/instances/${id}/stop`),
  restartInstance: (id: string) => request<void>("POST", `/api/v1/instances/${id}/restart`),
  destroyInstance: (id: string, retain: boolean) =>
    request<void>("DELETE", `/api/v1/instances/${id}?retain_backups=${retain}`),
  connection: (id: string) => request<{ dsn: string }>("GET", `/api/v1/instances/${id}/connection`),

  listClusters: () => request<{ clusters: Cluster[] }>("GET", "/api/v1/clusters"),
  getCluster: (id: string) => request<{ cluster: Cluster; members: Instance[] }>("GET", `/api/v1/clusters/${id}`),
  createCluster: (input: { name: string; replicas: number; password: string; repo_type?: string; pg_version?: string; pool_mode?: string; parameters?: Record<string, string>; extensions?: string[] }) =>
    request<void>("POST", "/api/v1/clusters", input),
  destroyCluster: (id: string) => request<void>("DELETE", `/api/v1/clusters/${id}?retain_backups=true`),
  clusterConnection: (id: string) => request<{ dsn: string }>("GET", `/api/v1/clusters/${id}/connection`),

  listBackups: (id: string) => request<{ backups: Backup[] }>("GET", `/api/v1/instances/${id}/backups`),
  createBackup: (id: string, type: string, annotation?: string) =>
    request<void>("POST", `/api/v1/instances/${id}/backups`, annotation ? { type, annotation } : { type }),
  deleteBackup: (id: string, label: string) =>
    request<void>("DELETE", `/api/v1/instances/${id}/backups/${encodeURIComponent(label)}`),
  verifyBackups: (id: string) => request<void>("POST", `/api/v1/instances/${id}/backups/verify`),
  restore: (id: string, input: { type?: string; target?: string; set?: string; delta?: boolean }) =>
    request<void>("POST", `/api/v1/instances/${id}/restore`, input),
  poolStats: (clusterId: string) =>
    request<{ pools: PoolStat[]; stats: PoolDbStat[] }>("GET", `/api/v1/clusters/${clusterId}/pool/stats`),

  latestMetrics: (id: string) => request<{ metrics: Record<string, MetricSample> }>("GET", `/api/v1/instances/${id}/metrics`),
  rangeMetrics: (id: string, metric: string, since: string) =>
    request<{ samples: MetricSample[] }>("GET", `/api/v1/instances/${id}/metrics/${metric}?since=${encodeURIComponent(since)}`),
  topQueries: (id: string) => request<{ queries: QueryStat[] }>("GET", `/api/v1/instances/${id}/queries`),

  health: () => request<{ reports: HealthReport[]; alerts: Alert[] }>("GET", "/api/v1/health"),

  // Active alerts (persisted, transition-tracked).
  listAlerts: () => request<{ alerts: ActiveAlert[] }>("GET", "/api/v1/alerts"),

  // Durable event timeline.
  listEvents: (params?: { instance_id?: string; type?: string; limit?: number }) => {
    const q = new URLSearchParams();
    if (params?.instance_id) q.set("instance_id", params.instance_id);
    if (params?.type) q.set("type", params.type);
    if (params?.limit) q.set("limit", String(params.limit));
    const qs = q.toString();
    return request<{ events: EventItem[] }>("GET", `/api/v1/events/history${qs ? `?${qs}` : ""}`);
  },

  // Instance container logs (tail).
  instanceLogs: (id: string) => request<{ logs: string }>("GET", `/api/v1/instances/${id}/logs`),

  // Ad-hoc SQL console.
  runSQL: (id: string, query: string) =>
    request<{ columns: string[]; rows: unknown[][]; rows_affected: number; command: string; truncated: boolean }>(
      "POST",
      `/api/v1/instances/${id}/sql`,
      { query },
    ),
  // One-shot container command.
  execCommand: (id: string, command: string[]) =>
    request<{ exit_code: number; stdout: string; stderr: string }>("POST", `/api/v1/instances/${id}/exec`, { command }),

  // TimescaleDB management.
  listHypertables: (id: string) =>
    request<{ hypertables: Hypertable[] }>("GET", `/api/v1/instances/${id}/timescale/hypertables`),
  timescaleJobs: (id: string) =>
    request<{ jobs: TimescaleJob[] }>("GET", `/api/v1/instances/${id}/timescale/jobs`),
  createHypertable: (id: string, input: { table: string; time_column: string }) =>
    request<void>("POST", `/api/v1/instances/${id}/timescale/hypertables`, input),
  addRetention: (id: string, input: { hypertable: string; drop_after: string }) =>
    request<void>("POST", `/api/v1/instances/${id}/timescale/retention`, input),
  removeRetention: (id: string, hypertable: string) =>
    request<void>("DELETE", `/api/v1/instances/${id}/timescale/retention?hypertable=${encodeURIComponent(hypertable)}`),
  enableCompression: (id: string, input: { hypertable: string; segment_by?: string; order_by?: string; compress_after?: string }) =>
    request<void>("POST", `/api/v1/instances/${id}/timescale/compression`, input),
  removeCompression: (id: string, hypertable: string) =>
    request<void>("DELETE", `/api/v1/instances/${id}/timescale/compression?hypertable=${encodeURIComponent(hypertable)}`),

  // Remote backup & restore (migrate-in). The capture password is write-only:
  // it is sent once and never returned or stored client-side.
  captureRemoteBackup: (body: {
    host: string;
    port: number;
    user: string;
    password: string;
    dbname: string;
    sslmode: string;
  }) => request<{ backup: RemoteDump }>("POST", "/api/v1/remote/backups", body),
  listRemoteBackups: () => request<{ backups: RemoteDump[] }>("GET", "/api/v1/remote/backups"),
  restoreRemoteBackup: (
    id: string,
    body: {
      target: RemoteRestoreTarget;
      name: string;
      password: string;
      repo_type: string;
      pg_version: string;
      replicas?: number;
    },
  ) => request<{ target: RemoteRestoreTarget; id: string }>("POST", `/api/v1/remote/backups/${id}/restore`, body),

  listUsers: () => request<{ users: User[] }>("GET", "/api/v1/users"),
  createUser: (input: { email: string; password: string; role: string }) =>
    request<{ user: User }>("POST", "/api/v1/users", input),
  disableUser: (id: string) => request<void>("POST", `/api/v1/users/${id}/disable`),
  enableUser: (id: string) => request<void>("POST", `/api/v1/users/${id}/enable`),
};
