"use client";

import { Card, CardBody, CardHeader, CardTitle, Spinner } from "@/components/ui";
import { api } from "@/lib/api";
import { useQuery } from "@tanstack/react-query";

export function LogsTab({ id, running }: { id: string; running: boolean }) {
  const logs = useQuery({
    queryKey: ["logs", id],
    queryFn: () => api.instanceLogs(id),
    refetchInterval: 5000,
    enabled: running,
  });

  if (!running) {
    return (
      <p className="rounded-md border border-line bg-ink-850 px-4 py-8 text-center text-sm text-fg-muted">
        Logs are available while the instance is running.
      </p>
    );
  }

  return (
    <Card>
      <CardHeader>
        <CardTitle>Container logs · tail</CardTitle>
        {logs.isFetching && <Spinner />}
      </CardHeader>
      <CardBody className="p-0">
        <pre className="max-h-[520px] overflow-auto rounded-b-lg bg-ink-800 px-4 py-3 font-mono text-[11px] leading-relaxed text-fg">
          {logs.data?.logs?.trim() || "No log output yet."}
        </pre>
      </CardBody>
    </Card>
  );
}
