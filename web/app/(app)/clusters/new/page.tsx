"use client";

import { AdvancedTuning, ParamRow, rowsToRecord } from "@/components/advanced-tuning";
import { PageHeader } from "@/components/shell";
import { Button, Card, CardBody, Field, Input, Select } from "@/components/ui";
import { api } from "@/lib/api";
import { useQueryClient } from "@tanstack/react-query";
import { Minus, Network, Plus } from "lucide-react";
import { useRouter } from "next/navigation";
import { useState } from "react";

export default function NewClusterPage() {
  const router = useRouter();
  const qc = useQueryClient();
  const [name, setName] = useState("");
  const [replicas, setReplicas] = useState(1);
  const [repoType, setRepoType] = useState<"s3" | "local">("local");
  const [pgVersion, setPgVersion] = useState("16");
  const [password, setPassword] = useState("");
  const [paramRows, setParamRows] = useState<ParamRow[]>([]);
  const [exts, setExts] = useState<string[]>([]);
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  const nameValid = /^[a-z][a-z0-9-]{1,36}$/.test(name); // leave room for -rN suffix
  const valid = nameValid && password.length >= 8;

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
      <PageHeader title="New cluster" subtitle="Provision a primary, replicas, and a query router." />

      <Card>
        <CardBody>
          <form onSubmit={onSubmit} className="space-y-6">
            <Field label="Name" hint="Members are named <cluster>-p, <cluster>-r1, …">
              <Input value={name} onChange={(e) => setName(e.target.value.toLowerCase())} placeholder="orders" autoFocus />
              {name && !nameValid && <span className="text-xs text-danger">Must match [a-z][a-z0-9-]{"{1,36}"}</span>}
            </Field>

            <div>
              <span className="mb-2 block font-mono text-[11px] uppercase tracking-wider text-fg-muted">Read replicas</span>
              <div className="flex items-center gap-3">
                <button
                  type="button"
                  onClick={() => setReplicas((r) => Math.max(0, r - 1))}
                  className="grid h-9 w-9 place-items-center rounded-md border border-line text-fg-muted hover:border-line-bright"
                >
                  <Minus className="h-4 w-4" />
                </button>
                <div className="flex items-center gap-2 font-display text-2xl tnum text-fg">
                  <Network className="h-5 w-5 text-azure" />
                  {replicas}
                </div>
                <button
                  type="button"
                  onClick={() => setReplicas((r) => Math.min(9, r + 1))}
                  className="grid h-9 w-9 place-items-center rounded-md border border-line text-fg-muted hover:border-line-bright"
                >
                  <Plus className="h-4 w-4" />
                </button>
                <span className="ml-2 text-xs text-fg-faint">streaming standbys (0–9)</span>
              </div>
            </div>

            <div className="grid grid-cols-3 gap-4">
              <Field label="Backup repository">
                <Select value={repoType} onChange={(e) => setRepoType(e.target.value as "s3" | "local")}>
                  <option value="local">Local volume</option>
                  <option value="s3">Object store (S3/MinIO)</option>
                </Select>
              </Field>
              <Field label="Postgres version">
                <Select value={pgVersion} onChange={(e) => setPgVersion(e.target.value)}>
                  {["17", "16", "15", "14", "13"].map((v) => (
                    <option key={v} value={v}>
                      {v}
                    </option>
                  ))}
                </Select>
              </Field>
              <Field label="Superuser password" hint="Min 8 characters.">
                <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} />
              </Field>
            </div>

            <AdvancedTuning rows={paramRows} setRows={setParamRows} exts={exts} setExts={setExts} />

            {error && <div className="rounded-md border border-danger/30 bg-danger/10 px-3 py-2 text-xs text-danger">{error}</div>}

            <div className="flex items-center justify-end gap-3 border-t border-line pt-5">
              <Button type="button" variant="ghost" onClick={() => router.back()}>
                Cancel
              </Button>
              <Button type="submit" disabled={!valid || submitting}>
                {submitting ? "Provisioning…" : "Provision cluster"}
              </Button>
            </div>
          </form>
        </CardBody>
      </Card>
    </div>
  );
}
