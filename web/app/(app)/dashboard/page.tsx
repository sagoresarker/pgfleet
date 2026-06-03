"use client";

import { PageHeader } from "@/components/shell";
import { InstanceStatus } from "@/components/status";
import { Badge, Button, Card, CardBody, CardHeader, CardTitle, EmptyState, SkeletonRows, Spinner, Stat } from "@/components/ui";
import { api } from "@/lib/api";
import { useQuery } from "@tanstack/react-query";
import { AlertTriangle, Database, Plus, ShieldCheck } from "lucide-react";
import Link from "next/link";

export default function DashboardPage() {
  const instances = useQuery({ queryKey: ["instances"], queryFn: api.listInstances, refetchInterval: 5000 });
  const health = useQuery({ queryKey: ["health"], queryFn: api.health, refetchInterval: 10000 });

  const items = instances.data?.instances ?? [];
  const running = items.filter((i) => i.status === "running").length;
  const alerts = health.data?.alerts ?? [];

  return (
    <div className="rise">
      <PageHeader
        title="Fleet"
        subtitle="Overview of every managed Postgres instance."
        action={
          <Button asChild>
            <Link href="/instances/new">
              <Plus className="h-4 w-4" />
              New instance
            </Link>
          </Button>
        }
      />

      <div className="mb-8 grid grid-cols-2 gap-4 md:grid-cols-4">
        <Card>
          <CardBody>
            <Stat label="Instances" value={String(items.length)} sub={`${running} running`} />
          </CardBody>
        </Card>
        <Card>
          <CardBody>
            <Stat label="Healthy" value={String(running)} tone={running === items.length ? undefined : "signal"} />
          </CardBody>
        </Card>
        <Card>
          <CardBody>
            <Stat label="Alerts" value={String(alerts.length)} tone={alerts.length ? "danger" : undefined} />
          </CardBody>
        </Card>
        <Card>
          <CardBody>
            <Stat label="Backup repos" value={new Set(items.map((i) => i.repo_type)).size ? String(items.length) : "0"} sub="protected" />
          </CardBody>
        </Card>
      </div>

      <div className="grid gap-6 lg:grid-cols-3">
        <div className="lg:col-span-2">
          <Card>
            <CardHeader>
              <CardTitle>Instances</CardTitle>
              {instances.isFetching && <Spinner />}
            </CardHeader>
            <CardBody className="p-0">
              {instances.isLoading ? (
                <div className="p-5">
                  <SkeletonRows rows={4} />
                </div>
              ) : items.length === 0 ? (
                <EmptyState
                  icon={<Database className="h-5 w-5" />}
                  title="No instances yet"
                  description="Provision your first managed Postgres instance to begin."
                  action={
                    <Button asChild size="sm">
                      <Link href="/instances/new">
                        <Plus className="h-4 w-4" />
                        New instance
                      </Link>
                    </Button>
                  }
                />
              ) : (
                <ul className="divide-y divide-line">
                  {items.map((i) => (
                    <li key={i.id}>
                      <Link
                        href={`/instances/${i.id}`}
                        className="flex items-center gap-4 px-5 py-4 transition-colors hover:bg-ink-800/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-azure/50"
                      >
                        <Database className="h-4 w-4 text-fg-faint" />
                        <div className="min-w-0 flex-1">
                          <div className="font-display text-sm text-fg">{i.name}</div>
                          <div className="font-mono text-[11px] text-fg-faint">
                            pg{i.pg_version} · {i.repo_type} · :{i.host_port || "—"}
                          </div>
                        </div>
                        <InstanceStatus status={i.status} />
                      </Link>
                    </li>
                  ))}
                </ul>
              )}
            </CardBody>
          </Card>
        </div>

        <Card>
          <CardHeader>
            <CardTitle>Alerts</CardTitle>
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
            {alerts.length === 0 ? (
              <p className="text-sm text-fg-muted">No outstanding reliability issues across the fleet.</p>
            ) : (
              alerts.map((a, idx) => (
                <div key={idx} className="flex items-start gap-2.5 rounded-md border border-danger/20 bg-danger/5 px-3 py-2.5">
                  <AlertTriangle className="mt-0.5 h-3.5 w-3.5 shrink-0 text-danger" />
                  <div>
                    <Link href={`/instances/${a.instance_id}`} className="font-mono text-[11px] text-azure hover:underline">
                      {a.instance_id.slice(0, 8)}
                    </Link>
                    <p className="text-xs text-fg">{a.message}</p>
                  </div>
                </div>
              ))
            )}
          </CardBody>
        </Card>
      </div>
    </div>
  );
}
