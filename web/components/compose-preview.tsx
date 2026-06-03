"use client";

import { api } from "@/lib/api";
import { useQuery } from "@tanstack/react-query";
import { Check, Copy, Download } from "lucide-react";
import { useState } from "react";
import { Button, Modal, useToast } from "./ui";

/**
 * ComposePreview shows the generated docker-compose.yml inline (with copy +
 * download) so an operator can read exactly what will run before saving it,
 * instead of downloading blind.
 */
export function ComposePreview({
  kind,
  id,
  name,
  open,
  onOpenChange,
}: {
  kind: "instance" | "cluster";
  id: string;
  name: string;
  open: boolean;
  onOpenChange: (o: boolean) => void;
}) {
  const toast = useToast();
  const [copied, setCopied] = useState(false);
  const q = useQuery({
    queryKey: ["compose", kind, id],
    enabled: open,
    queryFn: () => (kind === "instance" ? api.previewInstanceCompose(id) : api.previewClusterCompose(id)),
  });

  async function copy() {
    if (!q.data) return;
    // navigator.clipboard is undefined on insecure (non-HTTPS, non-localhost)
    // origins — guard so the button doesn't throw an unhandled rejection.
    if (!navigator.clipboard) {
      toast.push("Clipboard unavailable here — select the text to copy", "danger");
      return;
    }
    try {
      await navigator.clipboard.writeText(q.data);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
      toast.push("Compose file copied", "healthy");
    } catch {
      toast.push("Copy failed", "danger");
    }
  }

  async function download() {
    try {
      await (kind === "instance" ? api.exportInstanceCompose(id, name) : api.exportClusterCompose(id, name));
      toast.push("docker-compose.yml downloaded", "healthy");
    } catch (e) {
      toast.push(e instanceof Error ? e.message : "Download failed", "danger");
    }
  }

  return (
    <Modal
      open={open}
      onOpenChange={onOpenChange}
      size="lg"
      title="docker-compose.yml"
      description={`Run ${name} yourself — set POSTGRES_PASSWORD in a .env file beside this file, then \`docker compose up -d\`.`}
      footer={
        <>
          <Button variant="ghost" size="sm" onClick={copy} disabled={!q.data}>
            {copied ? <Check className="h-4 w-4 text-healthy" /> : <Copy className="h-4 w-4" />}
            {copied ? "Copied" : "Copy"}
          </Button>
          <Button size="sm" onClick={download} disabled={!q.data}>
            <Download className="h-4 w-4" /> Download
          </Button>
        </>
      }
    >
      {q.isLoading ? (
        <div className="h-64 animate-pulse rounded-md bg-ink-700/70" aria-hidden="true" />
      ) : q.error ? (
        <p className="rounded-md border border-danger/30 bg-danger/10 px-3 py-2 text-sm text-danger">
          {q.error instanceof Error ? q.error.message : "Could not generate the compose file."}
        </p>
      ) : (
        <pre className="max-h-[60vh] overflow-auto rounded-md border border-line bg-ink-850 p-4 font-mono text-xs leading-relaxed text-fg-muted">
          <code>{q.data}</code>
        </pre>
      )}
    </Modal>
  );
}
