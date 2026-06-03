"use client";

import { api, type EventItem } from "@/lib/api";
import { useQuery } from "@tanstack/react-query";
import * as DropdownPrimitive from "@radix-ui/react-dropdown-menu";
import { Activity, ArrowUpRight } from "lucide-react";
import Link from "next/link";

// eventStatus derives a live status from the event type string. Operation events
// follow a "<thing>.<verb>" convention (backup.started / backup.completed /
// backup.failed), so the verb tells us whether it's in flight, done, or failed.
function eventStatus(type: string): "running" | "success" | "error" {
  const t = type.toLowerCase();
  if (/fail|error|denied|refused|degraded|stale|saturat/.test(t)) return "error";
  if (/start|begin|provision|progress|running|restor|clon|creating|pending/.test(t)) return "running";
  return "success";
}

const ledFor = { running: "led-signal led-pulse", success: "led-healthy", error: "led-danger" } as const;

function timeAgo(iso: string): string {
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) return "";
  const s = Math.max(0, Math.floor((Date.now() - then) / 1000));
  if (s < 60) return `${s}s`;
  if (s < 3600) return `${Math.floor(s / 60)}m`;
  if (s < 86400) return `${Math.floor(s / 3600)}h`;
  return `${Math.floor(s / 86400)}d`;
}

/**
 * ActivityCenter is a global, always-reachable feed of what the control plane is
 * doing — backups, clones, provisioning, failovers — so an operator can watch
 * the live status of any action from any page (à la a cloud console's
 * notifications). It polls the durable event timeline every few seconds.
 */
export function ActivityCenter() {
  const { data } = useQuery({ queryKey: ["events", "recent"], queryFn: () => api.listEvents({ limit: 25 }), refetchInterval: 4000 });
  const events = data?.events ?? [];
  // "Live" = anything in-flight or very recent, which lights the bell.
  const active = events.some((e) => eventStatus(e.type) === "running" || Date.now() - new Date(e.created_at).getTime() < 15000);

  return (
    <DropdownPrimitive.Root>
      <DropdownPrimitive.Trigger asChild>
        <button
          aria-label="Activity"
          className="relative grid h-9 w-9 cursor-pointer place-items-center rounded-md border border-line text-fg-muted transition-colors hover:border-line-bright hover:text-fg focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-azure/50"
        >
          <Activity className="h-4 w-4" />
          {active && <span className="absolute -right-0.5 -top-0.5 h-2.5 w-2.5 rounded-full bg-signal shadow-[0_0_8px_var(--color-signal)]" />}
        </button>
      </DropdownPrimitive.Trigger>
      <DropdownPrimitive.Portal>
        <DropdownPrimitive.Content
          align="end"
          sideOffset={8}
          className="z-50 w-[360px] overflow-hidden rounded-xl border border-line bg-ink-900 shadow-[0_24px_60px_-20px_rgba(0,0,0,0.6)] data-[state=open]:animate-[fadeIn_120ms_ease-out]"
        >
          <div className="flex items-center justify-between border-b border-line px-4 py-3">
            <span className="font-display text-sm font-semibold text-fg">Activity</span>
            <span className="flex items-center gap-1.5 font-mono text-[10px] uppercase tracking-wider text-fg-faint">
              <span className={"led " + (active ? "led-signal led-pulse" : "led-idle")} />
              live
            </span>
          </div>

          <div className="max-h-[60vh] overflow-y-auto">
            {events.length === 0 ? (
              <div className="px-4 py-10 text-center text-sm text-fg-muted">No activity yet. Operations you run appear here live.</div>
            ) : (
              <ul className="divide-y divide-line/60">
                {events.map((e) => (
                  <ActivityRow key={e.id} event={e} />
                ))}
              </ul>
            )}
          </div>

          <DropdownPrimitive.Item asChild>
            <Link
              href="/events"
              className="flex items-center justify-center gap-1.5 border-t border-line px-4 py-2.5 font-mono text-[11px] uppercase tracking-wider text-azure outline-none transition-colors hover:bg-ink-800"
            >
              View full timeline <ArrowUpRight className="h-3 w-3" />
            </Link>
          </DropdownPrimitive.Item>
        </DropdownPrimitive.Content>
      </DropdownPrimitive.Portal>
    </DropdownPrimitive.Root>
  );
}

function ActivityRow({ event: e }: { event: EventItem }) {
  const status = eventStatus(e.type);
  const href = e.instance_id ? `/instances/${e.instance_id}` : e.cluster_id ? `/clusters/${e.cluster_id}` : undefined;
  const body = (
    <div className="flex items-start gap-2.5 px-4 py-2.5">
      <span className={"led mt-1.5 shrink-0 " + ledFor[status]} />
      <div className="min-w-0 flex-1">
        <div className="flex items-center justify-between gap-2">
          <span className="truncate font-mono text-[11px] text-fg-muted">{e.type}</span>
          <time className="shrink-0 font-mono text-[10px] text-fg-faint tnum" dateTime={e.created_at}>
            {timeAgo(e.created_at)}
          </time>
        </div>
        <p className="mt-0.5 line-clamp-2 text-xs text-fg">{e.message}</p>
      </div>
    </div>
  );
  return (
    <li className="transition-colors hover:bg-ink-800/60">
      {href ? (
        <DropdownPrimitive.Item asChild>
          <Link href={href} className="block outline-none">
            {body}
          </Link>
        </DropdownPrimitive.Item>
      ) : (
        body
      )}
    </li>
  );
}
