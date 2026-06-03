"use client";

import { PageHeader } from "@/components/shell";
import { ClusterStatus } from "@/components/status";
import { Badge, Button, Card, CardBody, CardHeader, CardTitle, Spinner, Stat } from "@/components/ui";
import { api } from "@/lib/api";
import { can, useAuth } from "@/lib/auth";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Crown, Database, Eye, EyeOff, Network, Trash2 } from "lucide-react";
import Link from "next/link";
import { useParams } from "next/navigation";
import { useState } from "react";

export default function ClusterDetailPage() {
  const { id } = useParams<{ id: string }>();
  const { user } = useAuth();
  const writable = can(user?.role, "write");

  const { data, isLoading } = useQuery({
    queryKey: ["cluster", id],
    queryFn: () => api.getCluster(id),
    refetchInterval: 4000,
  });

  if (isLoading || !data) {
    return (
      <div className="grid place-items-center py-24">
        <Spinner className="h-6 w-6" />
      </div>
    );
  }

  const { cluster: c, members } = data;
  const replicas = members.filter((m) => m.role === "replica");

  return (
    <div className="rise">
      <div className="mb-4">
        <Link href="/clusters" className="font-mono text-[11px] uppercase tracking-wider text-fg-faint hover:text-azure">
          ← clusters
        </Link>
      </div>
      <PageHeader title={c.name} subtitle="High-availability cluster" action={<ClusterStatus status={c.status} />} />

      {c.last_error && c.status === "error" && (
        <div className="mb-6 rounded-md border border-danger/30 bg-danger/10 px-4 py-3 text-sm text-danger">{c.last_error}</div>
      )}

      <div className="mb-6 grid grid-cols-3 gap-4">
        <Card>
          <CardBody>
            <Stat label="Members" value={String(members.length)} sub={`1 primary · ${replicas.length} replicas`} />
          </CardBody>
        </Card>
        <Card>
          <CardBody>
            <Stat label="Router port" value={c.router_port ? String(c.router_port) : "—"} />
          </CardBody>
        </Card>
        <Card>
          <CardBody>
            <Stat label="Routing" value="R/W split" sub="reads → replicas" />
          </CardBody>
        </Card>
      </div>

      <div className="grid gap-6 lg:grid-cols-3">
        <div className="lg:col-span-2">
          <Card>
            <CardHeader>
              <CardTitle>Topology</CardTitle>
            </CardHeader>
            <CardBody className="p-0">
              <ul className="divide-y divide-line">
                {members.map((m) => (
                  <MemberRow key={m.id} member={m} />
                ))}
              </ul>
            </CardBody>
          </Card>
        </div>

        <div className="space-y-6">
          <ConnectionCard id={id} ready={c.status === "running"} />
          {writable && <DestroyCard id={id} />}
        </div>
      </div>
    </div>
  );
}

function MemberRow({ member }: { member: { id: string; name: string; role: string; status: string } }) {
  const running = member.status === "running";
  const latest = useQuery({
    queryKey: ["metrics-latest", member.id],
    queryFn: () => api.latestMetrics(member.id),
    refetchInterval: 6000,
    enabled: running,
  });
  const m = latest.data?.metrics ?? {};
  const conns = m.connections?.value;
  const cache = m.cache_hit_ratio?.value;
  const lag = m.replication_lag_seconds?.value;

  return (
    <li className="flex items-center gap-3 px-5 py-3.5">
      {member.role === "primary" ? (
        <Crown className="h-4 w-4 text-signal" />
      ) : (
        <Database className="h-4 w-4 text-fg-faint" />
      )}
      <div className="min-w-0 flex-1">
        <Link href={`/instances/${member.id}`} className="font-display text-sm text-fg hover:text-azure">
          {member.name}
        </Link>
        {running && (
          <div className="mt-0.5 flex gap-3 font-mono text-[11px] text-fg-faint tnum">
            {conns !== undefined && <span>{Math.round(conns)} conn</span>}
            {cache !== undefined && <span>{cache.toFixed(1)}% cache</span>}
            {member.role === "replica" && lag !== undefined && (
              <span className={lag > 10 ? "text-danger" : lag > 2 ? "text-signal" : "text-healthy"}>
                {lag < 1 ? "<1s" : `${Math.round(lag)}s`} lag
              </span>
            )}
          </div>
        )}
      </div>
      <Badge tone={member.role === "primary" ? "signal" : "neutral"}>{member.role}</Badge>
      <span className="font-mono text-xs text-fg-faint">{member.status}</span>
    </li>
  );
}

function ConnectionCard({ id, ready }: { id: string; ready: boolean }) {
  const [revealed, setRevealed] = useState(false);
  const conn = useQuery({ queryKey: ["cluster-conn", id], queryFn: () => api.clusterConnection(id), enabled: revealed && ready });

  return (
    <Card>
      <CardHeader>
        <CardTitle>Router endpoint</CardTitle>
        <Button size="sm" variant="ghost" onClick={() => setRevealed((r) => !r)} disabled={!ready}>
          {revealed ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
          {revealed ? "Hide" : "Reveal"}
        </Button>
      </CardHeader>
      <CardBody>
        <div className="mb-2 flex items-center gap-2 text-xs text-fg-muted">
          <Network className="h-3.5 w-3.5 text-azure" />
          Apps connect here; reads and writes are split automatically.
        </div>
        <code className="block overflow-x-auto rounded-md border border-line bg-ink-900 px-3 py-2.5 font-mono text-xs text-azure">
          {!ready ? "router provisioning…" : revealed ? conn.data?.dsn ?? "loading…" : "postgres://postgres:••••••••@•••••:6432/postgres"}
        </code>
      </CardBody>
    </Card>
  );
}

function DestroyCard({ id }: { id: string }) {
  const qc = useQueryClient();
  const [confirming, setConfirming] = useState(false);
  const destroy = useMutation({
    mutationFn: () => api.destroyCluster(id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["clusters"] });
      window.location.href = "/clusters";
    },
  });

  return (
    <Card>
      <CardHeader>
        <CardTitle>Danger zone</CardTitle>
      </CardHeader>
      <CardBody>
        {!confirming ? (
          <Button size="sm" variant="danger" className="w-full" onClick={() => setConfirming(true)}>
            <Trash2 className="h-4 w-4" /> Destroy cluster
          </Button>
        ) : (
          <div className="space-y-2">
            <p className="text-xs text-fg-muted">Removes the router and all members (backups retained).</p>
            <div className="flex gap-2">
              <Button size="sm" variant="danger" className="flex-1" onClick={() => destroy.mutate()} disabled={destroy.isPending}>
                Yes, destroy
              </Button>
              <Button size="sm" variant="ghost" onClick={() => setConfirming(false)}>
                Cancel
              </Button>
            </div>
          </div>
        )}
      </CardBody>
    </Card>
  );
}
