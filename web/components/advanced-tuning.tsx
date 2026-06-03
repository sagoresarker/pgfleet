"use client";

import { Input } from "@/components/ui";
import { ChevronDown, Plus, X } from "lucide-react";
import { useState } from "react";

// Mirror of internal/pgconfig allowedExtensions. The backend re-validates, so a
// drift here can never bypass the security boundary.
export const ALLOWED_EXTENSIONS = ["pg_trgm", "pgcrypto", "uuid-ossp", "hstore", "citext"];

export type ParamRow = { key: string; value: string };

// rowsToRecord drops blank-key rows and trims keys.
export function rowsToRecord(rows: ParamRow[]): Record<string, string> {
  const out: Record<string, string> = {};
  for (const r of rows) {
    const k = r.key.trim();
    if (k) out[k] = r.value;
  }
  return out;
}

export function AdvancedTuning({
  rows,
  setRows,
  exts,
  setExts,
}: {
  rows: ParamRow[];
  setRows: (r: ParamRow[]) => void;
  exts: string[];
  setExts: (e: string[]) => void;
}) {
  const [open, setOpen] = useState(false);

  function setRow(i: number, patch: Partial<ParamRow>) {
    setRows(rows.map((r, idx) => (idx === i ? { ...r, ...patch } : r)));
  }
  function toggleExt(name: string) {
    setExts(exts.includes(name) ? exts.filter((e) => e !== name) : [...exts, name]);
  }

  return (
    <div className="rounded-md border border-line">
      <button
        type="button"
        onClick={() => setOpen((o) => !o)}
        className="flex w-full items-center justify-between px-4 py-3 text-left"
      >
        <span className="font-mono text-[11px] uppercase tracking-wider text-fg-muted">
          Advanced · Postgres tuning
        </span>
        <ChevronDown className={`h-4 w-4 text-fg-faint transition-transform ${open ? "rotate-180" : ""}`} />
      </button>

      {open && (
        <div className="space-y-5 border-t border-line px-4 py-4">
          <div>
            <span className="mb-2 block text-xs text-fg-muted">
              Parameters (GUCs). Platform-managed keys like wal_level are rejected.
            </span>
            <div className="space-y-2">
              {rows.map((r, i) => (
                <div key={i} className="flex items-center gap-2">
                  <Input
                    value={r.key}
                    onChange={(e) => setRow(i, { key: e.target.value })}
                    placeholder="work_mem"
                    className="flex-1 font-mono text-xs"
                  />
                  <span className="text-fg-faint">=</span>
                  <Input
                    value={r.value}
                    onChange={(e) => setRow(i, { value: e.target.value })}
                    placeholder="8MB"
                    className="flex-1 font-mono text-xs"
                  />
                  <button
                    type="button"
                    onClick={() => setRows(rows.filter((_, idx) => idx !== i))}
                    className="grid h-8 w-8 shrink-0 place-items-center rounded-md border border-line text-fg-faint hover:border-danger/60 hover:text-danger"
                    aria-label="Remove parameter"
                  >
                    <X className="h-3.5 w-3.5" />
                  </button>
                </div>
              ))}
            </div>
            <button
              type="button"
              onClick={() => setRows([...rows, { key: "", value: "" }])}
              className="mt-2 inline-flex items-center gap-1.5 text-xs text-azure hover:text-azure-bright"
            >
              <Plus className="h-3.5 w-3.5" /> Add parameter
            </button>
          </div>

          <div>
            <span className="mb-2 block text-xs text-fg-muted">Extensions</span>
            <div className="flex flex-wrap gap-2">
              {ALLOWED_EXTENSIONS.map((name) => {
                const active = exts.includes(name);
                return (
                  <button
                    key={name}
                    type="button"
                    onClick={() => toggleExt(name)}
                    className={`rounded-md border px-3 py-1.5 font-mono text-xs transition-colors ${
                      active
                        ? "border-azure/50 bg-azure/10 text-azure"
                        : "border-line text-fg-muted hover:border-line-bright"
                    }`}
                  >
                    {name}
                  </button>
                );
              })}
            </div>
          </div>
        </div>
      )}
    </div>
  );
}
