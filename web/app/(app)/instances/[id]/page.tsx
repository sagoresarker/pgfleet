"use client";

import { RestoreDialog } from "@/components/restore-dialog";
import { PageHeader } from "@/components/shell";
import { InstanceStatus } from "@/components/status";
import { Badge, Button, Card, CardBody, CardHeader, CardTitle, Spinner, Stat } from "@/components/ui";
import { api } from "@/lib/api";
import { can, useAuth } from "@/lib/auth";
import { formatBytes } from "@/lib/utils";
import * as Tabs from "@radix-ui/react-tabs";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Eye, EyeOff, Play, Plus, Power, RefreshCw, Trash2 } from "lucide-react";
import Link from "next/link";
import { useParams } from "next/navigation";
import { useState } from "react";
import { AnalyticsTab } from "./analytics";
import { ConsoleTab } from "./console";
import { LogsTab } from "./logs";
import { TimescaleTab } from "./timescale";

export default function InstanceDetailPage() {
  const { id } = useParams<{ id: string }>();
  const { user } = useAuth();
  const writable = can(user?.role, "write");
  const qc = useQueryClient();

  const instance = useQuery({
    queryKey: ["instance", id],
    queryFn: () => api.getInstance(id),
    refetchInterval: 4000,
  });
  const backups = useQuery({ queryKey: ["backups", id], queryFn: () => api.listBackups(id), refetchInterval: 8000 });

  const inst = instance.data?.instance;
  const backupList = backups.data?.backups ?? [];

  const lifecycle = useMutation({
    mutationFn: (action: "start" | "stop" | "restart") =>
      action === "start" ? api.startInstance(id) : action === "stop" ? api.stopInstance(id) : api.restartInstance(id),
    onSuccess: () => qc.invalidateQueries({ queryKey: ["instance", id] }),
  });

  const takeBackup = useMutation({
    mutationFn: (type: string) => api.createBackup(id, type),
    onSuccess: () => setTimeout(() => qc.invalidateQueries({ queryKey: ["backups", id] }), 1500),
  });

  if (instance.isLoading || !inst) {
    return (
      <div className="grid place-items-center py-24">
        <Spinner className="h-6 w-6" />
      </div>
    );
  }

  return (
    <div className="rise">
      <div className="mb-4">
        <Link href="/instances" className="font-mono text-[11px] uppercase tracking-wider text-fg-faint hover:text-azure">
          ← instances
        </Link>
      </div>
      <PageHeader
        title={inst.name}
        subtitle={`Postgres ${inst.pg_version} · stanza ${inst.stanza}`}
        action={<InstanceStatus status={inst.status} />}
      />

      {inst.last_error && inst.status === "error" && (
        <div className="mb-6 rounded-md border border-danger/30 bg-danger/10 px-4 py-3 text-sm text-danger">
          {inst.last_error}
        </div>
      )}

      {writable && (
        <div className="mb-6 flex flex-wrap gap-2">
          {inst.status === "stopped" ? (
            <Button size="sm" variant="outline" onClick={() => lifecycle.mutate("start")} disabled={lifecycle.isPending}>
              <Play className="h-4 w-4" /> Start
            </Button>
          ) : (
            <Button size="sm" variant="outline" onClick={() => lifecycle.mutate("stop")} disabled={lifecycle.isPending}>
              <Power className="h-4 w-4" /> Stop
            </Button>
          )}
          <Button size="sm" variant="outline" onClick={() => lifecycle.mutate("restart")} disabled={lifecycle.isPending}>
            <RefreshCw className="h-4 w-4" /> Restart
          </Button>
          <DestroyButton id={id} />
        </div>
      )}

      <Tabs.Root defaultValue="overview">
        <Tabs.List className="mb-6 flex gap-1 border-b border-line">
          {[
            { value: "overview", label: "Overview" },
            { value: "backups", label: "Backups" },
            { value: "analytics", label: "Analytics" },
            { value: "console", label: "Console" },
            { value: "logs", label: "Logs" },
            ...(inst.extensions?.includes("timescaledb") ? [{ value: "timescaledb", label: "TimescaleDB" }] : []),
          ].map((t) => (
            <Tabs.Trigger
              key={t.value}
              value={t.value}
              className="border-b-2 border-transparent px-4 py-2.5 font-display text-sm text-fg-muted transition-colors data-[state=active]:border-azure data-[state=active]:text-fg"
            >
              {t.label}
            </Tabs.Trigger>
          ))}
        </Tabs.List>

        <Tabs.Content value="overview">
          <OverviewTab
            id={id}
            repoType={inst.repo_type}
            hostPort={inst.host_port}
            backupCount={backupList.length}
            parameters={inst.parameters}
            extensions={inst.extensions}
          />
        </Tabs.Content>

        <Tabs.Content value="backups">
          <Card>
            <CardHeader>
              <CardTitle>Backup catalog</CardTitle>
              <div className="flex items-center gap-2">
                {writable && (
                  <>
                    <Button size="sm" variant="outline" onClick={() => takeBackup.mutate("full")} disabled={takeBackup.isPending}>
                      <Plus className="h-4 w-4" /> {takeBackup.isPending ? "Starting…" : "Backup"}
                    </Button>
                    <RestoreDialog instanceId={id} backups={backupList} />
                  </>
                )}
              </div>
            </CardHeader>
            <CardBody className="p-0">
              {backupList.length === 0 ? (
                <p className="px-5 py-10 text-center text-sm text-fg-muted">
                  No backups yet. Take a full backup to protect this instance.
                </p>
              ) : (
                <ul className="divide-y divide-line">
                  {backupList.map((b) => (
                    <li key={b.id} className="grid grid-cols-[1fr_auto_auto] items-center gap-4 px-5 py-3.5">
                      <div>
                        <div className="font-mono text-xs text-fg">{b.label}</div>
                        <div className="font-mono text-[11px] text-fg-faint">
                          {b.wal_start} → {b.wal_stop}
                        </div>
                      </div>
                      <Badge tone={b.type === "full" ? "azure" : "neutral"}>{b.type}</Badge>
                      <span className="font-mono text-xs text-fg-muted tnum">{formatBytes(b.repo_size)}</span>
                    </li>
                  ))}
                </ul>
              )}
            </CardBody>
          </Card>
        </Tabs.Content>

        <Tabs.Content value="analytics">
          <AnalyticsTab id={id} running={inst.status === "running"} />
        </Tabs.Content>

        <Tabs.Content value="console">
          <ConsoleTab id={id} running={inst.status === "running"} writable={writable} />
        </Tabs.Content>

        <Tabs.Content value="logs">
          <LogsTab id={id} running={inst.status === "running"} />
        </Tabs.Content>

        {inst.extensions?.includes("timescaledb") && (
          <Tabs.Content value="timescaledb">
            <TimescaleTab id={id} running={inst.status === "running"} writable={writable} />
          </Tabs.Content>
        )}
      </Tabs.Root>
    </div>
  );
}

function OverviewTab({
  id,
  repoType,
  hostPort,
  backupCount,
  parameters,
  extensions,
}: {
  id: string;
  repoType: string;
  hostPort: number;
  backupCount: number;
  parameters?: Record<string, string>;
  extensions?: string[];
}) {
  const paramEntries = Object.entries(parameters ?? {});
  const hasConfig = paramEntries.length > 0 || (extensions?.length ?? 0) > 0;
  return (
    <div className="grid gap-6 lg:grid-cols-3">
      <div className="grid grid-cols-3 gap-4 lg:col-span-2">
        <Card>
          <CardBody>
            <Stat label="Backups" value={String(backupCount)} />
          </CardBody>
        </Card>
        <Card>
          <CardBody>
            <Stat label="Repository" value={repoType.toUpperCase()} />
          </CardBody>
        </Card>
        <Card>
          <CardBody>
            <Stat label="Port" value={hostPort ? String(hostPort) : "—"} />
          </CardBody>
        </Card>
        <div className="col-span-3">
          <ConnectionCard id={id} />
        </div>
        {hasConfig && (
          <div className="col-span-3">
            <Card>
              <CardHeader>
                <CardTitle>Postgres tuning</CardTitle>
              </CardHeader>
              <CardBody className="space-y-4">
                {paramEntries.length > 0 && (
                  <div className="space-y-1.5">
                    {paramEntries.map(([k, v]) => (
                      <div key={k} className="flex items-center justify-between font-mono text-xs">
                        <span className="text-fg-muted">{k}</span>
                        <span className="text-fg">{v}</span>
                      </div>
                    ))}
                  </div>
                )}
                {(extensions?.length ?? 0) > 0 && (
                  <div className="flex flex-wrap gap-2">
                    {extensions!.map((e) => (
                      <span key={e} className="rounded-md border border-azure/40 bg-azure/10 px-2 py-1 font-mono text-[11px] text-azure">
                        {e}
                      </span>
                    ))}
                  </div>
                )}
              </CardBody>
            </Card>
          </div>
        )}
      </div>
    </div>
  );
}

function ConnectionCard({ id }: { id: string }) {
  const [revealed, setRevealed] = useState(false);
  const conn = useQuery({ queryKey: ["connection", id], queryFn: () => api.connection(id), enabled: revealed });

  return (
    <Card>
      <CardHeader>
        <CardTitle>Connection string</CardTitle>
        <Button size="sm" variant="ghost" onClick={() => setRevealed((r) => !r)}>
          {revealed ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
          {revealed ? "Hide" : "Reveal"}
        </Button>
      </CardHeader>
      <CardBody>
        <code className="block overflow-x-auto rounded-md border border-line bg-ink-900 px-3 py-2.5 font-mono text-xs text-azure">
          {revealed ? conn.data?.dsn ?? "loading…" : "postgres://postgres:••••••••@•••••/postgres"}
        </code>
      </CardBody>
    </Card>
  );
}

function DestroyButton({ id }: { id: string }) {
  const qc = useQueryClient();
  const [confirming, setConfirming] = useState(false);
  const destroy = useMutation({
    mutationFn: () => api.destroyInstance(id, true),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["instances"] });
      window.location.href = "/instances";
    },
  });

  if (!confirming) {
    return (
      <Button size="sm" variant="danger" onClick={() => setConfirming(true)}>
        <Trash2 className="h-4 w-4" /> Destroy
      </Button>
    );
  }
  return (
    <div className="flex items-center gap-2 rounded-md border border-danger/40 bg-danger/10 px-2 py-1">
      <span className="font-mono text-[11px] text-danger">confirm?</span>
      <Button size="sm" variant="danger" onClick={() => destroy.mutate()} disabled={destroy.isPending}>
        Yes, destroy
      </Button>
      <Button size="sm" variant="ghost" onClick={() => setConfirming(false)}>
        No
      </Button>
    </div>
  );
}
