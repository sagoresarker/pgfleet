"use client";

import { PageHeader } from "@/components/shell";
import { InstanceStatus } from "@/components/status";
import {
  ActionMenu,
  ActionMenuItem,
  ActionMenuSeparator,
  Button,
  Card,
  CardBody,
  ConfirmDialog,
  EmptyState,
  SkeletonRows,
  Spinner,
  useToast,
} from "@/components/ui";
import { api, type Instance } from "@/lib/api";
import { can, useAuth } from "@/lib/auth";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { ChevronRight, Database, ExternalLink, MoreHorizontal, Plus, SquareTerminal, Trash2 } from "lucide-react";
import Link from "next/link";
import { useRouter } from "next/navigation";
import { useState } from "react";

export default function InstancesPage() {
  const { user } = useAuth();
  const writable = can(user?.role, "write");
  const { data, isLoading, isFetching } = useQuery({
    queryKey: ["instances"],
    queryFn: api.listInstances,
    refetchInterval: 5000,
  });
  const items = data?.instances ?? [];

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
          ) : (
            <ul className="divide-y divide-line">
              {items.map((i) => (
                <InstanceRow key={i.id} instance={i} writable={writable} />
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

function InstanceRow({ instance: i, writable }: { instance: Instance; writable: boolean }) {
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
          <div className="truncate font-display text-sm text-fg">{i.name}</div>
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
