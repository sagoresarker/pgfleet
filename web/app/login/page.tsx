"use client";

import { Logo } from "@/components/logo";
import { Button, Field, Input } from "@/components/ui";
import { useAuth } from "@/lib/auth";
import { ArrowRight } from "lucide-react";
import { useRouter } from "next/navigation";
import { useEffect, useState } from "react";

export default function LoginPage() {
  const { user, loading, login } = useAuth();
  const router = useRouter();
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [submitting, setSubmitting] = useState(false);

  useEffect(() => {
    if (!loading && user) router.replace("/dashboard");
  }, [loading, user, router]);

  async function onSubmit(e: React.FormEvent) {
    e.preventDefault();
    setError(null);
    setSubmitting(true);
    try {
      await login(email, password);
      router.replace("/dashboard");
    } catch (err) {
      setError(err instanceof Error ? err.message : "Sign in failed");
    } finally {
      setSubmitting(false);
    }
  }

  return (
    <div className="relative grid min-h-screen place-items-center overflow-hidden px-6">
      {/* Atmospheric glow */}
      <div className="pointer-events-none absolute -top-1/3 left-1/2 h-[60vh] w-[60vh] -translate-x-1/2 rounded-full bg-azure/10 blur-[120px]" />
      <div className="pointer-events-none absolute bottom-0 right-0 h-[40vh] w-[40vh] rounded-full bg-violet/5 blur-[100px]" />

      <div className="relative w-full max-w-sm rise">
        <div className="mb-8 flex justify-center">
          <Logo />
        </div>

        <div className="rounded-xl border border-line bg-ink-850/80 p-7 shadow-[0_32px_64px_-32px_rgba(15,31,51,0.18)] backdrop-blur">
          <h1 className="font-display text-lg font-semibold tracking-tight">Sign in</h1>
          <p className="mt-1 text-sm text-fg-muted">Access the Postgres control plane.</p>

          <form onSubmit={onSubmit} className="mt-6 space-y-4">
            <Field label="Email">
              <Input
                type="email"
                value={email}
                onChange={(e) => setEmail(e.target.value)}
                placeholder="you@example.com"
                autoFocus
                required
              />
            </Field>
            <Field label="Password">
              <Input type="password" value={password} onChange={(e) => setPassword(e.target.value)} required />
            </Field>

            {error && (
              <div className="rounded-md border border-danger/30 bg-danger/10 px-3 py-2 text-xs text-danger">{error}</div>
            )}

            <Button type="submit" className="w-full" disabled={submitting}>
              {submitting ? "Authenticating…" : "Enter control plane"}
              {!submitting && <ArrowRight className="h-4 w-4" />}
            </Button>
          </form>
        </div>

        <p className="mt-6 text-center font-mono text-[10px] uppercase tracking-[0.2em] text-fg-faint">
          managed postgres · backups · pitr
        </p>
      </div>
    </div>
  );
}
