import { cn } from "@/lib/utils";

export function Logo({ className }: { className?: string }) {
  return (
    <div className={cn("flex items-center gap-2.5", className)}>
      <div className="relative grid h-8 w-8 place-items-center overflow-hidden rounded-md border border-azure/30 bg-ink-800">
        <div className="absolute inset-0 scanline" />
        {/* stacked-disk fleet mark */}
        <svg width="18" height="18" viewBox="0 0 18 18" fill="none" className="relative">
          <ellipse cx="9" cy="4.5" rx="6" ry="2.4" stroke="var(--color-azure)" strokeWidth="1.2" />
          <path d="M3 4.5v4c0 1.3 2.7 2.4 6 2.4s6-1.1 6-2.4v-4" stroke="var(--color-azure)" strokeWidth="1.2" opacity="0.7" />
          <path d="M3 8.5v4c0 1.3 2.7 2.4 6 2.4s6-1.1 6-2.4v-4" stroke="var(--color-azure-bright)" strokeWidth="1.2" opacity="0.45" />
        </svg>
      </div>
      <div className="leading-none">
        <div className="font-display text-sm font-semibold tracking-tight text-fg">PgFleet</div>
        <div className="font-mono text-[9px] uppercase tracking-[0.2em] text-fg-faint">control plane</div>
      </div>
    </div>
  );
}
