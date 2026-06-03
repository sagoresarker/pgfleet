"use client";

import { PageHeader } from "@/components/shell";
import {
  Badge,
  Button,
  Card,
  CardBody,
  CardHeader,
  CardTitle,
  ConfirmDialog,
  EmptyState,
  Field,
  Input,
  Modal,
  Select,
  SkeletonRows,
  StatusLed,
  Table,
  Td,
  Th,
  THead,
  Tr,
  useToast,
} from "@/components/ui";
import { api, type ActiveAlert, type AlertKind, type AlertRule } from "@/lib/api";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Bell, Database, Pencil, Plus, ShieldCheck, SlidersHorizontal, Trash2 } from "lucide-react";
import Link from "next/link";
import { useState } from "react";

/* ── kind metadata (label + display↔stored unit conversion) ───────────────── */
type KindMeta = { value: AlertKind; label: string; unit: string; factor: number; hint: string };
const KINDS: KindMeta[] = [
  { value: "disk_full", label: "Disk space low", unit: "% free", factor: 1, hint: "Fire when free disk drops below this percent." },
  { value: "replication_lag", label: "Replication lag", unit: "seconds", factor: 1, hint: "Fire when a replica lags more than this many seconds." },
  { value: "backup_stale", label: "Backup stale", unit: "hours", factor: 3600, hint: "Fire when the newest backup is older than this many hours." },
  { value: "connection_saturation", label: "Connection saturation", unit: "% used", factor: 1, hint: "Fire when connection utilization exceeds this percent." },
];
const kindMeta = (k: string): KindMeta => KINDS.find((m) => m.value === k) ?? KINDS[0];
const kindLabel = (k: string): string => kindMeta(k).label;

function timeAgo(iso: string): string {
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) return "—";
  const secs = Math.max(0, Math.floor((Date.now() - then) / 1000));
  if (secs < 60) return `${secs}s ago`;
  const mins = Math.floor(secs / 60);
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  return `${Math.floor(hours / 24)}d ago`;
}

type Tab = "active" | "rules";

export default function AlertsPage() {
  const [tab, setTab] = useState<Tab>("active");
  return (
    <div className="rise">
      <PageHeader
        eyebrow="Operations"
        title="Alerts"
        subtitle="Active reliability alerts across the fleet, and the rules that decide when they fire."
      />
      <div className="mb-6 inline-flex rounded-lg border border-line bg-ink-900 p-1">
        <TabButton active={tab === "active"} onClick={() => setTab("active")} icon={<Bell className="h-4 w-4" />} label="Active" />
        <TabButton active={tab === "rules"} onClick={() => setTab("rules")} icon={<SlidersHorizontal className="h-4 w-4" />} label="Rules" />
      </div>
      {tab === "active" ? <ActiveAlerts /> : <RulesPanel />}
    </div>
  );
}

function TabButton({ active, onClick, icon, label }: { active: boolean; onClick: () => void; icon: React.ReactNode; label: string }) {
  return (
    <button
      type="button"
      onClick={onClick}
      aria-pressed={active}
      className={
        "inline-flex cursor-pointer items-center gap-2 rounded-md px-4 py-1.5 font-display text-sm transition-colors " +
        (active ? "bg-azure/10 text-azure" : "text-fg-muted hover:text-fg")
      }
    >
      {icon}
      {label}
    </button>
  );
}

/* ── Active alerts ───────────────────────────────────────────────────────── */
type SeverityFilter = "all" | "critical" | "warning";

function ActiveAlerts() {
  const { data, isLoading } = useQuery({ queryKey: ["alerts"], queryFn: () => api.listAlerts(), refetchInterval: 5000 });
  const [filter, setFilter] = useState<SeverityFilter>("all");

  const firing = (data?.alerts ?? [])
    .filter((a) => a.state === "firing")
    .sort((a, b) => {
      if (a.severity !== b.severity) return a.severity === "critical" ? -1 : 1;
      return new Date(b.fired_at).getTime() - new Date(a.fired_at).getTime();
    });

  const visible = filter === "all" ? firing : firing.filter((a) => a.severity === filter);
  const filters: { value: SeverityFilter; label: string; count: number }[] = [
    { value: "all", label: "All", count: firing.length },
    { value: "critical", label: "Critical", count: firing.filter((a) => a.severity === "critical").length },
    { value: "warning", label: "Warning", count: firing.filter((a) => a.severity === "warning").length },
  ];

  if (isLoading) return <SkeletonRows rows={3} />;
  if (firing.length === 0)
    return (
      <Card className="border-healthy/30">
        <CardBody className="py-4">
          <EmptyState
            icon={<ShieldCheck className="h-5 w-5 text-healthy" />}
            title="No active alerts — the fleet is healthy"
            description="Reliability checks run continuously. Anything that needs attention will surface here."
          />
        </CardBody>
      </Card>
    );

  return (
    <>
      <div className="mb-6 flex flex-wrap gap-2" role="group" aria-label="Filter alerts by severity">
        {filters.map((f) => (
          <button
            key={f.value}
            type="button"
            onClick={() => setFilter(f.value)}
            aria-pressed={filter === f.value}
            className={`inline-flex cursor-pointer items-center gap-2 rounded-md border px-3 py-1.5 font-mono text-xs transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-azure/50 ${
              filter === f.value ? "border-azure/50 bg-azure/10 text-azure" : "border-line text-fg-muted hover:border-line-bright hover:text-fg"
            }`}
          >
            {f.label}
            <span className="rounded bg-ink-700/70 px-1.5 tnum text-fg-muted">{f.count}</span>
          </button>
        ))}
      </div>
      {visible.length === 0 ? (
        <Card>
          <CardBody className="py-4">
            <EmptyState icon={<ShieldCheck className="h-5 w-5" />} title="No alerts match this filter" description="Switch to “All” to see every firing alert." />
          </CardBody>
        </Card>
      ) : (
        <ul className="space-y-3">
          {visible.map((alert) => (
            <AlertRow key={alert.id} alert={alert} />
          ))}
        </ul>
      )}
    </>
  );
}

function AlertRow({ alert }: { alert: ActiveAlert }) {
  const critical = alert.severity === "critical";
  return (
    <li>
      <Card className={critical ? "border-danger/30" : undefined}>
        <CardBody className="flex items-start gap-4">
          <StatusLed status={critical ? "danger" : "signal"} pulse />
          <div className="min-w-0 flex-1 space-y-2">
            <div className="flex flex-wrap items-center gap-2">
              <span className="font-display text-sm font-medium tracking-tight text-fg">{kindLabel(alert.kind)}</span>
              <Badge tone={critical ? "danger" : "signal"}>{alert.severity}</Badge>
              <span className="ml-auto font-mono text-[11px] text-fg-faint tnum">{timeAgo(alert.fired_at)}</span>
            </div>
            <p className="text-sm text-fg-muted">{alert.message}</p>
            <div className="flex flex-wrap items-center gap-x-4 gap-y-1 font-mono text-[11px] text-fg-faint">
              <Link
                href={`/instances/${alert.instance_id}`}
                className="inline-flex items-center gap-1.5 rounded transition-colors hover:text-azure focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-azure/50"
              >
                <Database className="h-3 w-3" />
                <span className="tnum">{alert.instance_id}</span>
              </Link>
              {alert.value !== undefined && alert.threshold !== undefined && (
                <span className="tnum">
                  {alert.value} vs {alert.threshold}
                </span>
              )}
            </div>
          </div>
        </CardBody>
      </Card>
    </li>
  );
}

/* ── Rules panel ─────────────────────────────────────────────────────────── */
function RulesPanel() {
  const { data, isLoading } = useQuery({ queryKey: ["alert-rules"], queryFn: () => api.listAlertRules() });
  const [editing, setEditing] = useState<AlertRule | null>(null);
  const [creating, setCreating] = useState(false);
  const rules = data?.rules ?? [];

  return (
    <Card>
      <CardHeader>
        <CardTitle>Alert rules</CardTitle>
        <Button size="sm" variant="outline" onClick={() => setCreating(true)}>
          <Plus className="h-4 w-4" /> New rule
        </Button>
      </CardHeader>
      <CardBody className="p-0">
        {isLoading ? (
          <div className="p-5">
            <SkeletonRows rows={2} />
          </div>
        ) : rules.length === 0 ? (
          <EmptyState
            icon={<SlidersHorizontal className="h-5 w-5" />}
            title="No custom rules"
            description="The fleet uses sensible built-in thresholds. Add a rule to override one — globally or for a single instance."
            action={
              <Button size="sm" onClick={() => setCreating(true)}>
                <Plus className="h-4 w-4" /> New rule
              </Button>
            }
          />
        ) : (
          <Table>
            <THead>
              <Th>Condition</Th>
              <Th>Scope</Th>
              <Th align="right">Threshold</Th>
              <Th>Severity</Th>
              <Th>State</Th>
              <Th align="right">Actions</Th>
            </THead>
            <tbody>
              {rules.map((r) => (
                <RuleRow key={r.id} rule={r} onEdit={() => setEditing(r)} />
              ))}
            </tbody>
          </Table>
        )}
      </CardBody>

      {creating && <RuleModal open={creating} onOpenChange={setCreating} />}
      {editing && <RuleModal open onOpenChange={(o) => !o && setEditing(null)} rule={editing} />}
    </Card>
  );
}

function RuleRow({ rule, onEdit }: { rule: AlertRule; onEdit: () => void }) {
  const qc = useQueryClient();
  const toast = useToast();
  const [confirmDel, setConfirmDel] = useState(false);
  const meta = kindMeta(rule.kind);
  const del = useMutation({
    mutationFn: () => api.deleteAlertRule(rule.id),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["alert-rules"] });
      toast.push("Rule deleted", "danger");
    },
    onError: (e) => toast.push(e instanceof Error ? e.message : "Delete failed", "danger"),
  });

  return (
    <Tr>
      <Td className="font-display text-fg">{meta.label}</Td>
      <Td className="font-mono text-xs text-fg-muted">{rule.instance_id ? rule.instance_id.slice(0, 8) : "all instances"}</Td>
      <Td align="right" className="font-mono text-xs tnum text-fg">
        {rule.threshold / meta.factor} <span className="text-fg-faint">{meta.unit}</span>
      </Td>
      <Td>
        <Badge tone={rule.severity === "critical" ? "danger" : "signal"}>{rule.severity}</Badge>
      </Td>
      <Td>
        <span className="inline-flex items-center gap-1.5 font-mono text-[11px] text-fg-muted">
          <span className={"led " + (rule.enabled ? "led-healthy" : "led-idle")} />
          {rule.enabled ? "enabled" : "disabled"}
        </span>
      </Td>
      <Td align="right">
        <div className="flex justify-end gap-1">
          <button onClick={onEdit} aria-label="Edit rule" className="cursor-pointer rounded p-1.5 text-fg-faint transition-colors hover:bg-ink-700 hover:text-fg">
            <Pencil className="h-3.5 w-3.5" />
          </button>
          <button onClick={() => setConfirmDel(true)} aria-label="Delete rule" className="cursor-pointer rounded p-1.5 text-fg-faint transition-colors hover:bg-danger/10 hover:text-danger">
            <Trash2 className="h-3.5 w-3.5" />
          </button>
        </div>
        <ConfirmDialog
          open={confirmDel}
          onOpenChange={setConfirmDel}
          title="Delete this rule?"
          description="The fleet reverts to the built-in default threshold for this condition."
          danger
          confirmLabel="Delete rule"
          loading={del.isPending}
          onConfirm={() => del.mutate()}
        />
      </Td>
    </Tr>
  );
}

function RuleModal({ open, onOpenChange, rule }: { open: boolean; onOpenChange: (o: boolean) => void; rule?: AlertRule }) {
  const qc = useQueryClient();
  const toast = useToast();
  const instances = useQuery({ queryKey: ["instances"], queryFn: api.listInstances });
  const editMeta = rule ? kindMeta(rule.kind) : KINDS[0];

  const [kind, setKind] = useState<AlertKind>(rule?.kind ?? "disk_full");
  const [value, setValue] = useState<string>(rule ? String(rule.threshold / editMeta.factor) : "");
  const [severity, setSeverity] = useState<string>(rule?.severity ?? "warning");
  const [scope, setScope] = useState<string>(rule?.instance_id ?? "");
  const [enabled, setEnabled] = useState<boolean>(rule?.enabled ?? true);
  const meta = kindMeta(kind);
  const num = Number(value);
  const valid = value !== "" && Number.isFinite(num) && num >= 0;

  const save = useMutation({
    mutationFn: () => {
      const body = {
        instance_id: scope || null,
        kind,
        threshold: num * meta.factor,
        severity,
        enabled,
      };
      return rule ? api.updateAlertRule(rule.id, body) : api.createAlertRule(body);
    },
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["alert-rules"] });
      toast.push(rule ? "Rule updated" : "Rule created", "healthy");
      onOpenChange(false);
    },
    onError: (e) => toast.push(e instanceof Error ? e.message : "Save failed", "danger"),
  });

  return (
    <Modal
      open={open}
      onOpenChange={onOpenChange}
      title={rule ? "Edit alert rule" : "New alert rule"}
      description="Override a built-in threshold globally or for one instance."
      footer={
        <>
          <Button variant="ghost" size="sm" onClick={() => onOpenChange(false)} disabled={save.isPending}>
            Cancel
          </Button>
          <Button size="sm" loading={save.isPending} disabled={!valid} onClick={() => save.mutate()}>
            {rule ? "Save changes" : "Create rule"}
          </Button>
        </>
      }
    >
      <div className="space-y-4">
        <Field label="Condition" hint={meta.hint}>
          <Select value={kind} onChange={(e) => setKind(e.target.value as AlertKind)} disabled={!!rule}>
            {KINDS.map((k) => (
              <option key={k.value} value={k.value}>
                {k.label}
              </option>
            ))}
          </Select>
        </Field>
        <div className="grid grid-cols-2 gap-4">
          <Field label={`Threshold (${meta.unit})`}>
            <Input type="number" value={value} onChange={(e) => setValue(e.target.value)} min={0} step="any" inputMode="decimal" placeholder="e.g. 15" />
          </Field>
          <Field label="Severity">
            <Select value={severity} onChange={(e) => setSeverity(e.target.value)}>
              <option value="warning">Warning</option>
              <option value="critical">Critical</option>
            </Select>
          </Field>
        </div>
        <Field label="Scope" hint="Apply to every instance, or scope to one.">
          <Select value={scope} onChange={(e) => setScope(e.target.value)}>
            <option value="">All instances</option>
            {(instances.data?.instances ?? []).map((i) => (
              <option key={i.id} value={i.id}>
                {i.name}
              </option>
            ))}
          </Select>
        </Field>
        <label className="flex cursor-pointer items-center gap-2.5 rounded-md border border-line bg-ink-850 px-3 py-2.5">
          <input type="checkbox" checked={enabled} onChange={(e) => setEnabled(e.target.checked)} className="h-4 w-4 cursor-pointer accent-azure" />
          <span className="text-sm text-fg-muted">
            <span className="font-medium text-fg">Enabled</span> — evaluate this rule on every check.
          </span>
        </label>
      </div>
    </Modal>
  );
}
