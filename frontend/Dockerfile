# syntax=docker/dockerfile:1

FROM oven/bun:1 AS deps
WORKDIR /app
COPY package.json bun.lock ./
RUN bun install --frozen-lockfile

FROM node:22-bookworm-slim AS build
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY . .
ENV NEXT_TELEMETRY_DISABLED=1
# The Monaco runtime is copied out of node_modules into public/ before the
# build, exactly as `bun run build` does outside the image. It is not optional
# packaging: without it the editor falls back to fetching itself from a CDN,
# and the Query tab is a permanent "Loading…" on any install whose browser
# cannot reach the public internet — which is most of them, since this is
# normally reached down a tunnel.
#
# Dependencies are resolved by bun, but the compile itself runs under Node:
# the baseline bun binary in the official image segfaults tearing down after
# a Next build. Invoking next's own entrypoint keeps npm out of the picture,
# and is why the script is called here rather than through the package script
# that wraps it.
RUN node scripts/sync-monaco.mjs \
 && node node_modules/next/dist/bin/next build

FROM node:22-bookworm-slim AS runtime
WORKDIR /app
ENV NODE_ENV=production NEXT_TELEMETRY_DISABLED=1 PORT=3000 HOSTNAME=0.0.0.0

RUN groupadd --system --gid 1001 nodejs \
 && useradd --system --uid 1001 --gid nodejs nextjs

COPY --from=build --chown=nextjs:nodejs /app/.next/standalone ./
COPY --from=build --chown=nextjs:nodejs /app/.next/static ./.next/static
COPY --from=build --chown=nextjs:nodejs /app/public ./public

# The frontend serves static assets and nothing privileged, so unlike the
# backend it has no reason to run as root.
USER nextjs
EXPOSE 3000
CMD ["node", "server.js"]
