"use client";

import { PageHeader } from "@/components/shell";
import { Badge, Card, Spinner } from "@/components/ui";
import { api, type EventItem } from "@/lib/api";
import { useQuery } from "@tanstack/react-query";
import Link from "next/link";
import { useState } from "react";

type EventType =
  | "all"
  | "alert"
  | "provisioning"
  | "status_change"
  | "health_transition"
  | "lifecycle";

const TYPE_FILTERS: { value: EventType; label: string }[] = [
  { value: "all", label: "all" },
  { value: "alert", label: "alert" },
  { value: "provisioning", label: "provisioning" },
  { value: "status_change", label: "status_change" },
  { value: "health_transition", label: "health_transition" },
  { value: "lifecycle", label: "lifecycle" },
];

function timeAgo(iso: string): string {
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) return "—";
  const secs = Math.floor((Date.now() - then) / 1000);
  if (secs < 45) return "just now";
  const mins = Math.floor(secs / 60);
  if (mins < 60) return `${mins}m ago`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) return `${hours}h ago`;
  const days = Math.floor(hours / 24);
  return `${days}d ago`;
}

function dotClass(type: string): string {
  switch (type) {
    case "alert":
      return "bg-signal";
    case "provisioning":
      return "bg-azure";
    case "health_transition":
      return "bg-violet";
    case "status_change":
      return "bg-fg-muted";
    default:
      return "bg-fg-faint";
  }
}

function badgeTone(type: string): "azure" | "signal" | "violet" | "neutral" {
  switch (type) {
    case "alert":
      return "signal";
    case "provisioning":
      return "azure";
    case "health_transition":
      return "violet";
    default:
      return "neutral";
  }
}

export default function EventsPage() {
  const [typeFilter, setTypeFilter] = useState("");

  const { data, isLoading } = useQuery({
    queryKey: ["events", typeFilter],
    queryFn: () => api.listEvents({ type: typeFilter || undefined, limit: 200 }),
    refetchInterval: 8000,
  });
  const events: EventItem[] = data?.events ?? [];

  return (
    <div className="rise">
      <PageHeader
        title="Events"
        subtitle="Lifecycle, health, and alert history (survives restarts)"
      />

      <div className="mb-6 flex flex-wrap gap-2">
        {TYPE_FILTERS.map((f) => {
          const active = (f.value === "all" ? "" : f.value) === typeFilter;
          return (
            <button
              key={f.value}
              type="button"
              onClick={() => setTypeFilter(f.value === "all" ? "" : f.value)}
              className={`rounded-md border px-3 py-1.5 font-mono text-xs transition-colors ${
                active
                  ? "border-azure/50 bg-azure/10 text-azure"
                  : "border-line text-fg-muted hover:border-line-bright"
              }`}
            >
              {f.label}
            </button>
          );
        })}
      </div>

      {isLoading ? (
        <div className="grid place-items-center py-16">
          <Spinner className="h-6 w-6" />
        </div>
      ) : events.length === 0 ? (
        <Card>
          <div className="grid place-items-center py-16 text-center">
            <p className="text-sm text-fg-muted">No events recorded yet.</p>
          </div>
        </Card>
      ) : (
        <ol className="ml-2 border-l border-line">
          {events.map((e) => (
            <li key={e.id} className="relative pb-7 pl-6 last:pb-0">
              <span
                className={`absolute -left-[5px] top-1.5 h-2.5 w-2.5 rounded-full ring-2 ring-ink-900 ${dotClass(
                  e.type
                )}`}
              />
              <div className="flex flex-wrap items-center gap-2">
                <Badge tone={badgeTone(e.type)}>{e.type}</Badge>
                {e.instance_id && (
                  <Link
                    href={`/instances/${e.instance_id}`}
                    className="font-mono text-xs text-azure transition-colors hover:text-azure-bright"
                  >
                    {e.instance_id.slice(0, 8)}
                  </Link>
                )}
                <span className="ml-auto font-mono text-[11px] text-fg-faint tnum">
                  {timeAgo(e.created_at)}
                </span>
              </div>
              <p className="mt-1.5 text-sm text-fg">{e.message}</p>
              {e.metadata && Object.keys(e.metadata).length > 0 && (
                <div className="mt-2 flex flex-wrap gap-1.5">
                  {Object.entries(e.metadata).map(([k, v]) => (
                    <span
                      key={k}
                      className="rounded border border-line bg-ink-700/50 px-1.5 py-0.5 font-mono text-[10px] text-fg-muted"
                    >
                      {k}={v}
                    </span>
                  ))}
                </div>
              )}
            </li>
          ))}
        </ol>
      )}
    </div>
  );
}
