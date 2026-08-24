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
# Monaco is served from this origin rather than from a CDN, so its files have
# to be in public/ before the build. Invoking next directly below skips the
# prebuild hook that does this during development, hence the explicit call.
RUN node scripts/sync-monaco.mjs
# Dependencies are resolved by bun, but the compile itself runs under Node:
# the baseline bun binary in the official image segfaults tearing down after
# a Next build. Invoking next's own entrypoint keeps npm out of the picture.
RUN node node_modules/next/dist/bin/next build

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
