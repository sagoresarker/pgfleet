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
  type LucideIcon,
} from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, useState, type ReactNode } from "react";
import { ActivityCenter } from "./activity-center";
import { Logo } from "./logo";
import { Spinner } from "./ui";

type NavItem = { href: string; label: string; icon: LucideIcon };
type NavGroup = { label: string; items: NavItem[] };

// Navigation is grouped by job-to-be-done so the panel reads like a product,
// not a flat link dump: what you run, how you operate it, who can touch it.
const navGroups: NavGroup[] = [
  {
    label: "Overview",
    items: [{ href: "/dashboard", label: "Fleet", icon: Boxes }],
  },
  {
    label: "Databases",
    items: [
      { href: "/instances", label: "Instances", icon: Database },
      { href: "/clusters", label: "Clusters", icon: Network },
    ],
  },
  {
    label: "Operations",
    items: [
      { href: "/monitoring", label: "Monitoring", icon: Activity },
      { href: "/health", label: "Reliability", icon: ShieldCheck },
      { href: "/alerts", label: "Alerts", icon: Bell },
      { href: "/events", label: "Events", icon: ScrollText },
    ],
  },
  {
    label: "Data",
    items: [{ href: "/remote", label: "Remote backup", icon: DownloadCloud }],
  },
  {
    label: "Administration",
    items: [
      { href: "/users", label: "Access", icon: Users },
      { href: "/audit", label: "Audit log", icon: ClipboardList },
    ],
  },
];

function NavItems({ pathname, onNavigate }: { pathname: string; onNavigate?: () => void }) {
  return (
    <nav className="flex-1 overflow-y-auto px-3 py-2">
      {navGroups.map((group) => (
        <div key={group.label} className="mb-1">
          <div className="px-3 pb-1 pt-4 font-mono text-[9px] uppercase tracking-[0.18em] text-fg-faint/80">
            {group.label}
          </div>
          <div className="space-y-0.5">
            {group.items.map((item) => {
              const active = pathname === item.href || pathname.startsWith(item.href + "/");
              const Icon = item.icon;
              return (
                <Link
                  key={item.href}
                  href={item.href}
                  onClick={onNavigate}
                  aria-current={active ? "page" : undefined}
                  className={cn(
                    "group relative flex items-center gap-3 rounded-md px-3 py-2 text-sm transition-colors",
                    active ? "bg-azure/10 text-azure" : "text-fg-muted hover:bg-ink-800 hover:text-fg"
                  )}
                >
                  {active && (
                    <span className="absolute left-0 top-1/2 h-5 w-0.5 -translate-y-1/2 rounded-full bg-azure shadow-[0_0_8px_var(--color-azure)]" />
                  )}
                  <Icon className={cn("h-4 w-4 shrink-0", active ? "text-azure" : "text-fg-faint group-hover:text-fg-muted")} />
                  <span className="font-display tracking-tight">{item.label}</span>
                </Link>
              );
            })}
          </div>
        </div>
      ))}
    </nav>
  );
}

function UserCard({ email, role, onLogout }: { email: string; role: string; onLogout: () => void }) {
  return (
    <div className="border-t border-line p-3">
      <div className="flex items-center gap-2.5 rounded-lg border border-line bg-ink-850 px-2.5 py-2">
        <div className="grid h-8 w-8 shrink-0 place-items-center rounded-full bg-gradient-to-br from-azure/30 to-violet/20 font-mono text-[11px] font-semibold uppercase text-azure ring-1 ring-azure/30">
          {email.slice(0, 2)}
        </div>
        <div className="min-w-0 flex-1">
          <div className="truncate text-xs text-fg" title={email}>
            {email}
          </div>
          <div className="mt-0.5 flex items-center gap-1.5 font-mono text-[10px] uppercase tracking-wider text-fg-faint">
            <span className="led led-healthy" />
            {role}
          </div>
        </div>
        <button
          onClick={onLogout}
          className="cursor-pointer rounded-md p-1.5 text-fg-faint transition-colors hover:bg-ink-700 hover:text-danger focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-danger/40"
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
      <aside className="sticky top-0 hidden h-dvh w-[248px] shrink-0 flex-col border-r border-line bg-ink-900/60 backdrop-blur-xl lg:flex">
        <div className="flex items-center justify-between gap-2 border-b border-line px-5 py-[18px]">
          <Logo />
          <ActivityCenter />
        </div>
        <NavItems pathname={pathname} />
        <UserCard email={user.email} role={user.role} onLogout={doLogout} />
      </aside>

      {/* Mobile drawer + scrim */}
      {mobileOpen && (
        <div className="fixed inset-0 z-50 lg:hidden">
          <button
            className="absolute inset-0 cursor-default bg-ink-950/70 backdrop-blur-sm"
            aria-label="Close navigation"
            onClick={() => setMobileOpen(false)}
          />
          <aside className="relative flex h-full w-72 max-w-[82vw] flex-col border-r border-line bg-ink-900 rise">
            <div className="flex items-center justify-between border-b border-line px-5 py-[18px]">
              <Logo />
              <button
                onClick={() => setMobileOpen(false)}
                aria-label="Close navigation"
                className="cursor-pointer rounded-md p-1.5 text-fg-faint hover:bg-ink-800 hover:text-fg"
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
            className="cursor-pointer rounded-md p-1.5 text-fg-muted hover:bg-ink-800 hover:text-fg"
          >
            <Menu className="h-5 w-5" />
          </button>
          <Logo />
          <div className="ml-auto">
            <ActivityCenter />
          </div>
        </header>

        <main className="min-w-0 flex-1">
          <div className="mx-auto max-w-[1280px] px-5 py-7 sm:px-8 sm:py-9">{children}</div>
        </main>
      </div>
    </div>
  );
}

export function PageHeader({
  title,
  subtitle,
  eyebrow,
  action,
}: {
  title: string;
  subtitle?: string;
  eyebrow?: string;
  action?: ReactNode;
}) {
  return (
    <header className="mb-8 flex flex-col gap-4 border-b border-line pb-6 sm:flex-row sm:items-end sm:justify-between">
      <div className="min-w-0">
        {eyebrow && (
          <div className="mb-1.5 font-mono text-[10px] uppercase tracking-[0.2em] text-fg-faint">{eyebrow}</div>
        )}
        <h1 className="font-display text-[26px] font-semibold leading-tight tracking-tight text-fg">{title}</h1>
        {subtitle && <p className="mt-1.5 max-w-2xl text-sm text-fg-muted">{subtitle}</p>}
      </div>
      {action && <div className="shrink-0">{action}</div>}
    </header>
  );
}
