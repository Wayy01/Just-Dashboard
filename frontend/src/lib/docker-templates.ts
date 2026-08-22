import type { ContainerSpec } from "@/lib/types"

/**
 * Starting points, not an app store.
 *
 * The gap between "I want to run a database" and a filled-in create form is
 * where most people give up on a Docker UI, and the tools that close it —
 * Yacht, CasaOS — do it with a template catalogue that then goes stale and
 * becomes the product's main maintenance burden. This is the small version of
 * that idea: a dozen images almost every server ends up running, pre-filled
 * with the ports, volumes and settings they actually need, and nothing else.
 *
 * Every entry is a *starting point* the operator then edits, which is why each
 * one leaves the things that must be decided — a database password, a domain —
 * visibly empty rather than filled with a default nobody should ship.
 *
 * Ports are bound to 127.0.0.1 throughout. Anything that should be reachable
 * from outside belongs behind the reverse proxy this dashboard already
 * manages, and a template that published a database on every interface would
 * be the single most harmful thing in this feature.
 */

export type Template = {
  id: string
  name: string
  blurb: string
  category: "web" | "database" | "tools"
  /** Anything the operator must fill in before this will start. */
  requires?: string
  spec: () => ContainerSpec
}

const base = (over: Partial<ContainerSpec>): ContainerSpec => ({
  name: "",
  image: "",
  env: [],
  ports: [],
  mounts: [],
  labels: [],
  networks: [],
  limits: {},
  restartPolicy: "unless-stopped",
  start: true,
  pull: "missing",
  ...over,
})

export const TEMPLATES: Template[] = [
  {
    id: "nginx",
    name: "Nginx",
    blurb: "A web server, and the usual way to serve static files or sit in front of something else.",
    category: "web",
    spec: () =>
      base({
        name: "nginx",
        image: "nginx:alpine",
        ports: [{ hostIp: "127.0.0.1", hostPort: 8080, containerPort: 80, protocol: "tcp" }],
        limits: { memoryMb: 256 },
      }),
  },
  {
    id: "caddy",
    name: "Caddy",
    blurb: "A web server that gets its own HTTPS certificates. Simpler than nginx for a single site.",
    category: "web",
    spec: () =>
      base({
        name: "caddy",
        image: "caddy:2-alpine",
        ports: [
          { hostIp: "127.0.0.1", hostPort: 8080, containerPort: 80, protocol: "tcp" },
        ],
        mounts: [
          { type: "volume", source: "caddy-data", target: "/data" },
          { type: "volume", source: "caddy-config", target: "/config" },
        ],
        limits: { memoryMb: 256 },
      }),
  },
  {
    id: "postgres",
    name: "PostgreSQL",
    blurb: "The default choice of relational database for most new applications.",
    category: "database",
    requires: "POSTGRES_PASSWORD — the image refuses to start without it.",
    spec: () =>
      base({
        name: "postgres",
        image: "postgres:16-alpine",
        env: [
          { name: "POSTGRES_PASSWORD", value: "" },
          { name: "POSTGRES_USER", value: "postgres" },
          { name: "POSTGRES_DB", value: "postgres" },
        ],
        ports: [{ hostIp: "127.0.0.1", hostPort: 5432, containerPort: 5432, protocol: "tcp" }],
        mounts: [{ type: "volume", source: "postgres-data", target: "/var/lib/postgresql/data" }],
        limits: { memoryMb: 1024 },
        health: {
          test: ["pg_isready -U postgres"],
          intervalSeconds: 30,
          timeoutSeconds: 5,
          retries: 3,
          startPeriodSeconds: 20,
        },
      }),
  },
  {
    id: "mysql",
    name: "MySQL",
    blurb: "The relational database most older applications and WordPress expect.",
    category: "database",
    requires: "MYSQL_ROOT_PASSWORD — the image refuses to start without it.",
    spec: () =>
      base({
        name: "mysql",
        image: "mysql:8",
        env: [
          { name: "MYSQL_ROOT_PASSWORD", value: "" },
          { name: "MYSQL_DATABASE", value: "app" },
        ],
        ports: [{ hostIp: "127.0.0.1", hostPort: 3306, containerPort: 3306, protocol: "tcp" }],
        mounts: [{ type: "volume", source: "mysql-data", target: "/var/lib/mysql" }],
        limits: { memoryMb: 1024 },
      }),
  },
  {
    id: "mariadb",
    name: "MariaDB",
    blurb: "A drop-in replacement for MySQL, and the version most self-hosted projects test against.",
    category: "database",
    requires: "MARIADB_ROOT_PASSWORD — the image refuses to start without it.",
    spec: () =>
      base({
        name: "mariadb",
        image: "mariadb:11",
        env: [
          { name: "MARIADB_ROOT_PASSWORD", value: "" },
          { name: "MARIADB_DATABASE", value: "app" },
        ],
        ports: [{ hostIp: "127.0.0.1", hostPort: 3306, containerPort: 3306, protocol: "tcp" }],
        mounts: [{ type: "volume", source: "mariadb-data", target: "/var/lib/mysql" }],
        limits: { memoryMb: 1024 },
      }),
  },
  {
    id: "redis",
    name: "Redis",
    blurb: "An in-memory store, used for caches, queues and sessions.",
    category: "database",
    spec: () =>
      base({
        name: "redis",
        image: "redis:7-alpine",
        // Redis keeps everything in memory and only writes a snapshot
        // periodically; the volume is what survives a restart.
        mounts: [{ type: "volume", source: "redis-data", target: "/data" }],
        ports: [{ hostIp: "127.0.0.1", hostPort: 6379, containerPort: 6379, protocol: "tcp" }],
        limits: { memoryMb: 512 },
      }),
  },
  {
    id: "mongo",
    name: "MongoDB",
    blurb: "A document database, for applications that store JSON-shaped data.",
    category: "database",
    spec: () =>
      base({
        name: "mongo",
        image: "mongo:7",
        env: [
          { name: "MONGO_INITDB_ROOT_USERNAME", value: "root" },
          { name: "MONGO_INITDB_ROOT_PASSWORD", value: "" },
        ],
        ports: [{ hostIp: "127.0.0.1", hostPort: 27017, containerPort: 27017, protocol: "tcp" }],
        mounts: [{ type: "volume", source: "mongo-data", target: "/data/db" }],
        limits: { memoryMb: 1024 },
      }),
  },
  {
    id: "uptime-kuma",
    name: "Uptime Kuma",
    blurb: "Watches your sites and services and tells you when one stops answering.",
    category: "tools",
    spec: () =>
      base({
        name: "uptime-kuma",
        image: "louislam/uptime-kuma:1",
        ports: [{ hostIp: "127.0.0.1", hostPort: 3001, containerPort: 3001, protocol: "tcp" }],
        mounts: [{ type: "volume", source: "uptime-kuma-data", target: "/app/data" }],
        limits: { memoryMb: 512 },
      }),
  },
  {
    id: "vaultwarden",
    name: "Vaultwarden",
    blurb: "A password manager that speaks the Bitwarden protocol, so the official apps work with it.",
    category: "tools",
    requires: "Put it behind the reverse proxy with HTTPS before using it — the clients require it.",
    spec: () =>
      base({
        name: "vaultwarden",
        image: "vaultwarden/server:latest",
        env: [{ name: "SIGNUPS_ALLOWED", value: "false" }],
        ports: [{ hostIp: "127.0.0.1", hostPort: 8081, containerPort: 80, protocol: "tcp" }],
        mounts: [{ type: "volume", source: "vaultwarden-data", target: "/data" }],
        limits: { memoryMb: 512 },
      }),
  },
  {
    id: "gitea",
    name: "Gitea",
    blurb: "A small, self-hosted git forge with pull requests, issues and CI.",
    category: "tools",
    spec: () =>
      base({
        name: "gitea",
        image: "gitea/gitea:1",
        env: [
          { name: "USER_UID", value: "1000" },
          { name: "USER_GID", value: "1000" },
        ],
        ports: [
          { hostIp: "127.0.0.1", hostPort: 3000, containerPort: 3000, protocol: "tcp" },
          { hostIp: "127.0.0.1", hostPort: 2222, containerPort: 22, protocol: "tcp" },
        ],
        mounts: [{ type: "volume", source: "gitea-data", target: "/data" }],
        limits: { memoryMb: 1024 },
      }),
  },
  {
    id: "adminer",
    name: "Adminer",
    blurb: "A one-file web interface for the databases on this server. Handy, and not something to leave exposed.",
    category: "tools",
    spec: () =>
      base({
        name: "adminer",
        image: "adminer:latest",
        ports: [{ hostIp: "127.0.0.1", hostPort: 8082, containerPort: 8080, protocol: "tcp" }],
        limits: { memoryMb: 256 },
      }),
  },
  {
    id: "watchtower",
    name: "Watchtower",
    blurb:
      "Pulls newer images and restarts containers on its own. Consider the update check on the Images tab first — it tells you what is out of date without restarting anything unasked.",
    category: "tools",
    requires: "It needs the Docker socket, which is root on this server. Only run it if that trade is worth it to you.",
    spec: () =>
      base({
        name: "watchtower",
        image: "containrrr/watchtower:latest",
        mounts: [
          { type: "bind", source: "/var/run/docker.sock", target: "/var/run/docker.sock" },
        ],
        env: [{ name: "WATCHTOWER_CLEANUP", value: "true" }],
        limits: { memoryMb: 128 },
      }),
  },
]

export const TEMPLATE_CATEGORIES: { id: Template["category"]; label: string }[] = [
  { id: "web", label: "Web servers" },
  { id: "database", label: "Databases" },
  { id: "tools", label: "Tools" },
]
