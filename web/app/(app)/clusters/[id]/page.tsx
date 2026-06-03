"use client";

import { ComposePreview } from "@/components/compose-preview";
import { PageHeader } from "@/components/shell";
import { RouterObservability } from "@/components/routing";
import { ClusterStatus } from "@/components/status";
import {
  ActionMenu,
  ActionMenuItem,
  ActionMenuSeparator,
  Badge,
  Button,
  Card,
  CardBody,
  CardHeader,
  CardTitle,
  ConfirmDialog,
  EmptyState,
  SkeletonRows,
  Stat,
  Table,
  Td,
  Th,
  THead,
  Tr,
  useToast,
} from "@/components/ui";
import { api } from "@/lib/api";
import { can, useAuth } from "@/lib/auth";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import {
  ChevronLeft,
  Copy,
  Crown,
  Database,
  Eye,
  EyeOff,
  FileDown,
  MoreHorizontal,
  Network,
  Trash2,
} from "lucide-react";
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
      <div>
        <div className="mb-4 h-3 w-24 rounded bg-ink-700/70" />
        <div className="mb-2 h-8 w-64 animate-pulse rounded-md bg-ink-700/70" />
        <div className="mb-8 h-4 w-40 animate-pulse rounded-md bg-ink-700/70" />
        <SkeletonRows rows={3} />
      </div>
    );
  }

  const { cluster: c, members } = data;
  const primary = members.filter((m) => m.role === "primary");
  const replicas = members.filter((m) => m.role === "replica");
  const ready = c.status === "running";

  return (
    <div className="rise">
      <div className="mb-4">
        <Link
          href="/clusters"
          className="inline-flex items-center gap-1 font-mono text-[11px] uppercase tracking-wider text-fg-faint transition-colors hover:text-azure"
        >
          <ChevronLeft className="h-3.5 w-3.5" /> clusters
        </Link>
      </div>
      <PageHeader
        title={c.name}
        subtitle={`High-availability cluster · router :${c.router_port || "—"}`}
        action={
          <div className="flex items-center gap-3">
            <ClusterStatus status={c.status} />
            {writable && <ClusterToolbar id={id} name={c.name} />}
          </div>
        }
      />

      {c.last_error && c.status === "error" && (
        <div role="alert" className="mb-6 rounded-md border border-danger/30 bg-danger/10 px-4 py-3 text-sm text-danger">
          {c.last_error}
        </div>
      )}

      <div className="mb-6 grid grid-cols-1 gap-4 sm:grid-cols-3">
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
        <div className="space-y-6 lg:col-span-2">
          <Card>
            <CardHeader>
              <CardTitle>Primary</CardTitle>
              <Badge tone="signal">read / write</Badge>
            </CardHeader>
            <CardBody className="p-0">
              {primary.length > 0 ? (
                <ul className="divide-y divide-line">
                  {primary.map((m) => (
                    <MemberRow key={m.id} member={m} />
                  ))}
                </ul>
              ) : (
                <p className="px-5 py-4 text-sm text-fg-muted">No primary elected yet.</p>
              )}
            </CardBody>
          </Card>

          <Card>
            <CardHeader>
              <CardTitle>Read replicas</CardTitle>
              <Badge tone="neutral">{replicas.length}</Badge>
            </CardHeader>
            <CardBody className="p-0">
              {replicas.length > 0 ? (
                <ul className="divide-y divide-line">
                  {replicas.map((m) => (
                    <MemberRow key={m.id} member={m} />
                  ))}
                </ul>
              ) : (
                <p className="px-5 py-4 text-sm text-fg-muted">No replicas configured. Reads are served by the primary.</p>
              )}
            </CardBody>
          </Card>
        </div>

        <div className="space-y-6">
          <ConnectionCard id={id} ready={ready} />
        </div>
      </div>

      <div className="mt-6 space-y-6">
        <RouterObservability id={id} ready={ready} />
        <PoolStatsPanel id={id} ready={ready} />
      </div>
    </div>
  );
}

/* ---- Live PgCat pool stats (SHOW POOLS / SHOW STATS) ---- */
function PoolStatsPanel({ id, ready }: { id: string; ready: boolean }) {
  const q = useQuery({
    queryKey: ["pool-stats", id],
    enabled: ready,
    refetchInterval: 5000,
    retry: false,
    queryFn: () => api.poolStats(id),
  });
  const pools = q.data?.pools ?? [];
  const stats = q.data?.stats ?? [];

  return (
    <Card>
      <CardHeader>
        <CardTitle>Router pool · live</CardTitle>
        <span className="flex items-center gap-1.5 font-mono text-[11px] text-fg-faint">
          <span className={"led " + (q.isFetching ? "led-signal led-pulse" : q.error ? "led-idle" : "led-healthy")} />
          {q.error ? "unavailable" : "PgCat admin"}
        </span>
      </CardHeader>
      <CardBody className="p-0">
        {!ready ? (
          <p className="px-5 py-8 text-center text-sm text-fg-muted">Pool stats are available once the router is running.</p>
        ) : q.isLoading ? (
          <div className="p-5">
            <SkeletonRows rows={2} />
          </div>
        ) : q.error ? (
          <p className="px-5 py-8 text-center text-sm text-fg-muted">
            Could not reach the PgCat admin interface. It becomes available shortly after the router starts.
          </p>
        ) : pools.length === 0 ? (
          <EmptyState icon={<Network className="h-5 w-5" />} title="No pools yet" description="The router has no active pools to report." />
        ) : (
          <Table>
            <THead>
              <Th>Pool</Th>
              <Th align="right">Clients (active/wait/idle)</Th>
              <Th align="right">Servers (active/idle)</Th>
              <Th align="right">Max wait</Th>
              <Th align="right">Avg query</Th>
            </THead>
            <tbody>
              {pools.map((p, i) => {
                const s = stats.find((x) => x.database === p.database);
                return (
                  <Tr key={i}>
                    <Td>
                      <span className="font-display text-fg">{p.database}</span>
                      <span className="ml-2 font-mono text-[11px] text-fg-faint">{p.pool_mode}</span>
                    </Td>
                    <Td align="right" className="font-mono text-xs tnum">
                      <span className="text-fg">{p.cl_active}</span>
                      <span className={"px-1 " + (p.cl_waiting > 0 ? "text-danger" : "text-fg-faint")}>/ {p.cl_waiting}</span>
                      <span className="text-fg-faint">/ {p.cl_idle}</span>
                    </Td>
                    <Td align="right" className="font-mono text-xs text-fg-muted tnum">
                      {p.sv_active} / {p.sv_idle}
                    </Td>
                    <Td align="right" className={"font-mono text-xs tnum " + (p.maxwait > 0 ? "text-signal" : "text-fg-muted")}>
                      {p.maxwait}s
                    </Td>
                    <Td align="right" className="font-mono text-xs text-fg-muted tnum">
                      {s ? `${(s.avg_query_time / 1000).toFixed(1)}ms` : "—"}
                    </Td>
                  </Tr>
                );
              })}
            </tbody>
          </Table>
        )}
      </CardBody>
    </Card>
  );
}

/* ---- Toolbar: secondary + destructive actions in a tidy Actions menu ---- */
function ClusterToolbar({ id, name }: { id: string; name: string }) {
  const [destroyOpen, setDestroyOpen] = useState(false);
  const [composeOpen, setComposeOpen] = useState(false);

  return (
    <div className="flex items-center gap-2">
      <ActionMenu
        trigger={
          <Button size="sm" variant="outline" aria-label="More actions">
            <MoreHorizontal className="h-4 w-4" /> Actions
          </Button>
        }
      >
        <ActionMenuItem icon={<FileDown className="h-4 w-4" />} onSelect={() => setComposeOpen(true)}>
          View docker-compose
        </ActionMenuItem>
        <ActionMenuItem icon={<Database className="h-4 w-4" />} disabled>
          Backups managed per member
        </ActionMenuItem>
        <ActionMenuSeparator />
        <ActionMenuItem icon={<Trash2 className="h-4 w-4" />} danger onSelect={() => setDestroyOpen(true)}>
          Destroy cluster…
        </ActionMenuItem>
      </ActionMenu>

      <ComposePreview kind="cluster" id={id} name={name} open={composeOpen} onOpenChange={setComposeOpen} />
      <DestroyModal id={id} name={name} open={destroyOpen} onOpenChange={setDestroyOpen} />
    </div>
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
  const destroy = useMutation({
    mutationFn: () => api.destroyCluster(id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["clusters"] });
      toast.push(`Destroyed ${name}`, "danger");
      window.location.href = "/clusters";
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
    <li>
      <Link
        href={`/instances/${member.id}`}
        className="flex items-center gap-3 px-5 py-3.5 transition-colors hover:bg-ink-800/50 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-inset focus-visible:ring-azure/50"
      >
        {member.role === "primary" ? (
          <Crown className="h-4 w-4 shrink-0 text-signal" />
        ) : (
          <Database className="h-4 w-4 shrink-0 text-fg-faint" />
        )}
        <div className="min-w-0 flex-1">
          <div className="font-display text-sm text-fg">{member.name}</div>
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
      </Link>
    </li>
  );
}

function ConnectionCard({ id, ready }: { id: string; ready: boolean }) {
  const [revealed, setRevealed] = useState(false);
  const toast = useToast();
  const conn = useQuery({
    queryKey: ["cluster-conn", id],
    queryFn: () => api.clusterConnection(id),
    enabled: revealed && ready,
  });

  async function copy() {
    if (conn.data?.dsn) {
      await navigator.clipboard.writeText(conn.data.dsn);
      toast.push("Router connection string copied", "healthy");
    }
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Router endpoint</CardTitle>
        <div className="flex items-center gap-2">
          {revealed && conn.data?.dsn && (
            <Button size="sm" variant="ghost" onClick={copy}>
              <Copy className="h-4 w-4" /> Copy
            </Button>
          )}
          <Button size="sm" variant="ghost" onClick={() => setRevealed((r) => !r)} disabled={!ready}>
            {revealed ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
            {revealed ? "Hide" : "Reveal"}
          </Button>
        </div>
      </CardHeader>
      <CardBody>
        <div className="mb-2 flex items-center gap-2 text-xs text-fg-muted">
          <Network className="h-3.5 w-3.5 text-azure" />
          Apps connect here; reads and writes are split automatically.
        </div>
        <code className="block overflow-x-auto rounded-md border border-line bg-ink-850 px-3 py-2.5 font-mono text-xs text-azure">
          {!ready
            ? "router provisioning…"
            : revealed
              ? conn.data?.dsn ?? "loading…"
              : "postgres://postgres:••••••••@•••••:6432/postgres"}
        </code>
      </CardBody>
    </Card>
  );
}
