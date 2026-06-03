import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";
import { BackupLineage } from "@/components/backup-lineage";
import type { Backup } from "@/lib/api";

function bk(label: string, type: string, extra: Partial<Backup> = {}): Backup {
  return {
    id: label,
    label,
    type,
    repo_size: 1024,
    logical_size: 2048,
    wal_start: "",
    wal_stop: "",
    error: false,
    ...extra,
  };
}

describe("BackupLineage", () => {
  it("renders nothing with no backups", () => {
    const { container } = render(<BackupLineage backups={[]} />);
    expect(container.firstChild).toBeNull();
  });

  it("groups diffs/incrs under their parent full into generations", () => {
    const backups = [
      bk("20260101-000000F", "full"),
      bk("20260101-000000F_20260101-060000D", "diff"),
      bk("20260101-000000F_20260101-120000I", "incr"),
      bk("20260102-000000F", "full"), // a second generation
    ];
    render(<BackupLineage backups={backups} />);
    // Two generations (two full backups → two generation cards).
    expect(screen.getByText(/2 generations/)).toBeDefined();
    // Each backup type chip is rendered.
    expect(screen.getAllByText("full")).toHaveLength(2);
    expect(screen.getByText("diff")).toBeDefined();
    expect(screen.getByText("incr")).toBeDefined();
  });

  it("shows a backup's name annotation", () => {
    render(<BackupLineage backups={[bk("20260101-000000F", "full", { annotations: { name: "pre-upgrade" } })]} />);
    expect(screen.getByText("pre-upgrade")).toBeDefined();
    expect(screen.getByText(/1 generation/)).toBeDefined();
  });
});
