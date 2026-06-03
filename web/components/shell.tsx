"use client";

import { useAuth } from "@/lib/auth";
import { cn } from "@/lib/utils";
import { Activity, Bell, Boxes, Database, LogOut, Network, ScrollText, ShieldCheck, Users } from "lucide-react";
import Link from "next/link";
import { usePathname, useRouter } from "next/navigation";
import { useEffect, type ReactNode } from "react";
import { Logo } from "./logo";
import { Badge, Spinner } from "./ui";

const nav = [
  { href: "/dashboard", label: "Fleet", icon: Boxes },
  { href: "/instances", label: "Instances", icon: Database },
  { href: "/clusters", label: "Clusters", icon: Network },
  { href: "/health", label: "Reliability", icon: ShieldCheck },
  { href: "/alerts", label: "Alerts", icon: Bell },
  { href: "/events", label: "Events", icon: ScrollText },
  { href: "/users", label: "Access", icon: Users },
];

export function AppShell({ children }: { children: ReactNode }) {
  const { user, loading, logout } = useAuth();
  const router = useRouter();
  const pathname = usePathname();

  useEffect(() => {
    if (!loading && !user) router.replace("/login");
  }, [loading, user, router]);

  if (loading || !user) {
    return (
      <div className="grid min-h-screen place-items-center">
        <Spinner className="h-6 w-6" />
      </div>
    );
  }

  return (
    <div className="flex min-h-screen">
      <aside className="sticky top-0 flex h-screen w-60 shrink-0 flex-col border-r border-line bg-ink-900/70 backdrop-blur">
        <div className="px-5 py-5">
          <Logo />
        </div>
        <nav className="flex-1 space-y-0.5 px-3">
          {nav.map((item) => {
            const active = pathname === item.href || pathname.startsWith(item.href + "/");
            const Icon = item.icon;
            return (
              <Link
                key={item.href}
                href={item.href}
                className={cn(
                  "group flex items-center gap-3 rounded-md px-3 py-2 text-sm transition-colors",
                  active ? "bg-azure/10 text-azure" : "text-fg-muted hover:bg-ink-700/50 hover:text-fg"
                )}
              >
                <Icon className={cn("h-4 w-4", active ? "text-azure" : "text-fg-faint group-hover:text-fg-muted")} />
                <span className="font-display tracking-tight">{item.label}</span>
                {active && <span className="ml-auto h-1.5 w-1.5 rounded-full bg-azure shadow-[0_0_8px_var(--color-azure)]" />}
              </Link>
            );
          })}
        </nav>
        <div className="border-t border-line p-3">
          <div className="flex items-center gap-3 rounded-md px-2 py-2">
            <div className="grid h-8 w-8 place-items-center rounded-full border border-line-bright bg-ink-800 font-mono text-xs uppercase text-azure">
              {user.email.slice(0, 2)}
            </div>
            <div className="min-w-0 flex-1">
              <div className="truncate text-xs text-fg">{user.email}</div>
              <Badge tone="azure" className="mt-0.5">
                {user.role}
              </Badge>
            </div>
            <button
              onClick={() => logout().then(() => router.replace("/login"))}
              className="text-fg-faint transition-colors hover:text-danger"
              aria-label="Sign out"
            >
              <LogOut className="h-4 w-4" />
            </button>
          </div>
        </div>
      </aside>

      <main className="min-w-0 flex-1">
        <div className="mx-auto max-w-6xl px-8 py-8">{children}</div>
      </main>
    </div>
  );
}

export function PageHeader({ title, subtitle, action }: { title: string; subtitle?: string; action?: ReactNode }) {
  return (
    <header className="mb-8 flex items-end justify-between gap-4">
      <div>
        <div className="mb-1 flex items-center gap-2 font-mono text-[10px] uppercase tracking-[0.2em] text-fg-faint">
          <Activity className="h-3 w-3 text-azure" />
          pgfleet
        </div>
        <h1 className="font-display text-2xl font-semibold tracking-tight text-fg">{title}</h1>
        {subtitle && <p className="mt-1 text-sm text-fg-muted">{subtitle}</p>}
      </div>
      {action}
    </header>
  );
}
