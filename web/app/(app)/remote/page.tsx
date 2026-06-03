"use client";

import { PageHeader } from "@/components/shell";
import {
  Badge,
  Button,
  Card,
  CardBody,
  CardHeader,
  CardTitle,
  EmptyState,
  Field,
  Input,
  PasswordInput,
  Select,
  SkeletonRows,
  useToast,
} from "@/components/ui";
import { api, type RemoteDump, type RemoteRestoreTarget } from "@/lib/api";
import { formatBytes } from "@/lib/utils";
import * as Dialog from "@radix-ui/react-dialog";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Database, DownloadCloud, RotateCcw, Server, X } from "lucide-react";
import { useRouter } from "next/navigation";
import { useState } from "react";

type SSLMode = "disable" | "require" | "verify-full";

export default function RemoteBackupPage() {
  const dumps = useQuery({
    queryKey: ["remote-backups"],
    queryFn: api.listRemoteBackups,
    refetchInterval: 8000,
  });
  const list = dumps.data?.backups ?? [];

  return (
    <div className="rise">
      <PageHeader
        title="Remote backup"
        subtitle="Capture a logical backup of an external Postgres you don't manage, then restore it into a freshly-provisioned instance or cluster."
      />

      <div className="grid gap-6 lg:grid-cols-[minmax(0,420px)_1fr]">
        <CaptureCard />

        <Card>
          <CardHeader>
            <CardTitle>Captured dumps</CardTitle>
            {list.length > 0 && <Badge tone="neutral">{list.length}</Badge>}
          </CardHeader>
          <CardBody className="p-0">
            {dumps.isLoading ? (
              <div className="p-5">
                <SkeletonRows rows={3} />
              </div>
            ) : list.length === 0 ? (
              <EmptyState
                icon={<DownloadCloud className="h-5 w-5" />}
                title="No remote dumps captured yet"
                description="Enter the credentials for an external Postgres on the left. PgFleet connects, captures a logical backup, and lists it here ready to restore."
              />
            ) : (
              <ul className="divide-y divide-line">
                {list.map((d) => (
                  <DumpRow key={d.id} dump={d} />
                ))}
              </ul>
            )}
          </CardBody>
        </Card>
      </div>
    </div>
  );
}

function CaptureCard() {
  const qc = useQueryClient();
  const toast = useToast();

  const [host, setHost] = useState("");
  const [port, setPort] = useState("5432");
  const [user, setUser] = useState("");
  const [password, setPassword] = useState("");
  const [dbname, setDbname] = useState("");
  const [sslmode, setSslmode] = useState<SSLMode>("require");
  const [error, setError] = useState<string | null>(null);

  const capture = useMutation({
    mutationFn: () =>
      api.captureRemoteBackup({
        host: host.trim(),
        port: Number(port) || 5432,
        user: user.trim(),
        password,
        dbname: dbname.trim(),
        sslmode,
      }),
    onSuccess: (res) => {
      // Clear only the password from memory immediately; keep host/db so the
      // operator can capture another database without retyping.
      setPassword("");
      const captured = res.backup;
      toast.push(`Captured ${captured.source_db} from ${captured.source_host}`, "healthy");
      qc.invalidateQueries({ queryKey: ["remote-backups"] });
    },
    onError: (err) => setError(err instanceof Error ? err.message : "Capture failed"),
  });

  const valid =
    host.trim().length > 0 && user.trim().length > 0 && password.length > 0 && dbname.trim().length > 0;

  function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError(null);
    if (!valid) {
      setError("Host, user, password, and database are required.");
      return;
    }
    capture.mutate();
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Capture a remote database</CardTitle>
      </CardHeader>
      <CardBody>
        <form onSubmit={onSubmit} className="space-y-5" noValidate>
          <div className="grid grid-cols-[1fr_auto] gap-3">
            <Field label="Host" hint="Hostname or IP of the external Postgres.">
              <Input
                value={host}
                onChange={(e) => setHost(e.target.value)}
                placeholder="db.example.com"
                autoComplete="off"
                required
                aria-required="true"
              />
            </Field>
            <Field label="Port">
              <Input
                type="number"
                value={port}
                onChange={(e) => setPort(e.target.value)}
                placeholder="5432"
                className="w-24"
                min={1}
                max={65535}
              />
            </Field>
          </div>

          <Field label="User">
            <Input
              value={user}
              onChange={(e) => setUser(e.target.value)}
              placeholder="postgres"
              autoComplete="username"
              required
              aria-required="true"
            />
          </Field>

          <Field label="Password" hint="Used once to connect. Never stored or echoed back.">
            <PasswordInput
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              autoComplete="new-password"
              required
              aria-required="true"
            />
          </Field>

          <Field label="Database">
            <Input
              value={dbname}
              onChange={(e) => setDbname(e.target.value)}
              placeholder="appdb"
              autoComplete="off"
              required
              aria-required="true"
            />
          </Field>

          <Field label="SSL mode" hint="verify-full validates the server certificate.">
            <Select value={sslmode} onChange={(e) => setSslmode(e.target.value as SSLMode)}>
              <option value="disable">disable</option>
              <option value="require">require</option>
              <option value="verify-full">verify-full</option>
            </Select>
          </Field>

          {error && (
            <div
              role="alert"
              aria-live="assertive"
              className="rounded-md border border-danger/30 bg-danger/10 px-3 py-2 text-xs text-danger"
            >
              {error}
            </div>
          )}

          <Button type="submit" loading={capture.isPending} disabled={!valid} className="w-full">
            <DownloadCloud className="h-4 w-4" />
            {capture.isPending ? "Capturing…" : "Capture backup"}
          </Button>
        </form>
      </CardBody>
    </Card>
  );
}

function DumpRow({ dump }: { dump: RemoteDump }) {
  return (
    <li className="flex flex-wrap items-center gap-x-4 gap-y-2 px-5 py-4">
      <div className="flex min-w-0 flex-1 items-center gap-3">
        <Database className="h-4 w-4 shrink-0 text-fg-faint" />
        <div className="min-w-0">
          <div className="truncate font-display text-sm text-fg">{dump.source_db}</div>
          <div className="truncate font-mono text-[11px] text-fg-faint">{dump.source_host}</div>
        </div>
      </div>
      <Badge tone="azure">pg{dump.server_major}</Badge>
      <span className="font-mono text-xs text-fg-muted tnum">{formatBytes(dump.size_bytes)}</span>
      <span className="font-mono text-[11px] text-fg-faint tnum">{formatTimestamp(dump.created_at)}</span>
      <RestoreDialog dump={dump} />
    </li>
  );
}

function RestoreDialog({ dump }: { dump: RemoteDump }) {
  const router = useRouter();
  const toast = useToast();

  const [open, setOpen] = useState(false);
  const [target, setTarget] = useState<RemoteRestoreTarget>("instance");
  const [name, setName] = useState("");
  const [password, setPassword] = useState("");
  const [pgVersion, setPgVersion] = useState(String(dump.server_major || "16"));
  const [repoType, setRepoType] = useState<"s3" | "local">("s3");
  const [replicas, setReplicas] = useState("1");
  const [confirming, setConfirming] = useState(false);
  const [error, setError] = useState<string | null>(null);

  const nameValid = /^[a-z][a-z0-9-]{1,38}$/.test(name);
  const replicasNum = Number(replicas) || 0;
  const valid =
    nameValid && password.length >= 8 && (target === "instance" || replicasNum >= 1);

  function resetState() {
    setConfirming(false);
    setError(null);
    setPassword("");
  }

  const restore = useMutation({
    mutationFn: () =>
      api.restoreRemoteBackup(dump.id, {
        target,
        name: name.trim(),
        password,
        repo_type: repoType,
        pg_version: pgVersion,
        replicas: target === "cluster" ? replicasNum : undefined,
      }),
    onSuccess: () => {
      const dest = name.trim();
      // The provisioned target id isn't readable from a 202 body via the shared
      // client, so route to the list view where the new resource will appear.
      const path = target === "cluster" ? "/clusters" : "/instances";
      setPassword("");
      setOpen(false);
      toast.push(`Restoring into ${dest}…`, "azure");
      router.push(path);
    },
    onError: (err) => {
      setConfirming(false);
      setError(err instanceof Error ? err.message : "Restore failed");
    },
  });

  function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError(null);
    if (!valid) {
      setError("Provide a valid name and a superuser password of at least 8 characters.");
      return;
    }
    // Provisioning creates real infrastructure — require an explicit confirm.
    if (!confirming) {
      setConfirming(true);
      return;
    }
    restore.mutate();
  }

  return (
    <Dialog.Root
      open={open}
      onOpenChange={(o) => {
        setOpen(o);
        if (!o) resetState();
      }}
    >
      <Dialog.Trigger asChild>
        <Button variant="outline" size="sm">
          <RotateCcw className="h-4 w-4" />
          Restore
        </Button>
      </Dialog.Trigger>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-50 bg-[#0f1f33]/40 backdrop-blur-sm data-[state=open]:animate-in" />
        <Dialog.Content className="fixed left-1/2 top-1/2 z-50 w-full max-w-lg -translate-x-1/2 -translate-y-1/2 rounded-xl border border-line bg-ink-900 p-6 shadow-[0_40px_80px_-32px_rgba(15,31,51,0.25)]">
          <div className="mb-1 flex items-center justify-between">
            <Dialog.Title className="font-display text-base font-semibold">Restore into a new target</Dialog.Title>
            <Dialog.Close
              aria-label="Close"
              className="cursor-pointer rounded text-fg-faint transition-colors hover:text-fg focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-azure/50"
            >
              <X className="h-4 w-4" />
            </Dialog.Close>
          </div>
          <Dialog.Description className="mb-5 text-sm text-fg-muted">
            PgFleet provisions a fresh target, then loads{" "}
            <span className="font-mono text-fg">{dump.source_db}</span> into it.
          </Dialog.Description>

          <form onSubmit={onSubmit} className="space-y-5" noValidate>
            <div>
              <span className="mb-2 block font-mono text-[11px] uppercase tracking-wider text-fg-muted">Target</span>
              <div className="grid grid-cols-2 gap-3">
                <TargetOption
                  active={target === "instance"}
                  onClick={() => setTarget("instance")}
                  icon={<Database className="h-4 w-4" />}
                  title="Single instance"
                  desc="One standalone Postgres"
                />
                <TargetOption
                  active={target === "cluster"}
                  onClick={() => setTarget("cluster")}
                  icon={<Server className="h-4 w-4" />}
                  title="Cluster"
                  desc="Primary + replicas"
                />
              </div>
            </div>

            <Field label="Target name" hint="Lowercase, starts with a letter, 2–39 chars.">
              <Input
                value={name}
                onChange={(e) => setName(e.target.value.toLowerCase())}
                placeholder="migrated-db"
                autoFocus
                required
                aria-required="true"
              />
              {name && !nameValid && (
                <span className="text-xs text-danger">Must match [a-z][a-z0-9-]{"{1,38}"}</span>
              )}
            </Field>

            <div className="grid grid-cols-2 gap-4">
              <Field label="Postgres version">
                <Select value={pgVersion} onChange={(e) => setPgVersion(e.target.value)}>
                  {["17", "16", "15", "14", "13"].map((v) => (
                    <option key={v} value={v}>
                      {v}
                    </option>
                  ))}
                </Select>
              </Field>
              <Field label="Backup repository">
                <Select value={repoType} onChange={(e) => setRepoType(e.target.value as "s3" | "local")}>
                  <option value="s3">Object store (S3)</option>
                  <option value="local">Local volume</option>
                </Select>
              </Field>
            </div>

            <div className="grid grid-cols-2 gap-4">
              <Field label="Superuser password" hint="Min 8 characters.">
                <PasswordInput
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  autoComplete="new-password"
                  required
                  aria-required="true"
                />
              </Field>
              {target === "cluster" && (
                <Field label="Replicas" hint="At least 1.">
                  <Input
                    type="number"
                    value={replicas}
                    onChange={(e) => setReplicas(e.target.value)}
                    min={1}
                    max={5}
                  />
                </Field>
              )}
            </div>

            {error && (
              <div
                role="alert"
                aria-live="assertive"
                className="rounded-md border border-danger/30 bg-danger/10 px-3 py-2 text-xs text-danger"
              >
                {error}
              </div>
            )}

            {confirming && !error && (
              <div
                role="alert"
                className="rounded-md border border-signal/30 bg-signal/10 px-3 py-2 text-xs text-signal"
              >
                This provisions real resources. Click “Yes, restore” to proceed.
              </div>
            )}

            <div className="flex justify-end gap-3 border-t border-line pt-5">
              <Dialog.Close asChild>
                <Button type="button" variant="ghost">
                  Cancel
                </Button>
              </Dialog.Close>
              <Button type="submit" variant={confirming ? "danger" : "primary"} loading={restore.isPending} disabled={!valid}>
                {confirming ? "Yes, restore" : "Restore"}
              </Button>
            </div>
          </form>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

function TargetOption({
  active,
  onClick,
  icon,
  title,
  desc,
}: {
  active: boolean;
  onClick: () => void;
  icon: React.ReactNode;
  title: string;
  desc: string;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      className={`flex cursor-pointer items-start gap-3 rounded-lg border px-4 py-3 text-left transition-all duration-150 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-azure/50 ${
        active ? "border-azure/60 bg-azure/10" : "border-line bg-ink-900 hover:border-line-bright"
      }`}
    >
      <span className={active ? "text-azure" : "text-fg-faint"}>{icon}</span>
      <span>
        <span className="block font-display text-sm text-fg">{title}</span>
        <span className="block font-mono text-[11px] text-fg-faint">{desc}</span>
      </span>
    </button>
  );
}

function formatTimestamp(iso: string): string {
  const d = new Date(iso);
  if (Number.isNaN(d.getTime())) return "—";
  return d.toLocaleString(undefined, {
    year: "numeric",
    month: "short",
    day: "2-digit",
    hour: "2-digit",
    minute: "2-digit",
  });
}
