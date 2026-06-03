import { cn } from "@/lib/utils";
import { Slot } from "@radix-ui/react-slot";
import { forwardRef, type ButtonHTMLAttributes, type HTMLAttributes, type InputHTMLAttributes } from "react";

/* ---- Button ---- */
type ButtonVariant = "primary" | "ghost" | "outline" | "danger";
type ButtonSize = "sm" | "md";

const buttonVariants: Record<ButtonVariant, string> = {
  primary:
    "bg-azure text-white hover:bg-azure-bright shadow-[0_1px_2px_rgba(37,99,235,0.25),0_8px_20px_-10px_rgba(37,99,235,0.45)] font-medium",
  ghost: "text-fg-muted hover:text-fg hover:bg-ink-700",
  outline: "border border-line-bright text-fg hover:border-azure/60 hover:text-azure bg-ink-900",
  danger: "border border-danger/40 text-danger hover:bg-danger/10",
};

export const Button = forwardRef<
  HTMLButtonElement,
  ButtonHTMLAttributes<HTMLButtonElement> & { variant?: ButtonVariant; size?: ButtonSize; asChild?: boolean }
>(({ className, variant = "primary", size = "md", asChild, ...props }, ref) => {
  const Comp = asChild ? Slot : "button";
  return (
    <Comp
      ref={ref}
      className={cn(
        "inline-flex items-center justify-center gap-2 rounded-md font-display tracking-tight transition-all duration-150 disabled:opacity-40 disabled:pointer-events-none focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-azure/50",
        size === "sm" ? "h-8 px-3 text-xs" : "h-10 px-4 text-sm",
        buttonVariants[variant],
        className
      )}
      {...props}
    />
  );
});
Button.displayName = "Button";

/* ---- Card ---- */
export function Card({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return (
    <div
      className={cn(
        "rounded-lg border border-line bg-ink-900",
        "shadow-[0_1px_2px_rgba(15,31,51,0.04),0_12px_32px_-18px_rgba(15,31,51,0.16)]",
        className
      )}
      {...props}
    />
  );
}

export function CardHeader({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("flex items-center justify-between gap-3 border-b border-line px-5 py-4", className)} {...props} />;
}

export function CardTitle({ className, ...props }: HTMLAttributes<HTMLHeadingElement>) {
  return <h3 className={cn("font-display text-sm font-medium tracking-tight text-fg", className)} {...props} />;
}

export function CardBody({ className, ...props }: HTMLAttributes<HTMLDivElement>) {
  return <div className={cn("px-5 py-4", className)} {...props} />;
}

/* ---- Badge ---- */
type BadgeTone = "neutral" | "azure" | "healthy" | "signal" | "danger" | "violet";
const badgeTones: Record<BadgeTone, string> = {
  neutral: "border-line-bright text-fg-muted bg-ink-700/50",
  azure: "border-azure/30 text-azure bg-azure/10",
  healthy: "border-healthy/30 text-healthy bg-healthy/10",
  signal: "border-signal/30 text-signal bg-signal/10",
  danger: "border-danger/30 text-danger bg-danger/10",
  violet: "border-violet/30 text-violet bg-violet/10",
};

export function Badge({ tone = "neutral", className, ...props }: HTMLAttributes<HTMLSpanElement> & { tone?: BadgeTone }) {
  return (
    <span
      className={cn(
        "inline-flex items-center gap-1.5 rounded border px-2 py-0.5 font-mono text-[10px] uppercase tracking-wider",
        badgeTones[tone],
        className
      )}
      {...props}
    />
  );
}

/* ---- Status LED ---- */
export function StatusLed({ status, pulse }: { status: "healthy" | "signal" | "danger" | "idle"; pulse?: boolean }) {
  return <span className={cn("led", `led-${status}`, pulse && "led-pulse")} />;
}

/* ---- Input + Field ---- */
export const Input = forwardRef<HTMLInputElement, InputHTMLAttributes<HTMLInputElement>>(({ className, ...props }, ref) => (
  <input
    ref={ref}
    className={cn(
      "h-10 w-full rounded-md border border-line bg-ink-900 px-3 text-sm text-fg placeholder:text-fg-faint",
      "transition-colors focus:border-azure/60 focus:outline-none focus:ring-1 focus:ring-azure/40",
      className
    )}
    {...props}
  />
));
Input.displayName = "Input";

export function Field({ label, hint, children }: { label: string; hint?: string; children: React.ReactNode }) {
  return (
    <label className="block space-y-1.5">
      <span className="font-mono text-[11px] uppercase tracking-wider text-fg-muted">{label}</span>
      {children}
      {hint && <span className="block text-xs text-fg-faint">{hint}</span>}
    </label>
  );
}

export function Select({ className, ...props }: InputHTMLAttributes<HTMLSelectElement> & { children: React.ReactNode }) {
  return (
    <select
      className={cn(
        "h-10 w-full rounded-md border border-line bg-ink-900 px-3 text-sm text-fg",
        "focus:border-azure/60 focus:outline-none focus:ring-1 focus:ring-azure/40",
        className
      )}
      {...(props as React.SelectHTMLAttributes<HTMLSelectElement>)}
    />
  );
}

/* ---- Spinner ---- */
export function Spinner({ className }: { className?: string }) {
  return (
    <span
      className={cn("inline-block h-4 w-4 animate-spin rounded-full border-2 border-line-bright border-t-azure", className)}
    />
  );
}

/* ---- Stat ---- */
export function Stat({ label, value, sub, tone }: { label: string; value: string; sub?: string; tone?: BadgeTone }) {
  return (
    <div className="space-y-1">
      <div className="font-mono text-[10px] uppercase tracking-wider text-fg-faint">{label}</div>
      <div className={cn("font-display text-2xl tnum", tone === "signal" && "text-signal", tone === "danger" && "text-danger")}>
        {value}
      </div>
      {sub && <div className="text-xs text-fg-muted">{sub}</div>}
    </div>
  );
}
