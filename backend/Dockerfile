# syntax=docker/dockerfile:1

FROM golang:1.25-bookworm AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
# CGO stays off so the binary runs on a slim base without a libc mismatch;
# the SQLite driver is pure Go precisely so this works.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/just-dashboard ./cmd/server

FROM debian:bookworm-slim

# The dashboard drives the host through these tools; a scratch image would
# leave most of the product non-functional. They are the same binaries an
# operator would use over SSH.
RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates \
      curl \
      gnupg \
      tzdata \
      git \
      openssh-client \
      systemd \
      tmux \
      procps \
      iproute2 \
      iptables \
      ufw \
      fail2ban \
      cron \
      postgresql-client \
      default-mysql-client \
      certbot \
      logrotate \
    && rm -rf /var/lib/apt/lists/*

# Debian ships neither a standalone docker CLI nor the compose v2 plugin, and
# this image must not carry a second daemon — only the client that talks to
# the host's socket. Docker's own repository is the supported source for both.
RUN install -m 0755 -d /etc/apt/keyrings \
    && curl -fsSL https://download.docker.com/linux/debian/gpg \
       -o /etc/apt/keyrings/docker.asc \
    && chmod a+r /etc/apt/keyrings/docker.asc \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/docker.asc] \
https://download.docker.com/linux/debian bookworm stable" > /etc/apt/sources.list.d/docker.list \
    && apt-get update \
    && apt-get install -y --no-install-recommends docker-ce-cli docker-compose-plugin \
    && rm -rf /var/lib/apt/lists/*

COPY --from=build /out/just-dashboard /usr/local/bin/just-dashboard

# Runs as root deliberately: managing systemd units, the firewall, host
# accounts and a PTY all require it. The security boundary is the network
# allowlist plus mandatory 2FA in front of the API, not the process UID.
WORKDIR /var/lib/just-dashboard
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD ["/usr/local/bin/just-dashboard", "-healthcheck"]

ENTRYPOINT ["/usr/local/bin/just-dashboard"]
