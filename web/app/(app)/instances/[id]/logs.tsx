"use client";

import { EmptyState } from "@/components/ui";
import { api } from "@/lib/api";
import { useQuery } from "@tanstack/react-query";
import { ScrollText } from "lucide-react";

export function LogsTab({ id, running }: { id: string; running: boolean }) {
  const logs = useQuery({
    queryKey: ["logs", id],
    queryFn: () => api.instanceLogs(id),
    refetchInterval: 5000,
    enabled: running,
  });

  if (!running) {
    return (
      <EmptyState
        icon={<ScrollText className="h-5 w-5" />}
        title="Logs unavailable"
        description="Container logs stream while the instance is running."
      />
    );
  }

  const text = logs.data?.logs?.trim();
  return (
    <div className="overflow-hidden rounded-xl border border-line shadow-sm">
      <div className="flex items-center justify-between border-b border-[#1c2940] bg-[#0e1726] px-3.5 py-2 font-mono text-[11px] text-[#8a97ad]">
        <span className="flex items-center gap-2">
          <ScrollText className="h-3.5 w-3.5" /> container logs · tail
        </span>
        <span className="flex items-center gap-1.5">
          <span className={"led " + (logs.isFetching ? "led-signal led-pulse" : "led-healthy")} />
          {logs.isFetching ? "streaming" : "live"}
        </span>
      </div>
      <pre className="max-h-[520px] overflow-auto whitespace-pre-wrap bg-[#0b1320] px-4 py-3 font-mono text-[11px] leading-relaxed text-[#cdd9ea]">
        {text || <span className="text-[#475569]">No log output yet.</span>}
      </pre>
    </div>
  );
}
