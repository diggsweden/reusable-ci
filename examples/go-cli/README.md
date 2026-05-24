<!--
SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government

SPDX-License-Identifier: CC0-1.0
-->

# Go CLI example

A Go command-line tool released as standalone binaries. This is the
`artifact-first` Go path: `build-go.yml` compiles release binaries before the
publish stage.

## Files

- `artifacts.yml` declares `project-type: go` with `config.build-mode: artifact-first`.
- `release-workflow.yml` calls the release orchestrator and attaches `release-artifacts/*/my-cli-*` to the GitHub Release.
- `pullrequest-workflow.yml` runs reusable-ci PR checks and leaves `go test`, `go vet`, and `govulncheck` to a caller-owned test workflow.

## Output Layout

`build-go.yml` writes binaries under:

```text
dist/linux-amd64/my-cli-linux-amd64
dist/linux-arm64/my-cli-linux-arm64
dist/darwin-amd64/my-cli-darwin-amd64
dist/darwin-arm64/my-cli-darwin-arm64
```

Set `config.platforms` to change the release target list.

GitHub release creation downloads the build artifact contents under
`release-artifacts/<goos>-<goarch>/`. Filenames include the platform suffix so
GitHub Release assets have unique basenames.
