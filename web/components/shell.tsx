"use client";

import { useAuth } from "@/lib/auth";
import { cn } from "@/lib/utils";
import {
  Activity,
  Bell,
  Boxes,
  ClipboardList,
  Database,
  DownloadCloud,
  LogOut,
  Menu,
  Network,
  ScrollText,
  ShieldCheck,
  Users,
  X,
} from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, useState, type ReactNode } from "react";
import { Logo } from "./logo";
import { Badge, Spinner } from "./ui";

const nav = [
  { href: "/dashboard", label: "Fleet", icon: Boxes },
  { href: "/instances", label: "Instances", icon: Database },
  { href: "/clusters", label: "Clusters", icon: Network },
  { href: "/remote", label: "Remote backup", icon: DownloadCloud },
  { href: "/health", label: "Reliability", icon: ShieldCheck },
  { href: "/alerts", label: "Alerts", icon: Bell },
  { href: "/events", label: "Events", icon: ScrollText },
  { href: "/users", label: "Access", icon: Users },
  { href: "/audit", label: "Audit log", icon: ClipboardList },
];

function NavItems({ pathname, onNavigate }: { pathname: string; onNavigate?: () => void }) {
  return (
    <nav className="flex-1 space-y-0.5 px-3">
      {nav.map((item) => {
        const active = pathname === item.href || pathname.startsWith(item.href + "/");
        const Icon = item.icon;
        return (
          <Link
            key={item.href}
            href={item.href}
            onClick={onNavigate}
            aria-current={active ? "page" : undefined}
            className={cn(
              "group flex items-center gap-3 rounded-md px-3 py-2 text-sm transition-colors",
              active ? "bg-azure/10 text-azure" : "text-fg-muted hover:bg-ink-700/50 hover:text-fg"
            )}
          >
            <Icon className={cn("h-4 w-4 shrink-0", active ? "text-azure" : "text-fg-faint group-hover:text-fg-muted")} />
            <span className="font-display tracking-tight">{item.label}</span>
            {active && <span className="ml-auto h-1.5 w-1.5 rounded-full bg-azure shadow-[0_0_8px_var(--color-azure)]" />}
          </Link>
        );
      })}
    </nav>
  );
}

function UserCard({ email, role, onLogout }: { email: string; role: string; onLogout: () => void }) {
  return (
    <div className="border-t border-line p-3">
      <div className="flex items-center gap-3 rounded-md px-2 py-2">
        <div className="grid h-8 w-8 shrink-0 place-items-center rounded-full border border-line-bright bg-ink-800 font-mono text-xs uppercase text-azure">
          {email.slice(0, 2)}
        </div>
        <div className="min-w-0 flex-1">
          <div className="truncate text-xs text-fg" title={email}>
            {email}
          </div>
          <Badge tone="azure" className="mt-0.5">
            {role}
          </Badge>
        </div>
        <button
          onClick={onLogout}
          className="cursor-pointer rounded p-1.5 text-fg-faint transition-colors hover:text-danger focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-danger/40"
          aria-label="Sign out"
        >
          <LogOut className="h-4 w-4" />
        </button>
      </div>
    </div>
  );
}

export function AppShell({ children }: { children: ReactNode }) {
  const { user, loading, logout } = useAuth();
  const router = useRouter();
  const pathname = usePathname();
  const [mobileOpen, setMobileOpen] = useState(false);

  useEffect(() => {
    if (!loading && !user) router.replace("/login");
  }, [loading, user, router]);

  // Close the mobile drawer whenever the route changes.
  useEffect(() => setMobileOpen(false), [pathname]);

  if (loading || !user) {
    return (
      <div className="grid min-h-dvh place-items-center">
        <Spinner className="h-6 w-6" />
      </div>
    );
  }

  const doLogout = () => logout().then(() => router.replace("/login"));

  return (
    <div className="flex min-h-dvh">
      {/* Desktop sidebar */}
      <aside className="sticky top-0 hidden h-dvh w-60 shrink-0 flex-col border-r border-line bg-ink-900/70 backdrop-blur lg:flex">
        <div className="px-5 py-5">
          <Logo />
        </div>
        <NavItems pathname={pathname} />
        <UserCard email={user.email} role={user.role} onLogout={doLogout} />
      </aside>

      {/* Mobile drawer + scrim */}
      {mobileOpen && (
        <div className="fixed inset-0 z-50 lg:hidden">
          <button
            className="absolute inset-0 cursor-default bg-fg/40 backdrop-blur-sm"
            aria-label="Close navigation"
            onClick={() => setMobileOpen(false)}
          />
          <aside className="relative flex h-full w-64 max-w-[80vw] flex-col border-r border-line bg-ink-900 rise">
            <div className="flex items-center justify-between px-5 py-5">
              <Logo />
              <button
                onClick={() => setMobileOpen(false)}
                aria-label="Close navigation"
                className="cursor-pointer rounded p-1.5 text-fg-faint hover:text-fg"
              >
                <X className="h-5 w-5" />
              </button>
            </div>
            <NavItems pathname={pathname} onNavigate={() => setMobileOpen(false)} />
            <UserCard email={user.email} role={user.role} onLogout={doLogout} />
          </aside>
        </div>
      )}

      <div className="flex min-w-0 flex-1 flex-col">
        {/* Mobile top bar */}
        <header className="sticky top-0 z-40 flex items-center gap-3 border-b border-line bg-ink-900/80 px-4 py-3 backdrop-blur lg:hidden">
          <button
            onClick={() => setMobileOpen(true)}
            aria-label="Open navigation"
            aria-expanded={mobileOpen}
            className="cursor-pointer rounded-md p-1.5 text-fg-muted hover:bg-ink-700/50 hover:text-fg"
          >
            <Menu className="h-5 w-5" />
          </button>
          <Logo />
        </header>

        <main className="min-w-0 flex-1">
          <div className="mx-auto max-w-6xl px-5 py-6 sm:px-8 sm:py-8">{children}</div>
        </main>
      </div>
    </div>
  );
}

export function PageHeader({ title, subtitle, action }: { title: string; subtitle?: string; action?: ReactNode }) {
  return (
    <header className="mb-8 flex flex-col gap-4 sm:flex-row sm:items-end sm:justify-between">
      <div>
        <div className="mb-1 flex items-center gap-2 font-mono text-[10px] uppercase tracking-[0.2em] text-fg-faint">
          <Activity className="h-3 w-3 text-azure" />
          pgfleet
        </div>
        <h1 className="font-display text-2xl font-semibold tracking-tight text-fg">{title}</h1>
        {subtitle && <p className="mt-1 text-sm text-fg-muted">{subtitle}</p>}
      </div>
      {action && <div className="shrink-0">{action}</div>}
    </header>
  );
}
