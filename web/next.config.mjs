// Security headers for the dashboard documents. The session token lives in
// localStorage, so a strict Content-Security-Policy is the main mitigation that
// limits the blast radius of any XSS (no foreign script/style/connect origins).
// 'unsafe-inline' on style-src is required by Tailwind's injected styles;
// script-src stays self-only ('unsafe-eval' is only needed in dev for HMR).
const isDev = process.env.NODE_ENV !== "production";
const csp = [
  "default-src 'self'",
  `script-src 'self'${isDev ? " 'unsafe-eval'" : ""}`,
  "style-src 'self' 'unsafe-inline'",
  "img-src 'self' data:",
  "font-src 'self' data:",
  // connect-src allows same-origin XHR/WebSocket; ws: for dev HMR.
  `connect-src 'self'${isDev ? " ws:" : ""}`,
  "frame-ancestors 'none'",
  "base-uri 'self'",
  "form-action 'self'",
].join("; ");

const securityHeaders = [
  { key: "Content-Security-Policy", value: csp },
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
