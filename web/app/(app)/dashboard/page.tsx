"use client";

import { PageHeader } from "@/components/shell";
import { ClusterStatus, InstanceStatus } from "@/components/status";
import {
  Badge,
  Button,
  Card,
  CardBody,
  CardHeader,
  CardTitle,
  EmptyState,
  SkeletonRows,
  Stat,
  StatusLed,
} from "@/components/ui";
import { api, type Instance } from "@/lib/api";
import { useQuery } from "@tanstack/react-query";
import {
  AlertTriangle,
  ArrowRight,
  Boxes,
  Database,
  Network,
  Plus,
  ShieldCheck,
  SquareTerminal,
} from "lucide-react";
import Link from "next/link";

// A degraded instance is one an operator should look at: hard errors or
// transient lifecycle states that haven't settled into "running".
const DEGRADED: ReadonlySet<Instance["status"]> = new Set([
  "error",
  "stopped",
  "destroying",
] as const);

export default function DashboardPage() {
  const instances = useQuery({ queryKey: ["instances"], queryFn: api.listInstances, refetchInterval: 5000 });
  const clusters = useQuery({ queryKey: ["clusters"], queryFn: api.listClusters, refetchInterval: 8000 });
  const health = useQuery({ queryKey: ["health"], queryFn: api.health, refetchInterval: 10000 });

  const items = instances.data?.instances ?? [];
  const clusterList = clusters.data?.clusters ?? [];
  const reports = health.data?.reports ?? [];
  const alerts = health.data?.alerts ?? [];

  const running = items.filter((i) => i.status === "running").length;
  const degraded = items.filter((i) => DEGRADED.has(i.status)).length;
  const protectedCount = reports.filter((r) => r.has_backup).length;
  const backupFreshness =
    reports.length === 0 ? "—" : `${protectedCount}/${reports.length}`;

  const loading = instances.isLoading;

  return (
    <div className="rise">
      <PageHeader
        title="Fleet"
        subtitle="Live overview of every managed Postgres instance and cluster."
        action={
          <Button asChild>
            <Link href="/instances/new">
              <Plus className="h-4 w-4" />
              New instance
            </Link>
          </Button>
        }
      />

      {/* KPI row */}
      <div className="mb-8 grid grid-cols-2 gap-4 md:grid-cols-3 xl:grid-cols-6">
        <KpiCard>
          <Stat label="Instances" value={String(items.length)} sub={`${running} running`} />
        </KpiCard>
        <KpiCard>
          <Stat label="Clusters" value={String(clusterList.length)} sub="HA groups" />
        </KpiCard>
        <KpiCard>
          <Stat
            label="Healthy"
            value={String(running)}
            sub={items.length ? `of ${items.length}` : "none yet"}
            tone={items.length && running < items.length ? "signal" : undefined}
          />
        </KpiCard>
        <KpiCard>
          <Stat
            label="Degraded"
            value={String(degraded)}
            sub={degraded ? "need attention" : "all clear"}
            tone={degraded ? "danger" : undefined}
          />
        </KpiCard>
        <KpiCard>
          <Stat
            label="Backup coverage"
            value={backupFreshness}
            sub="have a backup"
            tone={reports.length && protectedCount < reports.length ? "signal" : undefined}
          />
        </KpiCard>
        <KpiCard>
          <Stat
            label="Active alerts"
            value={String(alerts.length)}
            sub={alerts.length ? "firing" : "clear"}
            tone={alerts.length ? "danger" : undefined}
          />
        </KpiCard>
      </div>

      <div className="grid gap-6 lg:grid-cols-3">
        {/* Fleet list */}
        <div className="space-y-6 lg:col-span-2">
          <Card>
            <CardHeader>
              <CardTitle>Instances</CardTitle>
              <div className="flex items-center gap-2">
                {items.length > 0 && <Badge tone="neutral">{items.length}</Badge>}
                <Link
                  href="/instances"
                  className="inline-flex items-center gap-1 font-mono text-[11px] uppercase tracking-wider text-fg-faint transition-colors hover:text-azure focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-azure/50 rounded"
                >
                  View all <ArrowRight className="h-3 w-3" />
                </Link>
              </div>
            </CardHeader>
            <CardBody className="p-0">
              {loading ? (
                <div className="p-5">
                  <SkeletonRows rows={4} />
                </div>
              ) : items.length === 0 ? (
                <EmptyState
                  icon={<Database className="h-5 w-5" />}
                  title="No instances yet"
                  description="Provision your first managed Postgres instance to begin operating your fleet."
                  action={
                    <Button asChild size="sm">
                      <Link href="/instances/new">
                        <Plus className="h-4 w-4" />
                        Create instance
                      </Link>
                    </Button>
                  }
                />
              ) : (
                <ul className="divide-y divide-line">
                  {items.map((i) => (
                    <li
                      key={i.id}
                      className="group relative flex items-center gap-4 px-5 py-4 transition-colors hover:bg-ink-800/50 focus-within:bg-ink-800/50"
                    >
                      {/* Full-row link; the SQL pill sits above it on its own z-layer. */}
                      <Link
                        href={`/instances/${i.id}`}
                        className="absolute inset-0 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-azure/50"
                        aria-label={`Open ${i.name}`}
                      />
                      <Database className="h-4 w-4 shrink-0 text-fg-faint" />
                      <div className="min-w-0 flex-1">
                        <div className="flex items-center gap-2">
                          <span className="truncate font-display text-sm text-fg">{i.name}</span>
                          {i.role !== "standalone" && (
                            <Badge tone={i.role === "primary" ? "azure" : "neutral"}>{i.role}</Badge>
                          )}
                          {i.public && <Badge tone="signal">public</Badge>}
                        </div>
                        <div className="mt-0.5 truncate font-mono text-[11px] text-fg-faint tnum">
                          pg{i.pg_version} · {i.repo_type} · :{i.host_port || "—"}
                        </div>
                      </div>
                      {i.status === "running" && (
                        <Link
                          href={`/instances/${i.id}#console`}
                          className="relative z-10 flex shrink-0 items-center gap-1 rounded-md border border-line bg-ink-900 px-2.5 py-1.5 font-mono text-[10px] uppercase tracking-wider text-fg-muted transition-colors hover:border-azure/60 hover:text-azure focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-azure/50"
                          aria-label={`Run SQL on ${i.name}`}
                        >
                          <SquareTerminal className="h-3.5 w-3.5" /> SQL
                        </Link>
                      )}
                      <InstanceStatus status={i.status} />
                    </li>
                  ))}
                </ul>
              )}
            </CardBody>
          </Card>

          {/* Clusters */}
          <Card>
            <CardHeader>
              <CardTitle>Clusters</CardTitle>
              <Link
                href="/clusters"
                className="inline-flex items-center gap-1 font-mono text-[11px] uppercase tracking-wider text-fg-faint transition-colors hover:text-azure focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-azure/50 rounded"
              >
                View all <ArrowRight className="h-3 w-3" />
              </Link>
            </CardHeader>
            <CardBody className="p-0">
              {clusters.isLoading ? (
                <div className="p-5">
                  <SkeletonRows rows={2} />
                </div>
              ) : clusterList.length === 0 ? (
                <EmptyState
                  icon={<Network className="h-5 w-5" />}
                  title="No clusters"
                  description="High-availability clusters with a primary and replicas will appear here."
                />
              ) : (
                <ul className="divide-y divide-line">
                  {clusterList.map((c) => (
                    <li key={c.id}>
                      <Link
                        href={`/clusters/${c.id}`}
                        className="flex items-center gap-4 px-5 py-4 transition-colors hover:bg-ink-800/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-azure/50"
                      >
                        <Network className="h-4 w-4 shrink-0 text-fg-faint" />
                        <div className="min-w-0 flex-1">
                          <div className="truncate font-display text-sm text-fg">{c.name}</div>
                          <div className="mt-0.5 truncate font-mono text-[11px] text-fg-faint tnum">
                            router :{c.router_port || "—"}
                          </div>
                        </div>
                        <ClusterStatus status={c.status} />
                      </Link>
                    </li>
                  ))}
                </ul>
              )}
            </CardBody>
          </Card>
        </div>

        {/* Reliability sidebar: alerts + health summary */}
        <div className="space-y-6">
          <Card>
            <CardHeader>
              <CardTitle>Reliability alerts</CardTitle>
              {alerts.length === 0 ? (
                <Badge tone="healthy">
                  <ShieldCheck className="h-3 w-3" />
                  clear
                </Badge>
              ) : (
                <Badge tone="danger">{alerts.length}</Badge>
              )}
            </CardHeader>
            <CardBody className="space-y-3">
              {health.isLoading ? (
                <SkeletonRows rows={2} />
              ) : alerts.length === 0 ? (
                <div className="flex items-center gap-2 text-sm text-fg-muted">
                  <ShieldCheck className="h-4 w-4 shrink-0 text-healthy" />
                  No outstanding issues across the fleet.
                </div>
              ) : (
                alerts.slice(0, 6).map((a) => (
                  <Link
                    key={a.instance_id + ":" + a.message}
                    href={`/instances/${a.instance_id}`}
                    className="flex items-start gap-2.5 rounded-md border border-danger/20 bg-danger/5 px-3 py-2.5 transition-colors hover:border-danger/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-danger/40"
                  >
                    <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0 text-danger" />
                    <div className="min-w-0">
                      <span className="font-mono text-[11px] text-azure">{a.instance_id.slice(0, 8)}</span>
                      <p className="text-xs text-fg">{a.message}</p>
                    </div>
                  </Link>
                ))
              )}
              {alerts.length > 6 && (
                <Link
                  href="/alerts"
                  className="inline-flex items-center gap-1 font-mono text-[11px] uppercase tracking-wider text-fg-faint transition-colors hover:text-azure"
                >
                  {alerts.length - 6} more <ArrowRight className="h-3 w-3" />
                </Link>
              )}
            </CardBody>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>Health summary</CardTitle>
              <Link
                href="/health"
                className="inline-flex items-center gap-1 font-mono text-[11px] uppercase tracking-wider text-fg-faint transition-colors hover:text-azure focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-azure/50 rounded"
              >
                Details <ArrowRight className="h-3 w-3" />
              </Link>
            </CardHeader>
            <CardBody>
              {health.isLoading ? (
                <SkeletonRows rows={3} />
              ) : reports.length === 0 ? (
                <p className="text-sm text-fg-muted">
                  No health reports yet. They populate after the first scheduled check.
                </p>
              ) : (
                <ul className="space-y-2.5">
                  {reports.slice(0, 6).map((r) => {
                    const ok = r.issues.length === 0;
                    return (
                      <li key={r.instance_id} className="flex items-center gap-3">
                        <StatusLed status={ok ? "healthy" : "danger"} />
                        <Link
                          href={`/instances/${r.instance_id}`}
                          className="min-w-0 flex-1 truncate font-mono text-xs text-azure transition-colors hover:text-azure-bright"
                        >
                          {r.instance_id.slice(0, 12)}
                        </Link>
                        <span
                          className={
                            ok
                              ? "font-mono text-[11px] text-healthy"
                              : "font-mono text-[11px] text-danger tnum"
                          }
                        >
                          {ok ? "healthy" : `${r.issues.length} issue${r.issues.length > 1 ? "s" : ""}`}
                        </span>
                      </li>
                    );
                  })}
                </ul>
              )}
            </CardBody>
          </Card>

          {/* Quick stat: total managed footprint */}
          <Card>
            <CardBody className="flex items-center gap-3">
              <div className="grid h-9 w-9 shrink-0 place-items-center rounded-md border border-line bg-ink-800 text-azure">
                <Boxes className="h-4 w-4" />
              </div>
              <div className="min-w-0">
                <div className="font-mono text-[10px] uppercase tracking-wider text-fg-faint">Managed footprint</div>
                <div className="font-display text-sm text-fg tnum">
                  {items.length} instance{items.length === 1 ? "" : "s"} · {clusterList.length} cluster
                  {clusterList.length === 1 ? "" : "s"}
                </div>
              </div>
            </CardBody>
          </Card>
        </div>
      </div>
    </div>
  );
}

function KpiCard({ children }: { children: React.ReactNode }) {
  return (
    <Card>
      <CardBody>{children}</CardBody>
    </Card>
  );
}
