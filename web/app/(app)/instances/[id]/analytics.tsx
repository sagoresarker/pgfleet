"use client";

import { Card, CardBody, CardHeader, CardTitle, Spinner, Stat } from "@/components/ui";
import { api } from "@/lib/api";
import { formatBytes } from "@/lib/utils";
import { useQuery } from "@tanstack/react-query";
import { Area, AreaChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";

export function AnalyticsTab({ id, running }: { id: string; running: boolean }) {
  const latest = useQuery({
    queryKey: ["metrics-latest", id],
    queryFn: () => api.latestMetrics(id),
    refetchInterval: 5000,
    enabled: running,
  });
  const since = new Date(Date.now() - 60 * 60 * 1000).toISOString();
  const connSeries = useQuery({
    queryKey: ["metrics-range", id, "connections"],
    queryFn: () => api.rangeMetrics(id, "connections", since),
    refetchInterval: 15000,
    enabled: running,
  });
  const queries = useQuery({ queryKey: ["queries", id], queryFn: () => api.topQueries(id), refetchInterval: 15000, enabled: running });

  if (!running) {
    return <p className="rounded-md border border-line bg-ink-850 px-4 py-8 text-center text-sm text-fg-muted">Analytics are available while the instance is running.</p>;
  }

  const m = latest.data?.metrics ?? {};
  const series = (connSeries.data?.samples ?? []).map((s) => ({
    t: new Date(s.at).toLocaleTimeString([], { hour: "2-digit", minute: "2-digit" }),
    v: s.value,
  }));

  return (
    <div className="space-y-6">
      <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
        <Card>
          <CardBody>
            <Stat label="Connections" value={fmt(m.connections?.value)} sub={`${fmt(m.active_connections?.value)} active`} />
          </CardBody>
        </Card>
        <Card>
          <CardBody>
            <Stat label="Database size" value={m.db_size_bytes ? formatBytes(m.db_size_bytes.value) : "—"} />
          </CardBody>
        </Card>
        <Card>
          <CardBody>
            <Stat label="Commits" value={fmt(m.xact_commit?.value)} sub={`${fmt(m.xact_rollback?.value)} rollbacks`} />
          </CardBody>
        </Card>
        <Card>
          <CardBody>
            <Stat label="WAL written" value={m.wal_bytes ? formatBytes(m.wal_bytes.value) : "—"} />
          </CardBody>
        </Card>
      </div>

      <Card>
        <CardHeader>
          <CardTitle>Connections · last hour</CardTitle>
          {connSeries.isFetching && <Spinner />}
        </CardHeader>
        <CardBody>
          {series.length < 2 ? (
            <p className="py-10 text-center text-sm text-fg-muted">Collecting samples…</p>
          ) : (
            <ResponsiveContainer width="100%" height={200}>
              <AreaChart data={series} margin={{ left: -20, right: 8, top: 8 }}>
                <defs>
                  <linearGradient id="conn" x1="0" y1="0" x2="0" y2="1">
                    <stop offset="0%" stopColor="var(--color-azure)" stopOpacity={0.4} />
                    <stop offset="100%" stopColor="var(--color-azure)" stopOpacity={0} />
                  </linearGradient>
                </defs>
                <XAxis dataKey="t" tick={{ fill: "var(--color-fg-faint)", fontSize: 10 }} tickLine={false} axisLine={false} />
                <YAxis tick={{ fill: "var(--color-fg-faint)", fontSize: 10 }} tickLine={false} axisLine={false} allowDecimals={false} />
                <Tooltip
                  contentStyle={{
                    background: "var(--color-ink-800)",
                    border: "1px solid var(--color-line-bright)",
                    borderRadius: 8,
                    fontSize: 12,
                  }}
                  labelStyle={{ color: "var(--color-fg-muted)" }}
                />
                <Area type="monotone" dataKey="v" stroke="var(--color-azure)" strokeWidth={2} fill="url(#conn)" />
              </AreaChart>
            </ResponsiveContainer>
          )}
        </CardBody>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Top queries · pg_stat_statements</CardTitle>
        </CardHeader>
        <CardBody className="p-0">
          {(queries.data?.queries ?? []).length === 0 ? (
            <p className="px-5 py-8 text-center text-sm text-fg-muted">No query statistics captured yet.</p>
          ) : (
            <ul className="divide-y divide-line">
              {(queries.data?.queries ?? []).slice(0, 8).map((q, idx) => (
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

function fmt(v: number | undefined): string {
  if (v === undefined) return "—";
  return new Intl.NumberFormat().format(Math.round(v));
}
