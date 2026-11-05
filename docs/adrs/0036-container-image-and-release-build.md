# ADR 0036: Container image and release build

Status: Completed

Date: 2026-10-21

## Context

v1 ships as one static binary in a distroless container. The UI is built with Node and embedded, so the build has three stages and Node never reaches the runtime image.

## Decision

- Multi-stage Dockerfile: (1) Node stage runs `npm ci && npm run build` in `web/`; (2) Go stage copies the UI build into `internal/ui/dist` and runs `CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=…"`; (3) runtime stage `gcr.io/distroless/static-debian12:nonroot` with the binary only.
- Runs as the distroless `nonroot` user (uid 65532). Entrypoint `llm-proxy`, command `serve`.
- Multi-arch: `linux/amd64` and `linux/arm64`.
- Image tags follow release versions. Deployments pin a version, never `latest`.
- The container writes nothing to disk and needs egress only to the provider base URLs and Postgres.

## Consequences

- No shell in the image; debugging uses logs and metrics.
- The stop grace period must match `server.drain_timeout`.

## Work required

- Write the `Dockerfile` and `.dockerignore`.
- Add the release workflow: build and push multi-arch images with `docker buildx`, tag from the git tag, inject the version.
- Add a smoke test that runs the image against a Postgres container and checks `/healthz`, `/readyz` and `/ui/`.
- Add a size and non-root check to CI.
- Optionally publish release binaries for the personal local setup (ADR 0037).

## Depends on

- ADR 0028, ADR 0029.

## References

- architecture/deployment.md, "Container image", "What runs".
- architecture/frontend.md, "Build and embedding".
