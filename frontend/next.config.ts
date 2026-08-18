import type { NextConfig } from "next"

/**
 * The browser always talks to /api on the same origin as the app: the session
 * cookie is HttpOnly and SameSite=Strict, so a separate API host would simply
 * drop it.
 *
 * How /api reaches the Go backend differs by environment:
 *  - development: Next rewrites it, so `bun dev` needs no extra moving parts.
 *  - production: the compose stack puts a reverse proxy in front of both, and
 *    that proxy routes /api to the backend. Next never sees those requests.
 *
 * Rewrites are evaluated at build time, so VPSD_API_URL is read then — which
 * is exactly why production does not depend on it.
 */
const apiTarget = process.env.VPSD_API_URL ?? "http://127.0.0.1:8080"

const nextConfig: NextConfig = {
  output: "standalone",
  poweredByHeader: false,
  // This repo keeps its own instructions; the generated ones are noise.
  agentRules: false,
  async rewrites() {
    return [{ source: "/api/:path*", destination: `${apiTarget}/api/:path*` }]
  },
}

export default nextConfig
