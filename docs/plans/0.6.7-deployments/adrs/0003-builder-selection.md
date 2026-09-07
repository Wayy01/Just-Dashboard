# ADR 0003: Automatic builder selection

- Status: accepted
- Date: 2026-09-02
- Release: 0.6.7

## Context

The creation contract requires useful automatic build detection, exact preview, immutable OCI output,
bounded offline behavior, explicit build-secret scope, multiple architectures, and a support burden that
fits a self-hosted single-server project. The plan required a spike across Nixpacks, Cloud Native
Buildpacks (`pack`), and a small project-owned recipe layer.

## Evidence

| Candidate | Evidence | Disposition |
| --- | --- | --- |
| Nixpacks | The upstream README marks it maintenance mode and recommends Railpack. It is MIT licensed and emits OCI images, but selecting a maintenance-only builder for a new release would immediately create a migration obligation. | Rejected. |
| Cloud Native Buildpacks / `pack` | Active CNCF project and Apache-2.0 CLI. It has strong lifecycle isolation, caching, SBOM support, target metadata, and reproducibility when source, builder image, and buildpacks are fixed. It also requires choosing and maintaining trusted builder/buildpack images; buildpacks execute source during detect/build and the lifecycle has distinct privileged registry phases. | Not selected for 0.6.7; retained as a future adapter. |
| Project recipes over BuildKit | No new production dependency; exact generated Dockerfile and argv can be shown before build; uses the Docker/BuildKit boundary the product already ships; architecture follows the host builder; supported ecosystems and secret guarantees can fail closed. Coverage must be deliberately narrower and maintained here. | Selected. |

Primary references:

- [Nixpacks maintenance notice](https://github.com/railwayapp/nixpacks)
- [Railpack successor statement](https://github.com/railwayapp/railpack)
- [`pack` source and Apache-2.0 license](https://github.com/buildpacks/pack)
- [Buildpacks lifecycle phases](https://buildpacks.io/docs/for-buildpack-authors/concepts/lifecycle-phases/)
- [Buildpacks reproducibility limits](https://buildpacks.io/docs/for-app-developers/concepts/reproducibility/)
- [Buildpacks trusted-builder boundary](https://buildpacks.io/docs/for-platform-operators/how-to/integrate-ci/pack/concepts/trusted_builders/)

## Decision

0.6.7 ships a deterministic detector plus reviewed recipes that render a multi-stage Dockerfile and
explicit `docker buildx build` plan. Dockerfile, image, and Compose remain first-class alternatives, so an
unsupported repository is blocked with those recovery choices rather than built by a guess.

Initial automatic recipes cover:

- Node applications with exactly one recognized lockfile (`bun`, npm, pnpm, or yarn), including Next.js
  and common static-output scripts;
- Go modules with a detected main package or an explicit operator choice;
- Python applications with a pinned/locked requirements source and an explicit detected or selected
  ASGI/WSGI start target;
- static directories whose build output is explicit.

Detection is pure over a bounded file manifest and selected small file contents. It emits candidates with
evidence and confidence; ambiguity is a choice, never a hidden priority rule. The selected recipe,
versions, base-image digest, generated Dockerfile digest, build argv, cache policy, target platform, and
secret ids/scopes are stored in the immutable release.

Base images are maintained as a reviewed closed catalogue. A tag is resolved to a digest before the
release build and the digest is what the release records. Automatic recipes never run an install script
fetched by the detector. Ecosystem package commands run only inside the isolated BuildKit build, from the
repository's lockfile and selected recipe.

Build secrets use BuildKit secret mounts and are available only to the named recipe step. A recipe that
cannot keep a requested secret out of layers, cache metadata, and argv refuses the scope. Runtime secrets
are never passed to the build. Build output passes through the deployment redactor before persistence.

The builder reports unavailable when Docker, Buildx, a supported platform, or a required locked input is
missing. It does not fall back to host-native package installation or a mutable `latest` base.

## Reconsideration criteria

A later release may add Railpack or `pack` as another adapter after pinning the executable and builder
images, documenting update ownership and offline behavior, and passing the same preview, redaction,
multi-architecture, and live failure fixtures. It does not replace the explicit Dockerfile path.

## Consequences

- The first automatic-build matrix is narrower than hosted platforms, but every supported plan is
  inspectable and uses an existing security boundary.
- The project owns recipe updates and fixtures.
- No Nix, lifecycle, or builder-image dependency is added to the production image for 0.6.7.

