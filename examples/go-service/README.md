<!--
SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government

SPDX-License-Identifier: CC0-1.0
-->

# Go service example

A Go service shipped as a multi-arch container image. This is the
`container-first` Go path: the Containerfile is the build environment, and
`sbom-go.yml` emits the Build SBOM from `go.mod` / `go.sum` alongside the
container publish stage.

## Files

- `artifacts.yml` declares `project-type: go` with `config.build-mode: container-first`.
- `Containerfile.example` shows the required multi-stage pattern: `builder`, `export-binary`, and `runtime`.
- `release-workflow.yml` calls the release orchestrator; the container is published by `publish-container.yml`.
- `pullrequest-workflow.yml` runs reusable-ci PR checks and leaves `go test`, `go vet`, and `govulncheck` to a caller-owned test workflow.

## Containerfile Pattern

The build uses BuildKit's `TARGETOS` / `TARGETARCH` values, so `linux/amd64`
and `linux/arm64` compile natively on split GitHub-hosted runners. The optional
`extract.binary` block reuses the same builder stage to upload `${name}-binaries-${arch}`
artifacts that can be attached to a GitHub Release.
