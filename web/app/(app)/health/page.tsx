"use client";

import { PageHeader } from "@/components/shell";
import { Badge, Card, CardBody, CardHeader, CardTitle, Spinner, StatusLed } from "@/components/ui";
import { api } from "@/lib/api";
import { formatBytes } from "@/lib/utils";
import { useQuery } from "@tanstack/react-query";
import { CheckCircle2, ShieldAlert, ShieldCheck } from "lucide-react";
import Link from "next/link";

export default function HealthPage() {
  const { data, isLoading } = useQuery({ queryKey: ["health"], queryFn: api.health, refetchInterval: 10000 });
  const reports = data?.reports ?? [];
  const alerts = data?.alerts ?? [];

  return (
    <div className="rise">
      <PageHeader title="Reliability" subtitle="Archiving health, backup freshness, and restore drills across the fleet." />

      {isLoading ? (
        <div className="grid place-items-center py-24">
          <Spinner className="h-6 w-6" />
        </div>
      ) : (
        <div className="grid gap-6 lg:grid-cols-3">
          <div className="space-y-4 lg:col-span-2">
            {reports.length === 0 ? (
              <Card>
                <CardBody className="py-12 text-center text-sm text-fg-muted">
                  No health reports yet. Reports populate after the first scheduled check.
                </CardBody>
              </Card>
            ) : (
              reports.map((r) => (
                <Card key={r.instance_id}>
                  <CardHeader>
                    <Link href={`/instances/${r.instance_id}`} className="font-mono text-xs text-azure hover:underline">
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
                      <div className="mt-1 font-display text-sm text-fg tnum">{formatBytes(r.wal_bytes)}</div>
                    </div>
                    {r.issues.length > 0 && (
                      <ul className="col-span-full mt-1 space-y-1">
                        {r.issues.map((issue, i) => (
                          <li key={i} className="text-xs text-danger">
                            • {issue}
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
              {alerts.length === 0 ? (
                <div className="flex items-center gap-2 text-sm text-fg-muted">
                  <CheckCircle2 className="h-4 w-4 text-healthy" />
                  All systems nominal.
                </div>
              ) : (
                alerts.map((a, i) => (
                  <div key={i} className="rounded-md border border-danger/20 bg-danger/5 px-3 py-2 text-xs text-fg">
                    {a.message}
                  </div>
                ))
              )}
            </CardBody>
          </Card>
        </div>
      )}
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
