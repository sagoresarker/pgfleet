import { Logo } from "@/components/logo";
import {
  Activity,
  ArrowRight,
  Boxes,
  Clock,
  Database,
  GitBranch,
  Github,
  Network,
  ShieldCheck,
  Terminal,
} from "lucide-react";
import Link from "next/link";

export const metadata = {
  title: "PgFleet — Your own managed Postgres",
  description:
    "A self-hosted managed-Postgres control plane: provisioning, automatic backups, point-in-time recovery, HA replication with a query router, and analytics — all in Docker.",
};

const features = [
  {
    icon: Database,
    title: "One-click provisioning",
    body: "Spin up isolated Postgres instances in Docker with sane defaults, encrypted credentials, and a connection string in seconds.",
    tone: "azure",
  },
  {
    icon: Clock,
    title: "Backups & point-in-time recovery",
    body: "Continuous WAL archiving to S3/MinIO via pgBackRest. Restore to any second with a guided timeline — verified by automated restore drills.",
    tone: "signal",
  },
  {
    icon: Network,
    title: "Replication & query router",
    body: "Streaming read replicas behind a PgCat router that splits reads and writes and load-balances across healthy members.",
    tone: "azure",
  },
  {
    icon: Activity,
    title: "Live analytics",
    body: "Connections, throughput, checkpoints, and a pg_stat_statements query explorer — collected into an embedded time series.",
    tone: "violet",
  },
  {
    icon: ShieldCheck,
    title: "Reliability you can trust",
    body: "Archiving-health checks, backup-age and pg_wal alerts, automated restore drills, and a crash-safe control plane that reconciles on restart.",
    tone: "healthy",
  },
  {
    icon: Boxes,
    title: "Self-hosted, all Docker",
    body: "Your hardware, your data. The control plane, every instance, pgBackRest, and the router all run as containers you own.",
    tone: "neutral",
  },
] as const;

const toneText: Record<string, string> = {
  azure: "text-azure",
  signal: "text-signal",
  violet: "text-violet",
  healthy: "text-healthy",
  neutral: "text-fg-muted",
};

export default function LandingPage() {
  return (
    <div className="relative min-h-screen overflow-hidden">
      {/* atmosphere */}
      <div className="pointer-events-none absolute -top-40 left-1/2 h-[70vh] w-[90vw] max-w-5xl -translate-x-1/2 rounded-full bg-azure/10 blur-[140px]" />
      <div className="pointer-events-none absolute right-0 top-1/3 h-[40vh] w-[40vh] rounded-full bg-violet/5 blur-[120px]" />

      {/* nav */}
      <header className="relative z-10 mx-auto flex max-w-6xl items-center justify-between px-6 py-5">
        <Logo />
        <nav className="flex items-center gap-3">
          <a
            href="https://github.com/sagoresarker/pgfleet"
            className="hidden items-center gap-2 rounded-md px-3 py-2 text-sm text-fg-muted transition-colors hover:text-fg sm:inline-flex"
          >
            <Github className="h-4 w-4" />
            GitHub
          </a>
          <Link
            href="/dashboard"
            className="inline-flex items-center gap-2 rounded-md bg-azure px-4 py-2 font-display text-sm font-medium tracking-tight text-ink-950 shadow-[0_0_0_1px_rgba(91,157,255,0.4),0_10px_30px_-12px_rgba(91,157,255,0.7)] transition-colors hover:bg-azure-bright"
          >
            Open console
            <ArrowRight className="h-4 w-4" />
          </Link>
        </nav>
      </header>

      {/* hero */}
      <section className="relative z-10 mx-auto max-w-6xl px-6 pb-12 pt-12 md:pt-20">
        <div className="reveal mx-auto max-w-3xl text-center">
          <div className="mb-5 inline-flex items-center gap-2 rounded-full border border-line bg-ink-850/70 px-3 py-1.5 font-mono text-[10px] uppercase tracking-[0.2em] text-fg-muted">
            <span className="led led-healthy led-pulse" />
            open-source managed postgres
          </div>
          <h1 className="font-display text-4xl font-semibold leading-[1.05] tracking-tight text-fg glow-text md:text-6xl">
            Your own
            <br />
            managed Postgres.
          </h1>
          <p className="mx-auto mt-6 max-w-xl text-base text-fg-muted md:text-lg">
            Provisioning, automatic backups, point-in-time recovery, HA replication with a query router, and live
            analytics — a control plane you run yourself, entirely in Docker.
          </p>
          <div className="mt-8 flex flex-col items-center justify-center gap-3 sm:flex-row">
            <Link
              href="/dashboard"
              className="inline-flex w-full items-center justify-center gap-2 rounded-md bg-azure px-5 py-3 font-display text-sm font-medium tracking-tight text-ink-950 shadow-[0_0_0_1px_rgba(91,157,255,0.4),0_12px_32px_-12px_rgba(91,157,255,0.7)] transition-colors hover:bg-azure-bright sm:w-auto"
            >
              <Terminal className="h-4 w-4" />
              Open the console
            </Link>
            <a
              href="https://github.com/sagoresarker/pgfleet"
              className="inline-flex w-full items-center justify-center gap-2 rounded-md border border-line-bright bg-ink-800/40 px-5 py-3 font-display text-sm tracking-tight text-fg transition-colors hover:border-azure/60 hover:text-azure sm:w-auto"
            >
              <GitBranch className="h-4 w-4" />
              View the source
            </a>
          </div>
        </div>

        {/* faux instrument panel */}
        <div className="reveal mx-auto mt-16 max-w-4xl" style={{ animationDelay: "120ms" }}>
          <FleetPreview />
        </div>
      </section>

      {/* features */}
      <section className="relative z-10 mx-auto max-w-6xl px-6 py-20">
        <div className="mb-12 text-center">
          <div className="mb-2 font-mono text-[10px] uppercase tracking-[0.25em] text-fg-faint">capabilities</div>
          <h2 className="font-display text-2xl font-semibold tracking-tight text-fg md:text-3xl">
            Everything a managed service gives you
          </h2>
        </div>
        <div className="grid gap-px overflow-hidden rounded-xl border border-line bg-line md:grid-cols-2 lg:grid-cols-3">
          {features.map((f) => {
            const Icon = f.icon;
            return (
              <div key={f.title} className="group bg-ink-900 p-6 transition-colors hover:bg-ink-850">
                <Icon className={`h-5 w-5 ${toneText[f.tone]}`} />
                <h3 className="mt-4 font-display text-base font-medium tracking-tight text-fg">{f.title}</h3>
                <p className="mt-2 text-sm leading-relaxed text-fg-muted">{f.body}</p>
              </div>
            );
          })}
        </div>
      </section>

      {/* how it works */}
      <section className="relative z-10 mx-auto max-w-6xl px-6 py-12">
        <div className="rounded-xl border border-line bg-ink-850/60 p-8 md:p-10">
          <div className="mb-8 text-center">
            <div className="mb-2 font-mono text-[10px] uppercase tracking-[0.25em] text-fg-faint">architecture</div>
            <h2 className="font-display text-xl font-semibold tracking-tight text-fg md:text-2xl">
              A control plane on top of pgBackRest, Docker, and PgCat
            </h2>
          </div>
          <div className="grid grid-cols-2 gap-4 md:grid-cols-4">
            {[
              { icon: Terminal, label: "Control plane", sub: "Go · API · UI" },
              { icon: Database, label: "Instances", sub: "postgres in Docker" },
              { icon: Clock, label: "Backups", sub: "pgBackRest → S3" },
              { icon: Network, label: "Router", sub: "PgCat · R/W split" },
            ].map((n) => {
              const Icon = n.icon;
              return (
                <div key={n.label} className="rounded-lg border border-line bg-ink-900 p-5 text-center">
                  <Icon className="mx-auto h-5 w-5 text-azure" />
                  <div className="mt-3 font-display text-sm text-fg">{n.label}</div>
                  <div className="font-mono text-[10px] uppercase tracking-wider text-fg-faint">{n.sub}</div>
                </div>
              );
            })}
          </div>
        </div>
      </section>

      {/* final CTA */}
      <section className="relative z-10 mx-auto max-w-6xl px-6 py-20">
        <div className="relative overflow-hidden rounded-2xl border border-azure/30 bg-gradient-to-b from-azure/10 to-transparent p-10 text-center md:p-14">
          <div className="absolute inset-0 scanline" />
          <h2 className="relative font-display text-2xl font-semibold tracking-tight text-fg md:text-4xl">
            Run Postgres like a platform.
          </h2>
          <p className="relative mx-auto mt-3 max-w-lg text-sm text-fg-muted md:text-base">
            Bring up the control plane, sign in, and provision your first instance in under a minute.
          </p>
          <Link
            href="/dashboard"
            className="relative mt-7 inline-flex items-center gap-2 rounded-md bg-azure px-6 py-3 font-display text-sm font-medium tracking-tight text-ink-950 shadow-[0_0_0_1px_rgba(91,157,255,0.4),0_12px_32px_-12px_rgba(91,157,255,0.7)] transition-colors hover:bg-azure-bright"
          >
            Open the console
            <ArrowRight className="h-4 w-4" />
          </Link>
        </div>
      </section>

      {/* footer */}
      <footer className="relative z-10 border-t border-line">
        <div className="mx-auto flex max-w-6xl flex-col items-center justify-between gap-4 px-6 py-8 sm:flex-row">
          <Logo />
          <p className="font-mono text-[10px] uppercase tracking-[0.2em] text-fg-faint">
            self-hosted · postgres · backups · pitr · ha
          </p>
          <a
            href="https://github.com/sagoresarker/pgfleet"
            className="text-fg-faint transition-colors hover:text-azure"
            aria-label="GitHub repository"
          >
            <Github className="h-4 w-4" />
          </a>
        </div>
      </footer>
    </div>
  );
}

/* A faux fleet panel that previews the product's look. */
function FleetPreview() {
  const rows = [
    { name: "orders-db", meta: "pg16 · s3 · :54320", led: "led-healthy", status: "running" },
    { name: "analytics", meta: "pg16 · local · :54321", led: "led-healthy", status: "running" },
    { name: "billing", meta: "pg16 · s3 · provisioning", led: "led-signal led-pulse", status: "provisioning" },
  ];
  return (
    <div className="overflow-hidden rounded-xl border border-line bg-ink-850/90 shadow-[0_40px_80px_-32px_rgba(0,0,0,0.9)] backdrop-blur">
      <div className="flex items-center gap-2 border-b border-line px-4 py-3">
        <span className="h-2.5 w-2.5 rounded-full bg-danger/70" />
        <span className="h-2.5 w-2.5 rounded-full bg-signal/70" />
        <span className="h-2.5 w-2.5 rounded-full bg-healthy/70" />
        <span className="ml-3 font-mono text-[10px] uppercase tracking-[0.2em] text-fg-faint">pgfleet · fleet</span>
      </div>
      <div className="grid gap-4 p-5 md:grid-cols-[1.4fr_1fr]">
        <div className="space-y-2">
          {rows.map((r) => (
            <div key={r.name} className="flex items-center gap-3 rounded-md border border-line bg-ink-900 px-4 py-3">
              <Database className="h-4 w-4 text-fg-faint" />
              <div className="min-w-0 flex-1">
                <div className="font-display text-sm text-fg">{r.name}</div>
                <div className="font-mono text-[11px] text-fg-faint">{r.meta}</div>
              </div>
              <span className={`led ${r.led}`} />
              <span className="font-mono text-[10px] uppercase tracking-wider text-fg-muted">{r.status}</span>
            </div>
          ))}
        </div>
        <div className="rounded-md border border-line bg-ink-900 p-4">
          <div className="mb-3 font-mono text-[10px] uppercase tracking-wider text-fg-faint">point-in-time recovery</div>
          <div className="mb-2 flex justify-between font-mono text-[9px] uppercase tracking-wider text-fg-faint">
            <span>oldest</span>
            <span>now</span>
          </div>
          <div className="relative h-1.5 rounded-full bg-gradient-to-r from-ink-700 via-azure/30 to-azure/60">
            <span className="absolute left-[18%] top-1/2 h-3 w-3 -translate-x-1/2 -translate-y-1/2 rounded-full border-2 border-ink-900 bg-signal shadow-[0_0_8px_var(--color-signal)]" />
            <span className="absolute left-[58%] top-1/2 h-3 w-3 -translate-x-1/2 -translate-y-1/2 rounded-full border-2 border-ink-900 bg-signal shadow-[0_0_8px_var(--color-signal)]" />
            <span className="absolute right-0 top-1/2 h-3 w-3 -translate-y-1/2 translate-x-1/2 rounded-full border-2 border-ink-900 bg-azure shadow-[0_0_10px_var(--color-azure)]" />
          </div>
          <div className="mt-5 grid grid-cols-2 gap-3">
            <div>
              <div className="font-mono text-[9px] uppercase tracking-wider text-fg-faint">archiving</div>
              <div className="mt-1 flex items-center gap-1.5 text-xs text-fg">
                <span className="led led-healthy" /> ok
              </div>
            </div>
            <div>
              <div className="font-mono text-[9px] uppercase tracking-wider text-fg-faint">last backup</div>
              <div className="mt-1 font-display text-xs tnum text-fg">4m ago</div>
            </div>
          </div>
        </div>
      </div>
    </div>
  );
}
