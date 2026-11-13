# syntax=docker/dockerfile:1
# Container image (ADR 0036). Three stages: build the web UI, build the Go
# binary with the UI embedded, then copy the binary into a distroless image.

# Stage 1: web UI. Runs on the build host's platform: the output is static
# files, so it is the same for every target architecture.
FROM --platform=$BUILDPLATFORM node:22-alpine AS web
WORKDIR /src/web
COPY web/package.json web/package-lock.json ./
RUN npm ci
COPY web/ ./
RUN npm run build

# Stage 2: Go binary. Cross-compiles on the build host with CGO off, so no
# emulation is needed for linux/arm64.
FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS build
ARG TARGETOS
ARG TARGETARCH
ARG VERSION=dev
WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .
COPY --from=web /src/web/dist ./internal/ui/dist
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH \
    go build -trimpath -ldflags "-s -w -X main.version=${VERSION}" \
    -o /out/spyglass ./cmd/spyglass

# Stage 3: runtime. No shell, no Node, the binary only. Runs as the
# distroless nonroot user (uid 65532). The container writes nothing to disk.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/spyglass /usr/local/bin/spyglass
USER 65532:65532
EXPOSE 8080
ENTRYPOINT ["spyglass"]
CMD ["serve"]
