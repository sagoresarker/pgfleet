"use client";

import { PageHeader } from "@/components/shell";
import { Button, Card, CardBody, Field, Input, Select } from "@/components/ui";
import { api } from "@/lib/api";
import { useQueryClient } from "@tanstack/react-query";
import { Cloud, HardDrive } from "lucide-react";
import { useRouter } from "next/navigation";
import { useState } from "react";

export default function NewInstancePage() {
  const router = useRouter();
  const qc = useQueryClient();
  const [name, setName] = useState("");
  const [repoType, setRepoType] = useState<"s3" | "local">("s3");
  const [pgVersion, setPgVersion] = useState("16");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  const nameValid = /^[a-z][a-z0-9-]{1,38}$/.test(name);
  const valid = nameValid && password.length >= 8;

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError(null);
    setSubmitting(true);
    try {
      await api.createInstance({ name, repo_type: repoType, password, pg_version: pgVersion });
      qc.invalidateQueries({ queryKey: ["instances"] });
      router.push("/instances");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to create instance");
      setSubmitting(false);
    }
  }

  return (
    <div className="mx-auto max-w-xl rise">
      <PageHeader title="New instance" subtitle="Provision a managed Postgres instance with backups." />

      <Card>
        <CardBody className="space-y-6">
          <form onSubmit={onSubmit} className="space-y-6">
            <Field label="Name" hint="Lowercase, starts with a letter, 2–39 chars. Becomes the backup stanza.">
              <Input value={name} onChange={(e) => setName(e.target.value.toLowerCase())} placeholder="orders-db" autoFocus />
              {name && !nameValid && <span className="text-xs text-danger">Must match [a-z][a-z0-9-]{"{1,38}"}</span>}
            </Field>

            <div>
              <span className="mb-2 block font-mono text-[11px] uppercase tracking-wider text-fg-muted">Backup repository</span>
              <div className="grid grid-cols-2 gap-3">
                <RepoOption
                  active={repoType === "s3"}
                  onClick={() => setRepoType("s3")}
                  icon={<Cloud className="h-4 w-4" />}
                  title="Object store"
                  desc="S3 / MinIO bucket"
                />
                <RepoOption
                  active={repoType === "local"}
                  onClick={() => setRepoType("local")}
                  icon={<HardDrive className="h-4 w-4" />}
                  title="Local volume"
                  desc="Docker volume"
                />
              </div>
            </div>

            <div className="grid grid-cols-2 gap-4">
              <Field label="Postgres version">
                <Select value={pgVersion} onChange={(e) => setPgVersion(e.target.value)}>
                  <option value="16">16</option>
                </Select>
              </Field>
              <Field label="Superuser password" hint="Min 8 characters. Stored encrypted.">
                <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} />
              </Field>
            </div>

            {error && (
              <div className="rounded-md border border-danger/30 bg-danger/10 px-3 py-2 text-xs text-danger">{error}</div>
            )}

            <div className="flex items-center justify-end gap-3 border-t border-line pt-5">
              <Button type="button" variant="ghost" onClick={() => router.back()}>
                Cancel
              </Button>
              <Button type="submit" disabled={!valid || submitting}>
                {submitting ? "Provisioning…" : "Provision instance"}
              </Button>
            </div>
          </form>
        </CardBody>
      </Card>
    </div>
  );
}

function RepoOption({
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
      className={`flex items-start gap-3 rounded-lg border px-4 py-3 text-left transition-all ${
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
