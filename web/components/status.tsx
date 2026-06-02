import { Badge, StatusLed } from "./ui";
import type { Instance } from "@/lib/api";

type LedStatus = "healthy" | "signal" | "danger" | "idle";

const statusMap: Record<Instance["status"], { led: LedStatus; tone: "healthy" | "signal" | "danger" | "neutral" | "azure"; pulse?: boolean }> = {
  running: { led: "healthy", tone: "healthy" },
  provisioning: { led: "signal", tone: "signal", pulse: true },
  restoring: { led: "signal", tone: "azure", pulse: true },
  stopped: { led: "idle", tone: "neutral" },
  error: { led: "danger", tone: "danger" },
  destroying: { led: "danger", tone: "danger", pulse: true },
};

export function InstanceStatus({ status }: { status: Instance["status"] }) {
  const s = statusMap[status];
  return (
    <Badge tone={s.tone}>
      <StatusLed status={s.led} pulse={s.pulse} />
      {status}
    </Badge>
  );
}
