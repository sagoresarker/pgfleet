"use client";

import { PageHeader } from "@/components/shell";
import { Badge, Card, CardBody, EmptyState, SkeletonRows, StatusLed } from "@/components/ui";
import { api, type ActiveAlert } from "@/lib/api";
import { useQuery } from "@tanstack/react-query";
import { Database, ShieldCheck } from "lucide-react";
import Link from "next/link";
import { useState } from "react";

const kindLabels: Record<string, string> = {
  disk_full: "Disk space low",
  replication_lag: "Replication lag",
  backup_stale: "Backup stale",
  connection_saturation: "Connection saturation",
};

function kindLabel(kind: string): string {
  if (kindLabels[kind]) return kindLabels[kind];
  const pretty = kind.replace(/_/g, " ").trim();
  return pretty.charAt(0).toUpperCase() + pretty.slice(1);
}

function timeAgo(iso: string): string {
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) return "—";
  const secs = Math.max(0, Math.floor((Date.now() - then) / 1000));
  if (secs < 60) return `${secs}s ago`;
  const mins = Math.floor(secs / 60);
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  return `${days}d ago`;
}

type SeverityFilter = "all" | "critical" | "warning";

export default function AlertsPage() {
  const { data, isLoading } = useQuery({
    queryKey: ["alerts"],
    queryFn: () => api.listAlerts(),
    refetchInterval: 5000,
  });

  const [filter, setFilter] = useState<SeverityFilter>("all");

  const firing = (data?.alerts ?? [])
    .filter((a) => a.state === "firing")
    .sort((a, b) => {
      if (a.severity !== b.severity) return a.severity === "critical" ? -1 : 1;
      return new Date(b.fired_at).getTime() - new Date(a.fired_at).getTime();
    });

  const criticalCount = firing.filter((a) => a.severity === "critical").length;
  const warningCount = firing.filter((a) => a.severity === "warning").length;
  const visible = filter === "all" ? firing : firing.filter((a) => a.severity === filter);

  const filters: { value: SeverityFilter; label: string; count: number }[] = [
    { value: "all", label: "All", count: firing.length },
    { value: "critical", label: "Critical", count: criticalCount },
    { value: "warning", label: "Warning", count: warningCount },
  ];

  return (
    <div className="rise">
      <PageHeader title="Alerts" subtitle="Active reliability alerts across the fleet." />

      {isLoading ? (
        <SkeletonRows rows={3} />
      ) : firing.length === 0 ? (
        <Card className="border-healthy/30">
          <CardBody className="py-4">
            <EmptyState
              icon={<ShieldCheck className="h-5 w-5 text-healthy" />}
              title="No active alerts — the fleet is healthy"
              description="Reliability checks run continuously. Anything that needs attention will surface here."
            />
          </CardBody>
        </Card>
      ) : (
        <>
          <div className="mb-6 flex flex-wrap gap-2" role="group" aria-label="Filter alerts by severity">
            {filters.map((f) => {
              const active = filter === f.value;
              return (
                <button
                  key={f.value}
                  type="button"
                  onClick={() => setFilter(f.value)}
                  aria-pressed={active}
                  className={`inline-flex cursor-pointer items-center gap-2 rounded-md border px-3 py-1.5 font-mono text-xs transition-colors duration-150 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-azure/50 ${
                    active
                      ? "border-azure/50 bg-azure/10 text-azure"
                      : "border-line text-fg-muted hover:border-line-bright hover:text-fg"
                  }`}
                >
                  {f.label}
                  <span className="rounded bg-ink-700/70 px-1.5 tnum text-fg-muted">{f.count}</span>
                </button>
              );
            })}
          </div>

          {visible.length === 0 ? (
            <Card>
              <CardBody className="py-4">
                <EmptyState
                  icon={<ShieldCheck className="h-5 w-5" />}
                  title="No alerts match this filter"
                  description="Switch to “All” to see every firing alert."
                />
              </CardBody>
            </Card>
          ) : (
            <ul className="space-y-3">
              {visible.map((alert) => (
                <AlertRow key={alert.id} alert={alert} />
              ))}
            </ul>
          )}
        </>
      )}
    </div>
  );
}

function AlertRow({ alert }: { alert: ActiveAlert }) {
  const critical = alert.severity === "critical";

  return (
    <li>
      <Card className={critical ? "border-danger/30" : undefined}>
        <CardBody className="flex items-start gap-4">
          <StatusLed status={critical ? "danger" : "signal"} pulse />

          <div className="min-w-0 flex-1 space-y-2">
            <div className="flex flex-wrap items-center gap-2">
              <span className="font-display text-sm font-medium tracking-tight text-fg">{kindLabel(alert.kind)}</span>
              <Badge tone={critical ? "danger" : "signal"}>
                <StatusLed status={critical ? "danger" : "signal"} pulse />
                {alert.severity}
              </Badge>
              <span className="ml-auto font-mono text-[11px] text-fg-faint tnum">{timeAgo(alert.fired_at)}</span>
            </div>

            <p className="text-sm text-fg-muted">{alert.message}</p>

            <div className="flex flex-wrap items-center gap-x-4 gap-y-1 font-mono text-[11px] text-fg-faint">
              <Link
                href={`/instances/${alert.instance_id}`}
                className="inline-flex items-center gap-1.5 transition-colors hover:text-azure focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-azure/50 rounded"
              >
                <Database className="h-3 w-3" />
                <span className="tnum">{alert.instance_id}</span>
              </Link>
              {alert.value !== undefined && alert.threshold !== undefined && (
                <span className="tnum">
                  {alert.value} vs {alert.threshold}
                </span>
              )}
            </div>
          </div>
        </CardBody>
      </Card>
    </li>
  );
}
