"use client";

import { PageHeader } from "@/components/shell";
import { ClusterStatus } from "@/components/status";
import { Button, Card, CardBody, Spinner } from "@/components/ui";
import { api } from "@/lib/api";
import { useQuery } from "@tanstack/react-query";
import { Network, Plus } from "lucide-react";
import Link from "next/link";

export default function ClustersPage() {
  const { data, isLoading } = useQuery({ queryKey: ["clusters"], queryFn: api.listClusters, refetchInterval: 5000 });
  const items = data?.clusters ?? [];

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

      <Card>
        <div className="grid grid-cols-[1fr_auto_auto] items-center gap-4 border-b border-line px-5 py-3 font-mono text-[10px] uppercase tracking-wider text-fg-faint">
          <span>Name</span>
          <span>Router</span>
          <span>Status</span>
        </div>
        <CardBody className="p-0">
          {isLoading ? (
            <div className="grid place-items-center py-16">
              <Spinner className="h-6 w-6" />
            </div>
          ) : items.length === 0 ? (
            <div className="grid place-items-center py-16 text-center">
              <Network className="mb-3 h-8 w-8 text-fg-faint" />
              <p className="text-sm text-fg-muted">No clusters yet.</p>
              <p className="mt-1 max-w-sm text-xs text-fg-faint">
                A cluster gives you read replicas, automatic read/write routing, and a foundation for failover.
              </p>
            </div>
          ) : (
            <ul className="divide-y divide-line">
              {items.map((c) => (
                <li key={c.id}>
                  <Link
                    href={`/clusters/${c.id}`}
                    className="grid grid-cols-[1fr_auto_auto] items-center gap-4 px-5 py-4 transition-colors hover:bg-ink-800/50"
                  >
                    <div className="flex items-center gap-3">
                      <Network className="h-4 w-4 text-fg-faint" />
                      <span className="font-display text-sm text-fg">{c.name}</span>
                    </div>
                    <span className="font-mono text-xs text-fg-muted tnum">:{c.router_port || "—"}</span>
                    <ClusterStatus status={c.status} />
                  </Link>
                </li>
              ))}
            </ul>
          )}
        </CardBody>
      </Card>
    </div>
  );
}
