import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { Badge, Button } from "@/components/ui";
import { InstanceStatus } from "@/components/status";
import { formatBytes, relativeTime } from "@/lib/utils";

describe("utils", () => {
  it("formats bytes", () => {
    expect(formatBytes(0)).toBe("0 B");
    expect(formatBytes(512)).toBe("512 B");
    expect(formatBytes(1024)).toBe("1.0 KB");
    expect(formatBytes(1048576)).toBe("1.0 MB");
  });

  it("formats bytes safely for edge cases (no 'NaN undefined')", () => {
    expect(formatBytes(-5)).toBe("0 B"); // negative
    expect(formatBytes(NaN)).toBe("0 B");
    expect(formatBytes(Infinity)).toBe("0 B");
    // Petabyte-scale must not index past the units table.
    expect(formatBytes(1024 ** 5)).toBe("1.0 PB");
    expect(formatBytes(1024 ** 6)).toBe("1.0 EB");
  });

  it("formats relative time", () => {
    const now = new Date().toISOString();
    expect(relativeTime(now)).toMatch(/s ago/);
    expect(relativeTime("not-a-date")).toBe("—");
  });
});

describe("Button", () => {
  it("renders its label", () => {
    render(<Button>Provision</Button>);
    expect(screen.getByRole("button", { name: "Provision" })).toBeDefined();
  });
});

describe("Badge", () => {
  it("renders children", () => {
    render(<Badge tone="healthy">running</Badge>);
    expect(screen.getByText("running")).toBeDefined();
  });
});

describe("InstanceStatus", () => {
  it("shows the status label", () => {
    render(<InstanceStatus status="running" />);
    expect(screen.getByText("running")).toBeDefined();
  });

  it("shows error status", () => {
    render(<InstanceStatus status="error" />);
    expect(screen.getByText("error")).toBeDefined();
  });
});
