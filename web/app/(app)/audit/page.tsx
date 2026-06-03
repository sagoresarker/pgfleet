"use client";

import { PageHeader } from "@/components/shell";
import { Badge, Card, CardBody, EmptyState, SearchInput, Select, SkeletonRows } from "@/components/ui";
import { api } from "@/lib/api";
import { useQuery } from "@tanstack/react-query";
import { ScrollText, ShieldAlert } from "lucide-react";
import { useMemo, useState } from "react";

// Actions that change or expose data, highlighted so an auditor's eye is drawn
// to the privileged ones.
function tone(action: string): "danger" | "signal" | "azure" | "neutral" {
  if (/destroy|delete|disable|restore/.test(action)) return "danger";
  if (/exec|sql|dump|clone|visibility|sso|login\.failed/.test(action)) return "signal";
  if (/create|backup|enable|login/.test(action)) return "azure";
  return "neutral";
}

export default function AuditPage() {
  const [limit, setLimit] = useState(100);
  const audit = useQuery({
    queryKey: ["audit", limit],
    queryFn: () => api.listAudit(limit),
    refetchInterval: 15000,
  });
  const entries = audit.data?.entries ?? [];
  const [query, setQuery] = useState("");
  const [category, setCategory] = useState("all");

  // Action categories are the prefix before the first dot (instance, backup,
  // cluster, user, alertrule, …), derived from whatever is present.
  const categories = useMemo(() => {
    const set = new Set<string>();
    for (const e of entries) set.add(e.action.split(".")[0]);
    return [...set].sort();
  }, [entries]);

  const filtered = useMemo(() => {
    const q = query.trim().toLowerCase();
    return entries.filter((e) => {
      if (category !== "all" && !e.action.startsWith(category)) return false;
      if (!q) return true;
      return e.actor.toLowerCase().includes(q) || (e.target ?? "").toLowerCase().includes(q) || e.action.toLowerCase().includes(q);
    });
  }, [entries, query, category]);

  return (
    <div className="rise">
      <PageHeader
        title="Audit log"
        subtitle="An append-only record of who did what across the control plane."
        action={
          <div className="flex items-center gap-2 font-mono text-[11px] uppercase tracking-wider text-fg-muted">
            <ShieldAlert className="h-3.5 w-3.5 text-azure" />
            admin only
          </div>
        }
      />

      <div className="mb-4 flex flex-col gap-3 sm:flex-row sm:items-center">
        <SearchInput value={query} onChange={setQuery} placeholder="Search by email, target, or action…" className="sm:max-w-sm" />
        <div className="flex items-center gap-2">
          <Select value={category} onChange={(e) => setCategory(e.target.value)} className="h-9 w-44">
            <option value="all">All actions</option>
            {categories.map((c) => (
              <option key={c} value={c}>
                {c}
              </option>
            ))}
          </Select>
          <span className="font-mono text-[11px] text-fg-faint tnum">
            {filtered.length}/{entries.length}
          </span>
        </div>
      </div>

      <Card>
        <div className="grid grid-cols-[10rem_1fr_1fr_9rem] items-center gap-4 border-b border-line px-5 py-3 font-mono text-[10px] uppercase tracking-wider text-fg-faint">
          <span>Action</span>
          <span>Actor</span>
          <span>Target</span>
          <span className="text-right">When</span>
        </div>
        <CardBody className="p-0">
          {audit.isLoading ? (
            <div className="p-5">
              <SkeletonRows rows={6} />
            </div>
          ) : audit.error ? (
            <div role="alert" className="m-5 rounded-md border border-danger/30 bg-danger/10 px-3.5 py-2.5 font-mono text-xs text-danger">
              {audit.error instanceof Error ? audit.error.message : "Failed to load the audit log"}
            </div>
          ) : entries.length === 0 ? (
            <EmptyState icon={<ScrollText className="h-5 w-5" />} title="No audit entries yet" description="Actions taken in the dashboard will appear here." />
          ) : filtered.length === 0 ? (
            <EmptyState icon={<ScrollText className="h-5 w-5" />} title="No matching entries" description="No audit entries match the current search and filter." />
          ) : (
            <ul className="divide-y divide-line">
              {filtered.map((e) => (
                <li key={e.id} className="grid grid-cols-[10rem_1fr_1fr_9rem] items-center gap-4 px-5 py-3">
                  <Badge tone={tone(e.action)}>{e.action}</Badge>
                  <span className="truncate font-mono text-xs text-fg" title={e.actor}>
                    {e.actor}
                  </span>
                  <span className="truncate font-mono text-xs text-fg-muted" title={e.target}>
                    {e.target || "—"}
                  </span>
                  <time
                    className="text-right font-mono text-[11px] text-fg-faint tnum"
                    dateTime={e.created_at}
                    title={new Date(e.created_at).toLocaleString()}
                  >
                    {new Date(e.created_at).toLocaleString([], { month: "short", day: "2-digit", hour: "2-digit", minute: "2-digit" })}
                  </time>
                </li>
              ))}
            </ul>
          )}
        </CardBody>
        {entries.length >= limit && (
          <div className="border-t border-line px-5 py-2.5 text-center">
            <button
              onClick={() => setLimit((l) => Math.min(l + 100, 500))}
              className="cursor-pointer font-mono text-[11px] uppercase tracking-wider text-azure transition-colors hover:text-azure-bright"
            >
              Load more
            </button>
          </div>
        )}
      </Card>
    </div>
  );
}
