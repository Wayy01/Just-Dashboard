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
  ports: { ip?: string; privatePort: number; publicPort?: number; type: string }[]
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
}

export type ComposeStack = {
  name: string
  workingDir: string
  configFiles: string[]
  services: {
    name: string
    container: string
    state: string
    status: string
    image: string
  }[]
  running: number
  total: number
  managed: boolean
}

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
}

export type LogLine = {
  text: string
  level?: string
  timestamp?: string
  source?: string
  /**
   * "stdout" or "stderr", sent only by the container log socket, which streams
   * dockerx.LogLine rather than logsx.Line. File and journal tails do not have
   * two streams to distinguish.
   */
  stream?: string
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

export type TerminalSession = {
  id: string
  title: string
  shell: string
  /** The host account the shell logged in as. */
  user: string
  persisted: boolean
  tmuxName: string
  createdAt: string
  owner: string
  rows: number
  cols: number
  pid: number
  attached: number
  lastActive: string
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

export type DbConnection = {
  id: number
  name: string
  driver: "postgres" | "mysql" | "mongodb"
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
  raw: string
}

export type FirewallStatus = {
  backend: "ufw" | "iptables"
  available: boolean
  enabled: boolean
  defaultPolicy?: string
  rules: FirewallRule[]
  raw?: string
  error?: string
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
}

export type GitResult = {
  command: string
  output: string
  ok: boolean
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
  rebootRequired: boolean
  rebootPackages?: string[]
  lastChecked: string
  error?: string
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
