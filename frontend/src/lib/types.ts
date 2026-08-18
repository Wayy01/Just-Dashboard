export type Capability =
  | "read"
  | "service.control"
  | "file.write"
  | "terminal"
  | "destructive"
  | "system.admin"

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
  readRate: number
  writeRate: number
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
