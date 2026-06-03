"use client";

import { api, type MetricSample } from "@/lib/api";
import { useQuery } from "@tanstack/react-query";
import { Activity, Database, GaugeCircle, HardDrive, Waves } from "lucide-react";
import { Area, AreaChart, ResponsiveContainer, Tooltip, YAxis } from "recharts";
import { Card, CardBody, CardHeader, CardTitle, EmptyState } from "./ui";

const HOUR_MS = 60 * 60 * 1000;

/* ── shared data helpers ─────────────────────────────────────────────────── */

// toRate converts a cumulative counter series into a per-second rate series,
// clamping counter resets (negative deltas) to 0.
function toRate(samples: MetricSample[]): { at: string; value: number }[] {
  const out: { at: string; value: number }[] = [];
  for (let i = 1; i < samples.length; i++) {
    const dt = (new Date(samples[i].at).getTime() - new Date(samples[i - 1].at).getTime()) / 1000;
    if (dt <= 0) continue;
    out.push({ at: samples[i].at, value: Math.max(0, (samples[i].value - samples[i - 1].value) / dt) });
  }
  return out;
}

// sumRates merges rate series by TIMESTAMP (not array index): a counter reset or
// duplicate timestamp can make the input series differ in length, so zipping by
// index would add a commit-rate at T to a rollback-rate at T+1 and report a
// silently-wrong TPS.
function sumRates(...series: { at: string; value: number }[][]): { at: string; value: number }[] {
  const byAt = new Map<string, number>();
  for (const s of series) {
    for (const p of s) byAt.set(p.at, (byAt.get(p.at) ?? 0) + p.value);
  }
  return [...byAt.entries()]
    .map(([at, value]) => ({ at, value }))
    .sort((a, b) => a.at.localeCompare(b.at));
}

function useRange(id: string, metric: string, enabled: boolean) {
  const since = new Date(Date.now() - HOUR_MS).toISOString();
  return useQuery({
    queryKey: ["metrics-range", id, metric],
    queryFn: () => api.rangeMetrics(id, metric, since),
    refetchInterval: 15000,
    enabled,
  });
}

function fmtRate(n: number): string {
  if (n >= 1000) return (n / 1000).toFixed(1) + "k";
  return n.toFixed(n < 10 ? 1 : 0);
}

function fmtBytesRate(n: number): string {
  if (n >= 1024 * 1024) return (n / (1024 * 1024)).toFixed(1) + " MB/s";
  if (n >= 1024) return (n / 1024).toFixed(1) + " KB/s";
  return n.toFixed(0) + " B/s";
}

/* ── generic live area chart ─────────────────────────────────────────────── */

function LiveArea({
  data,
  color,
  gradientId,
  loading,
  empty,
  height = 150,
  formatValue,
}: {
  data: { at: string; value: number }[];
  color: string;
  gradientId: string;
  loading: boolean;
  empty: boolean;
  height?: number;
  formatValue: (n: number) => string;
}) {
  if (loading) return <div className="w-full animate-pulse rounded-md bg-ink-700/70" style={{ height }} aria-hidden="true" />;
  if (empty || data.length < 2)
    return (
      <div className="grid w-full place-items-center rounded-md border border-line bg-ink-850 text-xs text-fg-faint" style={{ height }}>
        collecting…
      </div>
    );
  return (
    <div className="w-full" style={{ height }}>
      <ResponsiveContainer width="100%" height="100%">
        <AreaChart data={data} margin={{ left: -24, right: 6, top: 6, bottom: 0 }}>
          <defs>
            <linearGradient id={gradientId} x1="0" y1="0" x2="0" y2="1">
              <stop offset="0%" stopColor={color} stopOpacity={0.4} />
              <stop offset="100%" stopColor={color} stopOpacity={0} />
            </linearGradient>
          </defs>
          <YAxis tick={{ fontSize: 10, fill: "var(--color-fg-faint)" }} width={42} tickFormatter={formatValue} />
          <Tooltip
            contentStyle={{ background: "var(--color-ink-800)", border: "1px solid var(--color-line)", borderRadius: 8, fontSize: 12, color: "var(--color-fg)" }}
            labelFormatter={() => ""}
            formatter={(v: number) => [formatValue(v), ""]}
          />
          <Area type="monotone" dataKey="value" stroke={color} strokeWidth={2} fill={`url(#${gradientId})`} isAnimationActive={false} />
        </AreaChart>
      </ResponsiveContainer>
    </div>
  );
}

/* ── TPS chart (commits + rollbacks per second) ──────────────────────────── */

export function TpsChart({ instanceId, running }: { instanceId: string; running: boolean }) {
  const commits = useRange(instanceId, "xact_commit", running);
  const rollbacks = useRange(instanceId, "xact_rollback", running);
  const cR = toRate(commits.data?.samples ?? []);
  const rR = toRate(rollbacks.data?.samples ?? []);
  const data = sumRates(cR, rR);
  const latest = data[data.length - 1]?.value;
  const loading = commits.isLoading || rollbacks.isLoading;

  return (
    <Card>
      <CardHeader>
        <CardTitle>
          <span className="inline-flex items-center gap-2">
            <Activity className="h-4 w-4 text-azure" /> Transactions / sec
          </span>
        </CardTitle>
        <span className="font-mono text-xs text-fg-muted tnum">{latest !== undefined ? `${fmtRate(latest)} tps` : "—"}</span>
      </CardHeader>
      <CardBody>
        <LiveArea data={data} color="var(--color-azure)" gradientId={`tps-${instanceId}`} loading={loading} empty={!running} formatValue={fmtRate} />
      </CardBody>
    </Card>
  );
}

/* ── WAL generation rate (bytes/sec) ─────────────────────────────────────── */

export function WalRateChart({ instanceId, running }: { instanceId: string; running: boolean }) {
  const wal = useRange(instanceId, "wal_bytes", running);
  const data = toRate(wal.data?.samples ?? []);
  const latest = data[data.length - 1]?.value;

  return (
    <Card>
      <CardHeader>
        <CardTitle>
          <span className="inline-flex items-center gap-2">
            <HardDrive className="h-4 w-4 text-signal" /> WAL generation rate
          </span>
        </CardTitle>
        <span className="font-mono text-xs text-fg-muted tnum">{latest !== undefined ? fmtBytesRate(latest) : "—"}</span>
      </CardHeader>
      <CardBody>
        <LiveArea data={data} color="var(--color-signal)" gradientId={`wal-${instanceId}`} loading={wal.isLoading} empty={!running} formatValue={fmtBytesRate} />
      </CardBody>
    </Card>
  );
}

/* ── Replication-lag "river" ─────────────────────────────────────────────── */

export function ReplicationLagRiver({ instanceId, name, running }: { instanceId: string; name?: string; running: boolean }) {
  const lag = useRange(instanceId, "replication_lag_seconds", running);
  const data = (lag.data?.samples ?? []).map((s) => ({ at: s.at, value: s.value }));
  const latest = data[data.length - 1]?.value;
  const tone = latest === undefined ? "var(--color-fg-faint)" : latest > 10 ? "var(--color-danger)" : latest > 2 ? "var(--color-signal)" : "var(--color-healthy)";

  return (
    <Card>
      <CardHeader>
        <CardTitle>
          <span className="inline-flex items-center gap-2">
            <Waves className="h-4 w-4 text-azure" /> Replication lag{name ? ` · ${name}` : ""}
          </span>
        </CardTitle>
        <span className="font-mono text-xs tnum" style={{ color: tone }}>
          {latest !== undefined ? (latest < 1 ? "<1s" : `${latest.toFixed(1)}s`) : "—"}
        </span>
      </CardHeader>
      <CardBody>
        {/* the flowing gradient band gives the "river" feel above the lag series */}
        <div
          className="mb-2 h-1.5 w-full rounded-full"
          style={{
            background: "linear-gradient(90deg, var(--color-azure), var(--color-violet), var(--color-azure))",
            backgroundSize: "200% 100%",
            animation: "river-drift 6s linear infinite",
            opacity: running ? 0.8 : 0.3,
          }}
          aria-hidden="true"
        />
        <LiveArea
          data={data}
          color={tone}
          gradientId={`lag-${instanceId}`}
          loading={lag.isLoading}
          empty={!running}
          formatValue={(n) => (n < 1 ? "<1s" : `${n.toFixed(0)}s`)}
        />
      </CardBody>
    </Card>
  );
}

/* ── Connection-pool gauge wall ──────────────────────────────────────────── */

export function ConnectionPoolGaugeWall({ instanceId, running }: { instanceId: string; running: boolean }) {
  const q = useQuery({
    queryKey: ["metrics-latest", instanceId],
    queryFn: () => api.latestMetrics(instanceId),
    refetchInterval: 6000,
    enabled: running,
  });
  const m = q.data?.metrics ?? {};
  const val = (k: string) => m[k]?.value;

  const util = val("connection_utilization");
  const conns = val("connections");
  const max = val("max_connections");
  const active = val("active_connections");
  const idleTx = val("idle_in_transaction");
  const waiting = val("waiting_sessions");

  return (
    <Card>
      <CardHeader>
        <CardTitle>
          <span className="inline-flex items-center gap-2">
            <GaugeCircle className="h-4 w-4 text-azure" /> Connection pool
          </span>
        </CardTitle>
        <span className="flex items-center gap-1.5 font-mono text-[11px] text-fg-faint">
          <span className={"led " + (q.isFetching ? "led-signal led-pulse" : running ? "led-healthy" : "led-idle")} />
          live
        </span>
      </CardHeader>
      <CardBody>
        {!running ? (
          <EmptyState icon={<Database className="h-5 w-5" />} title="Instance not running" description="Pool gauges appear when the instance is live." />
        ) : (
          <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
            <GaugeRing label="Utilization" value={util ?? 0} max={100} unit="%" warnAt={75} critAt={90} />
            <GaugeRing label="Connections" value={conns ?? 0} max={Math.max(1, max ?? 100)} sub={max ? `of ${Math.round(max)}` : undefined} warnAt={Math.max(1, max ?? 100) * 0.75} critAt={Math.max(1, max ?? 100) * 0.9} />
            <GaugeRing label="Active" value={active ?? 0} max={Math.max(1, conns ?? 1)} tone="healthy" />
            <GaugeRing label="Idle in txn" value={idleTx ?? 0} max={Math.max(1, conns ?? 1)} warnAt={1} critAt={5} integer />
            {waiting !== undefined && (
              <GaugeRing label="Waiting (locks)" value={waiting} max={Math.max(1, conns ?? 1)} warnAt={1} critAt={3} integer />
            )}
          </div>
        )}
      </CardBody>
    </Card>
  );
}

function GaugeRing({
  label,
  value,
  max,
  unit,
  sub,
  warnAt,
  critAt,
  tone,
  integer,
}: {
  label: string;
  value: number;
  max: number;
  unit?: string;
  sub?: string;
  warnAt?: number;
  critAt?: number;
  tone?: "healthy";
  integer?: boolean;
}) {
  const pct = Math.max(0, Math.min(1, max > 0 ? value / max : 0));
  const r = 26;
  const circ = 2 * Math.PI * r;
  const color =
    tone === "healthy"
      ? "var(--color-healthy)"
      : critAt !== undefined && value >= critAt
        ? "var(--color-danger)"
        : warnAt !== undefined && value >= warnAt
          ? "var(--color-signal)"
          : "var(--color-azure)";
  const display = integer ? Math.round(value).toString() : unit === "%" ? Math.round(value).toString() : value < 100 ? value.toFixed(value < 10 ? 1 : 0) : Math.round(value).toString();

  return (
    <div className="flex flex-col items-center">
      <div className="relative h-[72px] w-[72px]">
        <svg viewBox="0 0 64 64" className="h-full w-full -rotate-90">
          <circle cx="32" cy="32" r={r} fill="none" stroke="var(--color-ink-700)" strokeWidth="6" />
          <circle
            cx="32"
            cy="32"
            r={r}
            fill="none"
            stroke={color}
            strokeWidth="6"
            strokeLinecap="round"
            strokeDasharray={circ}
            strokeDashoffset={circ * (1 - pct)}
            style={{ transition: "stroke-dashoffset 0.5s ease, stroke 0.3s ease" }}
          />
        </svg>
        <div className="absolute inset-0 flex flex-col items-center justify-center">
          <span className="font-display text-base font-semibold tnum text-fg">{display}</span>
          {unit && <span className="font-mono text-[9px] text-fg-faint">{unit}</span>}
        </div>
      </div>
      <div className="mt-1.5 text-center">
        <div className="font-mono text-[10px] uppercase tracking-wider text-fg-muted">{label}</div>
        {sub && <div className="font-mono text-[10px] text-fg-faint tnum">{sub}</div>}
      </div>
    </div>
  );
}
