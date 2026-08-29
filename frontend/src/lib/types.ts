export type Capability =
  "read" | "service.control" | "file.write" | "terminal" | "destructive" | "system.admin"

export type Role = "admin" | "limited" | "readonly"

export type DashboardUser = {
  id: number
  username: string
  role: Role
  totpEnabled: boolean
  disabled: boolean
  mustChangePassword: boolean
  lastLoginAt: string
  createdAt: string
}

export type AuthStatus = {
  authenticated: boolean
  user?: DashboardUser
  capabilities?: Capability[]
  needsTotp: boolean
  needsEnrollment: boolean
  require2fa: boolean
}

export type HostInfo = {
  hostname: string
  os: string
  platform: string
  platformVersion: string
  kernelVersion: string
  kernelArch: string
  virtualization: string
  bootTime: string
  uptimeSeconds: number
  processes: number
  cpuModel: string
  cpuCores: number
  cpuMhz: number
}

export type Snapshot = {
  ts: string
  cpu: {
    totalPercent: number
    perCore: number[]
    loadAvg1: number
    loadAvg5: number
    loadAvg15: number
    cores: number
    /** Where the time went. `steal` is the one a total can never express. */
    modes: CPUModes
  }
  memory: {
    total: number
    used: number
    free: number
    available: number
    cached: number
    buffers: number
    usedPercent: number
  }
  swap: { total: number; used: number; free: number; usedPercent: number }
  mounts: MountStats[]
  net: NetStats[]
  uptimeSeconds: number
  pressure: Pressure
  sockets: Sockets
  procs: ProcCounts
}

/**
 * The CPU breakdown, as percentages of the interval that sum to 100.
 *
 * `iowait` and `steal` are why this exists. One "68% busy" figure cannot tell
 * apart a server doing work, a server waiting for a disk, and a hypervisor
 * running somebody else on the core you are paying for — and the fix for each
 * is completely different.
 */
export type CPUModes = {
  user: number
  system: number
  nice: number
  iowait: number
  irq: number
  softirq: number
  steal: number
  idle: number
}

/**
 * Kernel pressure stall information: the share of the last ten seconds during
 * which work was waiting rather than running.
 *
 * `supported` is false on a kernel built without PSI, which the UI must show
 * as "cannot tell" rather than as three reassuring zeroes.
 */
export type Pressure = {
  supported: boolean
  cpuSome: number
  memSome: number
  memFull: number
  ioSome: number
  ioFull: number
}

export type Sockets = {
  tcpInUse: number
  tcpTimeWait: number
  tcpOrphan: number
  udpInUse: number
  used: number
}

/** The run queue. `blocked` counts tasks stuck in uninterruptible sleep — the
 *  half of the story that turns high iowait from a curiosity into a cause. */
export type ProcCounts = {
  running: number
  blocked: number
  total: number
}

/**
 * One bucket of recorded history.
 *
 * Every series carries its peak next to its mean because the mean is what
 * hides the interesting moment: a 100% second inside a ten-minute bucket
 * averages away to nothing, and a chart drawn only from means reports a quiet
 * night that was not quiet.
 */
export type MetricsHistoryPoint = {
  ts: string
  samples: number
  cpu: number
  cpuPeak: number
  mem: number
  memPeak: number
  swap: number
  swapPeak: number
  rx: number
  rxPeak: number
  tx: number
  txPeak: number
  diskRead: number
  diskReadPeak: number
  diskWrite: number
  diskWritePeak: number
  load1: number
  load1Peak: number
  diskPercent: number
  memUsed: number

  /** The CPU breakdown. Means only: a stack of peaks would sum past 100. */
  cpuUser: number
  cpuSystem: number
  cpuIowait: number
  cpuSteal: number

  psiCpu: number
  psiCpuPeak: number
  psiMem: number
  psiMemPeak: number
  psiIo: number
  psiIoPeak: number

  /** Operations per second, and the service time each one took. */
  diskReads: number
  diskReadsPeak: number
  diskWrites: number
  diskWritesPeak: number
  diskAwait: number
  diskAwaitPeak: number
  diskBusy: number
  diskBusyPeak: number

  tcpConns: number
  tcpConnsPeak: number
  tcpTimeWait: number

  load5: number
  load15: number
  memAvailable: number
  procs: number
  procsPeak: number
}

/**
 * One container's recent shape, for a table row.
 *
 * Buckets are maxima rather than means: at sixty pixels wide a mean flattens
 * exactly the spike the thumbnail exists to surface.
 */
export type ContainerSparkline = {
  name: string
  cpu: number[]
  mem: number[]
  cpuPeak: number
  memPeak: number
}

/** One bucket of a single container's recorded history. */
export type ContainerHistoryPoint = {
  ts: string
  samples: number
  cpu: number
  cpuPeak: number
  mem: number
  memPeak: number
  memBytes: number
  memBytesPeak: number
  memLimit: number
  pids: number
  /** Bytes per second, differenced from the cumulative counters Docker reports. */
  netRx: number
  netTx: number
  blockRead: number
  blockWrite: number
}

export type ContainerHistory = {
  name: string
  from: string
  to: string
  stepSeconds: number
  sampleIntervalSeconds: number
  retentionSeconds: number
  earliest: string | null
  points: ContainerHistoryPoint[]
}

/** One bucket of one filesystem's recorded capacity. */
export type MountHistoryPoint = {
  ts: string
  samples: number
  usedPercent: number
  usedPercentPeak: number
  used: number
  total: number
  /** Inodes fill independently of bytes, on a disk the capacity chart calls empty. */
  inodesPercent: number
}

export type MountHistory = {
  mountpoint: string
  points: MountHistoryPoint[]
}

export type StorageHistory = {
  from: string
  to: string
  stepSeconds: number
  sampleIntervalSeconds: number
  retentionSeconds: number
  earliest: string | null
  mounts: MountHistory[]
}

/**
 * Something that happened to the server, positioned in time so a chart can
 * mark it.
 *
 * This is what turns an observation into a cause: "memory climbed at 14:20"
 * versus "memory climbed at 14:20, and api-server was deployed at 14:19".
 */
export type MetricEvent = {
  ts: string
  kind: "deploy" | "backup" | "reboot" | "action"
  title: string
  detail?: string
  severity: "info" | "warning" | "error"
  /** Non-zero for events that occupied a span, so a long deploy can be a band. */
  durationSeconds?: number
}

/** One thing worth telling the operator, with the reasoning attached. */
export type HealthFinding = {
  id: string
  level: "critical" | "warning" | "notice"
  title: string
  /** What was measured. */
  detail: string
  /** What to do about it — an opinion, kept separate from the fact. */
  advice?: string
  metric?: string
  value: number
  threshold: number
  since?: string
}

export type Health = {
  status: "ok" | "critical" | "warning" | "notice"
  findings: HealthFinding[]
  checkedAt: string
  /** False when nothing is recording, which makes the verdict a shallower one. */
  recorded: boolean
}

/** One ban or unban, read from fail2ban's own log rather than remembered here. */
export type BanEvent = {
  action: "ban" | "unban"
  jail: string
  ip: string
  at: string
}

export type MetricsHistory = {
  from: string
  to: string
  /** Width of one bucket. A gap wider than this is missing data, not a flat line. */
  stepSeconds: number
  sampleIntervalSeconds: number
  retentionSeconds: number
  /** Oldest sample still retained, or null when nothing has been recorded yet. */
  earliest: string | null
  points: MetricsHistoryPoint[]
}

export type MountStats = {
  device: string
  mountpoint: string
  fstype: string
  total: number
  used: number
  free: number
  usedPercent: number
  inodesTotal: number
  inodesUsed: number
  readBytes: number
  writeBytes: number
  readRate: number
  writeRate: number
  readOps: number
  writeOps: number
  readLatencyMs: number
  writeLatencyMs: number
  busyPercent: number
}

export type NetStats = {
  interface: string
  bytesSent: number
  bytesRecv: number
  packetsSent: number
  packetsRecv: number
  errIn: number
  errOut: number
  dropIn: number
  dropOut: number
  sendRate: number
  recvRate: number
  addrs: string[]
  isUp: boolean
}

export type DirEntry = {
  name: string
  path: string
  size: number
  isDir: boolean
  entries: number
}

export type ContainerPort = { ip?: string; privatePort: number; publicPort?: number; type: string }

export type Container = {
  id: string
  names: string[]
  name: string
  image: string
  imageId: string
  command: string
  state: string
  status: string
  health?: string
  createdAt: string
  startedAt?: string
  uptimeSeconds: number
  ports: ContainerPort[]
  labels: Record<string, string>
  networks: string[]
  composeStack?: string
  composeService?: string
  /** Writable-layer size. Present only when the listing was asked for sizes. */
  sizeRw?: number
}

export type ContainerDetail = Container & {
  env: string[]
  mounts: {
    type: string
    name?: string
    source: string
    destination: string
    mode: string
    rw: boolean
  }[]
  networkMode: string
  networkDetails: {
    name: string
    ipAddress: string
    gateway: string
    macAddress: string
    aliases: string[]
    networkId: string
  }[]
  restartPolicy: string
  privileged: boolean
  capAdd: string[]
  logPath: string
  exitCode: number
  error?: string
  restartCount: number
  entrypoint: string[]
  workingDir: string
  user: string
}

export type ContainerStats = {
  id: string
  name: string
  ts: string
  cpuPercent: number
  memUsage: number
  memLimit: number
  memPercent: number
  netRx: number
  netTx: number
  blockRead: number
  blockWrite: number
  pids: number
  onlineCpus: number
  /** Cumulative nanosecond totals. Only meaningful as a difference between two samples. */
  cpuTotal: number
  systemCpu: number
}

export type DockerImage = {
  id: string
  repoTags: string[]
  repoDigests: string[]
  size: number
  created: string
  containers: number
  labels: Record<string, string>
  dangling: boolean
}

export type DockerVolume = {
  name: string
  driver: string
  mountpoint: string
  createdAt: string
  scope: string
  labels: Record<string, string>
  size: number
  refCount: number
  inUse: boolean
}

export type DockerNetwork = {
  id: string
  name: string
  driver: string
  scope: string
  internal: boolean
  attachable: boolean
  ipv6: boolean
  created: string
  labels: Record<string, string>
  subnets: string[]
  containers: number
  /**
   * The containers attached, joined on the server from the container listing.
   * Docker's own network listing leaves its container map empty, so `containers`
   * was structurally zero on every host until this was joined in.
   */
  usedBy: string[]
}

export type ComposeService = {
  name: string
  container: string
  state: string
  status: string
  image: string
  health?: string
  ports: ContainerPort[]
  /** Declared by the compose file with no container behind it. */
  missing?: boolean
}

export type ComposeStack = {
  name: string
  workingDir: string
  configFiles: string[]
  services: ComposeService[]
  running: number
  total: number
  managed: boolean
}

export type StackDetail = ComposeStack & {
  configPath?: string
  declared: string[]
  declaredError?: string
  git?: {
    path: string
    branch?: string
    dirty: boolean
    changes: number
    ahead: number
    behind: number
    commit?: string
    subject?: string
  }
}

export type ComposeValidation = {
  valid: boolean
  error?: string
  normalised?: string
  services: string[]
}

/**
 * The dashboard's opinion of what Docker on this host is doing wrong.
 * `action` names a remedy the UI turns into a button; the empty ones are
 * findings whose fix is outside this panel.
 */
export type DockerFinding = {
  id: string
  level: "critical" | "warning" | "notice"
  title: string
  detail: string
  advice?: string
  scope: "container" | "stack" | "daemon" | string
  target?: string
  targetId?: string
  action?: string
  actionLabel?: string
}

export type DockerDiagnosis = {
  status: "ok" | "notice" | "warning" | "critical"
  findings: DockerFinding[]
  checkedAt: string
  checked: number
}

/** One line of `docker system df`. */
export type DockerDiskUsageLine = {
  total: number
  active: number
  size: number
  /**
   * What a prune would actually give back — Docker's own figure, which counts
   * an unused image's shared layers as staying put. The naive "size of the
   * things nothing is using" is always larger and is what made the old
   * reclaim button look broken.
   */
  reclaimable: number
}

export type DockerDiskUsage = {
  layersSize: number
  imagesSize: number
  containersSize: number
  volumesSize: number
  buildCacheSize: number
  images: DockerDiskUsageLine
  containers: DockerDiskUsageLine
  volumes: DockerDiskUsageLine
  buildCache: DockerDiskUsageLine
}

export type PruneReport = {
  kind: string
  spaceReclaimed: number
  items: string[]
  /** Set when this part of a sweep failed while the rest carried on. */
  error?: string
}

export type DockerEvent = {
  time: string
  type: string
  action: string
  name: string
  id?: string
  image?: string
  stack?: string
  exitCode?: string
  message: string
  level: "info" | "notice" | "error"
}

export type DockerEventFeed = {
  events: DockerEvent[]
  listening: boolean
  since: string
  buffered: number
}

/** Whether the tag a container runs still points where it did when pulled. */
export type ImageUpdateStatus = {
  ref: string
  state: "current" | "outdated" | "unknown" | "local"
  localDigest?: string
  remoteDigest?: string
  reason?: string
  checkedAt: string
}

export type ImageDetail = {
  id: string
  repoTags: string[]
  repoDigests: string[]
  size: number
  created: string
  architecture?: string
  os?: string
  author?: string
  labels: Record<string, string>
  entrypoint: string[]
  command: string[]
  workingDir?: string
  user?: string
  exposedPorts: string[]
  volumePaths: string[]
  env: string[]
  layers: {
    id: string
    created: string
    createdBy: string
    size: number
    comment?: string
    tags: string[]
  }[]
  usedBy: { id: string; name: string; state: string; stack?: string; service?: string }[]
}

export type VolumeUser = {
  id: string
  name: string
  state: string
  destination: string
  readOnly: boolean
  stack?: string
}

export type VolumeDetail = DockerVolume & {
  usedBy: VolumeUser[]
  options?: Record<string, string>
}

export type NetworkMember = {
  id: string
  name: string
  ipv4?: string
  ipv6?: string
  mac?: string
  aliases: string[]
  state?: string
  stack?: string
}

export type NetworkDetail = DockerNetwork & {
  gateway?: string
  options?: Record<string, string>
  members: NetworkMember[]
  system: boolean
}

export type FileChange = { path: string; kind: "modified" | "added" | "deleted" }

/**
 * The dashboard's description of a container to run — the shape the create
 * form speaks, translated to the Engine's ninety fields on the server.
 */
export type ContainerSpec = {
  name: string
  image: string
  command?: string[]
  entrypoint?: string[]
  env?: { name: string; value: string }[]
  ports?: PortMapping[]
  mounts?: MountSpec[]
  devices?: { host: string; container?: string; permissions?: string }[]
  labels?: { name: string; value: string }[]
  health?: HealthSpec
  limits: ResourceLimits
  networks?: string[]
  networkMode?: string
  hostname?: string
  extraHosts?: string[]
  dns?: string[]
  restartPolicy?: string
  maxRetries?: number
  /**
   * The log driver and its options. Present on the spec so an edit-and-recreate
   * keeps it — a field the round trip cannot carry is a setting silently reset
   * to Docker's unbounded default on the way through.
   */
  logging?: { driver?: string; options?: Record<string, string> }
  workingDir?: string
  user?: string
  stopSignal?: string
  privileged?: boolean
  capAdd?: string[]
  capDrop?: string[]
  init?: boolean
  autoRemove?: boolean
  tty?: boolean
  openStdin?: boolean
  readOnlyRootfs?: boolean
  pull?: "missing" | "always"
  start: boolean
}

export type PortMapping = {
  hostIp?: string
  hostPort: number
  containerPort: number
  protocol?: string
}

export type MountSpec = {
  /** "volume" is Docker-managed storage, "bind" a folder on the server, "tmpfs" memory. */
  type: "volume" | "bind" | "tmpfs"
  source?: string
  target: string
  readOnly?: boolean
  sizeMb?: number
}

export type HealthSpec = {
  test: string[]
  intervalSeconds?: number
  timeoutSeconds?: number
  startPeriodSeconds?: number
  retries?: number
  disable?: boolean
}

export type ResourceLimits = {
  memoryMb?: number
  memorySwapMb?: number
  cpus?: number
  pidsLimit?: number
  shmSizeMb?: number
}

export type CreateResult = {
  id: string
  name: string
  warnings: string[]
  started: boolean
}

export type SpecPreview = { run: string; compose: string }

export type PM2Process = {
  id: number
  name: string
  namespace: string
  status: string
  pid: number
  cpu: number
  memory: number
  restarts: number
  unstableRestarts: number
  uptimeMs: number
  execMode: string
  instances: number
  scriptPath: string
  cwd: string
  outLogPath: string
  errLogPath: string
  nodeVersion: string
  user: string
  watching: boolean
}

export type SystemdUnit = {
  name: string
  description: string
  loadState: string
  activeState: string
  subState: string
  unitFileState: string
  enabled: boolean
  mainPid?: number
  memoryBytes?: number
  tasks?: number
  activeSince?: number
  fragmentPath?: string
  result?: string
  restarts?: number
}

export type ProcessRow = {
  pid: number
  ppid: number
  name: string
  cmdline: string
  username: string
  status: string
  cpuPercent: number
  memPercent: number
  rss: number
  vms: number
  threads: number
  nice: number
  createTime: string
  cwd?: string
  exe?: string
}

export type CronJob = {
  line: number
  schedule: string
  /** Set only for /etc/crontab and /etc/cron.d entries, which name an account. */
  user?: string
  command: string
  comment?: string
  raw: string
  disabled: boolean
}

export type Crontab = {
  user: string
  source: string
  raw: string
  jobs: CronJob[]
  env: string[]
  comments: string[]
}

export type LogSource = {
  id: string
  label: string
  kind: "system" | "nginx" | "app" | "pm2" | "docker" | "journal"
  path?: string
  size?: number
  modified?: string
  rotated: boolean
  /** Rotated generations sitting next to a live file, and their total size. */
  archives?: number
  archiveBytes?: number
  /** One line saying what this source actually holds, for the reader who has never met auth.log. */
  detail?: string
  /** A live source's state — a stopped container still has logs worth reading. */
  status?: string
}

export type LogJournalUnit = {
  name: string
  description: string
  active: string
}

/**
 * Everything the viewer needs to offer a choice, in one request. The journal's
 * units ship with the sources rather than from a second fetch: putting every
 * unit in the source list would bury syslog under systemd's inventory, and
 * loading them separately leaves the unit picker empty for a second after the
 * journal is chosen, which reads as "this host has no units".
 */
export type LogSourceIndex = {
  sources: LogSource[]
  units: LogJournalUnit[]
  roots: string[]
  /** Why a source kind is absent — "no Docker here" is not the same as "no containers". */
  missing: Record<string, string>
}

export type LogLine = {
  text: string
  level?: string
  timestamp?: string
  source?: string
  /**
   * "stdout" or "stderr", sent wherever the producer distinguishes them — a
   * container and a PM2 process do, a plain file does not.
   */
  stream?: string
  /** 1-based line number within its file, set by the history search. */
  no?: number
  /** True for a line included because it sits next to a match, not because it matched. */
  context?: boolean
  /** Which file of a rotated set this came from. */
  file?: string
  /** Byte ranges of the search term, computed server-side where the browser cannot re-run a Go regexp. */
  match?: [number, number][]
}

/** One column of the search histogram, counted by level. */
export type LogBucket = {
  start: string
  total: number
  counts: Record<string, number>
}

export type LogSearchedFile = {
  path: string
  name: string
  archive: boolean
  scanned: number
  matched: number
  modified?: string
  error?: string
}

export type LogSearchResult = {
  lines: LogLine[]
  scanned: number
  matched: number
  truncated: boolean
  /** False when the scan ran out of time rather than out of file. */
  complete: boolean
  files: LogSearchedFile[]
  histogram: LogBucket[]
  bucketSeconds?: number
  first?: string
  last?: string
  tookMillis: number
}

/** The frame the live socket opens with, before any lines. */
export type LogStreamMeta = {
  kind: LogSource["kind"]
  label: string
  path?: string
  filtered: boolean
  prefill?: { lines: number; complete: boolean }
  archives?: number
  note?: string
}

export type LogRotateRule = {
  configFile: string
  patterns: string[]
  frequency?: string
  rotate?: string
  compress: boolean
  size?: string
  maxSize?: string
  missingOk: boolean
  options: string[]
}

export type LogRotateStatus = {
  available: boolean
  rules: LogRotateRule[]
  stateFile?: string
  lastRun?: string
}

/** The verdict for one file: is anything actually trimming this. */
export type LogRetention = {
  managed: boolean
  rule?: LogRotateRule
  pattern?: string
  summary: string
  level: "ok" | "warn" | "unknown"
  lastRun?: string
  available: boolean
}

export type JournalEntry = {
  timestamp: string
  message: string
  priority: number
  unit?: string
  pid?: string
  hostname?: string
  syslogIdentifier?: string
}

export type FileEntry = {
  name: string
  path: string
  size: number
  mode: string
  modeOctal: string
  isDir: boolean
  isSymlink: boolean
  linkTarget?: string
  linkBroken?: boolean
  modified: string
  owner: string
  group: string
  uid: number
  gid: number
  mimeHint?: string
}

export type FileListing = {
  path: string
  parent: string
  entries: FileEntry[]
  roots: string[]
}

export type FileContent = {
  path: string
  content: string
  size: number
  language: string
  binary: boolean
  modeOctal: string
}

export type VHost = {
  name: string
  kind: "nginx" | "caddy"
  path: string
  enabledPath?: string
  enabled: boolean
  serverNames: string[]
  listen: string[]
  upstreams: string[]
  tls: boolean
  certPath?: string
  modified: string
  size: number
}

export type Certificate = {
  name: string
  path: string
  domains: string[]
  issuer: string
  notBefore: string
  notAfter: string
  daysLeft: number
  expired: boolean
  expiring: boolean
  selfSigned: boolean
  source: string
  error?: string
}

export type Listener = {
  protocol: string
  address: string
  port: number
  pid: number
  process: string
  cmdline?: string
  user?: string
  exposed: boolean
}

export type DbDriver =
  "postgres" | "mysql" | "sqlite" | "sqlserver" | "clickhouse" | "oracle" | "mongodb" | "redis"

/**
 * What one engine can do, as the server reports it.
 *
 * The frontend deliberately keeps no table of its own: a tab that would 400 on
 * every request should not be offered, and the only thing that actually knows
 * which those are is the dialect registry on the server.
 */
export type DbDriverInfo = {
  id: DbDriver
  label: string
  kind: "sql" | "document" | "keyvalue"
  placeholder: string
  sql: boolean
  ddl: boolean
  columnTypes?: string[]
  filterOps?: string[]
}

export type DbConnection = {
  id: number
  name: string
  driver: DbDriver
  host: string
  port: string
  user: string
  database: string
  createdAt: string
}

/** One table, view or Mongo collection, as the schema browser lists it. */
export type DbTable = {
  schema: string
  name: string
  type: string
  estimatedRows: number
  size?: number
  comment?: string
}

export type DbColumn = {
  name: string
  type: string
  nullable: boolean
  default?: string
  key?: string
  position: number
}

export type QueryResult = {
  columns: string[]
  types: string[]
  rows: unknown[][]
  rowCount: number
  rowsAffected: number
  duration: string
  truncated: boolean
  statement: string
}

export type QueryRisk = {
  destructive: boolean
  level: "read" | "medium" | "high" | "critical"
  reasons: string[]
}

/** One index on a table, with the columns it covers in order. */
export type DbIndex = {
  name: string
  columns: string[]
  unique: boolean
  primary: boolean
}

/** One outgoing foreign key. Composite keys keep columns paired in order. */
export type DbForeignKey = {
  name: string
  columns: string[]
  refSchema?: string
  refTable: string
  refColumns: string[]
  onUpdate?: string
  onDelete?: string
}

/** Everything the Structure tab shows and everything row editing needs. */
export type DbTableDetail = {
  schema: string
  name: string
  columns: DbColumn[]
  primaryKey: string[]
  indexes: DbIndex[]
  foreignKeys: DbForeignKey[]
  createSql?: string
}

/** A named SQL snippet kept against a connection. */
export type DbSavedQuery = {
  id: number
  name: string
  sql: string
  createdAt: string
}

/** One entry in a connection's recent-statement history. */
export type DbHistoryEntry = {
  id: number
  sql: string
  risk: string
  success: boolean
  durationMs: number
  rowCount: number
  ranAt: string
}

export type OrmTarget = "prisma" | "drizzle" | "typescript" | "zod"

/** A generator the server offers, with the filename its download will use. */
export type OrmTargetInfo = {
  id: OrmTarget
  label: string
  filename: string
  description: string
}

/** One session the database server is currently running. */
export type DbActivity = {
  pid: string
  user?: string
  database?: string
  state?: string
  seconds: number
  query?: string
  client?: string
  wait?: string
  blockedBy?: string
  /** True for the connection that answered this request — never offer to kill it. */
  self?: boolean
}

export type DbActivityResponse = {
  sessions: DbActivity[]
  /** False on an engine with no server-side session list, e.g. SQLite. */
  supported: boolean
  reason?: string
}

/** One row found by the schema-wide value search. */
export type DbSearchMatch = {
  schema: string
  table: string
  column: string
  value: string
  row: Record<string, unknown>
}

export type DbSearchResult = {
  matches: DbSearchMatch[]
  tablesScanned: number
  tablesSkipped?: string[]
  truncated: boolean
}

/** What one table costs on disk. Row counts are the engine's estimate. */
export type DbTableSize = {
  schema: string
  table: string
  rows: number
  bytes: number
  dataBytes: number
  indexBytes: number
}

export type DbPoolStats = {
  open: number
  inUse: number
  idle: number
  waitCount: number
  waitDuration: string
  maxOpen: number
  maxIdleClosed: number
  maxLifetimeClosed: number
}

export type DbOverview = {
  schema: string
  tables: DbTableSize[]
  totalBytes: number
  totalRows: number
  tableCount: number
  /** False where the engine cannot report bytes — show rows and say so. */
  sizesKnown: boolean
  pool: DbPoolStats
}

/** One condition in the data grid's filter row. */
export type DbFilter = {
  column: string
  op: string
  value: string
}

/** A column being created or added, as the DDL form describes it. */
export type DbNewColumn = {
  name: string
  type: string
  notNull?: boolean
  primaryKey?: boolean
  default?: string
}

export type DbImportResult = {
  inserted: number
  failed: number
  errors: string[]
  errorsTruncated: boolean
  statement: string
}

/** Table name to column names, for editor completion. */
export type DbOutline = {
  schema: string
  tables: Record<string, string[]>
}

/** table -> its outgoing foreign keys, for the entity diagram. */
export type DbRelations = Record<string, DbForeignKey[]>

// --- Redis ---------------------------------------------------------------

export type RedisKeyInfo = {
  key: string
  type: string
  /** Seconds; -1 means no expiry, -2 means the key is gone. */
  ttl: number
  size: number
}

export type RedisPage = {
  keys: RedisKeyInfo[]
  cursor: number
  done: boolean
}

export type RedisZMember = { member: string; score: number }

export type RedisValue = {
  key: string
  type: string
  ttl: number
  string?: string
  list?: string[]
  set?: string[]
  hash?: Record<string, string>
  zset?: RedisZMember[]
  stream?: { id: string; values: Record<string, unknown> }[]
  truncated: boolean
}

// --- MongoDB -------------------------------------------------------------

export type MongoCollectionInfo = {
  indexes: DbIndex[]
  stats?: Record<string, unknown>
}

export type SystemUser = {
  username: string
  uid: number
  gid: number
  comment: string
  home: string
  shell: string
  groups: string[]
  system: boolean
  locked: boolean
  noPassword: boolean
  lastLogin?: string
  lastLoginFrom?: string
  sshKeyCount: number
  canLogin: boolean
}

export type SSHKey = {
  line: number
  type: string
  comment: string
  fingerprint: string
  bits?: number
  options?: string
  raw: string
}

export type FirewallRule = {
  number?: number
  action: string
  protocol?: string
  from: string
  to: string
  port?: string
  direction?: string
  comment?: string
  /** ufw's duplicate of the rule for the v6 table, not a second rule. */
  ipv6?: boolean
  service?: string
  /** Why this rule is dangerous, when it opens a sensitive port to everyone. */
  danger?: string
  raw: string
}

export type DefaultPolicy = {
  incoming?: string
  outgoing?: string
  routed?: string
}

/**
 * What this host's firewall can actually be told to do.
 *
 * ufw, firewalld and raw iptables answer the same questions differently — one
 * has an on/off switch, one has a service, one has no persistence at all — so
 * the status says what is possible and the UI hides the rest, with a reason.
 */
export type FirewallCapabilities = {
  editable: boolean
  toggle: boolean
  defaultPolicy: boolean
  logging: boolean
  reset: boolean
  profiles: boolean
  readOnlyReason?: string
}

export type FirewallStatus = {
  backend: "ufw" | "firewalld" | "iptables"
  available: boolean
  enabled: boolean
  defaultPolicy?: string
  policy: DefaultPolicy
  logging?: string
  /** firewalld's active zone. Absent for backends with no such idea. */
  zone?: string
  capabilities: FirewallCapabilities
  rules: FirewallRule[]
  raw?: string
  error?: string
}

/** A named port from the server's catalogue, with its warning attached. */
export type ServicePreset = {
  key: string
  name: string
  port: string
  protocol: string
  detail: string
  danger?: string
}

/** A ufw application profile, as the host's own packages define it. */
export type AppProfile = {
  name: string
  title?: string
  description?: string
  ports: string[]
}

export type Fail2banJail = {
  name: string
  currentlyFailed: number
  totalFailed: number
  currentlyBanned: number
  totalBanned: number
  bannedIps: string[]
  fileList: string[]
}

/** A jail's working policy: this many failures in this window earns this ban. */
/** What happened to a jail parameter change, in both halves. */
export type JailParamResult = {
  applied: boolean
  persisted: boolean
  file?: string
  output?: string
  warning?: string
}

export type JailConfig = {
  name: string
  banTime: number
  findTime: number
  maxRetry: number
  ignoreIp: string[]
  actions: string[]
  error?: string
}

export type Offender = {
  ip: string
  bans: number
  jails: string[]
  first: string
  last: string
}

export type BanSummary = {
  total: number
  bans: number
  unbans: number
  offenders: Offender[]
  byJail: Record<string, number>
  perDay: { day: string; count: number }[]
  since?: string
}

export type LoginSession = {
  user: string
  tty: string
  from: string
  loginTime?: string
  idle?: string
  pid?: number
  command?: string
  isSsh: boolean
}

export type BackupJob = {
  id: number
  name: string
  sources: string[]
  excludes: string[]
  targetKind: "local" | "s3" | "b2"
  target: {
    bucket?: string
    region?: string
    endpoint?: string
    prefix?: string
    path?: string
  }
  schedule: string
  retention: number
  enabled: boolean
  createdAt: string
  hasCredentials: boolean
  lastRun?: BackupRun
  nextRun?: string
}

export type BackupRun = {
  id: number
  jobId: number
  startedAt: string
  endedAt?: string
  status: "running" | "success" | "failed"
  artifact: string
  sizeBytes: number
  log: string
  trigger: string
  duration?: string
}

export type DeployProject = {
  id: number
  name: string
  repoPath: string
  branch: string
  composeFile: string
  preCommand?: string
  postCommand?: string
  hookId: string
  enabled: boolean
  createdAt: string
  hookUrl?: string
  currentSha?: string
  currentRef?: string
  dirty?: boolean
  lastRun?: DeployRun
  envVarCount: number
}

export type DeployRun = {
  id: number
  projectId: number
  startedAt: string
  endedAt?: string
  status: "running" | "success" | "failed"
  trigger: string
  actor: string
  fromCommit: string
  toCommit: string
  log: string
  duration?: string
  rollbackable: boolean
}

export type DeployCommit = {
  sha: string
  short: string
  author: string
  date: string
  subject: string
}

export type EnvVar = {
  key: string
  value?: string
  updatedAt: string
  masked: string
}

export type AuditEntry = {
  id: number
  ts: string
  userId: number
  username: string
  role: string
  ip: string
  actor: string
  action: string
  target: string
  method: string
  path: string
  status: number
  success: boolean
  detail: string
}

export type ApiToken = {
  id: number
  userId: number
  name: string
  prefix: string
  role: Role
  createdAt: string
  expiresAt?: string
  lastUsedAt?: string
  revoked: boolean
}

export type SessionInfo = {
  id: string
  userId: number
  twoFactorPassed: boolean
  ip: string
  userAgent: string
  createdAt: string
  lastSeenAt: string
  expiresAt: string
  current: boolean
}

// --- Git ---

export type GitRepo = {
  path: string
  name: string
  branch: string
  remote?: string
  head?: string
  subject?: string
  author?: string
  commitAt?: string
  dirty: boolean
  changes: number
  ahead: number
  behind: number
  detached: boolean
  untracked: number
}

export type GitFileChange = {
  path: string
  index: string
  worktree: string
  label: string
  staged: boolean
}

export type GitStatus = {
  repo: GitRepo
  files: GitFileChange[]
  clean: boolean
  stashes: number
}

export type GitCommit = {
  sha: string
  short: string
  subject: string
  author: string
  email: string
  at: string
  refs?: string
  insertions: number
  deletions: number
  files: number
  isMerge: boolean
  parents?: string[]
}

/** One node in the branch graph: a commit plus the lane its dot sits in. */
export type GitGraphCommit = GitCommit & { col: number }

export type GitGraph = {
  commits: GitGraphCommit[]
  /** How many lanes wide the busiest row gets — the canvas is sized from this. */
  lanes: number
}

export type GitBranch = {
  name: string
  current: boolean
  remote: boolean
  upstream?: string
  head?: string
  subject?: string
  at?: string
  ahead: number
  behind: number
  /** Another worktree that has this branch checked out; git refuses to delete
   *  or switch it even with -D. */
  worktree?: string
}

export type GitResult = {
  command: string
  output: string
  ok: boolean
}

/**
 * Who this server is, to GitHub, in one repository.
 *
 * The answer is per repository and not global, and `owner` is why: gh keeps
 * the token under the home of the host account that owns the checkout, which
 * is the same account git runs as when it pushes. Signing in for /srv/app does
 * not sign in for a repository owned by somebody else.
 */
export type GitHubAccount = {
  loggedIn: boolean
  host?: string
  login?: string
  name?: string
  avatarUrl?: string
  profileUrl?: string
  scopes?: string[]
  protocol?: string
  owner?: string
  /** Whether a push and a commit would actually use the account. */
  gitConfigured: boolean
  committerName?: string
  committerEmail?: string
  /** How origin is reached: an ssh remote never asks the token for anything. */
  remoteProtocol?: string
  /** gh's own words for why nobody is signed in. */
  reason?: string
}

export type GitHubStatus = {
  available: boolean
  account?: GitHubAccount
}

export type GitHubRepo = {
  nameWithOwner: string
  defaultBranch: string
  url: string
  private: boolean
  permission?: string
}

/** The code to type into github.com, and where to type it. */
export type GitHubDeviceStart = {
  id: string
  userCode: string
  verificationUri: string
  expiresIn: number
  interval: number
}

export type GitHubDeviceState = {
  status: "pending" | "complete" | "denied" | "expired"
  account?: GitHubAccount
  message?: string
}

export type GitPullRequest = {
  number: number
  title: string
  url: string
  state: string
  draft: boolean
  head: string
  base: string
  author?: string
  createdAt?: string
  comments: number
}

/**
 * The answer to "is this shell sitting inside a checkout" for a terminal's
 * working directory. `inRoots` is false for a real repository that falls
 * outside JD_GIT_ROOTS: it can be named but not operated on, since every other
 * git route is gated on those roots.
 */
export type GitDetect = {
  available: boolean
  inRoots?: boolean
  root?: string
  repo?: GitRepo
}

// --- System updates ---

export type UpdatePackage = {
  name: string
  current: string
  candidate: string
  origin?: string
  security: boolean
  arch?: string
}

export type UpdateReport = {
  available: boolean
  manager?: string
  packages: UpdatePackage[]
  securityCount: number
  /**
   * Whether this manager can tell a security update from any other. Alpine
   * and Arch publish no advisory data, so a zero count there means "cannot
   * tell", not "none outstanding".
   */
  securityFiltering: boolean
  rebootRequired: boolean
  rebootPackages?: string[]
  lastChecked: string
  error?: string
}

// --- The host's software ---
//
// Mirrors internal/updates' catalogue half. The upgrade report above answers
// "how far behind is this machine"; these answer "what is on it", "what else
// is there" and — the one nothing in this class offers — "now that it is
// installed, what do I type".

export type InstalledPackage = {
  name: string
  version: string
  arch?: string
  summary?: string
  /** Installed size in bytes, or absent where the manager reports none. */
  size?: number
  section?: string
  /** Somebody asked for this, as opposed to it arriving as a dependency. */
  explicit: boolean
  /** The package database says the system does not work without it. */
  essential?: boolean
  /** The pending version, joined on from the upgrade report by the server. */
  upgradable?: string
  security?: boolean
}

export type PackageInventory = {
  available: boolean
  manager?: string
  packages: InstalledPackage[]
  explicitCount: number
  totalSize?: number
  upgradeCount: number
  securityCount: number
  canInstall: boolean
  /**
   * Whether "and delete its configuration" means anything here. RPM keeps a
   * modified config file whatever it is asked, so the switch is hidden there
   * rather than shown doing nothing.
   */
  canPurge: boolean
  /**
   * When the repository index was last refreshed. Everything on the page is
   * read from that index, so a three-month-old one is a catalogue missing
   * every package added since — absent where the manager cannot say.
   */
  indexAge?: string
  canRefresh: boolean
  readAt: string
  error?: string
}

export type PackageSearchResult = {
  name: string
  version?: string
  summary?: string
  repository?: string
  installed: boolean
  installedVersion?: string
}

export type PackageDetail = {
  name: string
  version?: string
  installedVersion?: string
  installed: boolean
  summary?: string
  description?: string
  homepage?: string
  license?: string
  section?: string
  repository?: string
  arch?: string
  maintainer?: string
  size?: number
  dependencies?: string[]
  essential?: boolean
  /** Why the dashboard will not remove it; absent when it will. */
  protected?: string
  upgradable?: string
}

export type ManPage = {
  name: string
  /** The manual volume: 1 is a command, 5 a file format, 8 a root-only tool. */
  section: string
  path: string
}

/** What a package gives you and how to reach it, read from its own file list. */
export type PackageUsage = {
  package: string
  commands?: string[]
  manPages?: ManPage[]
  services?: string[]
  configFiles?: string[]
  docs?: string[]
  manual?: string
  manualFor?: string
  /** A page exists but `man` is not installed here, so it could not be read. */
  manUnavailable?: string
  truncated?: boolean
  /** No commands, pages or units — a library other packages use. */
  empty: boolean
}

/** How the dashboard itself can be reached, graded weakest-entry-first. */
/**
 * One entry from the host's own login accounting (wtmp, or btmp for failures).
 * Nothing here is recorded by the dashboard — it is the host's record, which
 * the Security page previously never showed.
 */
export type LoginRecord = {
  kind: "login" | "boot" | "shutdown"
  user: string
  tty: string
  from: string
  loginTime?: string
  endTime?: string
  /** How it ended when there is no end time: "down" or "crash". */
  ended?: string
  duration?: string
  active: boolean
}

export type Exposure = {
  grade: "tailscale" | "tunnel" | "private" | "public" | "open"
  summary: string
  allowlist: string[]
  interfaces: string[]
  tailscaleIp?: string
  recommendation?: string
}

/**
 * One terminal as the operator thinks of it: a named, filed piece of work that
 * may or may not have a PTY attached right now.
 *
 * `id` is present only while the dashboard is holding one; without it the
 * session is still running on the host and selecting it costs a reattach.
 */
export type TerminalWorkspace = {
  id?: string
  tmuxName?: string
  title: string
  folder?: string
  favourite: boolean
  /** One of TAG_COLOURS, or absent for "take the folder's". */
  colour?: string
  live: boolean
  persisted: boolean
  cwd?: string
  windows: number
  createdAt: string
  attached: number
  user?: string
  shell?: string
  owner?: string
}

/**
 * A folder in the rail. Unlike a session it has no tmux object of its own, so
 * the server keeps the record and reconciles it with what the sessions say.
 */
export type TerminalFolder = {
  name: string
  colour?: string
  collapsed?: boolean
}

/** A tmux window inside a session — a tab within a tab. */
export type TerminalWindow = {
  index: number
  name: string
  active: boolean
  panes: number
  cwd?: string
  colour?: string
  /** Flags tmux keeps: something happened here while you were elsewhere. */
  bell: boolean
  activity: boolean
  zoomed: boolean
  /** Every keystroke goes to every pane at once. */
  synchronized: boolean
}

/** One rectangle inside a window — tmux's third level. */
export type TerminalPane = {
  index: number
  active: boolean
  width: number
  height: number
  pid: number
  command?: string
  cwd?: string
  dead: boolean
  /** Where the rectangle sits in the window, in cells. Right/bottom inclusive. */
  left: number
  top: number
  right: number
  bottom: number
}

// --- detected and provisioned database servers ----------------------------

/**
 * A database server found running on this host. It deliberately carries no
 * password: what reaches the browser is the description of a connection that
 * could be made, not the means to make it.
 */
export type DbDetectedServer = {
  driver: DbConnection["driver"]
  container: string
  image: string
  host: string
  port: number
  user?: string
  database?: string
  /** Why this one cannot be connected to, when it cannot. */
  reason?: string
  /** The name of the connection already pointing at it, if there is one. */
  adopted?: string
  health?: string
  status?: string
}

export type DbDetected = { servers: DbDetectedServer[] }

/**
 * What POST /databases/sync did.
 *
 * `unreachable` is the half that has to be shown: a database this host is
 * running that was recognised and could not be connected to — almost always a
 * container on a compose network with no published port. Dropping it silently
 * is what made the reconcile look like it did nothing.
 */
export type DbUnreachableServer = {
  container: string
  driver: string
  reason: string
}

export type DbSyncResult = {
  added: string[]
  already: string[]
  unreachable?: DbUnreachableServer[]
  /** Servers running on the host itself, which nothing here can sign in to. */
  needsCredentials?: DbCredentialServer[]
}

/**
 * A database installed on the machine rather than in a container.
 *
 * Everything about it is known except the password: a container states its
 * credentials in its environment, and an apt-installed server keeps them in
 * its own catalogue, where no amount of reading the machine finds them. So the
 * page asks for that one thing, and the server dials before it saves anything.
 */
export type DbCredentialServer = {
  driver: string
  host: string
  port: number
  process?: string
  name: string
  /** The engine's own conventional account and database, to open the form with. */
  user?: string
  database?: string
}

export type DbProvisionOption = {
  engine: string
  label: string
  image: string
  driver: string
}

// --- schema graph (the diagram) -------------------------------------------

export type DbGraphColumn = {
  name: string
  type: string
  nullable: boolean
  primaryKey: boolean
  foreignKey?: string
  unique?: boolean
}

export type DbGraphTable = {
  schema: string
  name: string
  type: string
  rows: number
  columns: DbGraphColumn[]
}

export type DbGraphEdge = {
  name: string
  fromTable: string
  fromColumn: string
  toTable: string
  toColumn: string
  onDelete?: string
  cardinality: "one-to-one" | "many-to-one"
}

export type DbSchemaGraph = {
  schema: string
  tables: DbGraphTable[]
  edges: DbGraphEdge[]
  truncated: boolean
}

// ---------------------------------------------------------------------------
// The dashboard's own version, its changelog, and updating it in place.
//
// These mirror internal/selfupdate. The changelog is structured rather than
// markdown for the reason that package gives: the UI has to select the
// releases between the installed version and the newest, and paint a tag
// against each change — neither of which is a thing you can do to a paragraph.
// ---------------------------------------------------------------------------

export type ChangeKind = "added" | "changed" | "fixed" | "removed" | "security" | "deprecated"

export type ReleaseChange = {
  kind: ChangeKind
  text: string
  /** The sentence under the line, where the consequence is not obvious. */
  detail?: string
}

export type Release = {
  version: string
  /** A calendar day, YYYY-MM-DD. */
  date: string
  title: string
  summary?: string
  changes: ReleaseChange[]
  /** Needs the operator to do something by hand; never hidden behind a click. */
  breaking?: boolean
  breakingNote?: string
}

export type UpdateRunStatus = "pending" | "running" | "success" | "failed"
export type UpdatePhase = "queued" | "fetching" | "building" | "restarting" | "finished"

/** One upgrade attempt, as recorded on disk by the container that ran it. */
export type UpdateRun = {
  id: string
  status: UpdateRunStatus
  phase: UpdatePhase
  fromVersion: string
  toVersion: string
  ref: string
  dir: string
  compose: string
  image: string
  container: string
  health?: string
  fromCommit?: string
  toCommit?: string
  actor: string
  startedAt: string
  updatedAt: string
  finishedAt?: string
  error?: string
}

export type SelfUpdateReport = {
  version: string
  latest?: string
  available: boolean
  /** Releases newer than the installed version, newest first. */
  releases: Release[]
  /** Everything this build knows about its own past; needs no network. */
  history: Release[]
  breaking: boolean
  check: {
    enabled: boolean
    checkedAt?: string
    error?: string
    source?: string
    repo: string
    ref: string
  }
  install: {
    supported: boolean
    reason?: string
    dir?: string
    compose?: string
    /** Uncommitted changes in the checkout, in `git status --porcelain` form. */
    dirty?: string[]
  }
  run?: UpdateRun
  log?: string
}

/**
 * The security verdict. Mirrors netsec.Posture: the same three-field shape as
 * a health finding, because what was measured, what it means and what to do
 * are three different things and the UI renders them differently.
 */
export type SecurityFinding = {
  id: string
  level: "critical" | "warning" | "notice"
  title: string
  detail: string
  advice?: string
  area: "exposure" | "firewall" | "ssh" | "intrusion" | "ports" | "tls" | "updates"
  /** A remedy the dashboard can carry out itself, rendered as a button. */
  fix?: string
  fixLabel?: string
}

export type Posture = {
  status: "ok" | "notice" | "warning" | "critical"
  findings: SecurityFinding[]
  checkedAt: string
  checks: number
  /** Checks that could not run, because a check that did not run is not a pass. */
  skipped: string[]
}

export type SSHSetting = {
  key: string
  label: string
  value: string
  recommended: string
  secure: boolean
  detail: string
  risk?: string
  options?: string[]
  /** "list" is a space-separated set of account names; empty means unrestricted. */
  kind: "choice" | "number" | "list"
}

export type KeyedAccount = { user: string; keys: number }

/**
 * A systemd socket unit standing in front of sshd.
 *
 * Where one is active it owns the listener and sshd_config's Port is read and
 * ignored — the default on Ubuntu since 22.10. The page has to say so, because
 * otherwise the port control is the one setting that reports success and
 * changes nothing.
 */
export type SSHSocket = {
  unit?: string
  ports?: string[]
  dropIn?: string
}

export type SSHDConfig = {
  available: boolean
  source: string
  settings: SSHSetting[]
  ports: string[]
  managedFile?: string
  keyedAccounts: KeyedAccount[]
  hasMatchBlocks: boolean
  socket?: SSHSocket
  error?: string
}

export type SSHApplyResult = {
  written: boolean
  file: string
  valid: boolean
  output?: string
  reloaded: boolean
  reloadError?: string
  applied: string[]
  /** Whether the socket unit was moved onto the new port as well. */
  socketMoved?: boolean
  socketUnit?: string
  socketError?: string
}

export type Peer = {
  address: string
  count: number
  established: number
  ports: number[]
  processes: string[]
  private: boolean
  service?: string
}

export type Connections = {
  peers: Peer[]
  total: number
  listening: number
  loopback: number
}

export type NetInterface = {
  name: string
  addresses: string[]
  mac?: string
  mtu: number
  up: boolean
  loopback: boolean
  kind: "physical" | "tunnel" | "bridge" | "virtual" | "loopback"
  bytesSent: number
  bytesRecv: number
  public: boolean
}

export type Route = {
  destination: string
  gateway?: string
  interface?: string
  source?: string
  metric?: string
  family: "ipv4" | "ipv6"
  raw: string
}

export type NetworkInfo = {
  interfaces: NetInterface[]
  routes: Route[]
  resolvers: string[]
  search: string[]
}

export type ProbeResult = {
  tool: string
  target: string
  ok: boolean
  output: string
  records?: string[]
  duration: string
  error?: string
}

/** The live TLS report: what a visitor actually gets, not what is on disk. */
export type ProtocolResult = {
  name: string
  status: "offered" | "refused" | "unknown"
  detail?: string
}

export type ChainLink = {
  subject: string
  issuer: string
  notAfter: string
  isCa: boolean
  keyType?: string
  keyBits?: number
  selfIssued: boolean
}

export type HSTS = {
  maxAge: number
  includeSubDomains: boolean
  preload: boolean
  raw: string
}

export type HeaderCheck = {
  name: string
  value?: string
  present: boolean
  level: "important" | "optional"
  detail: string
}

export type HTTPScan = {
  statusCode: number
  server?: string
  plainRedirects: boolean
  plainStatus?: number
  plainLocation?: string
  plainError?: string
  hsts?: HSTS
  headers: HeaderCheck[]
}

export type ScanFinding = {
  id: string
  level: "critical" | "warning" | "notice"
  title: string
  detail: string
  advice?: string
}

export type TLSScan = {
  domain: string
  port: number
  checkedAt: string
  reachable: boolean
  error?: string
  grade: string
  summary: string
  negotiated?: string
  cipherSuite?: string
  protocols: ProtocolResult[]
  certificate?: Certificate
  chain: ChainLink[]
  chainComplete: boolean
  trusted: boolean
  trustError?: string
  nameMatches: boolean
  keyType?: string
  keyBits?: number
  signatureAlgorithm?: string
  fingerprint?: string
  serial?: string
  ocspStapled: boolean
  http?: HTTPScan
  findings: ScanFinding[]
}

export type DomainCheck = {
  domain: string
  addresses: string[]
  hostAddresses: string[]
  /**
   * False on every VPS behind provider NAT — AWS, Google Cloud, Azure and
   * Oracle all give the instance a private address and map a public one in
   * front of it — where the comparison cannot be made at all. Rendered as
   * "cannot tell", never as a mismatch.
   */
  hostAddressesKnown: boolean
  pointsHere: boolean
  behindProxy: boolean
  summary: string
  error?: string
}

export type CertbotCert = {
  name: string
  domains: string[]
  expiry: string
  daysLeft: number
  valid: boolean
  certPath?: string
  keyPath?: string
  serial?: string
}

export type CertbotState = {
  available: boolean
  version?: string
  certs: CertbotCert[]
  /** Whether anything is scheduled to renew these, and what. */
  autoRenew: boolean
  renewSource?: string
  raw?: string
  error?: string
}

/** A site as the dashboard describes it, not as nginx does. */
export type SiteLocation = {
  path: string
  upstream?: string
  root?: string
  webSockets: boolean
}

export type SiteSpec = {
  name: string
  domains: string[]
  kind: "proxy" | "static" | "redirect"
  upstream?: string
  root?: string
  redirectTo?: string
  permanent?: boolean
  tls: boolean
  certPath?: string
  keyPath?: string
  forceHttps: boolean
  hsts: boolean
  http2: boolean
  webSockets: boolean
  gzip: boolean
  blockExploits: boolean
  securityHeaders: boolean
  clientMaxBody?: string
  proxyTimeout?: number
  allowFrom: string[]
  denyFrom: string[]
  basicAuthFile?: string
  basicAuthRealm?: string
  accessLog: boolean
  locations: SiteLocation[]
  custom?: string
}

export type SiteResult = {
  name: string
  path: string
  content: string
  warnings: string[]
  validation?: { valid: boolean; output: string; command: string }
  enabled: boolean
  reloaded: boolean
  output?: string
}

/** A certbot DNS plugin — the only way to a wildcard, or past a CDN. */
export type DNSProvider = {
  key: string
  name: string
  plugin: string
  installed: boolean
  credentials: string
  defaultWait: number
}

export type ImportResult = {
  name: string
  certPath: string
  keyPath: string
  certificate: Certificate
  chainComplete: boolean
  warnings: string[]
}

/** One forwarded port for something that does not speak HTTP. */
export type StreamSpec = {
  name: string
  listen: number
  protocol: "tcp" | "udp"
  upstream: string
  proxyProtocol: boolean
  timeout?: number
  allowFrom: string[]
}

export type StreamStatus = {
  /** Whether nginx.conf actually pulls these in. Without it they are ignored. */
  included: boolean
  snippet: string
  dir: string
  streams: StreamSpec[]
}

/** An htpasswd file and who is in it. */
export type AuthFile = {
  name: string
  path: string
  users: string[]
}

/**
 * One long-running operation — a certificate issuance, a package upgrade, an
 * sshd apply.
 *
 * Unlike the compose runner's socket, a job is not owned by the console
 * watching it: closing the tab does not stop it, and reopening picks it up
 * where it is rather than starting it again.
 */
export type JobStatus = "running" | "succeeded" | "failed" | "cancelled"

export type Job = {
  id: string
  kind: string
  title: string
  target?: string
  status: JobStatus
  exitCode: number
  error?: string
  startedAt: string
  endedAt?: string
  startedBy?: string
  /** Every line produced, including any the buffer has dropped. */
  lines: number
}

export type JobLine = {
  /** Assigned by the job, and what a reconnecting console resumes from. */
  seq: number
  /** stdout, stderr, or status for the runner's own headings. */
  stream: string
  text: string
  at: string
}
