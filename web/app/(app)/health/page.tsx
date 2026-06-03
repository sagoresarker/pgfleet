"use client";

import { PageHeader } from "@/components/shell";
import {
  Badge,
  Card,
  CardBody,
  CardHeader,
  CardTitle,
  EmptyState,
  SkeletonRows,
  Stat,
  StatusLed,
} from "@/components/ui";
import { api } from "@/lib/api";
import { formatBytes } from "@/lib/utils";
import { useQuery } from "@tanstack/react-query";
import { ShieldAlert, ShieldCheck } from "lucide-react";
import Link from "next/link";

export default function HealthPage() {
  const { data, isLoading } = useQuery({ queryKey: ["health"], queryFn: api.health, refetchInterval: 10000 });
  const reports = data?.reports ?? [];
  const alerts = data?.alerts ?? [];

  const healthy = reports.filter((r) => r.issues.length === 0).length;
  const failing = reports.length - healthy;

  return (
    <div className="rise">
      <PageHeader title="Reliability" subtitle="Archiving health, backup freshness, and restore drills across the fleet." />

      {/* Summary KPIs */}
      <div className="mb-8 grid grid-cols-2 gap-4 sm:grid-cols-4">
        <Card>
          <CardBody>
            <Stat label="Checked" value={isLoading ? "—" : String(reports.length)} sub="instances" />
          </CardBody>
        </Card>
        <Card>
          <CardBody>
            <Stat label="Healthy" value={isLoading ? "—" : String(healthy)} />
          </CardBody>
        </Card>
        <Card>
          <CardBody>
            <Stat
              label="With issues"
              value={isLoading ? "—" : String(failing)}
              tone={failing ? "danger" : undefined}
            />
          </CardBody>
        </Card>
        <Card>
          <CardBody>
            <Stat
              label="Active alerts"
              value={isLoading ? "—" : String(alerts.length)}
              tone={alerts.length ? "danger" : undefined}
            />
          </CardBody>
        </Card>
      </div>

      <div className="grid gap-6 lg:grid-cols-3">
        <div className="space-y-4 lg:col-span-2">
          {isLoading ? (
            <SkeletonRows rows={4} />
          ) : reports.length === 0 ? (
            <Card>
              <CardBody className="py-4">
                <EmptyState
                  icon={<ShieldCheck className="h-5 w-5" />}
                  title="No health reports yet"
                  description="Reports populate after the first scheduled reliability check runs across your instances."
                />
              </CardBody>
            </Card>
          ) : (
            reports.map((r) => (
              <Card key={r.instance_id} className={r.issues.length > 0 ? "border-danger/30" : undefined}>
                <CardHeader>
                  <Link
                    href={`/instances/${r.instance_id}`}
                    className="font-mono text-xs text-azure hover:underline focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-azure/50 rounded"
                  >
                    {r.instance_id.slice(0, 12)}
                  </Link>
                  {r.issues.length === 0 ? (
                    <Badge tone="healthy">
                      <ShieldCheck className="h-3 w-3" /> healthy
                    </Badge>
                  ) : (
                    <Badge tone="danger">
                      <ShieldAlert className="h-3 w-3" /> {r.issues.length} issue{r.issues.length > 1 ? "s" : ""}
                    </Badge>
                  )}
                </CardHeader>
                <CardBody className="grid grid-cols-2 gap-4 md:grid-cols-4">
                  <Indicator ok={r.archiving_ok} label="WAL archiving" />
                  <Indicator ok={r.has_backup} label="Has backup" />
                  <Indicator ok={!r.drill_ran || r.drill_ok} label="Restore drill" pending={!r.drill_ran} />
                  <div>
                    <div className="font-mono text-[10px] uppercase tracking-wider text-fg-faint">pg_wal</div>
                    <div className="mt-1.5 font-display text-sm text-fg tnum">{formatBytes(r.wal_bytes)}</div>
                  </div>
                  {r.issues.length > 0 && (
                    <ul className="col-span-full mt-1 space-y-1.5 border-t border-line pt-3">
                      {r.issues.map((issue, i) => (
                        <li key={i} className="flex items-start gap-2 text-xs text-danger">
                          <ShieldAlert className="mt-0.5 h-3 w-3 shrink-0" />
                          {issue}
                        </li>
                      ))}
                    </ul>
                  )}
                </CardBody>
              </Card>
            ))
          )}
        </div>

        <Card>
          <CardHeader>
            <CardTitle>Active alerts</CardTitle>
            <Badge tone={alerts.length ? "danger" : "healthy"}>{alerts.length}</Badge>
          </CardHeader>
          <CardBody className="space-y-2">
            {isLoading ? (
              <SkeletonRows rows={2} />
            ) : alerts.length === 0 ? (
              <div className="flex items-center gap-2 text-sm text-fg-muted">
                <ShieldCheck className="h-4 w-4 shrink-0 text-healthy" />
                All systems nominal.
              </div>
            ) : (
              alerts.map((a, i) => (
                <Link
                  key={i}
                  href={`/instances/${a.instance_id}`}
                  className="block rounded-md border border-danger/20 bg-danger/5 px-3 py-2 text-xs text-fg transition-colors hover:border-danger/40 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-danger/40"
                >
                  <span className="font-mono text-[11px] text-azure">{a.instance_id.slice(0, 8)}</span>
                  <span className="mt-0.5 block">{a.message}</span>
                </Link>
              ))
            )}
          </CardBody>
        </Card>
      </div>
    </div>
  );
}

function Indicator({ ok, label, pending }: { ok: boolean; label: string; pending?: boolean }) {
  return (
    <div>
      <div className="font-mono text-[10px] uppercase tracking-wider text-fg-faint">{label}</div>
      <div className="mt-1.5 flex items-center gap-2">
        <StatusLed status={pending ? "idle" : ok ? "healthy" : "danger"} />
        <span className="text-sm text-fg">{pending ? "pending" : ok ? "ok" : "failing"}</span>
      </div>
    </div>
  );
}
