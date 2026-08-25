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
      default-mysql-client \
      certbot \
      logrotate \
    && rm -rf /var/lib/apt/lists/*

# PostgreSQL client tools, from PGDG rather than from Debian.
#
# pg_dump refuses to dump a server newer than itself — "aborting because of
# server version mismatch", not a warning — so the single `postgresql-client`
# Debian ships decided which servers this dashboard could back up at all. On
# bookworm that is 15, which cannot dump the Postgres 16 sitting next to it.
#
# PGDG carries every supported major in parallel under /usr/lib/postgresql/<n>,
# which is what `postgresTool` reads to pick the one matching the server it has
# been pointed at. Installing several is a few tens of megabytes and removes a
# whole class of "backups silently never worked".
RUN install -m 0755 -d /etc/apt/keyrings \
    && curl -fsSL https://www.postgresql.org/media/keys/ACCC4CF8.asc \
       -o /etc/apt/keyrings/pgdg.asc \
    && chmod a+r /etc/apt/keyrings/pgdg.asc \
    && echo "deb [signed-by=/etc/apt/keyrings/pgdg.asc] \
https://apt.postgresql.org/pub/repos/apt bookworm-pgdg main" > /etc/apt/sources.list.d/pgdg.list \
    && apt-get update \
    && apt-get install -y --no-install-recommends \
       postgresql-client-15 \
       postgresql-client-16 \
       postgresql-client-17 \
       postgresql-client-18 \
    && rm -rf /var/lib/apt/lists/*

# MongoDB's dump tools, from MongoDB rather than from Debian, which packages
# neither mongodump nor mongorestore at all.
#
# The dashboard can dump a Mongo database without them — dbx writes the archive
# through the driver it is already connected with — but mongodump's own format
# is what every other Mongo tool reads, so a backup taken here stays useful
# somewhere else. The built-in path remains the fallback, and remains what runs
# if this layer is ever dropped.
RUN install -m 0755 -d /etc/apt/keyrings \
    && curl -fsSL https://pgp.mongodb.com/server-7.0.asc \
       -o /etc/apt/keyrings/mongodb.asc \
    && chmod a+r /etc/apt/keyrings/mongodb.asc \
    && echo "deb [arch=$(dpkg --print-architecture) signed-by=/etc/apt/keyrings/mongodb.asc] \
https://repo.mongodb.org/apt/debian bookworm/mongodb-org/7.0 main" \
       > /etc/apt/sources.list.d/mongodb.list \
    && apt-get update \
    && apt-get install -y --no-install-recommends mongodb-database-tools \
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
