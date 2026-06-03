"use client";

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
  ConfirmDialog,
  EmptyState,
  SearchInput,
  SkeletonRows,
  Spinner,
  useToast,
} from "@/components/ui";
import { api, type Instance } from "@/lib/api";
import { can, useAuth } from "@/lib/auth";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ChevronRight, Database, ExternalLink, MoreHorizontal, Network, Plus, SquareTerminal, Trash2 } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useMemo, useState } from "react";

const STATUS_FILTERS = ["all", "running", "stopped", "error"] as const;
type StatusFilter = (typeof STATUS_FILTERS)[number];

export default function InstancesPage() {
  const { user } = useAuth();
  const writable = can(user?.role, "write");
  const { data, isLoading, isFetching } = useQuery({
    queryKey: ["instances"],
    queryFn: api.listInstances,
    refetchInterval: 5000,
  });
  const clusters = useQuery({ queryKey: ["clusters"], queryFn: api.listClusters });
  const clusterName = useMemo(() => {
    const m = new Map<string, string>();
    for (const c of clusters.data?.clusters ?? []) m.set(c.id, c.name);
    return m;
  }, [clusters.data]);

  const items = data?.instances ?? [];
  const [query, setQuery] = useState("");
  const [status, setStatus] = useState<StatusFilter>("all");

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return items.filter((i) => {
      if (status !== "all" && i.status !== status) return false;
      if (!q) return true;
      const cname = i.cluster_id ? clusterName.get(i.cluster_id) ?? "" : "";
      return (
        i.name.toLowerCase().includes(q) ||
        i.id.toLowerCase().includes(q) ||
        String(i.host_port).includes(q) ||
        cname.toLowerCase().includes(q)
      );
    });
  }, [items, query, status, clusterName]);

  return (
    <div className="rise">
      <PageHeader
        title="Instances"
        subtitle="Every managed Postgres instance in this control plane."
        action={
          <Button asChild>
            <Link href="/instances/new">
              <Plus className="h-4 w-4" />
              New instance
            </Link>
          </Button>
        }
      />

      {/* Filter toolbar: search + status. */}
      <div className="mb-4 flex flex-col gap-3 sm:flex-row sm:items-center">
        <SearchInput value={query} onChange={setQuery} placeholder="Search by name, id, port, or cluster…" className="sm:max-w-sm" />
        <div className="flex flex-wrap gap-1.5">
          {STATUS_FILTERS.map((s) => (
            <button
              key={s}
              type="button"
              onClick={() => setStatus(s)}
              aria-pressed={status === s}
              className={
                "cursor-pointer rounded-md border px-2.5 py-1.5 font-mono text-[11px] uppercase tracking-wider transition-colors " +
                (status === s ? "border-azure/50 bg-azure/10 text-azure" : "border-line text-fg-muted hover:border-line-bright hover:text-fg")
              }
            >
              {s}
            </button>
          ))}
          <span className="ml-1 self-center font-mono text-[11px] text-fg-faint tnum">
            {filtered.length}/{items.length}
          </span>
        </div>
      </div>

      <Card>
        <div className="grid grid-cols-[1fr_5rem_5rem_8rem_2.5rem] items-center gap-4 border-b border-line px-5 py-3 font-mono text-[10px] uppercase tracking-wider text-fg-faint">
          <span>Name</span>
          <span>Repo</span>
          <span>Endpoint</span>
          <span>Status</span>
          <span className="sr-only">Actions</span>
        </div>
        <CardBody className="p-0">
          {isLoading ? (
            <div className="p-5">
              <SkeletonRows rows={4} />
            </div>
          ) : items.length === 0 ? (
            <EmptyState
              icon={<Database className="h-5 w-5" />}
              title="No instances provisioned"
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
          ) : filtered.length === 0 ? (
            <EmptyState icon={<Database className="h-5 w-5" />} title="No matching instances" description="No instances match the current search and filter." />
          ) : (
            <ul className="divide-y divide-line">
              {filtered.map((i) => (
                <InstanceRow key={i.id} instance={i} writable={writable} clusterName={i.cluster_id ? clusterName.get(i.cluster_id) : undefined} />
              ))}
            </ul>
          )}
        </CardBody>
        {isFetching && !isLoading && (
          <div className="flex items-center gap-2 border-t border-line px-5 py-2 font-mono text-[10px] uppercase tracking-wider text-fg-faint">
            <Spinner className="h-3 w-3" /> syncing
          </div>
        )}
      </Card>
    </div>
  );
}

function InstanceRow({ instance: i, writable, clusterName }: { instance: Instance; writable: boolean; clusterName?: string }) {
  const [destroyOpen, setDestroyOpen] = useState(false);

  return (
    <li className="group relative grid grid-cols-[1fr_5rem_5rem_8rem_2.5rem] items-center gap-4 px-5 transition-colors hover:bg-ink-800/50 focus-within:bg-ink-800/50">
      {/* The link spans the row; the action menu sits above it via z-index. */}
      <Link
        href={`/instances/${i.id}`}
        className="absolute inset-0 rounded-none focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-azure/50"
        aria-label={`Open ${i.name}`}
      />
      <div className="flex items-center gap-3 py-4">
        <Database className="h-4 w-4 shrink-0 text-fg-faint" />
        <div className="min-w-0">
          <div className="flex items-center gap-2">
            <span className="truncate font-display text-sm text-fg">{i.name}</span>
            {i.cluster_id && (
              <Badge tone="violet" title={`Member of cluster ${clusterName ?? i.cluster_id}`}>
                <Network className="h-2.5 w-2.5" />
                {clusterName ?? "cluster"}
                {i.role === "primary" ? " · primary" : i.role === "replica" ? " · replica" : ""}
              </Badge>
            )}
          </div>
          <div className="font-mono text-[11px] text-fg-faint">pg{i.pg_version}</div>
        </div>
      </div>
      <span className="font-mono text-xs uppercase text-fg-muted">{i.repo_type}</span>
      <span className="font-mono text-xs text-fg-muted tnum">:{i.host_port || "—"}</span>
      <InstanceStatus status={i.status} />
      <div className="relative z-10 flex justify-end">
        {writable ? (
          <ActionMenu
            trigger={
              <Button size="sm" variant="ghost" aria-label={`Actions for ${i.name}`} className="h-8 w-8 px-0">
                <MoreHorizontal className="h-4 w-4" />
              </Button>
            }
          >
            <ActionMenuItem icon={<ExternalLink className="h-4 w-4" />} onSelect={() => (window.location.href = `/instances/${i.id}`)}>
              Open instance
            </ActionMenuItem>
            <ActionMenuItem
              icon={<SquareTerminal className="h-4 w-4" />}
              disabled={i.status !== "running"}
              onSelect={() => (window.location.href = `/instances/${i.id}#console`)}
            >
              Run SQL query…
            </ActionMenuItem>
            <ActionMenuSeparator />
            <ActionMenuItem icon={<Trash2 className="h-4 w-4" />} danger onSelect={() => setDestroyOpen(true)}>
              Destroy instance…
            </ActionMenuItem>
          </ActionMenu>
        ) : (
          <ChevronRight className="h-4 w-4 text-fg-faint transition-colors group-hover:text-fg-muted" aria-hidden="true" />
        )}
      </div>

      {writable && <DestroyModal id={i.id} name={i.name} open={destroyOpen} onOpenChange={setDestroyOpen} />}
    </li>
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
  const router = useRouter();
  const destroy = useMutation({
    mutationFn: () => api.destroyInstance(id, true),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["instances"] });
      toast.push(`Destroyed ${name}`, "danger");
      onOpenChange(false);
      router.refresh();
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
