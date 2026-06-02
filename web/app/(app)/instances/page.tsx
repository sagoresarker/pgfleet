"use client";

import { PageHeader } from "@/components/shell";
import { InstanceStatus } from "@/components/status";
import { Button, Card, CardBody, Spinner } from "@/components/ui";
import { api } from "@/lib/api";
import { useQuery } from "@tanstack/react-query";
import { Database, Plus } from "lucide-react";
import Link from "next/link";

export default function InstancesPage() {
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
        <div className="grid grid-cols-[1fr_auto_auto_auto] items-center gap-4 border-b border-line px-5 py-3 font-mono text-[10px] uppercase tracking-wider text-fg-faint">
          <span>Name</span>
          <span>Repo</span>
          <span>Endpoint</span>
          <span>Status</span>
        </div>
        <CardBody className="p-0">
          {isLoading ? (
            <div className="grid place-items-center py-16">
              <Spinner className="h-6 w-6" />
            </div>
          ) : items.length === 0 ? (
            <div className="grid place-items-center py-16 text-center">
              <Database className="mb-3 h-8 w-8 text-fg-faint" />
              <p className="text-sm text-fg-muted">No instances provisioned.</p>
            </div>
          ) : (
            <ul className="divide-y divide-line">
              {items.map((i) => (
                <li key={i.id}>
                  <Link
                    href={`/instances/${i.id}`}
                    className="grid grid-cols-[1fr_auto_auto_auto] items-center gap-4 px-5 py-4 transition-colors hover:bg-ink-800/50"
                  >
                    <div className="flex items-center gap-3">
                      <Database className="h-4 w-4 text-fg-faint" />
                      <div>
                        <div className="font-display text-sm text-fg">{i.name}</div>
                        <div className="font-mono text-[11px] text-fg-faint">pg{i.pg_version}</div>
                      </div>
                    </div>
                    <span className="font-mono text-xs uppercase text-fg-muted">{i.repo_type}</span>
                    <span className="font-mono text-xs text-fg-muted tnum">:{i.host_port || "—"}</span>
                    <InstanceStatus status={i.status} />
                  </Link>
                </li>
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
