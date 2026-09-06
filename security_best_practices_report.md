# Security Best-Practices Review

Date: 2026-09-05

Reviewed branch: `security/best-practices-review-20260905`

Base revision: `b27072d9481c4f676faa8ff6ef1e936d052f9a3e` (`main`)

Remediation completed: 2026-09-06 on the reviewed branch. Finding evidence and line references below
describe the base revision so the original review remains reproducible.

## Executive summary

Just Dashboard has a notably deliberate security design for a root-equivalent administration
application. The review confirmed several strong controls, including network allowlisting before
authentication, capability checks on API routes, secure session-cookie defaults, two-factor
authentication enabled by default, request-body limits, parameterized database access, argv-based
host command execution, audit middleware, and defensive archive path joining.

The review identified **2 High, 4 Medium, and 2 Low** issues. No Critical issue was identified. All
eight findings were subsequently remediated on this branch and are mapped to their fixes below.

The two highest-risk issues are:

1. A narrowed `JD_FILE_ROOTS` boundary can be bypassed when a path traverses an existing outward
   symlink followed by two or more nonexistent path components. File-write users can consequently
   create or extract content outside an allowed root.
2. Cookie-authenticated state-changing HTTP routes do not enforce an origin or anti-CSRF signal.
   `SameSite=Strict` blocks cross-*site* requests, but not requests from an attacker-controlled
   sibling origin on the same site. Because JSON handlers also accept simple `text/plain` requests,
   this can reach root-equivalent administrative operations when an administrator has a live
   session.

The review also found a read-only SSRF/scanning surface, an unrestricted recursive disk-usage
endpoint that bypasses file roots, archive extraction without expansion quotas, a supported
configuration that conflicts with the documented mandatory-2FA invariant, a WebSocket origin
check that ignores scheme, and no browser Content Security Policy.

This is a source review, not a claim that every finding is exploitable in every deployment. Each
finding below lists the required conditions and the controls that reduce its likelihood.

## Remediation status

| ID | Status | Fix |
| --- | --- | --- |
| JD-SEC-001 | Resolved | `c50140b` walks to and resolves the nearest existing ancestor before accepting missing descendants, with mutation regressions for symlink escapes. |
| JD-SEC-002 | Resolved | `3ec6d05` requires a browser-only mutation header, enforces JSON media types and single JSON values, and updates every frontend mutation path. |
| JD-SEC-003 | Resolved | `a1f20c7` moves live TLS, port-scan, and DNS probes behind `system.admin`. |
| JD-SEC-004 | Resolved | `1d97fb9` confines scans to file roots and adds result, traversal, directory-entry, and concurrency limits. |
| JD-SEC-005 | Resolved | `5400ff7` adds actual expanded-byte, entry-count, free-space, concurrency, cancellation, and execution-time extraction limits. |
| JD-SEC-006 | Resolved | `6939eed` removes the 2FA configuration bypass and always starts password sessions with a second factor outstanding. |
| JD-SEC-007 | Resolved | `ff2392b` compares WebSocket scheme, normalized hostname, and effective port, retaining only complete explicit exceptions. |
| JD-SEC-008 | Resolved | `b1f70f0` supplies per-document CSP nonces, a Caddy deny-all fallback, and a restrictive Permissions-Policy. |

Post-remediation validation:

- `go test ./...`: passed with the parent shell's `TMUX` and legacy configuration variables removed;
  those variables otherwise redirect the repository's real-tmux tests or alter their fixtures.
- `go test -race` passed for `internal/httpx`, `internal/files`, `internal/sysinfo`, `internal/auth`, and
  `internal/wsx`.
- `go vet ./...` and `go build ./...`: passed.
- `bun run lint` and `bun run build`: passed; all application routes are now dynamically rendered so
  each response receives a fresh CSP nonce.
- Caddy configuration validation and `docker compose config -q`: passed.
- A production build served through the shipped Caddy topology returned the nonce CSP and
  Permissions-Policy; all 14 emitted scripts carried the response nonce, and a headless Chromium load
  reported no CSP violations.

## Scope and validation

The review covered the Go backend and the TypeScript/React/Next.js frontend at the revision above,
with `CLAUDE.md` treated as the canonical architecture and security-invariant document. It focused
on authentication, authorization, state-changing routes, path confinement, command execution,
SSRF, uploads and archives, browser security boundaries, dependency exposure, and resource limits.

Validation performed:

- `bun run lint`: passed.
- `bun run build`: passed; 48 static pages generated.
- `go vet ./...`: passed in a Go 1.25 container.
- Backend tests: all exercised packages passed except one environment-dependent UFW parser test.
  `TestUFWParsesAPortWrittenWithoutAProtocol` reaches no detected firewall backend in the minimal
  container and then indexes an empty result. This appears to be a test-environment issue, not a
  security defect in the parser.
- `bun audit --production`: reported four DOMPurify advisories through Monaco Editor. The bundled
  Monaco call sites were inspected and do not use the affected configurations or APIs; details are
  in “Scanner alerts assessed as non-applicable.”
- `govulncheck ./...`: reported two Docker Engine vulnerabilities through the Docker client module.
  The vulnerable behavior is daemon-side and is not implemented by this application; details are
  below.
- Tracked-file secret-name scanning found no committed secret file. `.env.example` contains only
  example configuration.

## Findings overview

| ID | Severity | Finding |
| --- | --- | --- |
| JD-SEC-001 | High | File-root confinement can be bypassed through an outward symlink and missing descendants |
| JD-SEC-002 | High | Same-site CSRF can reach root-equivalent state-changing routes |
| JD-SEC-003 | Medium | TLS certificate scanner exposes authenticated SSRF and internal-network probing |
| JD-SEC-004 | Medium | Disk-usage endpoint bypasses `JD_FILE_ROOTS` and permits expensive recursive scans |
| JD-SEC-005 | Medium | Archive extraction lacks expanded-size, entry-count, and execution-time quotas |
| JD-SEC-006 | Medium | Supported configuration conflicts with the mandatory-2FA security invariant |
| JD-SEC-007 | Low | WebSocket origin validation compares host but not scheme |
| JD-SEC-008 | Low | Shipped frontend has no Content Security Policy |

## High severity

### JD-SEC-001 — File-root confinement can be bypassed through an outward symlink and missing descendants

**Rule:** GO-PATH-001 — prevent path traversal and enforce filesystem boundaries after canonical
resolution.

**Locations:**

- `backend/internal/files/files.go:52-86` (`Resolve`)
- `backend/internal/files/files.go:89-130` (`ResolveEntry`)
- `backend/internal/files/files.go:466-474` (`Mkdir`)
- `backend/internal/files/files.go:534-591` (`Copy` / `copyPath`)
- `backend/internal/files/archive.go:128-155` (`Extract`)
- `backend/internal/files/files_test.go:10-41` (existing confinement tests)
- `backend/internal/auth/roles.go:25-44` (role capabilities)
- `backend/internal/api/handlers_files.go:38-48,230-240,363-379` (file-write routes)
- `CLAUDE.md:1272-1278` (canonical path-resolution invariant)

**Evidence:** `Resolve` first tries `filepath.EvalSymlinks(abs)`. If the target does not exist, it
tries only the target’s immediate parent. When both the target and immediate parent are missing,
the function accepts and returns the lexically cleaned absolute path. `ResolveEntry` has the same
fallback. Mutation paths then pass the accepted path to `os.MkdirAll`, recursive copy, or archive
extraction.

For example, with an allowed root `/allowed` and an existing symlink
`/allowed/jump -> /outside`, resolving `/allowed/jump/missing/sub` fails for both the full path and
its immediate parent. The lexical prefix check succeeds because the unresolved string begins with
`/allowed`. `os.MkdirAll` subsequently follows `jump` and creates `/outside/missing/sub`. Copy and
extract can then write content under that directory.

The existing tests cover lexical traversal, an existing symlink whose target exists, and a single
nonexistent leaf beneath a real parent. They do not cover an outward symlink followed by multiple
nonexistent components. The implementation predates the documented invariant that every
client-supplied path must be resolved through the file-root confinement layer; the later invariant
did not close this edge case.

**Impact:** An authenticated user with `CapFileWrite`, including the limited role, can create
directories or write copied/extracted files outside configured file roots. Because the service is
root-equivalent, the escaped writes inherit the process’s filesystem privileges.

**Exploit conditions and mitigating factors:** This requires a deployment that narrows
`JD_FILE_ROOTS` below `/` and an existing outward-pointing symlink inside an allowed root. The
default root `/` has no meaningful outer boundary to escape, and the attacker still needs an
authenticated file-write role. Outward symlinks are nevertheless realistic in deployment,
release, storage, and compatibility trees.

**Recommended fix:**

1. For a missing target, walk upward to the nearest existing ancestor, resolve all symlinks in that
   ancestor, verify the resolved ancestor is inside a canonicalized allowed root, and only then
   append the missing suffix.
2. Add regression tests for an outward symlink followed by two or more nonexistent descendants for
   `Mkdir`, directory copy, and archive extraction.
3. Treat the canonical-string check as an interim defense. To eliminate time-of-check/time-of-use
   races in a root-equivalent service, perform mutations relative to trusted directory file
   descriptors using Linux `openat2` resolution restrictions (for example, `RESOLVE_BENEATH` and
   `RESOLVE_NO_MAGICLINKS`) or a carefully designed `openat`/`O_NOFOLLOW` traversal.

**False-positive notes:** A fully trusted-user deployment with `JD_FILE_ROOTS=/` is not gaining a
security boundary from the root list, so this issue has little incremental effect there. It is a
real boundary violation wherever operators rely on narrowed roots to constrain the limited role.

### JD-SEC-002 — Same-site CSRF can reach root-equivalent state-changing routes

**Rules:** GO-HTTP-006, REACT-CSRF-001, NEXT-CSRF-001 — cookie-authenticated mutations must validate
an unforgeable anti-CSRF signal and/or a trusted origin.

**Locations:**

- `backend/internal/httpx/authmw.go:18-30` (session cookie flags)
- `backend/internal/api/routes.go:79-86` (authenticated middleware chain)
- `backend/internal/httpx/respond.go:94-100` (JSON decoding)
- `frontend/src/lib/api.ts:92-103` (browser request headers)
- `backend/internal/api/handlers_docker.go:50-81` (container mutation routes)
- `backend/internal/api/handlers_docker_manage.go:39-126,503-527` (create/lifecycle handlers)
- `backend/internal/dockerx/create.go:376-386,441-450,557-572` (privileged and bind-mount options)

**Evidence:** Session cookies are correctly `HttpOnly`, `Secure`, and `SameSite=Strict`, but the
authenticated HTTP middleware does not validate `Origin`, `Referer`, Fetch Metadata, or a CSRF
token/header. The frontend does not send a universal anti-CSRF header. JSON decoding also does not
require `application/json`, so a cross-origin browser request can use the CORS-simple
`Content-Type: text/plain` while its body remains valid JSON.

`SameSite` is a site boundary rather than an origin boundary. An attacker-controlled sibling origin
such as `https://evil.example.com` is same-site with `https://dashboard.example.com`; a credentialed
request from that page can therefore include the dashboard cookie. The response does not need to
be readable for a state change to occur. Bodyless lifecycle POSTs require even fewer request
features. With an active administrator session, the container-creation surface permits privileged
mode, host networking, and bind mounts, so a successful forged mutation can become host control.

**Impact:** A malicious same-site sibling origin can perform administrative actions with the
victim’s active session, potentially obtaining root-equivalent control through container creation
or other service-management routes.

**Exploit conditions and mitigating factors:** The attacker needs control of another origin under
the same scheme and registrable domain, a victim with a live sufficiently privileged session, and
a request coming from an allowlisted client network. `SameSite=Strict` blocks ordinary unrelated
cross-site attacks. Default IP-address or localhost deployments may have no practical sibling
origin, but deployments under shared organizational domains commonly do.

**Recommended fix:** Add centralized protection to every unsafe HTTP method when browser session
authentication is used:

1. Require a custom header (for example, `X-JD-CSRF: 1`) or a synchronizer token that browsers on
   unrelated origins cannot send without a successful CORS preflight.
2. Validate `Origin` against an explicit normalized allowlist, with careful handling for requests
   that legitimately lack it; Fetch Metadata can be an additional defense.
3. Make the frontend send the header on all mutations.
4. Require `application/json` for JSON endpoints and reject trailing JSON values. Content-type
   enforcement is defense in depth, not a replacement for CSRF protection.
5. Exempt non-cookie authentication flows only deliberately, such as the separately authenticated
   HMAC webhook or bearer-token API clients.
6. Add integration tests rejecting same-site sibling-origin `text/plain` JSON requests and
   bodyless forged POSTs.

**False-positive notes:** A reverse proxy or external identity layer could add equivalent origin
or CSRF enforcement, but the shipped application and Caddy configuration do not. This does not
allow a random unrelated website to bypass `SameSite=Strict`.

## Medium severity

### JD-SEC-003 — TLS certificate scanner exposes authenticated SSRF and internal-network probing

**Rule:** GO-SSRF-001 — constrain server-side destinations and block private/control-plane address
ranges unless they are an explicit administrative feature.

**Locations:**

- `backend/internal/api/handlers_proxy.go:36-48` (route capabilities)
- `backend/internal/api/handlers_proxy_sites.go:171-194` (scan input)
- `backend/internal/proxysvc/tlsscan.go:140-175,351-400` (TLS and HTTP connections)
- `backend/internal/api/handlers_security.go:71-77` (administrative probe rationale)
- `backend/internal/api/handlers_security_test.go:412-429` (loopback accepted in scan test)

**Evidence:** The `/certificates/scan` route is registered for any authenticated role, before the
proxy-administration route group. The scan accepts an arbitrary domain and port,
opens a TLS connection to that destination, makes an HTTPS request to the selected port, and also
makes an HTTP request to port 80. It returns connection results, status, selected headers, and
redirect location. There is no private, loopback, link-local, or metadata-address restriction.

The neighboring generic network-probe endpoint is intentionally restricted to `CapSystemAdmin`
because its comments recognize that arbitrary probes turn the server into a network scanner. The
less-privileged certificate scanner has materially similar behavior, creating an authorization
inconsistency. A test explicitly uses a `127.0.0.1` destination.

**Impact:** A compromised read-only account can use the root-equivalent host to scan loopback and
internal services, send unauthenticated GET requests, and infer service availability and response
metadata that are not reachable from the user’s own network position.

**Exploit conditions and mitigating factors:** The attacker must authenticate and pass the outer
network allowlist. Response bodies are not returned, which limits data extraction, and the feature
is intentionally a scanner rather than a generic proxy.

**Recommended fix:** Require `CapSystemAdmin`, matching the generic network probes, or restrict
destinations to configured virtual hosts and watched certificate names. If arbitrary external
scanning remains supported, resolve through a controlled dialer that rejects loopback, private,
link-local, multicast, unspecified, and cloud metadata ranges for every resolved address and for
every redirect; connect to the validated address to resist DNS rebinding. Constrain ports and
consider making scans audited POST operations because they emit network traffic.

**False-positive notes:** If all read-only users are explicitly trusted to scan from the host, the
route may be intentional. That trust should be documented because it currently conflicts with the
authorization rationale on the generic probe route.

### JD-SEC-004 — Disk-usage endpoint bypasses `JD_FILE_ROOTS` and permits expensive recursive scans

**Rule:** GO-PATH-001 — enforce the documented file-root boundary on client-supplied paths; apply
resource bounds to attacker-controlled recursive work.

**Locations:**

- `backend/internal/api/handlers_system.go:15-25,188-204`
- `backend/internal/sysinfo/sysinfo.go:493-569`
- `CLAUDE.md:1272-1278`

**Evidence:** `GET /system/disk-usage` accepts an arbitrary `path` query parameter, defaults to `/`,
and sends it directly to `sysinfo.DirBreakdown` without calling the file service’s resolver. The
function enumerates the requested directory and recursively walks every child before sorting and
applying the result-count limit. The documented invariant says every client-supplied filesystem
path must pass through `files.Resolve` and remain under `JD_FILE_ROOTS`.

The endpoint implementation predates that invariant, and the later documentation change did not
bring the code into alignment.

**Impact:** A read-only authenticated user can enumerate names, paths, and aggregate sizes outside
narrowed file roots. Repeated requests can also trigger expensive full-tree walks against large or
slow filesystems, affecting service and host availability.

**Exploit conditions and mitigating factors:** Authentication and the network allowlist are still
required. When `JD_FILE_ROOTS=/`, which is the default, the information-scope bypass adds no new
filesystem reach. A handler timeout exists, but individual `ReadDir` calls are not interruptible
and the scan performs work before applying the output limit.

**Recommended fix:** Resolve the requested path through the file service and require it to fall
under an allowed root. If system overview intentionally needs broader access, define a separate,
explicit list of disk-usage roots and require an administrative capability; document that exception
in `CLAUDE.md`. Bound directory entries, visited files, total work, and concurrent scans, and return
partial/context-cancelled results without continuing background work.

**False-positive notes:** Operators who intentionally expose the entire filesystem to every role by
leaving `JD_FILE_ROOTS=/` may accept the disclosure. The unbounded-work concern remains.

### JD-SEC-005 — Archive extraction lacks expanded-size, entry-count, and execution-time quotas

**Rules:** GO-UPLOAD-001, GO-HTTP-002 — validate uploads and bound attacker-controlled request work.

**Locations:**

- `backend/internal/api/handlers_files.go:18-20,38-48,177-214,363-379`
- `backend/internal/files/archive.go:128-261`

**Evidence:** Uploads have a 2 GiB compressed/request-body limit, and archive paths are defensively
joined to prevent `../` traversal. Extraction, however, has no limit on total expanded bytes,
number of entries, per-entry size, filesystem free-space reserve, or execution duration. Both tar
and ZIP extraction stream each entry with unbounded `io.Copy`, and the extraction functions do not
accept a request context. A small highly compressed archive can therefore expand far beyond the
upload limit or create a very large number of files.

**Impact:** A limited file-write user can exhaust host disk space or inodes and consume sustained
CPU/I/O, degrading the dashboard and unrelated host services. Compression significantly lowers
the bandwidth and time needed compared with uploading the expanded content directly.

**Exploit conditions and mitigating factors:** The attacker must authenticate, pass the network
allowlist, and hold `CapFileWrite`. Such a user can already consume storage through normal uploads,
but an archive bomb substantially amplifies that capability.

**Recommended fix:** Enforce quotas on actual bytes written, entry count, per-entry size, path depth,
and extraction time. Treat archive-declared sizes as an early rejection signal but do not trust
them instead of counting copied bytes through a limiting writer. Check a configurable free-space
reserve, propagate cancellation/deadlines into extraction, define cleanup semantics for partial
output, and test high-ratio and high-entry-count archives.

**False-positive notes:** The file manager is intentionally powerful and may be restricted to
highly trusted operators. The missing amplification controls still create a preventable host-wide
availability risk.

### JD-SEC-006 — Supported configuration conflicts with the mandatory-2FA security invariant

**Rules:** GO-CONFIG-001, GO-AUTH-001 — secure deployment settings should fail closed and match the
documented authentication model.

**Locations:**

- `CLAUDE.md:11-13,1260-1268`
- `backend/internal/config/config.go:71-78`
- `backend/internal/auth/service.go:306-320`
- `install.sh:302-306`
- `docker-compose.yml:51`
- `.env.example:74-76`
- `README.md:350`

**Evidence:** The canonical security documentation describes two-factor authentication as mandatory
and says password-only sessions may access only two-factor setup and verification routes. In code,
`JD_REQUIRE_2FA=false` is a supported configuration. For a user who has not enrolled, the login
service then creates an already-elevated session after password authentication alone. The installer
offers this setting interactively, the Compose file passes it through, and public configuration
documentation advertises it.

The switch and authentication behavior existed before the later canonical invariant was written.
The discrepancy has not been resolved in either direction.

**Impact:** An operator can deploy the root-equivalent dashboard in a password-only mode that
violates the project’s stated authentication boundary. Credential theft then immediately grants the
account’s full capabilities rather than requiring a second factor.

**Exploit conditions and mitigating factors:** This is an operator-controlled configuration, not a
remotely mutable setting. The default is secure, and the installer warns before disabling 2FA. The
severity reflects configuration drift in a root-equivalent product rather than a default remote
bypass.

**Recommended fix:** Choose and enforce one model. Prefer removing the production switch and making
2FA mandatory for interactive human sessions, with any development-only bypass gated by an
explicit development mode that cannot be enabled accidentally. If optional 2FA is an intentional
product requirement, revise the canonical invariant and security claims, make the weakened mode
prominent in runtime health/UI warnings, and document which threat model it no longer satisfies.

**False-positive notes:** There is no vulnerability in a deployment that keeps the default
`JD_REQUIRE_2FA=true`. The issue is the conflict between a supported production mode and a stated
mandatory security property.

## Low severity

### JD-SEC-007 — WebSocket origin validation compares host but not scheme

**Rule:** GO-HTTP-006 — validate browser request origins as complete origins, not hostnames alone.

**Location:** `backend/internal/wsx/wsx.go:43-64`

**Evidence:** The WebSocket upgrader parses the `Origin` header but compares only `Origin.Host` to
`r.Host`. It does not require the expected scheme. Consequently an origin such as
`http://dashboard.example.com` can be accepted for a secure WebSocket served as
`https://dashboard.example.com` when the effective host/port strings match. The function’s own
comment identifies origin validation as the defense preventing malicious pages from opening an
authenticated terminal. No focused tests cover scheme mismatch or default-port normalization.

**Impact:** If an attacker can serve a cleartext page on the dashboard hostname and matching
effective port, that page may open an authenticated terminal, exec, or log WebSocket using the
victim’s live secure session.

**Exploit conditions and mitigating factors:** The default dashboard port `8443` usually produces a
host-string mismatch with a normal port-80 HTTP origin. Exploitation requires attacker control over
content on the same hostname and compatible port handling, plus a live authenticated session and
allowlisted network position.

**Recommended fix:** Compare normalized origin tuples—scheme, hostname, and effective port—against
explicit configured trusted origins. Derive the public scheme from trusted configuration or from
forwarded headers only when the immediate proxy is trusted. Add tests rejecting HTTP origins for
HTTPS/WSS service, including default ports and forwarded-proxy deployments.

**False-positive notes:** Deployments that never expose attacker-controlled HTTP content on the
dashboard hostname are not practically exploitable through this condition.

### JD-SEC-008 — Shipped frontend has no Content Security Policy

**Rules:** NEXT-CSP-001, REACT-CSP-001, JS-CSP-001 — deploy a restrictive CSP as browser-side
defense in depth.

**Locations:**

- `deploy/Caddyfile:82-88`
- `frontend/next.config.ts:22-30`
- `frontend/src/app/layout.tsx:13-29`
- `frontend/src/components/ui/chart.tsx:80-105`
- `backend/internal/httpx/middleware.go:102-113`

**Evidence:** The shipped Caddy configuration sets HSTS, `X-Content-Type-Options`,
`X-Frame-Options`, and a referrer policy, but no `Content-Security-Policy`. Next.js does not add one.
The Go middleware’s restrictive CSP applies to API/backend responses rather than the static
frontend. The frontend includes a static inline theme bootstrap and generated inline chart styles,
which will need deliberate nonce, hash, or externalization handling.

No exploitable user-controlled HTML/JavaScript sink was established in this review. This finding is
therefore defense in depth: a CSP would reduce the impact of a future injection defect in a UI that
controls root-equivalent actions.

**Impact:** A future XSS or compromised frontend dependency would face fewer browser-enforced
restrictions on script execution, framing, resource loading, and outbound connections.

**Exploit conditions and mitigating factors:** CSP absence is not independently exploitable. React
escaping, limited uses of `dangerouslySetInnerHTML`, sandboxed raw previews, and the existing
security headers reduce adjacent risks.

**Recommended fix:** Introduce a strict CSP in report-only mode, gather violations, then enforce it.
Prefer nonce- or hash-authorized scripts, externalize or hash the static theme bootstrap, and avoid
`unsafe-eval` if Monaco’s deployed bundle permits it. A reasonable target includes
`default-src 'self'`, `object-src 'none'`, `base-uri 'self'`, `frame-ancestors 'none'`, narrowly
scoped `connect-src` for same-origin HTTP/WebSocket access, and the minimum required style policy.
Add an explicit `Permissions-Policy` separately.

**False-positive notes:** An operator-managed outer proxy could inject a CSP, but the supported
Compose/Caddy deployment does not. A generic CSP should not be enabled without testing Monaco,
charts, themes, and WebSocket connections.

## Scanner alerts assessed as non-applicable

These items should remain visible to maintainers, but the reviewed application does not reach the
vulnerable behavior described by the advisories.

### DOMPurify through Monaco Editor

`bun audit --production` reports `dompurify@3.4.8` through `monaco-editor@0.56.0`. The advisories
cover specific unsafe state/configuration combinations: `IN_PLACE` sanitization after DOM
clobbering, persistent configuration reuse through `setConfig`, `CUSTOM_ELEMENT_HANDLING` predicate
reuse, and custom Trusted Types policy reuse. The bundled Monaco sanitizer was inspected: it uses
per-call configurations with `RETURN_DOM_FRAGMENT` or `RETURN_TRUSTED_TYPE`, installs temporary
hooks, and removes those hooks in a `finally` block. It does not use the affected configuration
features. No reachable XSS was demonstrated.

Primary advisories:

- [GHSA-55q2-fjhq-7xh7](https://github.com/advisories/GHSA-55q2-fjhq-7xh7)
- [GHSA-cmwh-pvxp-8882](https://github.com/advisories/GHSA-cmwh-pvxp-8882)
- [GHSA-c2j3-45gr-mqc4](https://github.com/advisories/GHSA-c2j3-45gr-mqc4)
- [GHSA-vxr8-fq34-vvx9](https://github.com/advisories/GHSA-vxr8-fq34-vvx9)

**Maintenance action:** Upgrade Monaco/DOMPurify when a compatible release is available and keep
the sanitizer call-pattern assumption covered during dependency updates. This is dependency hygiene,
not an actionable application vulnerability at the reviewed revision.

### Docker Engine advisories reported through the Go client dependency

`govulncheck` reports GO-2026-4887 and GO-2026-4883 because the application imports the Docker
module. The first issue is an authorization-plugin bypass in the Docker daemon when oversized
request bodies are used; the second is a daemon-side privilege-check problem during plugin
installation. Just Dashboard is a Docker API client. It neither implements the daemon authorization
layer nor exposes Docker plugin installation, so the vulnerable functions are not part of this
application’s security boundary.

Primary advisories:

- [GO-2026-4887](https://pkg.go.dev/vuln/GO-2026-4887) and the corresponding
  [Moby advisory](https://github.com/moby/moby/security/advisories/GHSA-x744-4wpc-v9h2)
- [GO-2026-4883](https://pkg.go.dev/vuln/GO-2026-4883) and the corresponding
  [Moby advisory](https://github.com/moby/moby/security/advisories/GHSA-pxq6-2prw-chj9)

**Maintenance action:** Patch the host Docker Engine independently where the affected features are
enabled. Upgrading the client module alone does not remediate a vulnerable daemon.

## Positive controls observed

- Network allowlisting is applied before authentication, reducing credential exposure to rejected
  clients.
- Session cookies use `HttpOnly`, `Secure`, and `SameSite=Strict`, and session material is not stored
  in browser local storage.
- Two-factor authentication is enabled by default, password hashing uses Argon2id, randomness uses
  `crypto/rand`, and stored sensitive values use authenticated encryption.
- API routes use centralized capability checks, with destructive actions separated and confirmed.
- State-changing authenticated requests pass through centralized audit middleware.
- JSON bodies and webhook bodies are bounded; the webhook verifies an HMAC over raw request bytes.
- SQL access is parameterized, and dynamically selected database identifiers are allowlisted or
  quoted.
- Host commands are generally executed with argv arrays rather than shell concatenation.
- Archive entry paths are defensively joined and checked against extraction traversal.
- Caddy binds the backend to loopback and supplies TLS/security headers in the supported deployment.
- Raw file previews use restrictive sandboxing and response policies.

## Suggested remediation order

1. Fix JD-SEC-001 and add regression tests for every mutating file operation.
2. Add centralized CSRF/origin enforcement for all cookie-authenticated mutations (JD-SEC-002).
3. Restrict the scan and disk-usage routes (JD-SEC-003 and JD-SEC-004).
4. Add archive expansion quotas and cancellation (JD-SEC-005).
5. Resolve the implementation/documentation decision on mandatory 2FA (JD-SEC-006).
6. Tighten WebSocket origin comparison and deploy a tested CSP (JD-SEC-007 and JD-SEC-008).
