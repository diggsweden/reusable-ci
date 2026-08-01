# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
#
# SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

# Dev-loop automation for reusable-ci.
#
# Lint/security orchestration is delegated to nanolinter (pinned in
# .mise.toml, plan in nanolinter.toml). `just lint` runs the verify plan;
# every per-check recipe (`lint-yaml`, `lint-go-vet`, …) is a thin
# `*args`-forwarding wrapper around `nanolinter lint <check>`.
#
# Quick start:
#   mise install         # install nanolinter + every check tool
#   just doctor          # confirm tool health
#   just verify          # run the verify plan + go tests

# Build paths and binary identity
bin := "./bin"
dist := "./dist"
executable := "reusable-ci"

# Dynamic build metadata. Mirrors .goreleaser.yml's ldflags so local builds
# report the same `--version` shape as released binaries. Backtick failures
# fall through to the sentinel values cmd/reusable-ci/main.go defaults to.
version := `git describe --tags --dirty --always --abbrev=12 2>/dev/null || echo dev`
commit := `git rev-parse HEAD 2>/dev/null || echo none`
build_date := `date -u +'%Y-%m-%dT%H:%M:%SZ'`

# Color codes used by _require-tool / clean-build error messages.
CYAN_BOLD := "\\033[1;36m"
GREEN := "\\033[1;32m"
BLUE := "\\033[1;34m"
MAGENTA := "\\033[1;35m"
RED := "\\033[1;31m"
NC := "\\033[0m"

# ==================================================================================== #
# DEFAULT - Show available recipes
# ==================================================================================== #

# Display available recipes
default:
    @printf "{{CYAN_BOLD}} Reusable CI{{NC}}\n"
    @printf "\n"
    @printf "Quick start: {{GREEN}}mise install{{NC}} | {{BLUE}}just verify{{NC}} | {{MAGENTA}}just lint-fix{{NC}}\n"
    @printf "\n"
    @just --list --unsorted

# ==================================================================================== #
# SETUP - Toolchain
# ==================================================================================== #

# ▪ Install pinned dev tools (alias for tools-install)
[group('setup')]
install: tools-install

# Install every tool pinned in .mise.toml
[group('setup')]
tools-install:
    @mise install

# Upgrade pinned tools and reinstall
[group('setup')]
tools-update:
    @mise upgrade
    @mise install

# Show nanolinter's tool/runtime health for the current verify plan
[group('setup')]
doctor: (_require-tool "nanolinter" "Install: mise install  (pinned in .mise.toml)")
    @nanolinter doctor

# ==================================================================================== #
# VERIFY - Composite gates
# ==================================================================================== #

# ▪ Run the nanolinter verify plan + go tests
[group('verify')]
verify: lint test

# ▪ Pre-commit one-shot: verify + build the host binary into bin/
[group('verify')]
dev: verify build-host

# ==================================================================================== #
# LINT - nanolinter verify plan
# ==================================================================================== #
#
# `just lint` runs the configured plan (nanolinter.toml). The per-check
# recipes forward *args so flags like `--disable-check`, `--lang go`,
# `--offline`, or `--fix` reach nanolinter directly:
#
#   just lint --offline                 # skip network-dependent checks
#   just lint --disable-check links     # one-off skip
#   just lint-markdown --fix            # fix markdown via nanolinter

# ▪ Run the full verify plan (nanolinter.toml)
[group('lint')]
lint *args: (_require-tool "nanolinter" "Install: mise install")
    @nanolinter verify {{args}}

# Run the Go umbrella suite (vet + staticcheck + vulncheck + golangci + wsl + format)
[group('lint')]
lint-go *args:
    @nanolinter lint go {{args}}

# Individual Go checks (each is also part of the umbrella above).
[group('lint')]
lint-go-vet *args:
    @nanolinter lint go-vet {{args}}

[group('lint')]
lint-go-staticcheck *args:
    @nanolinter lint go-staticcheck {{args}}

[group('lint')]
lint-go-vulncheck *args:
    @nanolinter lint go-vulncheck {{args}}

[group('lint')]
lint-go-golangci *args:
    @nanolinter lint go-golangci {{args}}

[group('lint')]
lint-go-wsl *args:
    @nanolinter lint go-wsl {{args}}

[group('lint')]
lint-go-format *args:
    @nanolinter lint go-format {{args}}

# Repo-hygiene and cross-language checks.
[group('lint')]
lint-version-control *args:
    @nanolinter lint version-control {{args}}

[group('lint')]
lint-commits *args:
    @nanolinter lint commits {{args}}

[group('lint')]
lint-secrets *args:
    @nanolinter lint secrets {{args}}

[group('lint')]
lint-license *args:
    @nanolinter lint license {{args}}

[group('lint')]
lint-yaml *args:
    @nanolinter lint yaml {{args}}

[group('lint')]
lint-markdown *args:
    @nanolinter lint markdown {{args}}

[group('lint')]
lint-shell *args:
    @nanolinter lint shell {{args}}

[group('lint')]
lint-shell-fmt *args:
    @nanolinter lint shell-fmt {{args}}

[group('lint')]
lint-actions *args:
    @nanolinter lint actions {{args}}

[group('lint')]
lint-container *args:
    @nanolinter lint container {{args}}

[group('lint')]
lint-xml *args:
    @nanolinter lint xml {{args}}

[group('lint')]
lint-toml *args:
    @nanolinter lint toml {{args}}

[group('lint')]
lint-typos *args:
    @nanolinter lint typos {{args}}

[group('lint')]
lint-links *args:
    @nanolinter lint links {{args}}

[group('lint')]
lint-sast *args:
    @nanolinter lint sast {{args}}

[group('lint')]
lint-osv *args:
    @nanolinter lint osv {{args}}

[group('lint')]
lint-trivy-fs *args:
    @nanolinter lint trivy-fs {{args}}

# Reusable-workflow contract check — guards against expression-shaped
# workflow_call defaults that GHA rejects at dispatch time. Not a
# nanolinter check; lives in the binary itself.
[group('lint')]
lint-workflow-contracts:
    @go run ./cmd/reusable-ci validate workflow input-defaults
    @go run ./cmd/reusable-ci validate workflow contract-residue

# ==================================================================================== #
# LINT-FIX - Auto-fix linting violations
# ==================================================================================== #

# ▪ Apply safe autofixes for every fix-capable check in the plan
[group('lint-fix')]
lint-fix *args: (_require-tool "nanolinter" "Install: mise install")
    @nanolinter fix {{args}}

# Normalize Go source: gofmt + go mod tidy. Use before commits when the
# diff is noisy from edits across many files.
[group('lint-fix')]
tidy:
    @go fmt ./...
    @go mod tidy

# ==================================================================================== #
# TEST - Go test suites
# ==================================================================================== #

# ▪ Run all Go tests (unit + integration)
[group('test')]
test: test-unit test-integration

# Run only Go unit tests (no //go:build integration tag — no real gpg / git / etc. required)
[group('test')]
test-unit:
    @go test -shuffle=on -count=1 -race -buildvcs=false ./...

# Run Go integration tests (requires real gpg / git on PATH)
[group('test')]
test-integration:
    @go test -shuffle=on -tags=integration -count=1 -race -buildvcs=false ./...

# Run binary end-to-end tests (builds the reusable-ci binary and exercises it as a black box)
[group('test')]
test-e2e:
    @go test -shuffle=on -tags=e2e -count=1 -buildvcs=false ./cmd/...

# Run unit tests with verbose output
[group('test')]
test-unit-verbose:
    @go test -v -shuffle=on -count=1 -race -buildvcs=false ./...

# Run integration tests with verbose output
[group('test')]
test-integration-verbose:
    @go test -v -shuffle=on -tags=integration -count=1 -race -buildvcs=false ./...

# Run binary end-to-end tests with verbose output
[group('test')]
test-e2e-verbose:
    @go test -v -shuffle=on -tags=e2e -count=1 -buildvcs=false ./cmd/...

# Run all tests with verbose output
[group('test')]
test-verbose: test-unit-verbose test-integration-verbose

# Run fuzz seed corpora only (CI-safe)
[group('test')]
test-fuzz:
    @go test -run=Fuzz ./...

# Run active fuzzing for all discovered fuzz tests
[group('test')]
test-fuzz-run fuzztime="14s":
    #!/usr/bin/env bash
    set -euo pipefail
    FUZZTIME="{{fuzztime}}"
    while IFS= read -r pkg; do
        while IFS= read -r testname; do
            [[ -n "$testname" ]] || continue
            go test "$pkg" -run=^$ -fuzz="^${testname}$" -fuzztime="$FUZZTIME"
        done < <(go test -list '^Fuzz' "$pkg" 2>/dev/null | grep '^Fuzz' || true)
    done < <(go list ./...)

# Run unit + integration coverage profile
[group('test')]
test-coverage:
    #!/usr/bin/env bash
    set -euo pipefail
    rm -f {{bin}}/coverage.out {{bin}}/coverage.html
    mkdir -p {{bin}}
    go test -tags=integration -count=1 -race -buildvcs=false -coverprofile={{bin}}/coverage.out ./...
    go tool cover -html={{bin}}/coverage.out -o={{bin}}/coverage.html
    printf 'Coverage written to %s and %s\n' "{{bin}}/coverage.out" "{{bin}}/coverage.html"

# Generate merged coverage plus HTML report (alias for test-coverage)
[group('test')]
test-coverage-html: test-coverage

# Run a single Go test by name (e.g. `just test-go-one TestClassify`)
[group('test')]
test-go-one name:
    @go test -v -count=1 -race -run "{{name}}" ./...

# Run tests for a single Go package (e.g. `just test-pkg ./internal/cliio`)
[group('test')]
test-pkg pkg:
    @go test -count=1 -race -buildvcs=false {{pkg}}

# ==================================================================================== #
# DOCS - Generated documentation
# ==================================================================================== #

# ▪ Regenerate docs/cli-reference.md from the urfave/cli command tree
[group('docs')]
gen-cli-reference:
    @go run ./cmd/gen-cli-reference > docs/cli-reference.md
    @printf "Regenerated docs/cli-reference.md\n"

# Verify docs/cli-reference.md is in sync with the CLI surface.
# Thin wrapper around the TestDocsCLIReferenceInSync go-test that
# already gates PR merges via the standard test infrastructure.
[group('docs')]
check-cli-reference:
    @go test ./internal/cli/docs/... -run '^TestDocsCLIReferenceInSync$' -count=1

# ==================================================================================== #
# BUILD - Local compilation
# ==================================================================================== #
#
# These recipes are for fast iteration outside the release path. The
# release binary is produced by .goreleaser.yml (see `just release-dry`
# to validate that config locally). LDFLAGS here mirror goreleaser's
# so `reusable-ci --version` reports the same shape either way.

# ▪ Build the host OS/arch binary into bin/ (fast dev iteration)
[group('build')]
build-host:
    #!/usr/bin/env bash
    set -euo pipefail
    GOARCH=$(just _host-goarch)
    GOOS=$(go env GOOS)
    mkdir -p {{bin}}
    CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" go build \
        -trimpath \
        -ldflags="-s -w -X main.version={{version}} -X main.commit={{commit}} -X main.date={{build_date}}" \
        -o {{bin}}/{{executable}} ./cmd/{{executable}}
    printf "Built %s/%s (%s/%s)\n" "{{bin}}" "{{executable}}" "$GOOS" "$GOARCH"

# Build linux+darwin × amd64+arm64 binaries into bin/ (release-shape sanity check)
[group('build')]
build-all:
    #!/usr/bin/env bash
    set -euo pipefail
    mkdir -p {{bin}}
    for goos in linux darwin; do
        for goarch in amd64 arm64; do
            out="{{bin}}/{{executable}}-${goos}-${goarch}"
            CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" go build \
                -trimpath \
                -ldflags="-s -w -X main.version={{version}} -X main.commit={{commit}} -X main.date={{build_date}}" \
                -o "$out" ./cmd/{{executable}}
            printf "Built %s\n" "$out"
        done
    done

# ==================================================================================== #
# RELEASE - Pre-tag validation
# ==================================================================================== #

# ▪ Snapshot build via goreleaser — validates .goreleaser.yml without tagging
[group('release')]
release-dry: (_require-tool "goreleaser" "Install: mise install goreleaser  (see https://goreleaser.com/install/)")
    @goreleaser check
    @goreleaser release --clean --snapshot

# ==================================================================================== #
# DEPENDENCIES - Routine dependency hygiene
# ==================================================================================== #
#
# Two axes of "update":
#   1. Go modules: `upgrade-deps` (minor/patch) + `upgrade-major` (semver-major TUI)
#   2. mise tools: `upgrade-mise` (rewrites .mise.toml pins to latest)
#                  vs. `tools-update` (just reinstalls per current pin)
#
# Use `upgrade-list` first to see what's available before bumping anything.

# ▪ Bump Go deps + mise tool pins
[group('dependencies')]
upgrade: upgrade-deps upgrade-mise

# Update direct + transitive Go dependencies (minor/patch only), then tidy
[group('dependencies')]
upgrade-deps:
    @go get -u -t ./...
    @go mod tidy

# Interactive TUI for major-version Go module bumps. `upgrade-deps` will
# never cross a semver-major boundary; this is the deliberate escape hatch.
[group('dependencies')]
upgrade-major: (_require-tool "go-mod-upgrade" "Install: go install github.com/oligot/go-mod-upgrade@latest")
    @go-mod-upgrade

# Rewrite every .mise.toml pin to the latest available version, then
# install. Review the resulting diff before committing — this is a
# different operation from `tools-update`, which keeps pins and only
# installs whatever the current pin spec already allows.
[group('dependencies')]
upgrade-mise:
    @mise upgrade --bump
    @mise install

# Show available updates without modifying anything. Combines the two
# bump surfaces: pinned dev tools (mise) and Go modules.
[group('dependencies')]
upgrade-list:
    @printf "=== mise (pinned tools) ===\n"
    @mise outdated || true
    @printf "\n=== Go modules (entries with [...] have updates available) ===\n"
    @go list -u -m all 2>/dev/null | grep -E '\[' || printf "No Go module updates available.\n"

# ==================================================================================== #
# GIT HOOK - Local pre-commit gating
# ==================================================================================== #

# Install the nanolinter-owned pre-commit hook (runs `verify --staged-with-stash`)
[group('hook')]
hook-install: (_require-tool "nanolinter" "Install: mise install")
    @nanolinter hook install

# Show whether the pre-commit hook is installed (local + global)
[group('hook')]
hook-status: (_require-tool "nanolinter" "Install: mise install")
    @nanolinter hook status

# Remove the nanolinter-owned pre-commit hook (refuses if hook is not nanolinter's)
[group('hook')]
hook-uninstall: (_require-tool "nanolinter" "Install: mise install")
    @nanolinter hook remove

# ==================================================================================== #
# MAINTENANCE - Clean build outputs and caches
# ==================================================================================== #

# ▪ Remove build outputs and clear Go/linter caches
[group('maintenance')]
clean: clean-build clean-caches

# Remove bin/ and dist/ (refuses to follow symlinks or non-directories)
[group('maintenance')]
clean-build:
    #!/usr/bin/env bash
    set -euo pipefail
    for dir in {{bin}} {{dist}}; do
        [[ -e "$dir" ]] || continue
        if [[ -L "$dir" || ! -d "$dir" ]]; then
            printf "{{RED}}✗ %s is not a regular directory; refusing to remove{{NC}}\n" "$dir" >&2
            exit 1
        fi
    done
    rm -rf {{bin}} {{dist}}
    printf "Removed %s and %s\n" "{{bin}}" "{{dist}}"

# Clear Go build/module/test/fuzz caches and golangci-lint's cache
[group('maintenance')]
clean-caches:
    @go clean -cache -modcache -testcache -fuzzcache
    @command -v golangci-lint >/dev/null 2>&1 && golangci-lint cache clean || true

# ==================================================================================== #
# INTERNAL
# ==================================================================================== #

# Fail fast with an actionable hint when a required external tool is missing.
# Use as a recipe prerequisite: `release-dry: (_require-tool "goreleaser" "Install: …")`.
[private]
_require-tool tool hint="":
    #!/usr/bin/env bash
    if command -v "{{tool}}" >/dev/null 2>&1; then
        exit 0
    fi
    printf "{{RED}}✗ %s not found{{NC}}\n" "{{tool}}" >&2
    if [[ -n "{{hint}}" ]]; then
        printf "  %s\n" "{{hint}}" >&2
    fi
    exit 1

# Print the host's Go GOARCH (amd64 / arm64); exit non-zero on unsupported arches.
# Use as `GOARCH=$(just _host-goarch)`.
[private]
_host-goarch:
    #!/usr/bin/env bash
    case "$(uname -m)" in
        x86_64) printf "amd64" ;;
        aarch64|arm64) printf "arm64" ;;
        *) printf "{{RED}}✗ Unsupported host architecture: %s{{NC}}\n" "$(uname -m)" >&2; exit 1 ;;
    esac
