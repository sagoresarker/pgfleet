"use client";

import { Badge, Button, Card, CardBody, CardHeader, CardTitle, Field, Input, Spinner } from "@/components/ui";
import { api, type Hypertable } from "@/lib/api";
import { formatBytes } from "@/lib/utils";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Clock, Database, Layers, Plus, Snowflake } from "lucide-react";
import { useState } from "react";

export function TimescaleTab({ id, running, writable }: { id: string; running: boolean; writable: boolean }) {
  const hypertables = useQuery({
    queryKey: ["hypertables", id],
    queryFn: () => api.listHypertables(id),
    refetchInterval: 8000,
    enabled: running,
  });
  const jobs = useQuery({
    queryKey: ["ts-jobs", id],
    queryFn: () => api.timescaleJobs(id),
    refetchInterval: 12000,
    enabled: running,
  });

  if (!running) {
    return (
      <p className="rounded-md border border-line bg-ink-850 px-4 py-8 text-center text-sm text-fg-muted">
        TimescaleDB management is available while the instance is running.
      </p>
    );
  }

  const tables = hypertables.data?.hypertables ?? [];

  return (
    <div className="space-y-6">
      {writable && <CreateHypertable id={id} />}

      <Card>
        <CardHeader>
          <CardTitle>Hypertables</CardTitle>
          {hypertables.isFetching && <Spinner />}
        </CardHeader>
        <CardBody className="p-0">
          {tables.length === 0 ? (
            <p className="px-5 py-8 text-center text-sm text-fg-muted">
              No hypertables yet. Convert a time-series table above to get started.
            </p>
          ) : (
            <ul className="divide-y divide-line">
              {tables.map((h) => (
                <HypertableRow key={`${h.schema}.${h.name}`} id={id} h={h} writable={writable} />
              ))}
            </ul>
          )}
        </CardBody>
      </Card>

      <Card>
        <CardHeader>
          <CardTitle>Background jobs</CardTitle>
        </CardHeader>
        <CardBody className="p-0">
          {(jobs.data?.jobs ?? []).length === 0 ? (
            <p className="px-5 py-6 text-center text-sm text-fg-muted">No policy jobs scheduled.</p>
          ) : (
            <ul className="divide-y divide-line">
              {jobs.data!.jobs.map((j) => (
                <li key={j.id} className="flex items-center gap-3 px-5 py-3 font-mono text-xs">
                  <Clock className="h-3.5 w-3.5 text-fg-faint" />
                  <span className="flex-1 text-fg">{j.application}</span>
                  <span className="text-fg-muted">every {j.schedule_interval}</span>
                  <Badge tone={j.last_run_status === "Success" ? "healthy" : "neutral"}>
                    {j.last_run_status || "pending"}
                  </Badge>
                </li>
              ))}
            </ul>
          )}
        </CardBody>
      </Card>
    </div>
  );
}

function CreateHypertable({ id }: { id: string }) {
  const qc = useQueryClient();
  const [table, setTable] = useState("");
  const [timeColumn, setTimeColumn] = useState("");
  const [error, setError] = useState<string | null>(null);
  const create = useMutation({
    mutationFn: () => api.createHypertable(id, { table, time_column: timeColumn }),
    onSuccess: () => {
      qc.invalidateQueries({ queryKey: ["hypertables", id] });
      setTable("");
      setTimeColumn("");
      setError(null);
    },
    onError: (e) => setError(e instanceof Error ? e.message : "Failed to create hypertable"),
  });

  const valid = /^[a-zA-Z_][a-zA-Z0-9_]*$/.test(table) && /^[a-zA-Z_][a-zA-Z0-9_]*$/.test(timeColumn);

  return (
    <Card>
      <CardHeader>
        <CardTitle>Create hypertable</CardTitle>
      </CardHeader>
      <CardBody>
        <div className="grid grid-cols-1 gap-4 sm:grid-cols-[1fr_1fr_auto] sm:items-end">
          <Field label="Table" hint="An existing time-series table.">
            <Input value={table} onChange={(e) => setTable(e.target.value)} placeholder="metrics" className="font-mono text-xs" />
          </Field>
          <Field label="Time column">
            <Input value={timeColumn} onChange={(e) => setTimeColumn(e.target.value)} placeholder="ts" className="font-mono text-xs" />
          </Field>
          <Button onClick={() => create.mutate()} disabled={!valid || create.isPending}>
            <Plus className="h-4 w-4" /> {create.isPending ? "Creating…" : "Create"}
          </Button>
        </div>
        {error && <div className="mt-3 rounded-md border border-danger/30 bg-danger/10 px-3 py-2 text-xs text-danger">{error}</div>}
      </CardBody>
    </Card>
  );
}

function HypertableRow({ id, h, writable }: { id: string; h: Hypertable; writable: boolean }) {
  const qc = useQueryClient();
  const name = h.schema && h.schema !== "public" ? `${h.schema}.${h.name}` : h.name;
  const [open, setOpen] = useState(false);
  const invalidate = () => qc.invalidateQueries({ queryKey: ["hypertables", id] });

  return (
    <li className="px-5 py-3.5">
      <div className="flex items-center gap-3">
        <Layers className="h-4 w-4 text-azure" />
        <button type="button" onClick={() => setOpen((o) => !o)} className="flex-1 text-left">
          <span className="font-display text-sm text-fg">{name}</span>
        </button>
        <span className="font-mono text-[11px] text-fg-faint tnum">{h.num_chunks} chunks</span>
        <span className="font-mono text-xs text-fg-muted tnum">{formatBytes(h.size_bytes)}</span>
        {h.compression_enabled ? (
          <Badge tone="azure">
            <Snowflake className="mr-1 inline h-3 w-3" />
            compressed
          </Badge>
        ) : (
          <Badge tone="neutral">uncompressed</Badge>
        )}
      </div>
      {open && writable && <PolicyControls id={id} hypertable={name} compressed={h.compression_enabled} onDone={invalidate} />}
    </li>
  );
}

function PolicyControls({ id, hypertable, compressed, onDone }: { id: string; hypertable: string; compressed: boolean; onDone: () => void }) {
  const [retention, setRetention] = useState("30 days");
  const [compressAfter, setCompressAfter] = useState("7 days");
  const [segmentBy, setSegmentBy] = useState("");
  const [msg, setMsg] = useState<string | null>(null);

  const addRetention = useMutation({
    mutationFn: () => api.addRetention(id, { hypertable, drop_after: retention }),
    onSuccess: () => { setMsg("Retention policy added."); onDone(); },
    onError: (e) => setMsg(e instanceof Error ? e.message : "Failed"),
  });
  const enableCompression = useMutation({
    mutationFn: () => api.enableCompression(id, { hypertable, segment_by: segmentBy || undefined, compress_after: compressAfter }),
    onSuccess: () => { setMsg("Compression enabled."); onDone(); },
    onError: (e) => setMsg(e instanceof Error ? e.message : "Failed"),
  });

  return (
    <div className="mt-3 grid grid-cols-1 gap-4 rounded-md border border-line bg-ink-850 p-4 sm:grid-cols-2">
      <div className="space-y-2">
        <span className="flex items-center gap-1.5 font-mono text-[11px] uppercase tracking-wider text-fg-muted">
          <Database className="h-3 w-3" /> Retention
        </span>
        <div className="flex gap-2">
          <Input value={retention} onChange={(e) => setRetention(e.target.value)} className="font-mono text-xs" />
          <Button size="sm" variant="outline" onClick={() => addRetention.mutate()} disabled={addRetention.isPending}>
            Drop after
          </Button>
        </div>
      </div>
      <div className="space-y-2">
        <span className="flex items-center gap-1.5 font-mono text-[11px] uppercase tracking-wider text-fg-muted">
          <Snowflake className="h-3 w-3" /> Compression
        </span>
        <div className="flex gap-2">
          <Input value={compressAfter} onChange={(e) => setCompressAfter(e.target.value)} className="font-mono text-xs" />
          <Button size="sm" variant="outline" onClick={() => enableCompression.mutate()} disabled={enableCompression.isPending}>
            {compressed ? "Update" : "Compress after"}
          </Button>
        </div>
        <Input value={segmentBy} onChange={(e) => setSegmentBy(e.target.value)} placeholder="segment by (optional column)" className="font-mono text-xs" />
      </div>
      {msg && <div className="sm:col-span-2 text-xs text-fg-muted">{msg}</div>}
    </div>
  );
}
