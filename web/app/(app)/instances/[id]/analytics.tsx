"use client";

import { Card, CardBody, CardHeader, CardTitle, Spinner, Stat } from "@/components/ui";
import { api } from "@/lib/api";
import { formatBytes } from "@/lib/utils";
import { useQuery } from "@tanstack/react-query";
import { Area, AreaChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";

type MetricMap = Record<string, { value: number } | undefined>;

export function AnalyticsTab({ id, running }: { id: string; running: boolean }) {
  const latest = useQuery({
    queryKey: ["metrics-latest", id],
    queryFn: () => api.latestMetrics(id),
    refetchInterval: 5000,
    enabled: running,
  });
  const queries = useQuery({ queryKey: ["queries", id], queryFn: () => api.topQueries(id), refetchInterval: 15000, enabled: running });

  if (!running) {
    return <p className="rounded-md border border-line bg-ink-850 px-4 py-8 text-center text-sm text-fg-muted">Analytics are available while the instance is running.</p>;
  }

  const m: MetricMap = latest.data?.metrics ?? {};
  const cacheHit = m.cache_hit_ratio?.value;
  const connUtil = m.connection_utilization?.value;

  return (
    <div className="space-y-6">
      {/* Primary gauges */}
      <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
        <StatCard label="Cache hit ratio" value={pct(cacheHit)} sub={tone(cacheHit, 99, 95)} />
        <StatCard label="Connections" value={fmt(m.connections?.value)} sub={connUtil !== undefined ? `${pct(connUtil)} of max` : `${fmt(m.active_connections?.value)} active`} />
        <StatCard label="Database size" value={m.db_size_bytes ? formatBytes(m.db_size_bytes.value) : "—"} />
        <StatCard label="WAL written" value={m.wal_bytes ? formatBytes(m.wal_bytes.value) : "—"} sub={`${fmt(m.wal_records?.value)} records`} />
      </div>

      {/* Secondary gauges */}
      <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
        <StatCard label="Commits" value={fmt(m.xact_commit?.value)} sub={`${fmt(m.xact_rollback?.value)} rollbacks`} />
        <StatCard label="Tuples r/w" value={fmt(m.tup_returned?.value)} sub={`${fmt((m.tup_inserted?.value ?? 0) + (m.tup_updated?.value ?? 0) + (m.tup_deleted?.value ?? 0))} written`} />
        <StatCard label="Locks waiting" value={fmt(m.locks_waiting?.value)} sub={`${fmt(m.locks_held?.value)} held`} />
        <StatCard label="Longest txn" value={secs(m.longest_transaction_seconds?.value)} sub={`${fmt(m.idle_in_transaction?.value)} idle-in-txn`} />
      </div>

      {/* Charts */}
      <div className="grid gap-6 lg:grid-cols-2">
        <TimeChart id={id} metric="connections" title="Connections · last hour" running={running} />
        <TimeChart id={id} metric="cache_hit_ratio" title="Cache hit ratio · last hour" running={running} domain={[0, 100]} unit="%" />
      </div>
      <div className="grid gap-6 lg:grid-cols-2">
        <TimeChart id={id} metric="active_connections" title="Active sessions · last hour" running={running} color="var(--color-violet)" />
        <TimeChart id={id} metric="deadlocks" title="Deadlocks · last hour" running={running} color="var(--color-danger)" />
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Top queries · pg_stat_statements</CardTitle>
        </CardHeader>
        <CardBody className="p-0">
          {(queries.data?.queries ?? []).length === 0 ? (
            <p className="px-5 py-8 text-center text-sm text-fg-muted">No query statistics captured yet.</p>
          ) : (
            <ul className="divide-y divide-line">
              {(queries.data?.queries ?? []).slice(0, 10).map((q, idx) => (
                <li key={idx} className="px-5 py-3">
                  <code className="block truncate font-mono text-xs text-fg">{q.query}</code>
                  <div className="mt-1 flex gap-4 font-mono text-[11px] text-fg-faint tnum">
                    <span>{q.calls} calls</span>
                    <span>{q.mean_time_ms.toFixed(2)}ms mean</span>
                    <span>{q.rows} rows</span>
                  </div>
                </li>
              ))}
            </ul>
          )}
        </CardBody>
      </Card>
    </div>
  );
}

function StatCard({ label, value, sub }: { label: string; value: string; sub?: string }) {
  return (
    <Card>
      <CardBody>
        <Stat label={label} value={value} sub={sub} />
      </CardBody>
    </Card>
  );
}

function TimeChart({
  id,
  metric,
  title,
  running,
  color = "var(--color-azure)",
  domain,
  unit,
}: {
  id: string;
  metric: string;
  title: string;
  running: boolean;
  color?: string;
  domain?: [number, number];
  unit?: string;
}) {
  const since = new Date(Date.now() - 60 * 60 * 1000).toISOString();
  const q = useQuery({
    queryKey: ["metrics-range", id, metric],
    queryFn: () => api.rangeMetrics(id, metric, since),
    refetchInterval: 15000,
    enabled: running,
  });
  const series = (q.data?.samples ?? []).map((s) => ({
    t: new Date(s.at).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }),
    v: unit === "%" ? Number(s.value.toFixed(2)) : s.value,
  }));
  const gradId = `grad-${metric}`;

  return (
    <Card>
      <CardHeader>
        <CardTitle>{title}</CardTitle>
        {q.isFetching && <Spinner />}
      </CardHeader>
      <CardBody>
        {series.length < 2 ? (
          <p className="py-10 text-center text-sm text-fg-muted">Collecting samples…</p>
        ) : (
          <ResponsiveContainer width="100%" height={180}>
            <AreaChart data={series} margin={{ left: -20, right: 8, top: 8 }}>
              <defs>
                <linearGradient id={gradId} x1="0" y1="0" x2="0" y2="1">
                  <stop offset="0%" stopColor={color} stopOpacity={0.35} />
                  <stop offset="100%" stopColor={color} stopOpacity={0} />
                </linearGradient>
              </defs>
              <XAxis dataKey="t" tick={{ fill: "var(--color-fg-faint)", fontSize: 10 }} tickLine={false} axisLine={false} />
              <YAxis
                tick={{ fill: "var(--color-fg-faint)", fontSize: 10 }}
                tickLine={false}
                axisLine={false}
                allowDecimals={unit === "%"}
                domain={domain ?? ["auto", "auto"]}
                width={40}
              />
              <Tooltip
                contentStyle={{
                  background: "var(--color-ink-900)",
                  border: "1px solid var(--color-line-bright)",
                  borderRadius: 8,
                  fontSize: 12,
                  boxShadow: "0 8px 24px -12px rgba(15,31,51,0.25)",
                }}
                labelStyle={{ color: "var(--color-fg-muted)" }}
                formatter={(v: number) => [unit ? `${v}${unit}` : new Intl.NumberFormat().format(v), title.split(" · ")[0]]}
              />
              <Area type="monotone" dataKey="v" stroke={color} strokeWidth={2} fill={`url(#${gradId})`} />
            </AreaChart>
          </ResponsiveContainer>
        )}
      </CardBody>
    </Card>
  );
}

function fmt(v: number | undefined): string {
  if (v === undefined) return "—";
  return new Intl.NumberFormat().format(Math.round(v));
}

function pct(v: number | undefined): string {
  if (v === undefined) return "—";
  return `${v.toFixed(1)}%`;
}

function secs(v: number | undefined): string {
  if (v === undefined) return "—";
  if (v < 1) return "<1s";
  if (v < 60) return `${Math.round(v)}s`;
  return `${Math.floor(v / 60)}m ${Math.round(v % 60)}s`;
}

function tone(v: number | undefined, good: number, ok: number): string {
  if (v === undefined) return "";
  if (v >= good) return "healthy";
  if (v >= ok) return "watch";
  return "low";
}
