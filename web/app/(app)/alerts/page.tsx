"use client";

import { PageHeader } from "@/components/shell";
import { Badge, Card, CardBody, Spinner, StatusLed } from "@/components/ui";
import { api, type ActiveAlert } from "@/lib/api";
import { useQuery } from "@tanstack/react-query";
import { Database, ShieldCheck } from "lucide-react";
import Link from "next/link";

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

export default function AlertsPage() {
  const { data, isLoading } = useQuery({
    queryKey: ["alerts"],
    queryFn: () => api.listAlerts(),
    refetchInterval: 5000,
  });

  const firing = (data?.alerts ?? [])
    .filter((a) => a.state === "firing")
    .sort((a, b) => {
      if (a.severity !== b.severity) return a.severity === "critical" ? -1 : 1;
      return new Date(b.fired_at).getTime() - new Date(a.fired_at).getTime();
    });

  return (
    <div className="rise">
      <PageHeader title="Alerts" subtitle="Active reliability alerts across the fleet" />

      {isLoading ? (
        <div className="grid place-items-center py-16">
          <Spinner className="h-6 w-6" />
        </div>
      ) : firing.length === 0 ? (
        <Card className="border-healthy/30">
          <CardBody className="grid place-items-center gap-3 py-16 text-center">
            <ShieldCheck className="h-8 w-8 text-healthy" />
            <p className="text-sm text-fg">No active alerts — the fleet is healthy.</p>
            <p className="max-w-sm text-xs text-fg-faint">
              Reliability checks run continuously. Anything that needs attention will surface here.
            </p>
          </CardBody>
        </Card>
      ) : (
        <ul className="space-y-3">
          {firing.map((alert) => (
            <AlertRow key={alert.id} alert={alert} />
          ))}
        </ul>
      )}
    </div>
  );
}

function AlertRow({ alert }: { alert: ActiveAlert }) {
  const critical = alert.severity === "critical";
  const led = critical ? "led-danger" : "led-signal";

  return (
    <li>
      <Card>
        <CardBody className="flex items-start gap-4">
          <span className={`led ${led} led-pulse mt-1.5 shrink-0`} aria-hidden />

          <div className="min-w-0 flex-1 space-y-2">
            <div className="flex flex-wrap items-center gap-2">
              <span className="font-display text-sm font-medium tracking-tight text-fg">
                {kindLabel(alert.kind)}
              </span>
              <Badge tone={critical ? "danger" : "signal"}>
                <StatusLed status={critical ? "danger" : "signal"} pulse />
                {alert.severity}
              </Badge>
              <span className="ml-auto font-mono text-[11px] text-fg-faint tnum">
                {timeAgo(alert.fired_at)}
              </span>
            </div>

            <p className="text-sm text-fg-muted">{alert.message}</p>

            <div className="flex flex-wrap items-center gap-x-4 gap-y-1 font-mono text-[11px] text-fg-faint">
              <Link
                href={`/instances/${alert.instance_id}`}
                className="inline-flex items-center gap-1.5 transition-colors hover:text-azure"
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
