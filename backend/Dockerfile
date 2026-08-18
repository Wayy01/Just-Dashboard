# syntax=docker/dockerfile:1

FROM golang:1.24-bookworm AS build
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
# CGO stays off so the binary runs on a slim base without a libc mismatch;
# the SQLite driver is pure Go precisely so this works.
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build -trimpath -ldflags="-s -w" -o /out/vps-dashboard ./cmd/server

FROM debian:bookworm-slim
# The dashboard drives the host through these tools; a scratch image would
# leave most of the product non-functional. They are the same binaries an
# operator would use over SSH.
RUN apt-get update && apt-get install -y --no-install-recommends \
      ca-certificates \
      tzdata \
      git \
      openssh-client \
      docker.io \
      docker-compose-plugin \
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
      nginx-common \
      logrotate \
    && rm -rf /var/lib/apt/lists/*

COPY --from=build /out/vps-dashboard /usr/local/bin/vps-dashboard

# Runs as root deliberately: managing systemd units, the firewall, host
# accounts and a PTY all require it. The security boundary is the network
# allowlist plus mandatory 2FA in front of the API, not the process UID.
WORKDIR /var/lib/vps-dashboard
EXPOSE 8080

HEALTHCHECK --interval=30s --timeout=5s --start-period=10s --retries=3 \
  CMD ["/usr/local/bin/vps-dashboard", "-healthcheck"]

ENTRYPOINT ["/usr/local/bin/vps-dashboard"]
