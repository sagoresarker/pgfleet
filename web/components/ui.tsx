"use client";

import { cn } from "@/lib/utils";
import { Slot } from "@radix-ui/react-slot";
import { Eye, EyeOff } from "lucide-react";
import {
  createContext,
  forwardRef,
  useCallback,
  useContext,
  useRef,
  useState,
  type ButtonHTMLAttributes,
  type HTMLAttributes,
  type InputHTMLAttributes,
  type ReactNode,
} from "react";

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
  ButtonHTMLAttributes<HTMLButtonElement> & {
    variant?: ButtonVariant;
    size?: ButtonSize;
    asChild?: boolean;
    loading?: boolean;
  }
>(({ className, variant = "primary", size = "md", asChild, loading, disabled, children, ...props }, ref) => {
  const Comp = asChild ? Slot : "button";
  const isDisabled = disabled || loading;
  return (
    <Comp
      ref={ref}
      // active:scale gives tactile press feedback (scale-feedback rule) without
      // shifting layout; cursor-pointer + visible focus ring for accessibility.
      className={cn(
        "inline-flex cursor-pointer items-center justify-center gap-2 rounded-md font-display tracking-tight transition-all duration-150 active:scale-[0.98] disabled:cursor-not-allowed disabled:opacity-40 disabled:active:scale-100 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-azure/50 focus-visible:ring-offset-1 focus-visible:ring-offset-ink-950",
        size === "sm" ? "h-8 px-3 text-xs" : "h-10 px-4 text-sm",
        buttonVariants[variant],
        className
      )}
      aria-busy={loading || undefined}
      {...(asChild ? {} : { disabled: isDisabled })}
      {...props}
    >
      {loading && <Spinner className="h-3.5 w-3.5 border-current/30 border-t-current" />}
      {children}
    </Comp>
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

/* ---- Skeleton ---- *
 * Shimmer placeholders. The UX guidance prefers skeletons over blocking
 * spinners for anything that can take >300ms (a fleet/list/metrics fetch), so
 * the layout is reserved and there is no content jump when data arrives. */
export function Skeleton({ className }: { className?: string }) {
  return <div className={cn("animate-pulse rounded-md bg-ink-700/70", className)} aria-hidden="true" />;
}

export function SkeletonRows({ rows = 4, className }: { rows?: number; className?: string }) {
  return (
    <div className={cn("space-y-2", className)} role="status" aria-label="Loading">
      {Array.from({ length: rows }).map((_, i) => (
        <Skeleton key={i} className="h-12 w-full" />
      ))}
    </div>
  );
}

/* ---- EmptyState ---- *
 * A helpful zero-data state: icon + what's missing + the action to fix it,
 * instead of a blank panel that reads as "broken" to an operator. */
export function EmptyState({
  icon,
  title,
  description,
  action,
  className,
}: {
  icon?: ReactNode;
  title: string;
  description?: string;
  action?: ReactNode;
  className?: string;
}) {
  return (
    <div className={cn("flex flex-col items-center justify-center px-6 py-12 text-center", className)}>
      {icon && (
        <div className="mb-4 grid h-12 w-12 place-items-center rounded-full border border-line bg-ink-800 text-fg-faint">
          {icon}
        </div>
      )}
      <h3 className="font-display text-sm font-medium tracking-tight text-fg">{title}</h3>
      {description && <p className="mt-1 max-w-sm text-sm text-fg-muted">{description}</p>}
      {action && <div className="mt-5">{action}</div>}
    </div>
  );
}

/* ---- Tooltip ---- *
 * Lightweight CSS tooltip for icon-only controls (keyboard-reachable via the
 * wrapping element's focus). Not a replacement for an aria-label, which icon
 * buttons must still carry. */
export function Tooltip({ label, children, side = "top" }: { label: string; children: ReactNode; side?: "top" | "bottom" }) {
  return (
    <span className="group/tt relative inline-flex">
      {children}
      <span
        role="tooltip"
        className={cn(
          "pointer-events-none absolute left-1/2 z-50 -translate-x-1/2 whitespace-nowrap rounded-md border border-line-bright bg-ink-900 px-2 py-1 text-[11px] text-fg opacity-0 shadow-lg transition-opacity duration-150 group-hover/tt:opacity-100 group-focus-within/tt:opacity-100",
          side === "top" ? "bottom-full mb-1.5" : "top-full mt-1.5"
        )}
      >
        {label}
      </span>
    </span>
  );
}

/* ---- Toast ---- *
 * Non-blocking, auto-dismissing feedback for async actions (backup started,
 * clone failed, …). aria-live=polite so screen readers announce without
 * stealing focus (toast-accessibility rule). */
type ToastTone = "healthy" | "danger" | "azure";
type ToastItem = { id: number; tone: ToastTone; message: string };
type ToastCtx = { push: (message: string, tone?: ToastTone) => void };

const ToastContext = createContext<ToastCtx | null>(null);

export function ToastProvider({ children }: { children: ReactNode }) {
  const [items, setItems] = useState<ToastItem[]>([]);
  const seq = useRef(0);

  const push = useCallback((message: string, tone: ToastTone = "azure") => {
    const id = ++seq.current;
    setItems((cur) => [...cur, { id, tone, message }]);
    setTimeout(() => setItems((cur) => cur.filter((t) => t.id !== id)), 4000);
  }, []);

  return (
    <ToastContext.Provider value={{ push }}>
      {children}
      <div
        className="pointer-events-none fixed bottom-4 right-4 z-[1000] flex w-full max-w-sm flex-col gap-2"
        aria-live="polite"
        aria-atomic="false"
      >
        {items.map((t) => (
          <div
            key={t.id}
            className={cn(
              "pointer-events-auto flex items-start gap-2.5 rounded-lg border bg-ink-900 px-4 py-3 text-sm shadow-[0_12px_32px_-12px_rgba(15,31,51,0.3)] rise",
              t.tone === "healthy" && "border-healthy/30",
              t.tone === "danger" && "border-danger/30",
              t.tone === "azure" && "border-azure/30"
            )}
          >
            <span
              className={cn(
                "led mt-1.5",
                t.tone === "healthy" && "led-healthy",
                t.tone === "danger" && "led-danger",
                t.tone === "azure" && "led-signal"
              )}
            />
            <span className="text-fg-muted">{t.message}</span>
          </div>
        ))}
      </div>
    </ToastContext.Provider>
  );
}

export function useToast(): ToastCtx {
  const ctx = useContext(ToastContext);
  // Fall back to a no-op so a component can call useToast() even if it is ever
  // rendered outside the provider (e.g. in a unit test) without crashing.
  return ctx ?? { push: () => {} };
}

/* ---- PasswordInput ---- *
 * Text input with a show/hide toggle (password-toggle rule) + autocomplete. */
export function PasswordInput({
  className,
  autoComplete = "current-password",
  ...props
}: InputHTMLAttributes<HTMLInputElement>) {
  const [show, setShow] = useState(false);
  return (
    <div className="relative">
      <Input
        type={show ? "text" : "password"}
        autoComplete={autoComplete}
        className={cn("pr-10", className)}
        {...props}
      />
      <button
        type="button"
        onClick={() => setShow((s) => !s)}
        aria-label={show ? "Hide password" : "Show password"}
        className="absolute right-2 top-1/2 -translate-y-1/2 cursor-pointer rounded p-1 text-fg-faint transition-colors hover:text-fg-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-azure/50"
      >
        {show ? <EyeOff className="h-4 w-4" /> : <Eye className="h-4 w-4" />}
      </button>
    </div>
  );
}
