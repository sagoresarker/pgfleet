"use client";

import { AdvancedTuning, ParamRow, rowsToRecord } from "@/components/advanced-tuning";
import { PageHeader } from "@/components/shell";
import { Button, Card, CardBody, Field, Input, PasswordInput, Select } from "@/components/ui";
import { api } from "@/lib/api";
import { useQueryClient } from "@tanstack/react-query";
import { ChevronLeft, Minus, Network, Plus } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";

export default function NewClusterPage() {
  const router = useRouter();
  const qc = useQueryClient();
  const [name, setName] = useState("");
  const [replicas, setReplicas] = useState(1);
  const [repoType, setRepoType] = useState<"s3" | "local">("local");
  const [pgVersion, setPgVersion] = useState("16");
  const [poolMode, setPoolMode] = useState<"transaction" | "session">("transaction");
  const [password, setPassword] = useState("");
  const [paramRows, setParamRows] = useState<ParamRow[]>([]);
  const [exts, setExts] = useState<string[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  const nameValid = /^[a-z][a-z0-9-]{1,36}$/.test(name); // leave room for -rN suffix
  const passwordValid = password.length >= 8;
  const valid = nameValid && passwordValid;

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError(null);
    setSubmitting(true);
    try {
      const parameters = rowsToRecord(paramRows);
      await api.createCluster({
        name,
        replicas,
        password,
        repo_type: repoType,
        pg_version: pgVersion,
        pool_mode: poolMode,
        parameters: Object.keys(parameters).length ? parameters : undefined,
        extensions: exts.length ? exts : undefined,
      });
      qc.invalidateQueries({ queryKey: ["clusters"] });
      router.push("/clusters");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to create cluster");
      setSubmitting(false);
    }
  }

  return (
    <div className="mx-auto max-w-xl rise">
      <div className="mb-4">
        <Link
          href="/clusters"
          className="inline-flex items-center gap-1 font-mono text-[11px] uppercase tracking-wider text-fg-faint transition-colors hover:text-azure"
        >
          <ChevronLeft className="h-3.5 w-3.5" /> clusters
        </Link>
      </div>
      <PageHeader title="New cluster" subtitle="Provision a primary, replicas, and a query router." />

      <Card>
        <CardBody>
          <form onSubmit={onSubmit} className="space-y-6" noValidate>
            <fieldset>
              <legend className="mb-2 font-mono text-[10px] uppercase tracking-[0.2em] text-fg-faint">Identity</legend>
              <Field label="Name *" hint="Members are named <cluster>-p, <cluster>-r1, …">
                <Input
                  value={name}
                  onChange={(e) => setName(e.target.value.toLowerCase())}
                  placeholder="orders"
                  autoFocus
                  required
                  autoComplete="off"
                  spellCheck={false}
                  aria-invalid={!!name && !nameValid}
                  aria-describedby={name && !nameValid ? "name-error" : undefined}
                />
                {name && !nameValid && (
                  <span id="name-error" role="alert" aria-live="polite" className="block text-xs text-danger">
                    Must match [a-z][a-z0-9-]{"{1,36}"} (lowercase, starts with a letter).
                  </span>
                )}
              </Field>
            </fieldset>

            <fieldset>
              <legend className="mb-2 block font-mono text-[11px] uppercase tracking-wider text-fg-muted">
                Read replicas
              </legend>
              <div className="flex items-center gap-3">
                <button
                  type="button"
                  onClick={() => setReplicas((r) => Math.max(0, r - 1))}
                  disabled={replicas <= 0}
                  aria-label="Remove a replica"
                  className="grid h-11 w-11 cursor-pointer place-items-center rounded-md border border-line text-fg-muted transition-colors hover:border-line-bright focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-azure/50 disabled:cursor-not-allowed disabled:opacity-40"
                >
                  <Minus className="h-4 w-4" />
                </button>
                <div
                  className="flex items-center gap-2 font-display text-2xl tnum text-fg"
                  role="status"
                  aria-live="polite"
                  aria-label={`${replicas} replicas`}
                >
                  <Network className="h-5 w-5 text-azure" />
                  {replicas}
                </div>
                <button
                  type="button"
                  onClick={() => setReplicas((r) => Math.min(9, r + 1))}
                  disabled={replicas >= 9}
                  aria-label="Add a replica"
                  className="grid h-11 w-11 cursor-pointer place-items-center rounded-md border border-line text-fg-muted transition-colors hover:border-line-bright focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-azure/50 disabled:cursor-not-allowed disabled:opacity-40"
                >
                  <Plus className="h-4 w-4" />
                </button>
                <span className="ml-2 text-xs text-fg-faint">streaming standbys (0–9)</span>
              </div>
            </fieldset>

            <fieldset className="grid grid-cols-3 gap-4">
              <legend className="sr-only">Engine</legend>
              <Field label="Backup repository *">
                <Select value={repoType} onChange={(e) => setRepoType(e.target.value as "s3" | "local")}>
                  <option value="local">Local volume</option>
                  <option value="s3">Object store (S3/MinIO)</option>
                </Select>
              </Field>
              <Field label="Postgres version *">
                <Select value={pgVersion} onChange={(e) => setPgVersion(e.target.value)}>
                  {["17", "16", "15", "14", "13"].map((v) => (
                    <option key={v} value={v}>
                      {v}
                    </option>
                  ))}
                </Select>
              </Field>
              <Field
                label="Router pool mode"
                hint="Transaction pooling shares server connections per transaction (best for many short-lived clients); session keeps one server connection per client."
              >
                <Select value={poolMode} onChange={(e) => setPoolMode(e.target.value as "transaction" | "session")}>
                  <option value="transaction">Transaction (recommended)</option>
                  <option value="session">Session</option>
                </Select>
              </Field>
              <Field label="Superuser password *" hint="Min 8 characters.">
                <PasswordInput
                  value={password}
                  onChange={(e) => setPassword(e.target.value)}
                  autoComplete="new-password"
                  required
                  aria-invalid={!!password && !passwordValid}
                  aria-describedby={password && !passwordValid ? "pw-error" : undefined}
                />
                {password && !passwordValid && (
                  <span id="pw-error" role="alert" aria-live="polite" className="block text-xs text-danger">
                    Password must be at least 8 characters.
                  </span>
                )}
              </Field>
            </fieldset>

            <AdvancedTuning rows={paramRows} setRows={setParamRows} exts={exts} setExts={setExts} />

            {error && (
              <div role="alert" aria-live="assertive" className="rounded-md border border-danger/30 bg-danger/10 px-3 py-2 text-xs text-danger">
                {error}
              </div>
            )}

            <div className="flex items-center justify-end gap-3 border-t border-line pt-5">
              <Button type="button" variant="ghost" onClick={() => router.back()}>
                Cancel
              </Button>
              <Button type="submit" loading={submitting} disabled={!valid}>
                {submitting ? "Provisioning…" : "Provision cluster"}
              </Button>
            </div>
          </form>
        </CardBody>
      </Card>
    </div>
  );
}
