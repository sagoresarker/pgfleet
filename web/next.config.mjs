// Static security headers for the dashboard. The Content-Security-Policy is set
// separately in middleware.ts because it needs a fresh per-request nonce (and to
// relax safely in dev for HMR) — Next.js reads that nonce and applies it to its
// own scripts, so production gets a strict nonce-based CSP without 'unsafe-inline'.
const securityHeaders = [
  { key: "X-Content-Type-Options", value: "nosniff" },
  { key: "X-Frame-Options", value: "DENY" },
  { key: "Referrer-Policy", value: "no-referrer" },
  { key: "Permissions-Policy", value: "camera=(), microphone=(), geolocation=()" },
];

/** @type {import('next').NextConfig} */
const nextConfig = {
  reactStrictMode: true,
  output: "standalone",
  async headers() {
    return [{ source: "/:path*", headers: securityHeaders }];
  },
  async rewrites() {
    // Proxy API calls to the Go control plane during development.
    const api = process.env.PGFLEET_API_URL || "http://localhost:8080";
    return [
      { source: "/api/:path*", destination: `${api}/api/:path*` },
      { source: "/healthz", destination: `${api}/healthz` },
    ];
  },
};

export default nextConfig;
