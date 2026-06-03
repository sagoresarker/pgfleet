"use client";

import { cn } from "@/lib/utils";
import * as DialogPrimitive from "@radix-ui/react-dialog";
import * as DropdownPrimitive from "@radix-ui/react-dropdown-menu";
import { Slot } from "@radix-ui/react-slot";
import { Eye, EyeOff, X } from "lucide-react";
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
      {/* When asChild, Radix Slot requires EXACTLY ONE child element, so the
          consumer's single child is passed straight through (no injected spinner
          sibling — that would trigger React.Children.only). The loading spinner
          only applies to real <button> usage. */}
      {asChild ? (
        children
      ) : (
        <>
          {loading && <Spinner className="h-3.5 w-3.5 border-current/30 border-t-current" />}
          {children}
        </>
      )}
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

/* ---- Modal ---- *
 * A consistent Radix Dialog: focus-trapped, Esc/scrim-dismiss, animated, with a
 * titled header + close affordance and an optional footer action row. Used for
 * every operation that needs a form or confirmation (clone, restore, destroy, …)
 * instead of inline expand-in-place controls. */
const modalSizes = { sm: "max-w-sm", md: "max-w-lg", lg: "max-w-2xl" } as const;

export function Modal({
  open,
  onOpenChange,
  title,
  description,
  trigger,
  children,
  footer,
  size = "md",
}: {
  open?: boolean;
  onOpenChange?: (open: boolean) => void;
  title: string;
  description?: string;
  trigger?: ReactNode;
  children?: ReactNode;
  footer?: ReactNode;
  size?: keyof typeof modalSizes;
}) {
  return (
    <DialogPrimitive.Root open={open} onOpenChange={onOpenChange}>
      {trigger && <DialogPrimitive.Trigger asChild>{trigger}</DialogPrimitive.Trigger>}
      <DialogPrimitive.Portal>
        <DialogPrimitive.Overlay className="fixed inset-0 z-50 bg-[#0f1f33]/45 backdrop-blur-sm data-[state=open]:animate-[fadeIn_150ms_ease-out]" />
        <DialogPrimitive.Content
          className={cn(
            "fixed left-1/2 top-1/2 z-50 w-[calc(100vw-2rem)] -translate-x-1/2 -translate-y-1/2 rounded-xl border border-line bg-ink-900 shadow-[0_40px_80px_-32px_rgba(15,31,51,0.3)] focus:outline-none data-[state=open]:animate-[modalIn_180ms_cubic-bezier(0.22,1,0.36,1)]",
            modalSizes[size]
          )}
        >
          <div className="flex items-start justify-between gap-4 border-b border-line px-6 py-4">
            <div className="min-w-0">
              <DialogPrimitive.Title className="font-display text-base font-semibold tracking-tight text-fg">
                {title}
              </DialogPrimitive.Title>
              {description && (
                <DialogPrimitive.Description className="mt-1 text-sm text-fg-muted">{description}</DialogPrimitive.Description>
              )}
            </div>
            <DialogPrimitive.Close
              className="-mr-1 shrink-0 cursor-pointer rounded-md p-1.5 text-fg-faint transition-colors hover:bg-ink-700/60 hover:text-fg focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-azure/50"
              aria-label="Close"
            >
              <X className="h-4 w-4" />
            </DialogPrimitive.Close>
          </div>
          {children && <div className="px-6 py-5">{children}</div>}
          {footer && <div className="flex items-center justify-end gap-2 border-t border-line px-6 py-4">{footer}</div>}
        </DialogPrimitive.Content>
      </DialogPrimitive.Portal>
    </DialogPrimitive.Root>
  );
}

/* Convenience wrapper for a destructive/confirmation modal. */
export function ConfirmDialog({
  open,
  onOpenChange,
  title,
  description,
  confirmLabel = "Confirm",
  cancelLabel = "Cancel",
  danger,
  loading,
  onConfirm,
  children,
}: {
  open: boolean;
  onOpenChange: (open: boolean) => void;
  title: string;
  description?: string;
  confirmLabel?: string;
  cancelLabel?: string;
  danger?: boolean;
  loading?: boolean;
  onConfirm: () => void;
  children?: ReactNode;
}) {
  return (
    <Modal
      open={open}
      onOpenChange={onOpenChange}
      title={title}
      description={description}
      size="sm"
      footer={
        <>
          <Button variant="ghost" size="sm" onClick={() => onOpenChange(false)} disabled={loading}>
            {cancelLabel}
          </Button>
          <Button variant={danger ? "danger" : "primary"} size="sm" loading={loading} onClick={onConfirm}>
            {confirmLabel}
          </Button>
        </>
      }
    >
      {children}
    </Modal>
  );
}

/* ---- ActionMenu (dropdown) ---- *
 * Collapses secondary/overflow actions into one "⋯ Actions" menu so toolbars
 * stay uncluttered (overflow-menu rule). Keyboard-navigable via Radix. */
export function ActionMenu({
  trigger,
  children,
  align = "end",
}: {
  trigger: ReactNode;
  children: ReactNode;
  align?: "start" | "end";
}) {
  return (
    <DropdownPrimitive.Root>
      <DropdownPrimitive.Trigger asChild>{trigger}</DropdownPrimitive.Trigger>
      <DropdownPrimitive.Portal>
        <DropdownPrimitive.Content
          align={align}
          sideOffset={6}
          className="z-50 min-w-48 rounded-lg border border-line bg-ink-900 p-1 shadow-[0_20px_44px_-16px_rgba(15,31,51,0.28)] data-[state=open]:animate-[fadeIn_120ms_ease-out]"
        >
          {children}
        </DropdownPrimitive.Content>
      </DropdownPrimitive.Portal>
    </DropdownPrimitive.Root>
  );
}

export function ActionMenuItem({
  icon,
  children,
  onSelect,
  danger,
  disabled,
}: {
  icon?: ReactNode;
  children: ReactNode;
  onSelect?: () => void;
  danger?: boolean;
  disabled?: boolean;
}) {
  return (
    <DropdownPrimitive.Item
      disabled={disabled}
      onSelect={(e) => {
        e.preventDefault();
        onSelect?.();
      }}
      className={cn(
        "flex cursor-pointer items-center gap-2.5 rounded-md px-2.5 py-2 text-sm text-fg-muted outline-none transition-colors data-[highlighted]:bg-ink-700/60 data-[highlighted]:text-fg data-[disabled]:cursor-not-allowed data-[disabled]:opacity-40",
        danger && "text-danger data-[highlighted]:bg-danger/10 data-[highlighted]:text-danger"
      )}
    >
      {icon && <span className="text-fg-faint">{icon}</span>}
      {children}
    </DropdownPrimitive.Item>
  );
}

export function ActionMenuSeparator() {
  return <DropdownPrimitive.Separator className="my-1 h-px bg-line" />;
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
