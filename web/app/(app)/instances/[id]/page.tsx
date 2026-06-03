"use client";

import { BackupLineage } from "@/components/backup-lineage";
import { ComposePreview } from "@/components/compose-preview";
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
  Select,
  Skeleton,
  SkeletonRows,
  Stat,
  Table,
  Td,
  Th,
  THead,
  Tooltip,
  Tr,
  useToast,
} from "@/components/ui";
import { api, type Backup } from "@/lib/api";
import { can, useAuth } from "@/lib/auth";
import { cn, formatBytes } from "@/lib/utils";
import * as Tabs from "@radix-ui/react-tabs";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ChevronLeft,
  Copy,
  Database,
  Download,
  Eye,
  EyeOff,
  FileDown,
  Globe,
  Lock,
  MoreHorizontal,
  Play,
  Plus,
  Power,
  RefreshCw,
  ShieldCheck,
  Trash2,
  Users,
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
  // screens can deep-link straight to the SQL console. Only KNOWN tab ids are
  // honored — a bogus hash (#doesnotexist) must fall back to Overview rather
  // than leave the operator on a blank, no-tab-active panel (N11).
  const knownTabs = ["overview", "databases", "roles", "backups", "analytics", "console", "logs", "timescaledb"];
  const [tab, setTab] = useState("overview");
  useEffect(() => {
    const h = typeof window !== "undefined" ? window.location.hash.replace("#", "") : "";
    if (h && knownTabs.includes(h)) setTab(h);
    // eslint-disable-next-line react-hooks/exhaustive-deps
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
            {writable && <InstanceToolbar id={id} inst={inst} backupCount={backupList.length} />}
          </div>
        }
      />

      {inst.last_error && inst.status === "error" && (
        <div role="alert" className="mb-6 rounded-md border border-danger/30 bg-danger/10 px-4 py-3 text-sm text-danger">
          {inst.last_error}
        </div>
      )}

      <Tabs.Root value={tab} onValueChange={changeTab}>
        {/* Edge-faded horizontal scroller: on narrow widths the tab strip
            scrolls instead of wrapping/overflowing, and the right fade hints
            there is more to reveal. */}
        <div className="relative mb-6 border-b border-line">
          <Tabs.List className="-mb-px flex gap-1 overflow-x-auto [scrollbar-width:none] [&::-webkit-scrollbar]:hidden">
            {[
              { value: "overview", label: "Overview" },
              { value: "databases", label: "Databases" },
              { value: "roles", label: "Roles" },
              { value: "backups", label: "Backups" },
              { value: "analytics", label: "Analytics" },
              { value: "console", label: "Console" },
              { value: "logs", label: "Logs" },
              ...(inst.extensions?.includes("timescaledb") ? [{ value: "timescaledb", label: "TimescaleDB" }] : []),
            ].map((t) => (
              <Tabs.Trigger
                key={t.value}
                value={t.value}
                className="shrink-0 cursor-pointer whitespace-nowrap rounded-t-md border-b-2 border-transparent px-3.5 py-2.5 font-display text-sm tracking-tight text-fg-muted transition-colors hover:text-fg focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-azure/50 data-[state=active]:border-azure data-[state=active]:text-fg"
              >
                {t.label}
              </Tabs.Trigger>
            ))}
          </Tabs.List>
          <div
            aria-hidden
            className="pointer-events-none absolute inset-y-0 right-0 w-8 bg-gradient-to-l from-ink-950 to-transparent"
          />
        </div>

        <Tabs.Content value="overview" className="focus:outline-none">
          <OverviewTab
            id={id}
            running={inst.status === "running"}
            repoType={inst.repo_type}
            hostPort={inst.host_port}
            isPublic={!!inst.public}
            backupCount={backupList.length}
            parameters={inst.parameters}
            extensions={inst.extensions}
          />
        </Tabs.Content>

        <Tabs.Content value="databases" className="focus:outline-none">
          <DatabasesTab id={id} running={inst.status === "running"} writable={writable} />
        </Tabs.Content>

        <Tabs.Content value="roles" className="focus:outline-none">
          <RolesTab id={id} running={inst.status === "running"} writable={writable} />
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
function InstanceToolbar({
  id,
  inst,
  backupCount,
}: {
  id: string;
  inst: { name: string; status: string; public?: boolean; role?: string; cluster_id?: string };
  backupCount: number;
}) {
  const qc = useQueryClient();
  const toast = useToast();
  const [cloneOpen, setCloneOpen] = useState(false);
  const [destroyOpen, setDestroyOpen] = useState(false);
  const [composeOpen, setComposeOpen] = useState(false);
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
        <ActionMenuItem icon={<FileDown className="h-4 w-4" />} onSelect={() => setComposeOpen(true)}>
          View docker-compose
        </ActionMenuItem>
        <ActionMenuSeparator />
        <ActionMenuItem icon={<Trash2 className="h-4 w-4" />} danger onSelect={() => setDestroyOpen(true)}>
          Destroy instance…
        </ActionMenuItem>
      </ActionMenu>

      <CloneModal id={id} sourceName={inst.name} backupCount={backupCount} open={cloneOpen} onOpenChange={setCloneOpen} />
      <ComposePreview kind="instance" id={id} name={inst.name} open={composeOpen} onOpenChange={setComposeOpen} />
      <DestroyModal
        id={id}
        name={inst.name}
        isClusterPrimary={inst.role === "primary" && !!inst.cluster_id}
        open={destroyOpen}
        onOpenChange={setDestroyOpen}
      />
    </div>
  );
}

function CloneModal({
  id,
  sourceName,
  backupCount,
  open,
  onOpenChange,
}: {
  id: string;
  sourceName: string;
  backupCount: number;
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
  const fresh = backupCount === 0;
  return (
    <Modal
      open={open}
      onOpenChange={onOpenChange}
      title="Clone instance"
      description={`Provision a new, independent copy of ${sourceName} as it is right now.`}
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
        <div className="flex items-start gap-2.5 rounded-md border border-azure/30 bg-azure/10 px-3 py-2.5 text-xs text-azure">
          <Database className="mt-0.5 h-4 w-4 shrink-0" />
          <span>
            Cloning first captures a fresh backup of <strong>{sourceName}</strong>
            {fresh ? " (it has none yet)" : ""}, then restores it into the new instance — so the clone reflects the
            current state. This can take a little while; follow progress in Events.
          </span>
        </div>
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
  isClusterPrimary,
  open,
  onOpenChange,
}: {
  id: string;
  name: string;
  isClusterPrimary?: boolean;
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

  // Guard: a cluster's PRIMARY must not be destroyed directly — that would
  // decapitate the cluster. Direct the operator to delete the cluster instead.
  if (isClusterPrimary) {
    return (
      <Modal
        open={open}
        onOpenChange={onOpenChange}
        title="Can't destroy a cluster primary"
        description={`${name} is the PRIMARY of a high-availability cluster.`}
        size="sm"
        footer={
          <Button variant="primary" size="sm" onClick={() => onOpenChange(false)}>
            Got it
          </Button>
        }
      >
        <div className="flex items-start gap-2.5 rounded-md border border-danger/30 bg-danger/10 px-3 py-2.5 text-xs text-danger">
          <Trash2 className="mt-0.5 h-4 w-4 shrink-0" />
          <span>
            Destroying the primary directly would break replication and risk data loss. Delete the whole{" "}
            <strong>cluster</strong> from the Clusters page — it tears down the members in the correct order.
          </span>
        </div>
      </Modal>
    );
  }

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
  const [backupOpen, setBackupOpen] = useState(false);
  const toast = useToast();
  const verify = useMutation({
    mutationFn: () => api.verifyBackups(id),
    onSuccess: () => toast.push("Repository verify started — see Events for the result", "azure"),
    onError: (e) => toast.push(e instanceof Error ? e.message : "Verify failed", "danger"),
  });

  return (
    <div className="space-y-6">
    {backupList.length > 0 && <BackupLineage backups={backupList} />}
    <Card>
      <CardHeader>
        <CardTitle>Backup catalog</CardTitle>
        {writable && (
          <div className="flex items-center gap-2">
            <Button size="sm" variant="outline" onClick={() => setBackupOpen(true)}>
              <Plus className="h-4 w-4" /> Backup
            </Button>
            <Tooltip label="Checksum-validate the whole repository">
              <Button size="sm" variant="outline" loading={verify.isPending} onClick={() => verify.mutate()}>
                <ShieldCheck className="h-4 w-4" /> Verify
              </Button>
            </Tooltip>
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
            description="Take a backup to protect this instance and unlock point-in-time recovery and cloning."
            action={
              writable ? (
                <Button size="sm" onClick={() => setBackupOpen(true)}>
                  <Plus className="h-4 w-4" /> Take first backup
                </Button>
              ) : undefined
            }
          />
        ) : (
          <ul className="divide-y divide-line">
            {backupList.map((b) => (
              <li key={b.id} className="grid grid-cols-[1fr_auto_auto_auto] items-center gap-4 px-5 py-3.5">
                <div className="min-w-0">
                  <div className="flex items-center gap-2">
                    <span className="truncate font-mono text-xs text-fg">{b.label}</span>
                    {b.annotations?.name && (
                      <span className="shrink-0 rounded border border-violet/30 bg-violet/10 px-1.5 py-0.5 font-mono text-[10px] text-violet">
                        {b.annotations.name}
                      </span>
                    )}
                  </div>
                  <div className="truncate font-mono text-[11px] text-fg-faint">
                    {b.wal_start} → {b.wal_stop}
                  </div>
                </div>
                <Badge tone={b.type === "full" ? "azure" : "neutral"}>{b.type}</Badge>
                <span className="font-mono text-xs text-fg-muted tnum">{formatBytes(b.repo_size)}</span>
                {writable ? <DeleteBackupButton id={id} label={b.label} /> : <span />}
              </li>
            ))}
          </ul>
        )}
      </CardBody>
      <BackupModal id={id} open={backupOpen} onOpenChange={setBackupOpen} />
    </Card>
    </div>
  );
}

function BackupModal({ id, open, onOpenChange }: { id: string; open: boolean; onOpenChange: (o: boolean) => void }) {
  const qc = useQueryClient();
  const toast = useToast();
  const [type, setType] = useState("full");
  const [note, setNote] = useState("");
  const take = useMutation({
    mutationFn: () => api.createBackup(id, type, { annotation: note.trim() || undefined }),
    onSuccess: () => {
      toast.push(`${type} backup started — follow progress in Events`, "azure");
      setNote("");
      onOpenChange(false);
      setTimeout(() => qc.invalidateQueries({ queryKey: ["backups", id] }), 1500);
    },
    onError: (e) => toast.push(e instanceof Error ? e.message : "Backup failed", "danger"),
  });
  return (
    <Modal
      open={open}
      onOpenChange={onOpenChange}
      title="Take a backup"
      description="pgBackRest backups stream to this instance's repository. Progress and completion are recorded in the Events log."
      footer={
        <>
          <Button variant="ghost" size="sm" onClick={() => onOpenChange(false)} disabled={take.isPending}>
            Cancel
          </Button>
          <Button size="sm" loading={take.isPending} onClick={() => take.mutate()}>
            <Plus className="h-4 w-4" /> Start backup
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        <Field label="Backup type" hint="Full = a complete standalone copy. Differential/Incremental build on the last full and are smaller/faster.">
          <Select value={type} onChange={(e) => setType(e.target.value)}>
            <option value="full">Full — complete standalone backup</option>
            <option value="diff">Differential — changes since the last full</option>
            <option value="incr">Incremental — changes since the last backup</option>
          </Select>
        </Field>
        <Field label="Note (optional)" hint="Stored as a pgBackRest annotation on this backup — e.g. “pre-upgrade” or a ticket number.">
          <Input value={note} onChange={(e) => setNote(e.target.value)} placeholder="pre-migration snapshot" maxLength={200} />
        </Field>
      </div>
    </Modal>
  );
}

function DeleteBackupButton({ id, label }: { id: string; label: string }) {
  const qc = useQueryClient();
  const toast = useToast();
  const [open, setOpen] = useState(false);
  const del = useMutation({
    mutationFn: () => api.deleteBackup(id, label),
    onSuccess: () => {
      toast.push("Backup deleted", "azure");
      setOpen(false);
      qc.invalidateQueries({ queryKey: ["backups", id] });
    },
    onError: (e) => toast.push(e instanceof Error ? e.message : "Delete failed", "danger"),
  });
  return (
    <>
      <button
        onClick={() => setOpen(true)}
        aria-label={`Delete backup ${label}`}
        className="cursor-pointer rounded p-1.5 text-fg-faint transition-colors hover:bg-danger/10 hover:text-danger focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-danger/40"
      >
        <Trash2 className="h-4 w-4" />
      </button>
      <ConfirmDialog
        open={open}
        onOpenChange={setOpen}
        title="Delete this backup?"
        description={`Remove ${label} from the repository. This cannot be undone; later incremental/differential backups that depend on it may also be affected.`}
        danger
        confirmLabel="Delete backup"
        loading={del.isPending}
        onConfirm={() => del.mutate()}
      />
    </>
  );
}

/* ---- Databases tab — lists the databases inside this instance ---- */
function DatabasesTab({ id, running, writable }: { id: string; running: boolean; writable: boolean }) {
  const [createOpen, setCreateOpen] = useState(false);
  const dbs = useQuery({
    queryKey: ["databases", id],
    enabled: running,
    refetchInterval: 20000,
    queryFn: () =>
      api.runSQL(
        id,
        "SELECT datname AS database, pg_catalog.pg_get_userbyid(datdba) AS owner, " +
          "pg_encoding_to_char(encoding) AS encoding, pg_size_pretty(pg_database_size(datname)) AS size " +
          "FROM pg_database WHERE NOT datistemplate ORDER BY datname",
      ),
  });

  if (!running) {
    return (
      <EmptyState
        icon={<Database className="h-5 w-5" />}
        title="Databases unavailable"
        description="Database listing is available while the instance is running."
      />
    );
  }

  const rows = dbs.data?.rows ?? [];
  const err = dbs.error instanceof Error ? dbs.error.message : null;

  return (
    <Card>
      <CardHeader>
        <CardTitle>Databases</CardTitle>
        <div className="flex items-center gap-3">
          <span className="font-mono text-[11px] text-fg-faint tnum">
            {rows.length} database{rows.length === 1 ? "" : "s"}
          </span>
          {writable && (
            <Button size="sm" variant="outline" onClick={() => setCreateOpen(true)}>
              <Plus className="h-4 w-4" /> Create database
            </Button>
          )}
        </div>
      </CardHeader>
      <CreateDatabaseModal id={id} open={createOpen} onOpenChange={setCreateOpen} />
      <DatabasesBody loading={dbs.isLoading} err={err} rows={rows} />
    </Card>
  );
}

function DatabasesBody({ loading, err, rows }: { loading: boolean; err: string | null; rows: unknown[][] }) {
  return (
    <CardBody className="p-0">
      {loading ? (
        <div className="p-5">
          <SkeletonRows rows={3} />
        </div>
      ) : err ? (
        <div role="alert" className="m-5 rounded-md border border-danger/30 bg-danger/10 px-3.5 py-2.5 font-mono text-xs text-danger">
          {err}
        </div>
      ) : rows.length === 0 ? (
        <EmptyState icon={<Database className="h-5 w-5" />} title="No databases" description="This instance has no non-template databases." />
      ) : (
        <Table>
          <THead>
            <Th>Database</Th>
            <Th>Owner</Th>
            <Th>Encoding</Th>
            <Th align="right">Size</Th>
          </THead>
          <tbody>
            {rows.map((r, i) => (
              <Tr key={i}>
                <Td className="font-display text-fg">{String(r[0])}</Td>
                <Td className="font-mono text-xs text-fg-muted">{String(r[1])}</Td>
                <Td className="font-mono text-xs text-fg-muted">{String(r[2])}</Td>
                <Td align="right" className="font-mono text-xs text-fg-muted tnum">{String(r[3])}</Td>
              </Tr>
            ))}
          </tbody>
        </Table>
      )}
    </CardBody>
  );
}

function CreateDatabaseModal({ id, open, onOpenChange }: { id: string; open: boolean; onOpenChange: (o: boolean) => void }) {
  const qc = useQueryClient();
  const toast = useToast();
  const [name, setName] = useState("");
  const [error, setError] = useState<string | null>(null);
  const create = useMutation({
    // The /sql runner executes raw operator SQL (no server-side quoting), so the
    // identifier is gated by the name regex below AND double-quote-escaped here
    // (doubling any ") so it is always a single safe quoted identifier (N7).
    mutationFn: () => api.runSQL(id, `CREATE DATABASE "${name.replace(/"/g, '""')}"`),
    onSuccess: () => {
      toast.push(`Database ${name} created`, "healthy");
      setName("");
      onOpenChange(false);
      qc.invalidateQueries({ queryKey: ["databases", id] });
    },
    onError: (e) => setError(e instanceof Error ? e.message : "Create failed"),
  });
  const valid = /^[a-z_][a-z0-9_]{0,62}$/.test(name);
  return (
    <Modal
      open={open}
      onOpenChange={onOpenChange}
      title="Create database"
      footer={
        <>
          <Button variant="ghost" size="sm" onClick={() => onOpenChange(false)} disabled={create.isPending}>
            Cancel
          </Button>
          <Button size="sm" loading={create.isPending} disabled={!valid} onClick={() => create.mutate()}>
            <Plus className="h-4 w-4" /> Create
          </Button>
        </>
      }
    >
      <Field label="Database name" hint="Lowercase letters, digits and underscores; must start with a letter or underscore.">
        <Input
          value={name}
          onChange={(e) => {
            setName(e.target.value.toLowerCase());
            setError(null);
          }}
          placeholder="analytics"
          autoComplete="off"
          spellCheck={false}
        />
      </Field>
      {error && (
        <div role="alert" aria-live="assertive" className="mt-3 rounded-md border border-danger/30 bg-danger/10 px-3 py-2 text-xs text-danger">
          {error}
        </div>
      )}
    </Modal>
  );
}

function CreateRoleModal({ id, open, onOpenChange }: { id: string; open: boolean; onOpenChange: (o: boolean) => void }) {
  const qc = useQueryClient();
  const toast = useToast();
  const [name, setName] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const create = useMutation({
    // Least-privilege by default: a LOGIN role with none of the elevated
    // attributes. /sql runs raw operator SQL, so we escape both the identifier
    // (double any ") and the password literal (double any ') here; the role name
    // is also regex-gated below.
    mutationFn: () =>
      api.runSQL(
        id,
        `CREATE ROLE "${name.replace(/"/g, '""')}" LOGIN PASSWORD '${password.replace(/'/g, "''")}' NOSUPERUSER NOCREATEDB NOCREATEROLE`,
      ),
    onSuccess: () => {
      toast.push(`Role ${name} created`, "healthy");
      setName("");
      setPassword("");
      onOpenChange(false);
      qc.invalidateQueries({ queryKey: ["roles", id] });
    },
    onError: (e) => setError(e instanceof Error ? e.message : "Create failed"),
  });
  const valid = /^[a-z_][a-z0-9_]{0,62}$/.test(name) && password.length >= 8;
  return (
    <Modal
      open={open}
      onOpenChange={onOpenChange}
      title="Create role"
      description="A least-privilege login role — no superuser, createdb or createrole. Grant it only what your app needs."
      footer={
        <>
          <Button variant="ghost" size="sm" onClick={() => onOpenChange(false)} disabled={create.isPending}>
            Cancel
          </Button>
          <Button size="sm" loading={create.isPending} disabled={!valid} onClick={() => create.mutate()}>
            <Plus className="h-4 w-4" /> Create role
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        <Field label="Role name" hint="Lowercase letters, digits and underscores; start with a letter or underscore.">
          <Input
            value={name}
            onChange={(e) => {
              setName(e.target.value.toLowerCase());
              setError(null);
            }}
            placeholder="app_readwrite"
            autoComplete="off"
            spellCheck={false}
          />
        </Field>
        <Field label="Password" hint="At least 8 characters.">
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

/* ---- Roles tab — lists the database roles/users in this instance ---- */
function RolesTab({ id, running, writable }: { id: string; running: boolean; writable: boolean }) {
  const [createOpen, setCreateOpen] = useState(false);
  const roles = useQuery({
    queryKey: ["roles", id],
    enabled: running,
    refetchInterval: 30000,
    queryFn: () =>
      api.runSQL(
        id,
        "SELECT rolname AS role, rolsuper AS superuser, rolcreatedb AS createdb, rolcanlogin AS login, " +
          "CASE WHEN rolconnlimit < 0 THEN 'unlimited' ELSE rolconnlimit::text END AS conn_limit " +
          "FROM pg_roles ORDER BY rolname",
      ),
  });

  if (!running) {
    return (
      <EmptyState icon={<Users className="h-5 w-5" />} title="Roles unavailable" description="Role listing is available while the instance is running." />
    );
  }
  const rows = roles.data?.rows ?? [];
  const err = roles.error instanceof Error ? roles.error.message : null;

  return (
    <Card>
      <CardHeader>
        <CardTitle>Roles &amp; users</CardTitle>
        <div className="flex items-center gap-3">
          <span className="font-mono text-[11px] text-fg-faint tnum">
            {rows.length} role{rows.length === 1 ? "" : "s"}
          </span>
          {writable && (
            <Button size="sm" variant="outline" onClick={() => setCreateOpen(true)}>
              <Plus className="h-4 w-4" /> Create role
            </Button>
          )}
        </div>
      </CardHeader>
      <CreateRoleModal id={id} open={createOpen} onOpenChange={setCreateOpen} />
      <CardBody className="p-0">
        {roles.isLoading ? (
          <div className="p-5">
            <SkeletonRows rows={3} />
          </div>
        ) : err ? (
          <div role="alert" className="m-5 rounded-md border border-danger/30 bg-danger/10 px-3.5 py-2.5 font-mono text-xs text-danger">
            {err}
          </div>
        ) : rows.length === 0 ? (
          <EmptyState icon={<Users className="h-5 w-5" />} title="No roles" description="This instance has no roles." />
        ) : (
          <Table>
            <THead>
              <Th>Role</Th>
              <Th>Attributes</Th>
              <Th align="right">Conn limit</Th>
            </THead>
            <tbody>
              {rows.map((r, i) => (
                <Tr key={i}>
                  <Td className="font-display text-fg">{String(r[0])}</Td>
                  <Td>
                    <div className="flex flex-wrap gap-1.5">
                      {String(r[1]) === "true" && <Badge tone="danger">superuser</Badge>}
                      {String(r[2]) === "true" && <Badge tone="azure">createdb</Badge>}
                      {String(r[3]) === "true" ? <Badge tone="healthy">login</Badge> : <Badge tone="neutral">no login</Badge>}
                    </div>
                  </Td>
                  <Td align="right" className="font-mono text-xs text-fg-muted tnum">{String(r[4])}</Td>
                </Tr>
              ))}
            </tbody>
          </Table>
        )}
      </CardBody>
    </Card>
  );
}

function OverviewTab({
  id,
  running,
  repoType,
  hostPort,
  isPublic,
  backupCount,
  parameters,
  extensions,
}: {
  id: string;
  running: boolean;
  repoType: string;
  hostPort: number;
  isPublic: boolean;
  backupCount: number;
  parameters?: Record<string, string>;
  extensions?: string[];
}) {
  const paramEntries = Object.entries(parameters ?? {});
  const hasConfig = paramEntries.length > 0 || (extensions?.length ?? 0) > 0;
  const engine = extensions?.includes("timescaledb") ? "TimescaleDB" : "PostgreSQL";
  return (
    <div className="space-y-6">
      {/* One coherent overview: prominent live metrics up top, then a compact
          metadata strip for the static facts — no redundant pile of cards. */}
      <OverviewStats
        id={id}
        running={running}
        engine={engine}
        repoType={repoType}
        hostPort={hostPort}
        isPublic={isPublic}
        backupCount={backupCount}
      />

      <ConnectionCard id={id} />

      {hasConfig && (
        <Card>
          <CardHeader>
            <CardTitle>Postgres tuning</CardTitle>
          </CardHeader>
          <CardBody className="space-y-4">
            {paramEntries.length > 0 && (
              <dl className="grid gap-x-8 gap-y-1.5 sm:grid-cols-2">
                {paramEntries.map(([k, v]) => (
                  <div key={k} className="flex items-center justify-between gap-4 border-b border-line/60 py-1.5 font-mono text-xs last:border-0">
                    <dt className="text-fg-muted">{k}</dt>
                    <dd className="text-fg tnum">{v}</dd>
                  </div>
                ))}
              </dl>
            )}
            {(extensions?.length ?? 0) > 0 && (
              <div className="flex flex-wrap gap-2 pt-1">
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

/* A single, designed overview stats area:
 *  - a row of PRIMARY live metrics (the numbers an operator watches), and
 *  - a compact METADATA strip of static facts (engine, repo, port, exposure).
 * The two database/role counts that used to duplicate the Databases/Roles tabs
 * are dropped; size + connections are what matter at a glance here. */
function OverviewStats({
  id,
  running,
  engine,
  repoType,
  hostPort,
  isPublic,
  backupCount,
}: {
  id: string;
  running: boolean;
  engine: string;
  repoType: string;
  hostPort: number;
  isPublic: boolean;
  backupCount: number;
}) {
  const q = useQuery({
    queryKey: ["overview-live", id],
    enabled: running,
    refetchInterval: 8000,
    queryFn: () =>
      api.runSQL(
        id,
        "SELECT " +
          "(SELECT count(*) FROM pg_stat_activity)::text, " +
          "(SELECT count(*) FROM pg_database WHERE NOT datistemplate)::text, " +
          "(SELECT count(*) FROM pg_roles)::text, " +
          "(SELECT pg_size_pretty(COALESCE(sum(pg_database_size(datname)),0)) FROM pg_database WHERE NOT datistemplate), " +
          "date_trunc('second', now() - pg_postmaster_start_time())::text",
      ),
  });
  const r = q.data?.rows?.[0];
  const v = (i: number) => (r ? String(r[i]) : running ? "…" : "—");

  const meta: { label: string; value: string; tone?: "signal" | "violet" }[] = [
    { label: "Engine", value: engine, tone: engine === "TimescaleDB" ? "violet" : undefined },
    { label: "Repository", value: repoType.toUpperCase() },
    { label: "Host port", value: hostPort ? String(hostPort) : "—" },
    { label: "Exposure", value: isPublic ? "Public" : "Private", tone: isPublic ? "signal" : undefined },
    { label: "Databases", value: v(1) },
    { label: "Roles", value: v(2) },
  ];

  return (
    <div className="space-y-4">
      {/* Primary metrics — the live numbers worth watching. */}
      <div className="grid grid-cols-2 gap-4 lg:grid-cols-4">
        <Card>
          <CardBody>
            <Stat label="Total size" value={v(3)} />
          </CardBody>
        </Card>
        <Card>
          <CardBody>
            <Stat label="Connections" value={v(0)} />
          </CardBody>
        </Card>
        <Card>
          <CardBody>
            <Stat label="Uptime" value={v(4)} />
          </CardBody>
        </Card>
        <Card>
          <CardBody>
            <Stat label="Backups" value={String(backupCount)} tone={backupCount === 0 ? "signal" : undefined} />
          </CardBody>
        </Card>
      </div>

      {/* Metadata strip — static facts, denser and visually subordinate.
          Cells share hairline rules via a 1px-gap grid over the line colour;
          robust across every breakpoint with no fragile nth-child math. */}
      <Card className="overflow-hidden">
        <dl className="grid grid-cols-2 gap-px bg-line sm:grid-cols-3 lg:grid-cols-6">
          {meta.map((m) => (
            <div key={m.label} className="flex flex-col gap-1 bg-ink-900 px-5 py-3.5">
              <dt className="font-mono text-[10px] uppercase tracking-wider text-fg-faint">{m.label}</dt>
              <dd
                className={cn(
                  "font-display text-base tnum text-fg",
                  m.tone === "signal" && "text-signal",
                  m.tone === "violet" && "text-violet",
                )}
              >
                {m.value}
              </dd>
            </div>
          ))}
        </dl>
      </Card>
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
