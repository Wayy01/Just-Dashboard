# Blueprints and game servers

## Why a blueprint system

A giant app store optimizes the first five minutes and creates a permanent maintenance and supply-chain
burden. The project already rejected that trade in the Docker starting points. 0.6.7 keeps that principle
but formalizes the small, useful version: reviewed, versioned blueprints that produce the same normalized
build/runtime plan as every other entry path.

A blueprint is not an executable script downloaded at click time. It is project-owned data plus a closed
set of permitted operations, shipped and tested with the dashboard release.

## Blueprint contract

Each immutable blueprint version declares:

- id, display name, category, description, icon id and documentation URL;
- provenance, license, maintainer, reviewed date, minimum dashboard version and update notes;
- workload profile: HTTP app, database, tool, worker, game server, or Compose stack;
- source images pinned by compatible tag policy and resolved to a digest at deployment;
- input fields with type, label, description, default, validation, sensitivity and required condition;
- generated secrets and reference variables;
- ports with protocol, purpose, internal default and exposure recommendation;
- volumes/files with purpose, required persistence, owner/mode and backup recommendation;
- resource defaults and minimums;
- health/readiness checks;
- install, release, startup and graceful-stop operations from a closed operation vocabulary;
- safe editable configuration files and optional structured property mappings;
- automation presets such as backup-before-update or daily graceful restart;
- compatibility/update detector and migration notes;
- smoke-test fixtures and expected normalized plan.

Blueprint parsing rejects unknown fields. Rendering is pure and deterministic: the same version and
inputs produce the same secret-free plan digest. Secret generation is a separate server action and does
not change non-secret plan output.

## Supply-chain policy

- Built-ins live in the repository and are code-reviewed.
- CI validates schema, URLs, licenses, image references, port conflicts, secret defaults, persistence,
  health checks, generated Compose/spec output and documentation.
- No blueprint may ship a hard-coded default password.
- Mutable image tags are allowed only when the update policy explains them; every release records the
  resolved digest.
- Install content downloaded at deployment has a fixed authoritative host, size limit and checksum or an
  API-resolved version artifact recorded in the run.
- Privileged features are denied by default and require an explicit blueprint security declaration plus
  the same server-side admin authorization as a hand-built container.
- Blueprint updates never mutate a live deployment. They produce a reviewable pending plan diff.
- Export emits a portable Compose/spec plus secret-name manifest so the workload can outlive the
  dashboard.

## Initial reviewed set

The release should prove categories rather than chase catalogue size:

- HTTP: Nginx/static, Uptime Kuma, Vaultwarden;
- databases: PostgreSQL, MariaDB/MySQL, Redis;
- tools: Gitea, Adminer;
- automation: n8n or equivalent after license/image review;
- game: Minecraft Java and Bedrock, with Java server software choices below.

Existing Docker templates are migration inputs. They either become blueprint versions or remain simple
container starting points backed by the same schema; there must not be two separate template catalogues.

## Game server profile

A game server adds domain-specific operations to an ordinary managed runtime:

- console input/output with explicit game process attachment;
- online player summary and supported moderation commands;
- version/runtime/update state;
- named TCP/UDP allocations and connection instructions;
- world/save directories and backup policies;
- scheduled console commands, restarts, backups and updates;
- editable server configuration with safe parsing where a known format exists;
- graceful save/stop sequence;
- EULA/license acceptance when required;
- import/export and mod/plugin directory links;
- clear separation between updating the server software and replacing its world/data.

Unsupported game protocols still receive console, files, schedules, ports, resources, backups and
ordinary container health. Player-specific controls appear only when a tested adapter can report them.

## Minecraft acceptance workload

Minecraft is the proof that the system can deploy more than an HTTP application. The creation journey
must cover:

### Choose the server

- Java or Bedrock;
- for Java: Vanilla, Paper, Purpur, Fabric, Forge, NeoForge, or a proxy profile where officially
  supportable;
- stable/recommended or an explicit version;
- new world, existing server import, world archive import, or supported modpack/server-pack import;
- a visible source and version for every downloaded server artifact.

Version availability comes from authoritative upstream APIs or release metadata through bounded,
cacheable adapters. If an upstream cannot be reached, creation explains that versions cannot currently be
verified; it does not substitute an unverified `latest` artifact.

### Configure the runtime

- server name and optional description;
- memory with a recommended range based on software/modpack and visible host headroom;
- CPU limit and optional affinity only in Advanced;
- maximum players, difficulty, game mode, whitelist, online mode, view/simulation distance, and PvP;
- explicit TCP/UDP port allocation with conflict and firewall evidence;
- persistent world/server-data volume;
- Java runtime image selected from the chosen server version;
- startup flags derived from memory and software, shown in Advanced;
- required Minecraft EULA acceptance with the exact linked agreement and recorded timestamp/actor.

The wizard must not promise a player count from memory alone. Recommendations are guidance with the
assumptions shown.

### Preflight

- artifact/version and checksum resolved;
- Java/image architecture compatible with the host;
- port free and firewall state known;
- volume writable with adequate disk/inode headroom;
- backup policy selected or explicitly declined;
- memory leaves a conservative host reserve;
- EULA accepted;
- stop command and readiness strategy available;
- imported archive passes path, size and root-layout checks.

### Runtime workspace

The game server project route adds these tabs/panels while retaining the ordinary release model:

- Overview: online/offline, version, players, connection address, CPU/memory/disk, last backup and update;
- Console: resumable output, command input, pause/follow/search, no shell escape;
- Players: online list plus whitelist, op, kick and ban only when the adapter supports reliable identity;
- Files: deep link into the server data root and quick links for `server.properties`, logs, worlds, mods
  and plugins;
- Backups: linked backup policy, runs, download and typed restore;
- Schedules: graceful restart, warning broadcasts, backups, console commands and update windows;
- Settings: structured known properties with the raw file preview and a restart-required indicator;
- Deployments: server software/image updates and rollback; world data is explicitly not rolled back.

### Safe update sequence

1. Detect and describe the available software/image update.
2. Resolve artifact and compatibility before stopping anything.
3. Require a successful recent backup according to policy.
4. Announce a configurable countdown to online players when supported.
5. Send save command, wait for confirmation where supported, then graceful stop.
6. Create a release from the new image/software and existing data volume.
7. Start and verify process readiness plus a game-port handshake.
8. If verification fails, restore the prior release artifact against the unchanged volume.
9. Never restore world data automatically. Offer the linked backup restore as a separate typed action.

### Schedules and automation

The basic schedule editor offers human choices such as “Every day at 04:00 UTC” and always displays the
equivalent cron expression and next three run times. Advanced accepts validated cron.

Actions can be chained only from a closed set: broadcast, save, backup, stop, start, restart, update, or
console command. The default update preset is broadcast -> save -> backup -> update -> verify. A failed
required parent prevents later steps and is visible in history.

## Import behavior

Minecraft import accepts a server directory selected through Files or an uploaded zip. It previews:

- detected server software/version and evidence;
- chosen root directory;
- world, mods/plugins, configuration, logs and cache paths;
- current Java/start command when inferable;
- proposed persistent mount and backup scope;
- proposed port and conflicts;
- files ignored as cache or runtime output.

Import copies into a dashboard-managed data location by default. “Use in place” is Advanced, path-bounded,
and explains ownership/mode effects. Nothing starts until the operator reviews the normalized plan.

## Blueprint and Minecraft test fixtures

- one fixture per blueprint version with expected normalized plan and rendered Compose/spec;
- generated secret values prove absent from snapshots and logs;
- version API fixtures cover success, unavailable, malformed and moved/latest responses;
- archive fixtures cover absolute paths, traversal, symlinks, excessive entries/size and ambiguous roots;
- Java/Bedrock TCP/UDP allocation conflicts;
- EULA absent/present and acceptance audit;
- new world, imported world, modded server and existing in-place server journeys;
- clean update, backup failure, graceful-stop timeout, readiness failure and artifact rollback;
- proof that release rollback does not alter the world volume;
- keyboard and screen-reader coverage for console status, schedule builder and destructive restore.

