"use client";

import { api, type Backup } from "@/lib/api";
import { useQueryClient } from "@tanstack/react-query";
import * as Dialog from "@radix-ui/react-dialog";
import { Clock, History, Layers, RotateCcw, Target, X } from "lucide-react";
import { useEffect, useState } from "react";
import { Button, Field, Input, Select } from "./ui";

type Mode = "latest" | "time" | "set" | "advanced";
type AdvType = "name" | "lsn" | "xid";

export function RestoreDialog({ instanceId, backups }: { instanceId: string; backups: Backup[] }) {
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const [mode, setMode] = useState<Mode>("latest");
  const [target, setTarget] = useState("");
  const [set, setSet] = useState(backups[0]?.label ?? "");
  const [advType, setAdvType] = useState<AdvType>("name");
  const [advTarget, setAdvTarget] = useState("");
  const [repo, setRepo] = useState<1 | 2>(1);
  const [delta, setDelta] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  // backups arrive async, after this dialog mounts; keep the "Backup" select's
  // value in sync so submitting "set" mode never posts an empty label.
  useEffect(() => {
    if (!set && backups.length > 0) setSet(backups[0].label);
  }, [backups, set]);

  async function onRestore() {
    setError(null);
    setSubmitting(true);
    try {
      const base =
        mode === "latest"
          ? {}
          : mode === "time"
            ? { type: "time", target: toPgTimestamp(target) }
            : mode === "advanced"
              ? { type: advType, target: advTarget.trim() }
              : { set };
      await api.restore(instanceId, { ...base, delta, ...(repo === 2 ? { repo: 2 } : {}) });
      qc.invalidateQueries({ queryKey: ["instance", instanceId] });
      setOpen(false);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Restore failed");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <Dialog.Root open={open} onOpenChange={setOpen}>
      <Dialog.Trigger asChild>
        <Button variant="outline" size="sm">
          <RotateCcw className="h-4 w-4" />
          Restore / PITR
        </Button>
      </Dialog.Trigger>
      <Dialog.Portal>
        <Dialog.Overlay className="fixed inset-0 z-50 bg-[#0f1f33]/40 backdrop-blur-sm data-[state=open]:animate-in" />
        <Dialog.Content className="fixed left-1/2 top-1/2 z-50 w-full max-w-lg -translate-x-1/2 -translate-y-1/2 rounded-xl border border-line bg-ink-900 p-6 shadow-[0_40px_80px_-32px_rgba(15,31,51,0.25)]">
          <div className="mb-1 flex items-center justify-between">
            <Dialog.Title className="font-display text-base font-semibold">Restore instance</Dialog.Title>
            <Dialog.Close className="text-fg-faint hover:text-fg">
              <X className="h-4 w-4" />
            </Dialog.Close>
          </div>
          <Dialog.Description className="mb-5 text-sm text-fg-muted">
            Restoring stops the instance, replays from the repository, then promotes.
          </Dialog.Description>

          {/* WAL timeline */}
          <WalTimeline backups={backups} />

          <div className="mt-5 grid grid-cols-2 gap-2 sm:grid-cols-4">
            <ModeTab active={mode === "latest"} onClick={() => setMode("latest")} icon={<History className="h-3.5 w-3.5" />} label="Latest" />
            <ModeTab active={mode === "time"} onClick={() => setMode("time")} icon={<Clock className="h-3.5 w-3.5" />} label="Point in time" />
            <ModeTab active={mode === "set"} onClick={() => setMode("set")} icon={<Layers className="h-3.5 w-3.5" />} label="Backup" />
            <ModeTab active={mode === "advanced"} onClick={() => setMode("advanced")} icon={<Target className="h-3.5 w-3.5" />} label="Target" />
          </div>

          <div className="mt-4 min-h-[64px]">
            {mode === "latest" && <p className="text-sm text-fg-muted">Recover to the most recent state in the archive.</p>}
            {mode === "time" && (
              <Field label="Recovery target" hint="The instance is recovered to exactly this moment.">
                <Input type="datetime-local" value={target} onChange={(e) => setTarget(e.target.value)} step="1" />
              </Field>
            )}
            {mode === "set" && (
              <Field label="Backup">
                <select
                  value={set}
                  onChange={(e) => setSet(e.target.value)}
                  className="h-10 w-full rounded-md border border-line bg-ink-900 px-3 text-sm text-fg focus:border-azure/60 focus:outline-none"
                >
                  {backups.map((b) => (
                    <option key={b.label} value={b.label}>
                      {b.label} ({b.type})
                    </option>
                  ))}
                </select>
              </Field>
            )}
            {mode === "advanced" && (
              <div className="grid grid-cols-[8rem_1fr] gap-3">
                <Field label="Target type" hint="Recover to a restore point, an LSN, or a transaction id.">
                  <Select value={advType} onChange={(e) => setAdvType(e.target.value as AdvType)}>
                    <option value="name">Named restore point</option>
                    <option value="lsn">LSN</option>
                    <option value="xid">Transaction id</option>
                  </Select>
                </Field>
                <Field
                  label="Value"
                  hint={advType === "lsn" ? "e.g. 0/16B6230" : advType === "xid" ? "e.g. 728193" : "a pg_create_restore_point() name"}
                >
                  <Input value={advTarget} onChange={(e) => setAdvTarget(e.target.value)} placeholder={advType === "lsn" ? "0/16B6230" : ""} spellCheck={false} />
                </Field>
              </div>
            )}
          </div>

          <Field label="Restore from repository" hint="Use the second repository to recover when the primary repo is unavailable.">
            <Select value={String(repo)} onChange={(e) => setRepo(Number(e.target.value) === 2 ? 2 : 1)}>
              <option value="1">repo1 — primary repository</option>
              <option value="2">repo2 — failover copy</option>
            </Select>
          </Field>

          <label className="mt-4 flex cursor-pointer items-start gap-2.5 rounded-md border border-line bg-ink-850 px-3 py-2.5">
            <input
              type="checkbox"
              checked={delta}
              onChange={(e) => setDelta(e.target.checked)}
              className="mt-0.5 h-4 w-4 cursor-pointer accent-azure"
            />
            <span className="text-xs text-fg-muted">
              <span className="font-medium text-fg">Delta restore</span> — only restore files that differ from what&apos;s
              already on disk. Much faster for large databases; leave off for a clean from-scratch restore.
            </span>
          </label>

          {error && (
            <div role="alert" className="mt-3 rounded-md border border-danger/30 bg-danger/10 px-3 py-2 text-xs text-danger">
              {error}
            </div>
          )}

          <div className="mt-6 flex justify-end gap-3 border-t border-line pt-5">
            <Dialog.Close asChild>
              <Button variant="ghost">Cancel</Button>
            </Dialog.Close>
            <Button
              onClick={onRestore}
              disabled={submitting || (mode === "time" && !target) || (mode === "advanced" && !advTarget.trim())}
            >
              {submitting ? "Restoring…" : "Begin restore"}
            </Button>
          </div>
        </Dialog.Content>
      </Dialog.Portal>
    </Dialog.Root>
  );
}

function WalTimeline({ backups }: { backups: Backup[] }) {
  return (
    <div className="rounded-lg border border-line bg-ink-900 p-4">
      <div className="mb-3 flex items-center justify-between font-mono text-[10px] uppercase tracking-wider text-fg-faint">
        <span>oldest</span>
        <span>wal archive</span>
        <span>now</span>
      </div>
      <div className="relative h-1.5 rounded-full bg-gradient-to-r from-ink-600 via-azure/40 to-azure/70">
        {backups.length === 0 && (
          <span className="absolute inset-0 grid place-items-center text-[10px] text-fg-faint">no backups yet</span>
        )}
        {backups.map((b, idx) => {
          const pct = backups.length === 1 ? 100 : (idx / (backups.length - 1)) * 100;
          return (
            <span
              key={b.label}
              title={`${b.label} (${b.type})`}
              className="absolute top-1/2 h-3 w-3 -translate-x-1/2 -translate-y-1/2 rounded-full border-2 border-line-bright bg-signal shadow-[0_0_8px_var(--color-signal)]"
              style={{ left: `${pct}%` }}
            />
          );
        })}
        <span className="absolute right-0 top-1/2 h-3 w-3 -translate-y-1/2 translate-x-1/2 rounded-full border-2 border-line-bright bg-azure shadow-[0_0_10px_var(--color-azure)]" />
      </div>
    </div>
  );
}

function ModeTab({ active, onClick, icon, label }: { active: boolean; onClick: () => void; icon: React.ReactNode; label: string }) {
  return (
    <button
      onClick={onClick}
      className={`flex items-center justify-center gap-1.5 rounded-md border px-2 py-2 font-display text-xs transition-all ${
        active ? "border-azure/60 bg-azure/10 text-azure" : "border-line text-fg-muted hover:border-line-bright"
      }`}
    >
      {icon}
      {label}
    </button>
  );
}

// toPgTimestamp converts a datetime-local value to a pgBackRest target string.
function toPgTimestamp(local: string): string {
  if (!local) return "";
  const d = new Date(local);
  const pad = (n: number) => String(n).padStart(2, "0");
  return `${d.getUTCFullYear()}-${pad(d.getUTCMonth() + 1)}-${pad(d.getUTCDate())} ${pad(d.getUTCHours())}:${pad(
    d.getUTCMinutes()
  )}:${pad(d.getUTCSeconds())}+00`;
}
