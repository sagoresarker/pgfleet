"use client";

import { Button, Card, CardBody, CardHeader, CardTitle, Spinner } from "@/components/ui";
import { api } from "@/lib/api";
import { useMutation } from "@tanstack/react-query";
import { Play, TerminalSquare } from "lucide-react";
import { useState } from "react";

export function ConsoleTab({ id, running, writable }: { id: string; running: boolean; writable: boolean }) {
  const [query, setQuery] = useState("SELECT * FROM pg_stat_activity LIMIT 20;");

  const run = useMutation({
    mutationFn: () => api.runSQL(id, query),
  });

  if (!running) {
    return (
      <p className="rounded-md border border-line bg-ink-850 px-4 py-8 text-center text-sm text-fg-muted">
        The console is available while the instance is running.
      </p>
    );
  }

  const result = run.data;
  const error = run.error instanceof Error ? run.error.message : null;

  return (
    <div className="space-y-6">
      <Card>
        <CardHeader>
          <CardTitle>SQL console</CardTitle>
          <Button size="sm" onClick={() => run.mutate()} disabled={run.isPending || !query.trim()}>
            {run.isPending ? <Spinner className="h-4 w-4" /> : <Play className="h-4 w-4" />} Run
          </Button>
        </CardHeader>
        <CardBody className="space-y-4">
          <textarea
            value={query}
            onChange={(e) => setQuery(e.target.value)}
            onKeyDown={(e) => {
              if ((e.metaKey || e.ctrlKey) && e.key === "Enter") run.mutate();
            }}
            spellCheck={false}
            rows={6}
            className="w-full rounded-md border border-line bg-ink-800 px-3 py-2.5 font-mono text-xs text-fg focus:border-azure focus:outline-none"
            placeholder="SELECT …   (⌘/Ctrl + Enter to run)"
          />

          {error && (
            <pre className="overflow-auto rounded-md border border-danger/30 bg-danger/10 px-3 py-2 font-mono text-xs text-danger">
              {error}
            </pre>
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
        </CardBody>
      </Card>

      {writable && <ExecCard id={id} />}
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
      <div className="rounded-md border border-healthy/30 bg-healthy/10 px-3 py-2 font-mono text-xs text-healthy">
        {command || "OK"} · {rowsAffected} row{rowsAffected === 1 ? "" : "s"} affected
      </div>
    );
  }
  return (
    <div>
      <div className="overflow-auto rounded-md border border-line">
        <table className="w-full border-collapse text-left font-mono text-xs">
          <thead>
            <tr className="border-b border-line bg-ink-800">
              {columns.map((c) => (
                <th key={c} className="whitespace-nowrap px-3 py-2 font-medium text-fg-muted">
                  {c}
                </th>
              ))}
            </tr>
          </thead>
          <tbody>
            {rows.map((row, i) => (
              <tr key={i} className="border-b border-line/60">
                {row.map((cell, j) => (
                  <td key={j} className="max-w-[28rem] truncate whitespace-nowrap px-3 py-1.5 text-fg tnum">
                    {renderCell(cell)}
                  </td>
                ))}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
      <p className="mt-2 font-mono text-[11px] text-fg-faint">
        {rows.length} row{rows.length === 1 ? "" : "s"}
        {truncated && ` (truncated to first ${rows.length})`}
      </p>
    </div>
  );
}

function renderCell(v: unknown): string {
  if (v === null || v === undefined) return "∅";
  if (typeof v === "object") return JSON.stringify(v);
  return String(v);
}

function ExecCard({ id }: { id: string }) {
  const [cmd, setCmd] = useState("");
  const exec = useMutation({ mutationFn: () => api.execCommand(id, ["bash", "-c", cmd]) });
  const res = exec.data;

  return (
    <Card>
      <CardHeader>
        <CardTitle>
          <span className="flex items-center gap-2">
            <TerminalSquare className="h-4 w-4 text-fg-faint" /> Container shell
          </span>
        </CardTitle>
      </CardHeader>
      <CardBody className="space-y-3">
        <div className="flex gap-2">
          <input
            value={cmd}
            onChange={(e) => setCmd(e.target.value)}
            onKeyDown={(e) => e.key === "Enter" && cmd.trim() && exec.mutate()}
            spellCheck={false}
            className="flex-1 rounded-md border border-line bg-ink-800 px-3 py-2 font-mono text-xs text-fg focus:border-azure focus:outline-none"
            placeholder="pgbackrest --stanza=… check"
          />
          <Button size="sm" variant="outline" onClick={() => exec.mutate()} disabled={exec.isPending || !cmd.trim()}>
            Run
          </Button>
        </div>
        {res && (
          <pre className="max-h-72 overflow-auto rounded-md border border-line bg-ink-800 px-3 py-2 font-mono text-[11px] text-fg">
            {(res.stdout || "") + (res.stderr ? `\n${res.stderr}` : "")}
            {`\n— exit ${res.exit_code} —`}
          </pre>
        )}
      </CardBody>
    </Card>
  );
}
