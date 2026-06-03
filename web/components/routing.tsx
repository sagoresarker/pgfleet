"use client";

import { api, type ClientStat, type RoutingBackend, type ServerStat } from "@/lib/api";
import { useQuery } from "@tanstack/react-query";
import { Crown, Database, Network, Users } from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { Area, AreaChart, ResponsiveContainer, Tooltip, YAxis } from "recharts";
import { Badge, Card, CardBody, CardHeader, CardTitle, EmptyState, SkeletonRows, Table, Td, Th, THead, Tr } from "./ui";

const MAX_POINTS = 60; // ~5 min at the 5s refetch cadence

type Point = { t: number; qps: number; latency: number; reads: number; writes: number };

/**
 * RouterObservability is the live picture of where the router is sending
 * traffic: a topology graph (router → primary/replicas) built from PgCat's
 * SHOW SERVERS, a read/write split, a queries-per-second history, and the raw
 * client/server connection tables. Per-connection counters in SHOW SERVERS
 * recycle as connections are reused, so the per-backend numbers read as a live
 * load share, while the q/s series comes from the monotonic SHOW STATS averages.
 */
export function RouterObservability({ id, ready }: { id: string; ready: boolean }) {
  const q = useQuery({
    queryKey: ["pool-stats", id],
    enabled: ready,
    refetchInterval: 5000,
    retry: false,
    queryFn: () => api.poolStats(id),
  });

  const routing = q.data?.routing ?? [];
  const stats = q.data?.stats ?? [];
  const clients = q.data?.clients ?? [];
  const servers = q.data?.servers ?? [];

  // Roll a small history of q/s + latency + read/write share for the charts.
  const [history, setHistory] = useState<Point[]>([]);
  const lastAt = useRef(0);
  useEffect(() => {
    if (!q.data || q.dataUpdatedAt === lastAt.current) return;
    lastAt.current = q.dataUpdatedAt;
    const qps = stats.reduce((a, s) => a + (s.avg_query_count || 0), 0);
    const latency = stats.reduce((m, s) => Math.max(m, s.avg_query_time || 0), 0) / 1000;
    const reads = routing.filter((b) => b.role === "replica").reduce((a, b) => a + b.query_count, 0);
    const writes = routing.filter((b) => b.role !== "replica").reduce((a, b) => a + b.query_count, 0);
    setHistory((h) => [...h, { t: q.dataUpdatedAt, qps, latency, reads, writes }].slice(-MAX_POINTS));
    // Depend only on the fetch timestamp: stats/routing are derived from q.data
    // and are fresh in this closure whenever dataUpdatedAt changes (the guard
    // above makes the body idempotent per fetch). Listing the freshly-allocated
    // arrays as deps would just churn the effect every render.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [q.dataUpdatedAt]);

  const reads = routing.filter((b) => b.role === "replica").reduce((a, b) => a + b.query_count, 0);
  const writes = routing.filter((b) => b.role !== "replica").reduce((a, b) => a + b.query_count, 0);

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader>
          <CardTitle>Query routing · live</CardTitle>
          <span className="flex items-center gap-1.5 font-mono text-[11px] text-fg-faint">
            <span className={"led " + (q.isFetching ? "led-signal led-pulse" : q.error ? "led-idle" : "led-healthy")} />
            {q.error ? "unavailable" : "PgCat admin"}
          </span>
        </CardHeader>
        <CardBody>
          {!ready ? (
            <p className="py-8 text-center text-sm text-fg-muted">Routing becomes visible once the router is running.</p>
          ) : q.isLoading ? (
            <SkeletonRows rows={2} />
          ) : q.error ? (
            <p className="py-8 text-center text-sm text-fg-muted">
              Could not reach the PgCat admin interface. It becomes available shortly after the router starts.
            </p>
          ) : routing.length === 0 ? (
            <EmptyState icon={<Network className="h-5 w-5" />} title="No backends reporting" description="The router has no active backends to report yet." />
          ) : (
            <div className="space-y-6">
              <Topology routing={routing} />
              <SplitBar reads={reads} writes={writes} />
              <ThroughputChart history={history} latest={history[history.length - 1]} />
            </div>
          )}
        </CardBody>
      </Card>

      <ConnectionTables clients={clients} servers={servers} loading={q.isLoading && ready} ready={ready} />
    </div>
  );
}

/* ---- Topology: router fanning out to its backends ---- */
function Topology({ routing }: { routing: RoutingBackend[] }) {
  const totalQ = Math.max(
    1,
    routing.reduce((a, b) => a + b.query_count, 0)
  );
  const totalConns = routing.reduce((a, b) => a + b.connections, 0);

  return (
    <div className="flex flex-col items-center">
      {/* Router node */}
      <div className="inline-flex items-center gap-3 rounded-lg border border-azure/40 bg-azure/5 px-4 py-2.5">
        <Network className="h-4 w-4 text-azure" />
        <div className="text-left">
          <div className="font-display text-sm text-fg">PgCat router</div>
          <div className="font-mono text-[10px] uppercase tracking-wider text-fg-faint">
            {totalConns} server conn{totalConns === 1 ? "" : "s"}
          </div>
        </div>
      </div>

      {/* Connector: animated flow stem out of the router */}
      <FlowEdge active={totalConns > 0} speed={1} height={22} />
      <div className="grid w-full gap-4 sm:grid-cols-2 lg:grid-cols-3">
        {routing.map((b) => (
          <BackendNode key={b.name} backend={b} share={b.query_count / totalQ} />
        ))}
      </div>
    </div>
  );
}

function BackendNode({ backend: b, share }: { backend: RoutingBackend; share: number }) {
  const primary = b.role !== "replica";
  const live = b.connections > 0;
  return (
    <div
      className={
        "relative rounded-lg border bg-ink-900 px-4 py-3 " +
        (primary ? "border-signal/40" : "border-line")
      }
    >
      {/* animated flow stub up to the branch row; faster = more traffic */}
      <div className="absolute -top-[18px] left-1/2 -translate-x-1/2">
        <FlowEdge active={live} speed={share} height={18} color={primary ? "var(--color-signal)" : "var(--color-azure)"} />
      </div>
      <div className="flex items-center justify-between gap-2">
        <div className="flex min-w-0 items-center gap-2">
          {primary ? <Crown className="h-4 w-4 shrink-0 text-signal" /> : <Database className="h-4 w-4 shrink-0 text-fg-faint" />}
          <span className="truncate font-display text-sm text-fg">{b.name}</span>
        </div>
        <Badge tone={primary ? "signal" : "neutral"}>{primary ? "writes" : "reads"}</Badge>
      </div>

      <div className="mt-2.5 flex items-center justify-between font-mono text-[11px] text-fg-muted tnum">
        <span className="flex items-center gap-1.5">
          <span className={"led " + (live ? "led-healthy" : "led-idle")} />
          {b.active_connections}/{b.connections} active
        </span>
        <span>{(share * 100).toFixed(0)}% of queries</span>
      </div>

      {/* load share bar */}
      <div className="mt-2 h-1.5 overflow-hidden rounded-full bg-ink-700/70">
        <div
          className={"h-full rounded-full " + (primary ? "bg-signal" : "bg-azure")}
          style={{ width: `${Math.max(2, share * 100).toFixed(1)}%` }}
        />
      </div>
    </div>
  );
}

/* ---- Animated flow edge: marching dashes whose speed scales with traffic ---- */
function FlowEdge({
  active,
  speed,
  height,
  color = "var(--color-azure)",
}: {
  active: boolean;
  speed: number; // 0..1 traffic share
  height: number;
  color?: string;
}) {
  // More share → shorter duration → faster marching dashes.
  const duration = `${(1.5 - Math.min(1, speed) * 1.1).toFixed(2)}s`;
  return (
    <svg width="2" height={height} aria-hidden="true" className="block">
      <line x1="1" y1="0" x2="1" y2={height} stroke="var(--color-line-bright)" strokeWidth="2" />
      {active && (
        <line x1="1" y1="0" x2="1" y2={height} stroke={color} strokeWidth="2" className="flow-line" style={{ animationDuration: duration }} />
      )}
    </svg>
  );
}

/* ---- Read/write split ---- */
function SplitBar({ reads, writes }: { reads: number; writes: number }) {
  const total = reads + writes;
  const rPct = total === 0 ? 0 : (reads / total) * 100;
  const wPct = total === 0 ? 0 : 100 - rPct;
  return (
    <div>
      <div className="mb-1.5 flex items-center justify-between font-mono text-[10px] uppercase tracking-wider text-fg-faint">
        <span className="text-azure">reads → replicas · {rPct.toFixed(0)}%</span>
        <span className="text-signal">{wPct.toFixed(0)}% · writes → primary</span>
      </div>
      <div className="flex h-2.5 overflow-hidden rounded-full bg-ink-700/70">
        <div className="h-full bg-azure" style={{ width: `${rPct}%` }} />
        <div className="h-full bg-signal" style={{ width: `${wPct}%` }} />
      </div>
      {total === 0 && <p className="mt-1.5 text-center text-xs text-fg-faint">No query traffic yet.</p>}
    </div>
  );
}

/* ---- Throughput sparkline ---- */
function ThroughputChart({ history, latest }: { history: Point[]; latest?: Point }) {
  return (
    <div>
      <div className="mb-1 flex items-baseline justify-between">
        <span className="font-mono text-[10px] uppercase tracking-wider text-fg-faint">throughput · last {history.length} samples</span>
        <span className="font-mono text-xs text-fg-muted tnum">
          {latest ? `${latest.qps.toFixed(1)} q/s · ${latest.latency.toFixed(1)}ms avg` : "—"}
        </span>
      </div>
      <div className="h-[120px] w-full">
        {history.length < 2 ? (
          <div className="grid h-full place-items-center rounded-md border border-line bg-ink-850 text-xs text-fg-faint">
            collecting…
          </div>
        ) : (
          <ResponsiveContainer width="100%" height="100%">
            <AreaChart data={history} margin={{ left: -28, right: 6, top: 6, bottom: 0 }}>
              <defs>
                <linearGradient id="qps-grad" x1="0" y1="0" x2="0" y2="1">
                  <stop offset="0%" stopColor="var(--color-azure)" stopOpacity={0.35} />
                  <stop offset="100%" stopColor="var(--color-azure)" stopOpacity={0} />
                </linearGradient>
              </defs>
              <YAxis tick={{ fontSize: 10, fill: "var(--color-fg-faint)" }} width={44} allowDecimals={false} />
              <Tooltip
                contentStyle={{ background: "var(--color-ink-900)", border: "1px solid var(--color-line)", borderRadius: 8, fontSize: 12 }}
                labelFormatter={() => ""}
                formatter={(v: number) => [`${v.toFixed(1)} q/s`, "throughput"]}
              />
              <Area type="monotone" dataKey="qps" stroke="var(--color-azure)" strokeWidth={2} fill="url(#qps-grad)" isAnimationActive={false} />
            </AreaChart>
          </ResponsiveContainer>
        )}
      </div>
    </div>
  );
}

/* ---- Clients & Servers connection tables ---- */
function ConnectionTables({
  clients,
  servers,
  loading,
  ready,
}: {
  clients: ClientStat[];
  servers: ServerStat[];
  loading: boolean;
  ready: boolean;
}) {
  const [tab, setTab] = useState<"clients" | "servers">("clients");
  if (!ready) return null;

  return (
    <Card>
      <CardHeader>
        <CardTitle>Connections · live</CardTitle>
        <div className="flex items-center gap-1 rounded-md border border-line p-0.5">
          <TabBtn active={tab === "clients"} onClick={() => setTab("clients")} icon={<Users className="h-3.5 w-3.5" />} label={`Clients (${clients.length})`} />
          <TabBtn active={tab === "servers"} onClick={() => setTab("servers")} icon={<Database className="h-3.5 w-3.5" />} label={`Servers (${servers.length})`} />
        </div>
      </CardHeader>
      <CardBody className="p-0">
        {loading ? (
          <div className="p-5">
            <SkeletonRows rows={2} />
          </div>
        ) : tab === "clients" ? (
          clients.length === 0 ? (
            <EmptyState icon={<Users className="h-5 w-5" />} title="No client connections" description="No application connections are open through the router right now." />
          ) : (
            <Table>
              <THead>
                <Th>Database</Th>
                <Th>User</Th>
                <Th>App</Th>
                <Th>State</Th>
                <Th align="right">Queries</Th>
                <Th align="right">Errors</Th>
                <Th align="right">Age</Th>
              </THead>
              <tbody>
                {clients.map((c) => (
                  <Tr key={c.client_id}>
                    <Td className="font-display text-fg">{c.database}</Td>
                    <Td className="font-mono text-xs text-fg-muted">{c.user}</Td>
                    <Td className="font-mono text-xs text-fg-faint">{c.application_name || "—"}</Td>
                    <Td><StateChip state={c.state} /></Td>
                    <Td align="right" className="font-mono text-xs tnum text-fg-muted">{c.query_count}</Td>
                    <Td align="right" className={"font-mono text-xs tnum " + (c.error_count > 0 ? "text-danger" : "text-fg-faint")}>{c.error_count}</Td>
                    <Td align="right" className="font-mono text-xs tnum text-fg-faint">{c.age_seconds}s</Td>
                  </Tr>
                ))}
              </tbody>
            </Table>
          )
        ) : servers.length === 0 ? (
          <EmptyState icon={<Database className="h-5 w-5" />} title="No server connections" description="The router holds no backend connections right now." />
        ) : (
          <Table>
            <THead>
              <Th>Backend</Th>
              <Th>Database</Th>
              <Th>State</Th>
              <Th align="right">Queries</Th>
              <Th align="right">Sent</Th>
              <Th align="right">Received</Th>
              <Th align="right">Age</Th>
            </THead>
            <tbody>
              {servers.map((s) => (
                <Tr key={s.server_id}>
                  <Td className="font-mono text-xs text-fg">{s.address}</Td>
                  <Td className="font-mono text-xs text-fg-muted">{s.database}</Td>
                  <Td><StateChip state={s.state} /></Td>
                  <Td align="right" className="font-mono text-xs tnum text-fg-muted">{s.query_count}</Td>
                  <Td align="right" className="font-mono text-xs tnum text-fg-faint">{formatShort(s.bytes_sent)}</Td>
                  <Td align="right" className="font-mono text-xs tnum text-fg-faint">{formatShort(s.bytes_received)}</Td>
                  <Td align="right" className="font-mono text-xs tnum text-fg-faint">{s.age_seconds}s</Td>
                </Tr>
              ))}
            </tbody>
          </Table>
        )}
      </CardBody>
    </Card>
  );
}

function StateChip({ state }: { state: string }) {
  const active = state.toLowerCase() === "active";
  return (
    <span className="inline-flex items-center gap-1.5 font-mono text-[11px] text-fg-muted">
      <span className={"led " + (active ? "led-healthy" : "led-idle")} />
      {state || "idle"}
    </span>
  );
}

function TabBtn({ active, onClick, icon, label }: { active: boolean; onClick: () => void; icon: React.ReactNode; label: string }) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={
        "inline-flex cursor-pointer items-center gap-1.5 rounded px-2.5 py-1 font-mono text-[11px] transition-colors " +
        (active ? "bg-azure/10 text-azure" : "text-fg-faint hover:text-fg-muted")
      }
    >
      {icon}
      {label}
    </button>
  );
}

// formatShort renders a byte counter compactly (e.g. 4.1k, 12M) for dense tables.
function formatShort(n: number): string {
  if (n < 1000) return String(n);
  if (n < 1_000_000) return (n / 1000).toFixed(1) + "k";
  if (n < 1_000_000_000) return (n / 1_000_000).toFixed(1) + "M";
  return (n / 1_000_000_000).toFixed(1) + "G";
}
