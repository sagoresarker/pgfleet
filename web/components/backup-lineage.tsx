"use client";

import { type Backup } from "@/lib/api";
import { formatBytes } from "@/lib/utils";
import { ArrowRight } from "lucide-react";
import { Badge, Card, CardBody, CardHeader, CardTitle } from "./ui";

/**
 * BackupLineage visualizes the pgBackRest reference chain. A diff/incr backup's
 * label is "<fullLabel>_<timestamp>D|I", so the prefix before "_" names the full
 * backup it descends from — that lets us reconstruct each generation (a full and
 * everything that builds on it) precisely from the catalog, without extra API
 * data. Generations are shown newest-first; within one, the full is the root and
 * diffs/incrs flow left-to-right in time order.
 */
export function BackupLineage({ backups }: { backups: Backup[] }) {
  if (backups.length === 0) return null;

  const generations = new Map<string, Backup[]>();
  for (const b of backups) {
    const key = b.label.split("_")[0]; // the full backup's label
    const list = generations.get(key) ?? [];
    list.push(b);
    generations.set(key, list);
  }
  // Newest generation first; chain within a generation oldest → newest.
  const ordered = [...generations.entries()]
    .map(([key, list]) => [key, [...list].sort((a, b) => a.label.localeCompare(b.label))] as const)
    .sort((a, b) => b[0].localeCompare(a[0]));

  return (
    <Card>
      <CardHeader>
        <CardTitle>Backup lineage</CardTitle>
        <span className="font-mono text-[11px] text-fg-faint">
          {ordered.length} generation{ordered.length === 1 ? "" : "s"}
        </span>
      </CardHeader>
      <CardBody className="space-y-4">
        {ordered.map(([key, chain]) => (
          <div key={key} className="rounded-lg border border-line bg-ink-850 p-3">
            <div className="mb-2.5 font-mono text-[10px] uppercase tracking-wider text-fg-faint">
              generation · {formatLabelTime(key)}
            </div>
            <div className="flex flex-wrap items-stretch gap-1.5">
              {chain.map((b, i) => (
                <div key={b.id} className="flex items-stretch gap-1.5">
                  {i > 0 && (
                    <span className="flex items-center text-fg-faint" aria-hidden="true">
                      <ArrowRight className="h-3.5 w-3.5" />
                    </span>
                  )}
                  <LineageNode backup={b} />
                </div>
              ))}
            </div>
          </div>
        ))}
      </CardBody>
    </Card>
  );
}

function LineageNode({ backup: b }: { backup: Backup }) {
  const tone = b.error ? "danger" : b.type === "full" ? "azure" : b.type === "diff" ? "violet" : "neutral";
  return (
    <div
      className={
        "min-w-[120px] rounded-md border bg-ink-900 px-2.5 py-2 " +
        (b.error ? "border-danger/40" : b.type === "full" ? "border-azure/40" : "border-line")
      }
    >
      <div className="flex items-center justify-between gap-2">
        <Badge tone={tone}>{b.type}</Badge>
        {b.annotations?.name && (
          <span className="truncate rounded border border-violet/30 bg-violet/10 px-1 py-0.5 font-mono text-[9px] text-violet">
            {b.annotations.name}
          </span>
        )}
      </div>
      <div className="mt-1.5 font-mono text-[10px] text-fg-muted">{formatLabelTime(ownTime(b.label))}</div>
      <div className="font-mono text-[11px] text-fg tnum">{formatBytes(b.repo_size)}</div>
    </div>
  );
}

// ownTime returns the timestamp segment for this backup's own label (the part
// after "_" for diff/incr, or the whole label for a full).
function ownTime(label: string): string {
  const parts = label.split("_");
  return parts[parts.length - 1];
}

// formatLabelTime turns a pgBackRest label segment (YYYYMMDD-HHMMSS<suffix>)
// into a readable "Mon DD, HH:MM" string. Falls back to the raw segment.
function formatLabelTime(seg: string): string {
  const m = seg.match(/^(\d{4})(\d{2})(\d{2})-(\d{2})(\d{2})(\d{2})/);
  if (!m) return seg;
  const [, y, mo, d, h, mi] = m;
  const months = ["Jan", "Feb", "Mar", "Apr", "May", "Jun", "Jul", "Aug", "Sep", "Oct", "Nov", "Dec"];
  const month = months[Number(mo) - 1] ?? mo;
  return `${month} ${Number(d)}, ${h}:${mi}`;
}
