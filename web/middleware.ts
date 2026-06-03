import { NextRequest, NextResponse } from "next/server";

// Content-Security-Policy is applied here (not in next.config) because it needs a
// fresh per-request nonce. Next.js reads the nonce from the request's CSP header
// and stamps it onto its own scripts, so production gets a strict
// nonce + 'strict-dynamic' policy with NO 'unsafe-inline' for scripts — which
// keeps the localStorage session token safe from injected scripts.
//
// In development the policy is relaxed: Next's dev server and Fast Refresh use
// inline + eval'd bootstrap scripts and a WebSocket for HMR, which a strict
// policy would block (that was the cause of the "inline script violates CSP" /
// "Connection closed" errors in local dev).
export function middleware(request: NextRequest) {
  const isDev = process.env.NODE_ENV !== "production";
  const nonce = btoa(crypto.randomUUID());

  const scriptSrc = isDev
    ? "'self' 'unsafe-inline' 'unsafe-eval'"
    : `'self' 'nonce-${nonce}' 'strict-dynamic'`;
  const connectSrc = isDev ? "'self' ws: wss:" : "'self'";

  const csp = [
    "default-src 'self'",
    `script-src ${scriptSrc}`,
    // Tailwind injects an inline <style>, so style needs 'unsafe-inline'.
    "style-src 'self' 'unsafe-inline'",
    "img-src 'self' data: blob:",
    "font-src 'self' data:",
    `connect-src ${connectSrc}`,
    "frame-ancestors 'none'",
    "base-uri 'self'",
    "form-action 'self'",
    "object-src 'none'",
  ].join("; ");

  // Pass the nonce + CSP forward so Next.js can apply the nonce to its scripts.
  const requestHeaders = new Headers(request.headers);
  requestHeaders.set("x-nonce", nonce);
  requestHeaders.set("content-security-policy", csp);

  const response = NextResponse.next({ request: { headers: requestHeaders } });
  response.headers.set("Content-Security-Policy", csp);
  return response;
}

export const config = {
  // Run on every route except Next's static assets and the image optimizer
  // (those are served with their own caching and don't need a per-request CSP).
  matcher: ["/((?!_next/static|_next/image|favicon.ico).*)"],
};
