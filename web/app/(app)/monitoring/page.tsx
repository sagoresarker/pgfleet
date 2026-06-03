"use client";

import { RouterObservability } from "@/components/routing";
import { ConnectionPoolGaugeWall, ReplicationLagRiver, TpsChart, WalRateChart } from "@/components/metrics-viz";
import { PageHeader } from "@/components/shell";
import { Card, CardBody, EmptyState, SkeletonRows } from "@/components/ui";
import { api, type Cluster, type Instance } from "@/lib/api";
import { useQuery } from "@tanstack/react-query";
import { Activity, Crown, Database, Network } from "lucide-react";
import { useMemo, useState } from "react";

type Target = { kind: "instance" | "cluster"; id: string } | null;

export default function MonitoringPage() {
  const instances = useQuery({ queryKey: ["instances"], queryFn: api.listInstances, refetchInterval: 8000 });
  const clusters = useQuery({ queryKey: ["clusters"], queryFn: api.listClusters, refetchInterval: 8000 });

  const insts = useMemo(() => instances.data?.instances ?? [], [instances.data]);
  const clus = useMemo(() => clusters.data?.clusters ?? [], [clusters.data]);

  const [picked, setPicked] = useState<Target>(null);
  // Resolve the active target: explicit pick, else first running instance, else first cluster.
  const target: Target =
    picked ??
    (insts.find((i) => i.status === "running")
      ? { kind: "instance", id: insts.find((i) => i.status === "running")!.id }
      : clus[0]
        ? { kind: "cluster", id: clus[0].id }
        : null);

  const loading = instances.isLoading || clusters.isLoading;
  const hasTargets = insts.length > 0 || clus.length > 0;

  return (
    <div className="rise">
      <PageHeader
        eyebrow="Operations"
        title="Monitoring"
        subtitle="Live throughput, WAL generation, replication health, and query routing across the fleet."
      />

      {loading ? (
        <Card>
          <CardBody>
            <SkeletonRows rows={3} />
          </CardBody>
        </Card>
      ) : !hasTargets ? (
        <Card>
          <CardBody>
            <EmptyState icon={<Activity className="h-5 w-5" />} title="Nothing to monitor yet" description="Provision an instance or a cluster to see live metrics here." />
          </CardBody>
        </Card>
      ) : (
        <div className="space-y-6">
          <TargetSelector instances={insts} clusters={clus} target={target} onPick={setPicked} />
          {target?.kind === "instance" && <InstanceMonitor instance={insts.find((i) => i.id === target.id)} />}
          {target?.kind === "cluster" && <ClusterMonitor clusterId={target.id} />}
        </div>
      )}
    </div>
  );
}

function TargetSelector({
  instances,
  clusters,
  target,
  onPick,
}: {
  instances: Instance[];
  clusters: Cluster[];
  target: Target;
  onPick: (t: Target) => void;
}) {
  return (
    <Card>
      <CardBody className="space-y-3">
        {instances.length > 0 && (
          <Group label="Instances">
            {instances.map((i) => (
              <Chip
                key={i.id}
                active={target?.kind === "instance" && target.id === i.id}
                onClick={() => onPick({ kind: "instance", id: i.id })}
                icon={i.role === "primary" ? <Crown className="h-3.5 w-3.5" /> : <Database className="h-3.5 w-3.5" />}
                label={i.name}
                live={i.status === "running"}
              />
            ))}
          </Group>
        )}
        {clusters.length > 0 && (
          <Group label="Clusters">
            {clusters.map((c) => (
              <Chip
                key={c.id}
                active={target?.kind === "cluster" && target.id === c.id}
                onClick={() => onPick({ kind: "cluster", id: c.id })}
                icon={<Network className="h-3.5 w-3.5" />}
                label={c.name}
                live={c.status === "running"}
              />
            ))}
          </Group>
        )}
      </CardBody>
    </Card>
  );
}

function Group({ label, children }: { label: string; children: React.ReactNode }) {
  return (
    <div className="flex flex-wrap items-center gap-2">
      <span className="mr-1 w-20 shrink-0 font-mono text-[10px] uppercase tracking-wider text-fg-faint">{label}</span>
      {children}
    </div>
  );
}

function Chip({
  active,
  onClick,
  icon,
  label,
  live,
}: {
  active: boolean;
  onClick: () => void;
  icon: React.ReactNode;
  label: string;
  live: boolean;
}) {
  return (
    <button
      type="button"
      onClick={onClick}
      className={
        "inline-flex cursor-pointer items-center gap-2 rounded-md border px-3 py-1.5 font-display text-sm transition-colors " +
        (active ? "border-azure/60 bg-azure/10 text-azure" : "border-line text-fg-muted hover:border-line-bright hover:text-fg")
      }
    >
      <span className={"led " + (live ? "led-healthy" : "led-idle")} />
      {icon}
      {label}
    </button>
  );
}

function InstanceMonitor({ instance }: { instance?: Instance }) {
  if (!instance) return null;
  const running = instance.status === "running";
  return (
    <div className="space-y-6">
      <div className="grid gap-6 lg:grid-cols-2">
        <TpsChart instanceId={instance.id} running={running} />
        <WalRateChart instanceId={instance.id} running={running} />
      </div>
      <ConnectionPoolGaugeWall instanceId={instance.id} running={running} />
      {instance.role === "replica" && <ReplicationLagRiver instanceId={instance.id} running={running} />}
    </div>
  );
}

function ClusterMonitor({ clusterId }: { clusterId: string }) {
  const { data } = useQuery({ queryKey: ["cluster", clusterId], queryFn: () => api.getCluster(clusterId), refetchInterval: 6000 });
  const members = data?.members ?? [];
  const replicas = members.filter((m) => m.role === "replica");
  const ready = data?.cluster.status === "running";

  return (
    <div className="space-y-6">
      <RouterObservability id={clusterId} ready={ready} />
      {replicas.length > 0 && (
        <div className="grid gap-6 lg:grid-cols-2">
          {replicas.map((r) => (
            <ReplicationLagRiver key={r.id} instanceId={r.id} name={r.name} running={r.status === "running"} />
          ))}
        </div>
      )}
    </div>
  );
}
