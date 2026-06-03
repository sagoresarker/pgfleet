"use client";

import { Badge, Button, EmptyState } from "@/components/ui";
import { api } from "@/lib/api";
import { useMutation } from "@tanstack/react-query";
import { CornerDownLeft, Database, Eraser, Play, Table2, TerminalSquare } from "lucide-react";
import { useState } from "react";

type Mode = "query" | "shell";

export function ConsoleTab({ id, running, writable }: { id: string; running: boolean; writable: boolean }) {
  const [mode, setMode] = useState<Mode>("query");

  if (!running) {
    return (
      <EmptyState
        icon={<TerminalSquare className="h-5 w-5" />}
        title="Console unavailable"
        description="The query console and container shell are available while the instance is running."
      />
    );
  }

  return (
    <div className="space-y-5">
      {/* Segmented mode toggle — one console surface, two tools. */}
      <div className="inline-flex rounded-lg border border-line bg-ink-850 p-1">
        <SegButton active={mode === "query"} onClick={() => setMode("query")} icon={<Database className="h-3.5 w-3.5" />}>
          SQL query
        </SegButton>
        {writable && (
          <SegButton active={mode === "shell"} onClick={() => setMode("shell")} icon={<TerminalSquare className="h-3.5 w-3.5" />}>
            Container shell
          </SegButton>
        )}
      </div>

      {mode === "query" ? <QueryConsole id={id} /> : <ShellConsole id={id} />}
    </div>
  );
}

function SegButton({
  active,
  onClick,
  icon,
  children,
}: {
  active: boolean;
  onClick: () => void;
  icon: React.ReactNode;
  children: React.ReactNode;
}) {
  return (
    <button
      onClick={onClick}
      aria-pressed={active}
      className={
        "flex cursor-pointer items-center gap-2 rounded-md px-3 py-1.5 font-display text-xs tracking-tight transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-azure/50 " +
        (active ? "bg-ink-900 text-fg shadow-sm" : "text-fg-muted hover:text-fg")
      }
    >
      {icon}
      {children}
    </button>
  );
}

/* ---- SQL query editor + results ---- */
function QueryConsole({ id }: { id: string }) {
  const [query, setQuery] = useState("SELECT * FROM pg_stat_activity LIMIT 20;");
  const run = useMutation({ mutationFn: () => api.runSQL(id, query) });
  const result = run.data;
  const error = run.error instanceof Error ? run.error.message : null;

  return (
    <div className="space-y-4">
      {/* Editor — a deliberate dark surface (a query editor reads best dark). */}
      <div className="overflow-hidden rounded-xl border border-line shadow-sm">
        <div className="flex items-center justify-between border-b border-[#1c2940] bg-[#0e1726] px-3 py-2">
          <span className="flex items-center gap-2 font-mono text-[11px] text-[#8a97ad]">
            <span className="h-2 w-2 rounded-full bg-[#3b82f6]" />
            query.sql
          </span>
          <div className="flex items-center gap-2">
            {query.trim() && (
              <button
                onClick={() => setQuery("")}
                className="flex cursor-pointer items-center gap-1 rounded px-2 py-1 font-mono text-[10px] text-[#6b7890] transition-colors hover:text-[#9fb0c9]"
                aria-label="Clear editor"
              >
                <Eraser className="h-3 w-3" /> clear
              </button>
            )}
            <Button size="sm" loading={run.isPending} disabled={!query.trim()} onClick={() => run.mutate()}>
              {!run.isPending && <Play className="h-3.5 w-3.5" />} Run
            </Button>
          </div>
        </div>
        <div className="relative">
          <textarea
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={(e) => {
              if ((e.metaKey || e.ctrlKey) && e.key === "Enter") run.mutate();
            }}
            spellCheck={false}
            rows={8}
            aria-label="SQL editor"
            className="block w-full resize-y bg-[#0b1320] px-4 py-3.5 font-mono text-[13px] leading-relaxed text-[#d7e1f0] caret-azure placeholder:text-[#475569] focus:outline-none"
            placeholder="SELECT … FROM …"
          />
          <span className="pointer-events-none absolute bottom-2.5 right-3 flex items-center gap-1 font-mono text-[10px] text-[#475569]">
            <CornerDownLeft className="h-3 w-3" /> ⌘/Ctrl + Enter
          </span>
        </div>
      </div>

      {error && (
        <div role="alert" className="overflow-auto rounded-lg border border-danger/30 bg-danger/10 px-3.5 py-2.5 font-mono text-xs text-danger">
          {error}
        </div>
      )}

      {result && !error && (
        <ResultTable
          columns={result.columns}
          rows={result.rows}
          command={result.command}
          rowsAffected={result.rows_affected}
          truncated={result.truncated}
        />
      )}

      {!result && !error && !run.isPending && (
        <div className="rounded-lg border border-dashed border-line px-4 py-8 text-center font-mono text-xs text-fg-faint">
          Run a query to see results here.
        </div>
      )}
    </div>
  );
}

function ResultTable({
  columns,
  rows,
  command,
  rowsAffected,
  truncated,
}: {
  columns: string[];
  rows: unknown[][];
  command: string;
  rowsAffected: number;
  truncated: boolean;
}) {
  if (columns.length === 0) {
    return (
      <div className="flex items-center gap-2 rounded-lg border border-healthy/30 bg-healthy/10 px-3.5 py-2.5 font-mono text-xs text-healthy">
        <span className="led led-healthy" />
        {command || "OK"} · {rowsAffected} row{rowsAffected === 1 ? "" : "s"} affected
      </div>
    );
  }
  return (
    <div className="overflow-hidden rounded-xl border border-line">
      <div className="max-h-[28rem] overflow-auto">
        <table className="w-full border-collapse text-left font-mono text-xs">
          <thead className="sticky top-0 z-10">
            <tr className="bg-ink-800">
              <th className="w-10 border-b border-line px-3 py-2 text-right font-medium text-fg-faint">#</th>
              {columns.map((c) => (
                <th key={c} className="whitespace-nowrap border-b border-line px-3 py-2 font-medium text-fg-muted">
                  {c}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.map((row, i) => (
              <tr key={i} className="transition-colors hover:bg-ink-800/60">
                <td className="border-b border-line/60 px-3 py-1.5 text-right text-fg-faint tnum">{i + 1}</td>
                {row.map((cell, j) => (
                  <td key={j} className="max-w-[28rem] truncate whitespace-nowrap border-b border-line/60 px-3 py-1.5 text-fg tnum" title={renderCell(cell)}>
                    {renderCell(cell)}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <div className="flex items-center justify-between border-t border-line bg-ink-850 px-3.5 py-2 font-mono text-[11px] text-fg-faint">
        <span>
          {rows.length} row{rows.length === 1 ? "" : "s"}
          {truncated && <span className="text-signal"> · truncated</span>}
        </span>
        <span>{command}</span>
      </div>
    </div>
  );
}

function renderCell(v: unknown): string {
  if (v === null || v === undefined) return "∅";
  if (typeof v === "object") return JSON.stringify(v);
  return String(v);
}

/* ---- Container shell (terminal) ---- */
function ShellConsole({ id }: { id: string }) {
  const [cmd, setCmd] = useState("");
  const exec = useMutation({ mutationFn: () => api.execCommand(id, ["bash", "-c", cmd]) });
  const res = exec.data;

  return (
    <div className="overflow-hidden rounded-xl border border-line shadow-sm">
      <div className="flex items-center gap-2 border-b border-[#1c2940] bg-[#0e1726] px-3 py-2 font-mono text-[11px] text-[#8a97ad]">
        <TerminalSquare className="h-3.5 w-3.5" /> container shell · bash -c
      </div>
      <div className="bg-[#0b1320] p-3">
        {res && (
          <pre className="mb-3 max-h-72 overflow-auto whitespace-pre-wrap rounded-md border border-[#1c2940] bg-[#070d18] px-3 py-2.5 font-mono text-[11px] leading-relaxed text-[#cdd9ea]">
            {res.stdout || ""}
            {res.stderr ? <span className="text-danger">{(res.stdout ? "\n" : "") + res.stderr}</span> : null}
            {"\n"}
            <span className={res.exit_code === 0 ? "text-healthy" : "text-danger"}>— exit {res.exit_code} —</span>
          </pre>
        )}
        <div className="flex items-center gap-2 rounded-md border border-[#1c2940] bg-[#070d18] px-3 py-2">
          <span className="font-mono text-sm text-healthy">$</span>
          <input
            value={cmd}
            onChange={(e) => setCmd(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && cmd.trim() && exec.mutate()}
            spellCheck={false}
            aria-label="Container command"
            className="flex-1 bg-transparent font-mono text-xs text-[#d7e1f0] caret-azure placeholder:text-[#475569] focus:outline-none"
            placeholder="pgbackrest --stanza=… check"
          />
          <Button size="sm" variant="outline" loading={exec.isPending} disabled={!cmd.trim()} onClick={() => exec.mutate()}>
            Run
          </Button>
        </div>
        <p className="mt-2 flex items-center gap-1.5 font-mono text-[10px] text-[#475569]">
          <Table2 className="h-3 w-3" /> runs one command in the container, then returns
        </p>
      </div>
      {res && (
        <div className="flex items-center gap-2 border-t border-line bg-ink-850 px-3.5 py-2">
          <Badge tone={res.exit_code === 0 ? "healthy" : "danger"}>exit {res.exit_code}</Badge>
        </div>
      )}
    </div>
  );
}
