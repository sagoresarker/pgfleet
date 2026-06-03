"use client";

import { RestoreDialog } from "@/components/restore-dialog";
import { PageHeader } from "@/components/shell";
import { InstanceStatus } from "@/components/status";
import {
  ActionMenu,
  ActionMenuItem,
  ActionMenuSeparator,
  Badge,
  Button,
  Card,
  CardBody,
  CardHeader,
  CardTitle,
  ConfirmDialog,
  EmptyState,
  Field,
  Input,
  Modal,
  PasswordInput,
  Skeleton,
  SkeletonRows,
  Stat,
  useToast,
} from "@/components/ui";
import { api, type Backup } from "@/lib/api";
import { can, useAuth } from "@/lib/auth";
import { formatBytes } from "@/lib/utils";
import * as Tabs from "@radix-ui/react-tabs";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ChevronLeft,
  Copy,
  Database,
  Download,
  Eye,
  EyeOff,
  Globe,
  Lock,
  MoreHorizontal,
  Play,
  Plus,
  Power,
  RefreshCw,
  Trash2,
} from "lucide-react";
import Link from "next/link";
import { useParams } from "next/navigation";
import { useEffect, useState } from "react";
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

  // Tabs are addressable by URL hash (e.g. /instances/<id>#console) so other
  // screens can deep-link straight to the SQL console.
  const [tab, setTab] = useState("overview");
  useEffect(() => {
    const h = typeof window !== "undefined" ? window.location.hash.replace("#", "") : "";
    if (h) setTab(h);
  }, []);
  function changeTab(v: string) {
    setTab(v);
    if (typeof window !== "undefined") history.replaceState(null, "", `#${v}`);
  }

  if (instance.isLoading || !inst) {
    return (
      <div>
        <div className="mb-4 h-3 w-24 rounded bg-ink-700/70" />
        <Skeleton className="mb-2 h-8 w-64" />
        <Skeleton className="mb-8 h-4 w-40" />
        <SkeletonRows rows={3} />
      </div>
    );
  }

  return (
    <div className="rise">
      <div className="mb-4">
        <Link
          href="/instances"
          className="inline-flex items-center gap-1 font-mono text-[11px] uppercase tracking-wider text-fg-faint transition-colors hover:text-azure"
        >
          <ChevronLeft className="h-3.5 w-3.5" /> instances
        </Link>
      </div>
      <PageHeader
        title={inst.name}
        subtitle={`Postgres ${inst.pg_version} · stanza ${inst.stanza}`}
        action={
          <div className="flex items-center gap-3">
            <InstanceStatus status={inst.status} />
            {writable && <InstanceToolbar id={id} inst={inst} />}
          </div>
        }
      />

      {inst.last_error && inst.status === "error" && (
        <div role="alert" className="mb-6 rounded-md border border-danger/30 bg-danger/10 px-4 py-3 text-sm text-danger">
          {inst.last_error}
        </div>
      )}

      <Tabs.Root value={tab} onValueChange={changeTab}>
        <Tabs.List className="mb-6 flex gap-1 overflow-x-auto border-b border-line">
          {[
            { value: "overview", label: "Overview" },
            { value: "backups", label: "Backups" },
            { value: "analytics", label: "Analytics" },
            { value: "console", label: "SQL Console" },
            { value: "logs", label: "Logs" },
            ...(inst.extensions?.includes("timescaledb") ? [{ value: "timescaledb", label: "TimescaleDB" }] : []),
          ].map((t) => (
            <Tabs.Trigger
              key={t.value}
              value={t.value}
              className="shrink-0 cursor-pointer border-b-2 border-transparent px-4 py-2.5 font-display text-sm text-fg-muted transition-colors hover:text-fg data-[state=active]:border-azure data-[state=active]:text-fg"
            >
              {t.label}
            </Tabs.Trigger>
          ))}
        </Tabs.List>

        <Tabs.Content value="overview" className="focus:outline-none">
          <OverviewTab
            id={id}
            repoType={inst.repo_type}
            hostPort={inst.host_port}
            isPublic={!!inst.public}
            backupCount={backupList.length}
            parameters={inst.parameters}
            extensions={inst.extensions}
          />
        </Tabs.Content>

        <Tabs.Content value="backups" className="focus:outline-none">
          <BackupsTab id={id} writable={writable} backupList={backupList} loading={backups.isLoading} />
        </Tabs.Content>

        <Tabs.Content value="analytics" className="focus:outline-none">
          <AnalyticsTab id={id} running={inst.status === "running"} />
        </Tabs.Content>

        <Tabs.Content value="console" className="focus:outline-none">
          <ConsoleTab id={id} running={inst.status === "running"} writable={writable} />
        </Tabs.Content>

        <Tabs.Content value="logs" className="focus:outline-none">
          <LogsTab id={id} running={inst.status === "running"} />
        </Tabs.Content>

        {inst.extensions?.includes("timescaledb") && (
          <Tabs.Content value="timescaledb" className="focus:outline-none">
            <TimescaleTab id={id} running={inst.status === "running"} writable={writable} />
          </Tabs.Content>
        )}
      </Tabs.Root>
    </div>
  );
}

/* ---- Toolbar: lifecycle inline, everything else in a tidy Actions menu ---- */
function InstanceToolbar({ id, inst }: { id: string; inst: { name: string; status: string; public?: boolean } }) {
  const qc = useQueryClient();
  const toast = useToast();
  const [cloneOpen, setCloneOpen] = useState(false);
  const [destroyOpen, setDestroyOpen] = useState(false);
  const [downloading, setDownloading] = useState(false);

  const lifecycle = useMutation({
    mutationFn: (action: "start" | "stop" | "restart") =>
      action === "start" ? api.startInstance(id) : action === "stop" ? api.stopInstance(id) : api.restartInstance(id),
    onSuccess: (_d, action) => {
      qc.invalidateQueries({ queryKey: ["instance", id] });
      toast.push(`Instance ${action} requested`, "azure");
    },
    onError: (e) => toast.push(e instanceof Error ? e.message : "Action failed", "danger"),
  });

  const visibility = useMutation({
    mutationFn: () => api.setVisibility(id, !inst.public),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["instance", id] });
      toast.push(`Instance is now ${!inst.public ? "public" : "private"}`, !inst.public ? "azure" : "healthy");
    },
    onError: (e) => toast.push(e instanceof Error ? e.message : "Could not change visibility", "danger"),
  });

  async function downloadDump() {
    setDownloading(true);
    toast.push("Preparing logical dump…", "azure");
    try {
      await api.downloadDump(id, inst.name);
    } catch (e) {
      toast.push(e instanceof Error ? e.message : "Download failed", "danger");
    } finally {
      setDownloading(false);
    }
  }

  const running = inst.status === "running";
  return (
    <div className="flex items-center gap-2">
      {inst.status === "stopped" ? (
        <Button
          size="sm"
          variant="outline"
          loading={lifecycle.isPending && lifecycle.variables === "start"}
          onClick={() => lifecycle.mutate("start")}
        >
          <Play className="h-4 w-4" /> Start
        </Button>
      ) : (
        <Button
          size="sm"
          variant="outline"
          loading={lifecycle.isPending && lifecycle.variables === "stop"}
          onClick={() => lifecycle.mutate("stop")}
        >
          <Power className="h-4 w-4" /> Stop
        </Button>
      )}
      <Button
        size="sm"
        variant="outline"
        loading={lifecycle.isPending && lifecycle.variables === "restart"}
        onClick={() => lifecycle.mutate("restart")}
      >
        <RefreshCw className="h-4 w-4" /> Restart
      </Button>

      <ActionMenu
        trigger={
          <Button size="sm" variant="outline" aria-label="More actions">
            <MoreHorizontal className="h-4 w-4" /> Actions
          </Button>
        }
      >
        <ActionMenuItem icon={<Copy className="h-4 w-4" />} onSelect={() => setCloneOpen(true)}>
          Clone instance…
        </ActionMenuItem>
        <ActionMenuItem
          icon={inst.public ? <Lock className="h-4 w-4" /> : <Globe className="h-4 w-4" />}
          onSelect={() => visibility.mutate()}
          disabled={visibility.isPending}
        >
          {inst.public ? "Make private" : "Make public"}
        </ActionMenuItem>
        <ActionMenuItem icon={<Download className="h-4 w-4" />} onSelect={downloadDump} disabled={downloading || !running}>
          Download logical dump
        </ActionMenuItem>
        <ActionMenuSeparator />
        <ActionMenuItem icon={<Trash2 className="h-4 w-4" />} danger onSelect={() => setDestroyOpen(true)}>
          Destroy instance…
        </ActionMenuItem>
      </ActionMenu>

      <CloneModal id={id} sourceName={inst.name} open={cloneOpen} onOpenChange={setCloneOpen} />
      <DestroyModal id={id} name={inst.name} open={destroyOpen} onOpenChange={setDestroyOpen} />
    </div>
  );
}

function CloneModal({
  id,
  sourceName,
  open,
  onOpenChange,
}: {
  id: string;
  sourceName: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const toast = useToast();
  const [name, setName] = useState(`${sourceName}-clone`);
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const clone = useMutation({
    mutationFn: () => api.cloneInstance(id, { name, password }),
    onSuccess: () => {
      toast.push(`Cloning into ${name}…`, "azure");
      onOpenChange(false);
      window.location.href = "/instances";
    },
    onError: (e) => setError(e instanceof Error ? e.message : "Clone failed"),
  });

  const valid = /^[a-z][a-z0-9-]{1,38}$/.test(name) && password.length >= 8;
  return (
    <Modal
      open={open}
      onOpenChange={onOpenChange}
      title="Clone instance"
      description={`Provision a new, independent instance from a backup of ${sourceName}.`}
      footer={
        <>
          <Button variant="ghost" size="sm" onClick={() => onOpenChange(false)} disabled={clone.isPending}>
            Cancel
          </Button>
          <Button size="sm" loading={clone.isPending} disabled={!valid} onClick={() => clone.mutate()}>
            <Copy className="h-4 w-4" /> Clone instance
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        <Field label="New instance name" hint="Lowercase letters, digits and hyphens; 2–39 chars.">
          <Input
            value={name}
            onChange={(e) => setName(e.target.value.toLowerCase())}
            placeholder="orders-clone"
            autoComplete="off"
            spellCheck={false}
          />
        </Field>
        <Field label="New superuser password" hint="At least 8 characters. The clone gets its own credentials.">
          <PasswordInput value={password} onChange={(e) => setPassword(e.target.value)} autoComplete="new-password" />
        </Field>
        {error && (
          <div role="alert" aria-live="assertive" className="rounded-md border border-danger/30 bg-danger/10 px-3 py-2 text-xs text-danger">
            {error}
          </div>
        )}
      </div>
    </Modal>
  );
}

function DestroyModal({
  id,
  name,
  open,
  onOpenChange,
}: {
  id: string;
  name: string;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const qc = useQueryClient();
  const toast = useToast();
  const destroy = useMutation({
    mutationFn: () => api.destroyInstance(id, true),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["instances"] });
      toast.push(`Destroyed ${name}`, "danger");
      window.location.href = "/instances";
    },
    onError: (e) => toast.push(e instanceof Error ? e.message : "Destroy failed", "danger"),
  });
  return (
    <ConfirmDialog
      open={open}
      onOpenChange={onOpenChange}
      title={`Destroy “${name}”?`}
      description="This permanently removes the container and its data volume. Backups already in the repository are retained."
      danger
      confirmLabel="Destroy instance"
      loading={destroy.isPending}
      onConfirm={() => destroy.mutate()}
    />
  );
}

/* ---- Backups tab ---- */
function BackupsTab({
  id,
  writable,
  backupList,
  loading,
}: {
  id: string;
  writable: boolean;
  backupList: Backup[];
  loading: boolean;
}) {
  const qc = useQueryClient();
  const toast = useToast();
  const takeBackup = useMutation({
    mutationFn: (type: string) => api.createBackup(id, type),
    onSuccess: () => {
      toast.push("Backup started", "azure");
      setTimeout(() => qc.invalidateQueries({ queryKey: ["backups", id] }), 1500);
    },
    onError: (e) => toast.push(e instanceof Error ? e.message : "Backup failed", "danger"),
  });

  return (
    <Card>
      <CardHeader>
        <CardTitle>Backup catalog</CardTitle>
        {writable && (
          <div className="flex items-center gap-2">
            <Button size="sm" variant="outline" loading={takeBackup.isPending} onClick={() => takeBackup.mutate("full")}>
              {!takeBackup.isPending && <Plus className="h-4 w-4" />} Backup
            </Button>
            <RestoreDialog instanceId={id} backups={backupList} />
          </div>
        )}
      </CardHeader>
      <CardBody className="p-0">
        {loading ? (
          <div className="p-5">
            <SkeletonRows rows={3} />
          </div>
        ) : backupList.length === 0 ? (
          <EmptyState
            icon={<Database className="h-5 w-5" />}
            title="No backups yet"
            description="Take a full backup to protect this instance and unlock point-in-time recovery."
            action={
              writable ? (
                <Button size="sm" loading={takeBackup.isPending} onClick={() => takeBackup.mutate("full")}>
                  {!takeBackup.isPending && <Plus className="h-4 w-4" />} Take first backup
                </Button>
              ) : undefined
            }
          />
        ) : (
          <ul className="divide-y divide-line">
            {backupList.map((b) => (
              <li key={b.id} className="grid grid-cols-[1fr_auto_auto] items-center gap-4 px-5 py-3.5">
                <div className="min-w-0">
                  <div className="truncate font-mono text-xs text-fg">{b.label}</div>
                  <div className="truncate font-mono text-[11px] text-fg-faint">
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
  );
}

function OverviewTab({
  id,
  repoType,
  hostPort,
  isPublic,
  backupCount,
  parameters,
  extensions,
}: {
  id: string;
  repoType: string;
  hostPort: number;
  isPublic: boolean;
  backupCount: number;
  parameters?: Record<string, string>;
  extensions?: string[];
}) {
  const paramEntries = Object.entries(parameters ?? {});
  const hasConfig = paramEntries.length > 0 || (extensions?.length ?? 0) > 0;
  return (
    <div className="space-y-6">
      <div className="grid grid-cols-2 gap-4 sm:grid-cols-4">
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
            <Stat label="Host port" value={hostPort ? String(hostPort) : "—"} />
          </CardBody>
        </Card>
        <Card>
          <CardBody>
            <Stat label="Exposure" value={isPublic ? "Public" : "Private"} tone={isPublic ? "signal" : undefined} />
          </CardBody>
        </Card>
      </div>

      <ConnectionCard id={id} />

      {hasConfig && (
        <Card>
          <CardHeader>
            <CardTitle>Postgres tuning</CardTitle>
          </CardHeader>
          <CardBody className="space-y-4">
            {paramEntries.length > 0 && (
              <div className="space-y-1.5">
                {paramEntries.map(([k, v]) => (
                  <div key={k} className="flex items-center justify-between gap-4 font-mono text-xs">
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
      )}
    </div>
  );
}

function ConnectionCard({ id }: { id: string }) {
  const [revealed, setRevealed] = useState(false);
  const toast = useToast();
  const conn = useQuery({ queryKey: ["connection", id], queryFn: () => api.connection(id), enabled: revealed });

  async function copy() {
    if (conn.data?.dsn) {
      await navigator.clipboard.writeText(conn.data.dsn);
      toast.push("Connection string copied", "healthy");
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Connection string</CardTitle>
        <div className="flex items-center gap-2">
          {revealed && conn.data?.dsn && (
            <Button size="sm" variant="ghost" onClick={copy}>
              <Copy className="h-4 w-4" /> Copy
            </Button>
          )}
          <Button size="sm" variant="ghost" onClick={() => setRevealed((r) => !r)}>
            {revealed ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
            {revealed ? "Hide" : "Reveal"}
          </Button>
        </div>
      </CardHeader>
      <CardBody>
        <code className="block overflow-x-auto rounded-md border border-line bg-ink-850 px-3 py-2.5 font-mono text-xs text-azure">
          {revealed ? conn.data?.dsn ?? "loading…" : "postgres://postgres:••••••••@•••••/postgres"}
        </code>
      </CardBody>
    </Card>
  );
}
