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
  Modal,
  PasswordInput,
  Select,
  SkeletonRows,
  useToast,
} from "@/components/ui";
import { api, type RemoteDump, type RemoteRestoreTarget } from "@/lib/api";
import { formatBytes } from "@/lib/utils";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Database, DownloadCloud, RotateCcw, Server } from "lucide-react";
import { useRouter } from "next/navigation";
import { useState } from "react";

type SSLMode = "disable" | "require" | "verify-full";

export default function RemoteBackupPage() {
  const [captureOpen, setCaptureOpen] = useState(false);

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
        action={
          <Button onClick={() => setCaptureOpen(true)}>
            <DownloadCloud className="h-4 w-4" />
            Capture database
          </Button>
        }
      />

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
              description="Capture an external Postgres by its credentials. PgFleet connects, takes a logical backup, and lists it here ready to restore."
              action={
                <Button size="sm" onClick={() => setCaptureOpen(true)}>
                  <DownloadCloud className="h-4 w-4" />
                  Capture a database
                </Button>
              }
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

      <CaptureModal open={captureOpen} onOpenChange={setCaptureOpen} />
    </div>
  );
}

function CaptureModal({
  open,
  onOpenChange,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
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
      // Drop the password from memory immediately; it is write-only.
      setPassword("");
      const captured = res.backup;
      toast.push(`Captured ${captured.source_db} from ${captured.source_host}`, "healthy");
      qc.invalidateQueries({ queryKey: ["remote-backups"] });
      onOpenChange(false);
    },
    onError: (err) => setError(err instanceof Error ? err.message : "Capture failed"),
  });

  const valid =
    host.trim().length > 0 && user.trim().length > 0 && password.length > 0 && dbname.trim().length > 0;

  function submit() {
    setError(null);
    if (!valid) {
      setError("Host, user, password, and database are required.");
      return;
    }
    capture.mutate();
  }

  return (
    <Modal
      open={open}
      onOpenChange={(o) => {
        onOpenChange(o);
        if (!o) {
          // Always clear the password when the modal closes.
          setPassword("");
          setError(null);
        }
      }}
      title="Capture a remote database"
      description="Provide read access to an external Postgres. PgFleet connects once, captures a logical backup, and never stores the password."
      footer={
        <>
          <Button variant="ghost" size="sm" onClick={() => onOpenChange(false)} disabled={capture.isPending}>
            Cancel
          </Button>
          <Button size="sm" loading={capture.isPending} disabled={!valid} onClick={submit}>
            <DownloadCloud className="h-4 w-4" />
            Capture backup
          </Button>
        </>
      }
    >
      <form
        onSubmit={(e) => {
          e.preventDefault();
          submit();
        }}
        className="space-y-4"
        noValidate
      >
        <div className="grid grid-cols-[1fr_auto] gap-3">
          <Field label="Host" hint="Hostname or IP of the external Postgres.">
            <Input
              value={host}
              onChange={(e) => setHost(e.target.value)}
              placeholder="db.example.com"
              autoComplete="off"
              autoFocus
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
              inputMode="numeric"
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

        {/* Hidden submit so Enter submits within the modal. */}
        <button type="submit" className="sr-only" aria-hidden tabIndex={-1} />
      </form>
    </Modal>
  );
}

function DumpRow({ dump }: { dump: RemoteDump }) {
  const [restoreOpen, setRestoreOpen] = useState(false);
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
      <Button variant="outline" size="sm" onClick={() => setRestoreOpen(true)}>
        <RotateCcw className="h-4 w-4" />
        Restore
      </Button>
      <RestoreModal dump={dump} open={restoreOpen} onOpenChange={setRestoreOpen} />
    </li>
  );
}

function RestoreModal({
  dump,
  open,
  onOpenChange,
}: {
  dump: RemoteDump;
  open: boolean;
  onOpenChange: (open: boolean) => void;
}) {
  const router = useRouter();
  const toast = useToast();

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
  const valid = nameValid && password.length >= 8 && (target === "instance" || replicasNum >= 1);

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
      onOpenChange(false);
      toast.push(`Restoring into ${dest}…`, "azure");
      router.push(path);
    },
    onError: (err) => {
      setConfirming(false);
      setError(err instanceof Error ? err.message : "Restore failed");
    },
  });

  function submit() {
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
    <Modal
      open={open}
      onOpenChange={(o) => {
        onOpenChange(o);
        if (!o) resetState();
      }}
      title="Restore into a new target"
      description={`PgFleet provisions a fresh target, then loads ${dump.source_db} into it.`}
      footer={
        <>
          <Button variant="ghost" size="sm" onClick={() => onOpenChange(false)} disabled={restore.isPending}>
            Cancel
          </Button>
          <Button
            size="sm"
            variant={confirming ? "danger" : "primary"}
            loading={restore.isPending}
            disabled={!valid}
            onClick={submit}
          >
            {confirming ? (
              "Yes, restore"
            ) : (
              <>
                <RotateCcw className="h-4 w-4" />
                Restore
              </>
            )}
          </Button>
        </>
      }
    >
      <form
        onSubmit={(e) => {
          e.preventDefault();
          submit();
        }}
        className="space-y-5"
        noValidate
      >
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

        <div className={`grid gap-4 ${target === "cluster" ? "grid-cols-2" : "grid-cols-1"}`}>
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
                inputMode="numeric"
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
          <div role="alert" className="rounded-md border border-signal/30 bg-signal/10 px-3 py-2 text-xs text-signal">
            This provisions real resources. Click “Yes, restore” to proceed.
          </div>
        )}

        {/* Hidden submit so Enter advances/confirms within the modal. */}
        <button type="submit" className="sr-only" aria-hidden tabIndex={-1} />
      </form>
    </Modal>
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
