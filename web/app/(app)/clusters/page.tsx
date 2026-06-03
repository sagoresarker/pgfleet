"use client";

import { PageHeader } from "@/components/shell";
import { ClusterStatus } from "@/components/status";
import {
  ActionMenu,
  ActionMenuItem,
  ActionMenuSeparator,
  Button,
  Card,
  CardBody,
  ConfirmDialog,
  EmptyState,
  SearchInput,
  SkeletonRows,
  useToast,
} from "@/components/ui";
import { api, type Cluster } from "@/lib/api";
import { can, useAuth } from "@/lib/auth";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ChevronRight, ExternalLink, MoreHorizontal, Network, Plus, Trash2 } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useMemo, useState } from "react";

export default function ClustersPage() {
  const { user } = useAuth();
  const writable = can(user?.role, "write");
  const { data, isLoading } = useQuery({ queryKey: ["clusters"], queryFn: api.listClusters, refetchInterval: 5000 });
  const items = data?.clusters ?? [];
  const [query, setQuery] = useState("");
  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return q ? items.filter((c) => c.name.toLowerCase().includes(q) || c.id.toLowerCase().includes(q)) : items;
  }, [items, query]);

  return (
    <div className="rise">
      <PageHeader
        title="Clusters"
        subtitle="High-availability clusters: a primary, streaming replicas, and a query router."
        action={
          <Button asChild>
            <Link href="/clusters/new">
              <Plus className="h-4 w-4" />
              New cluster
            </Link>
          </Button>
        }
      />

      {items.length > 0 && (
        <div className="mb-4">
          <SearchInput value={query} onChange={setQuery} placeholder="Search clusters…" className="sm:max-w-sm" />
        </div>
      )}

      <Card>
        <div className="grid grid-cols-[1fr_6rem_8rem_2.5rem] items-center gap-4 border-b border-line px-5 py-3 font-mono text-[10px] uppercase tracking-wider text-fg-faint">
          <span>Name</span>
          <span>Router</span>
          <span>Status</span>
          <span className="sr-only">Actions</span>
        </div>
        <CardBody className="p-0">
          {isLoading ? (
            <div className="p-5">
              <SkeletonRows rows={3} />
            </div>
          ) : items.length === 0 ? (
            <EmptyState
              icon={<Network className="h-5 w-5" />}
              title="No clusters yet"
              description="A cluster gives you read replicas, automatic read/write routing, and a foundation for failover."
              action={
                <Button asChild size="sm">
                  <Link href="/clusters/new">
                    <Plus className="h-4 w-4" />
                    New cluster
                  </Link>
                </Button>
              }
            />
          ) : filtered.length === 0 ? (
            <EmptyState icon={<Network className="h-5 w-5" />} title="No matching clusters" description="No clusters match your search." />
          ) : (
            <ul className="divide-y divide-line">
              {filtered.map((c) => (
                <ClusterRow key={c.id} cluster={c} writable={writable} />
              ))}
            </ul>
          )}
        </CardBody>
      </Card>
    </div>
  );
}

function ClusterRow({ cluster: c, writable }: { cluster: Cluster; writable: boolean }) {
  const [destroyOpen, setDestroyOpen] = useState(false);

  return (
    <li className="group relative grid grid-cols-[1fr_6rem_8rem_2.5rem] items-center gap-4 px-5 transition-colors hover:bg-ink-800/50 focus-within:bg-ink-800/50">
      <Link
        href={`/clusters/${c.id}`}
        className="absolute inset-0 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-azure/50"
        aria-label={`Open ${c.name}`}
      />
      <div className="flex items-center gap-3 py-4">
        <Network className="h-4 w-4 shrink-0 text-fg-faint" />
        <span className="truncate font-display text-sm text-fg">{c.name}</span>
      </div>
      <span className="font-mono text-xs text-fg-muted tnum">:{c.router_port || "—"}</span>
      <ClusterStatus status={c.status} />
      <div className="relative z-10 flex justify-end">
        {writable ? (
          <ActionMenu
            trigger={
              <Button size="sm" variant="ghost" aria-label={`Actions for ${c.name}`} className="h-8 w-8 px-0">
                <MoreHorizontal className="h-4 w-4" />
              </Button>
            }
          >
            <ActionMenuItem icon={<ExternalLink className="h-4 w-4" />} onSelect={() => (window.location.href = `/clusters/${c.id}`)}>
              Open cluster
            </ActionMenuItem>
            <ActionMenuSeparator />
            <ActionMenuItem icon={<Trash2 className="h-4 w-4" />} danger onSelect={() => setDestroyOpen(true)}>
              Destroy cluster…
            </ActionMenuItem>
          </ActionMenu>
        ) : (
          <ChevronRight className="h-4 w-4 text-fg-faint transition-colors group-hover:text-fg-muted" aria-hidden="true" />
        )}
      </div>

      {writable && <DestroyModal id={c.id} name={c.name} open={destroyOpen} onOpenChange={setDestroyOpen} />}
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
    mutationFn: () => api.destroyCluster(id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["clusters"] });
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
      description="This removes the query router and every member (primary and replicas). Backups already in the repository are retained."
      danger
      confirmLabel="Destroy cluster"
      loading={destroy.isPending}
      onConfirm={() => destroy.mutate()}
    />
  );
}
