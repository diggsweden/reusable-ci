# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
#
# SPDX-License-Identifier: CC0-1.0

# Quality checks and automation for reusable-ci
# Run 'just' to see available commands

devtools_repo := env("DEVBASE_CHECK_REPO", "https://github.com/diggsweden/devbase-check")
devtools_dir := env("XDG_DATA_HOME", env("HOME") + "/.local/share") + "/devbase-check"
lint := devtools_dir + "/linters"
colors := devtools_dir + "/utils/colors.sh"

# Color variables
CYAN_BOLD := "\\033[1;36m"
GREEN := "\\033[1;32m"
BLUE := "\\033[1;34m"
MAGENTA := "\\033[1;35m"
NC := "\\033[0m"

# ==================================================================================== #
# DEFAULT - Show available recipes
# ==================================================================================== #

# Display available recipes
default:
    @printf "{{CYAN_BOLD}} Reusable CI{{NC}}\n"
    @printf "\n"
    @printf "Quick start: {{GREEN}}just setup-devtools{{NC}} | {{BLUE}}just verify{{NC}} | {{MAGENTA}}just lint-all{{NC}}\n"
    @printf "\n"
    @just --list --unsorted

# ==================================================================================== #
# SETUP - Development environment setup
# ==================================================================================== #

# ▪ Setup devtools (clone or update from XDG_DATA_HOME)
[group('setup')]
setup-devtools:
    #!/usr/bin/env bash
    set -euo pipefail
    if [[ -d "{{devtools_dir}}" ]]; then
        # setup.sh handles update checks with 1-hour cache
        if [[ -f "{{devtools_dir}}/scripts/setup.sh" ]]; then
            "{{devtools_dir}}/scripts/setup.sh" "{{devtools_repo}}" "{{devtools_dir}}"
        fi
    else
        printf "Cloning devbase-check to %s...\n" "{{devtools_dir}}"
        mkdir -p "$(dirname "{{devtools_dir}}")"
        git clone --depth 1 "{{devtools_repo}}" "{{devtools_dir}}"
        git -C "{{devtools_dir}}" fetch --tags --depth 1 --quiet
        latest=$(git -C "{{devtools_dir}}" describe --tags --abbrev=0 origin/main 2>/dev/null || echo "")
        if [[ -n "$latest" ]]; then
            git -C "{{devtools_dir}}" fetch --depth 1 origin tag "$latest" --quiet
            git -C "{{devtools_dir}}" checkout "$latest" --quiet
        fi
        printf "Installed devbase-check %s\n" "${latest:-main}"
    fi

# Check required tools are installed
[group('setup')]
check-tools: _ensure-devtools
    @{{devtools_dir}}/scripts/check-tools.sh --check-devtools mise git just rumdl yamlfmt actionlint gitleaks shellcheck shfmt gommitlint reuse

# Install tools via mise (alias for tools-install)
[group('setup')]
install: tools-install

# Install tools via mise
[group('setup')]
tools-install: _ensure-devtools
    #!/usr/bin/env bash
    source "{{colors}}"
    just_header "Install development tools" "mise install"
    just_run "Tools installation" mise install

# Update tools via mise
[group('setup')]
tools-update: _ensure-devtools
    #!/usr/bin/env bash
    source "{{colors}}"
    just_header "Update development tools" "mise upgrade && mise install"
    just_run "Tools update" mise upgrade
    just_run "Tools update" mise install

# ==================================================================================== #
# VERIFY - Quality assurance
# ==================================================================================== #

# ▪ Run linting and tests
[group('verify')]
verify: _ensure-devtools
    @just lint
    @just test

# ==================================================================================== #
# LINT - Code quality checks
# ==================================================================================== #

# ▪ Run all linters (override in project justfile to customize)
[group('lint')]
lint: lint-all

[group('lint')]
lint-all: _ensure-devtools
    @{{devtools_dir}}/scripts/verify.sh

# Validate version control
[group('lint')]
lint-version-control:
    @{{lint}}/version-control.sh

# Validate commit messages (gommitlint)
[group('lint')]
lint-commits:
    @{{lint}}/commits.sh

# Scan for secrets (gitleaks)
[group('lint')]
lint-secrets:
    @{{lint}}/secrets.sh

# Lint YAML files (yamlfmt)
[group('lint')]
lint-yaml:
    @{{lint}}/yaml.sh check

# Lint markdown files (rumdl)
[group('lint')]
lint-markdown:
    @{{lint}}/markdown.sh check

# Lint shell scripts (shellcheck)
[group('lint')]
lint-shell:
    @{{lint}}/shell.sh

# Check shell formatting (shfmt)
[group('lint')]
lint-shell-fmt:
    @{{lint}}/shell-fmt.sh check

# Lint GitHub Actions (actionlint)
[group('lint')]
lint-actions:
    @{{lint}}/github-actions.sh

# Validate reusable workflow contracts
[group('lint')]
lint-workflow-contracts:
	@go run ./cmd/reusable-ci validate workflow-input-defaults

# Check license compliance
[group('lint')]
lint-license:
    @{{lint}}/license.sh

# Lint containers
[group('lint')]
lint-container:
    @{{lint}}/container.sh

# Lint XML files
[group('lint')]
lint-xml:
    @{{lint}}/xml.sh

# ==================================================================================== #
# LINT-FIX - Auto-fix linting violations
# ==================================================================================== #

# ▪ Fix all auto-fixable issues
[group('lint-fix')]
lint-fix: _ensure-devtools lint-yaml-fix lint-markdown-fix lint-shell-fmt-fix
    #!/usr/bin/env bash
    source "{{colors}}"
    just_success "All auto-fixes completed"

# Fix YAML formatting
[group('lint-fix')]
lint-yaml-fix:
    @{{lint}}/yaml.sh fix

# Fix markdown formatting
[group('lint-fix')]
lint-markdown-fix:
    @{{lint}}/markdown.sh fix

# Fix shell formatting
[group('lint-fix')]
lint-shell-fmt-fix:
    @{{lint}}/shell-fmt.sh fix

# ==================================================================================== #
# TEST - Run tests
# ==================================================================================== #

# ▪ Run all Go tests (unit + integration)
[group('test')]
test: test-unit test-integration

# Run only Go unit tests (no //go:build integration tag — no real gpg / git / etc. required)
[group('test')]
test-unit:
    @go test -count=1 -race -buildvcs=false ./...

# Run Go integration tests (requires real gpg / git on PATH)
[group('test')]
test-integration:
    @go test -tags=integration -count=1 -race -buildvcs=false ./...

# Run binary end-to-end tests (builds the reusable-ci binary and exercises it as a black box)
[group('test')]
test-e2e:
    @go test -tags=e2e -count=1 -buildvcs=false ./cmd/...

# Run unit tests with verbose output
[group('test')]
test-unit-verbose:
    @go test -v -count=1 -race -buildvcs=false ./...

# Run integration tests with verbose output
[group('test')]
test-integration-verbose:
    @go test -v -tags=integration -count=1 -race -buildvcs=false ./...

# Run binary end-to-end tests with verbose output
[group('test')]
test-e2e-verbose:
    @go test -v -tags=e2e -count=1 -buildvcs=false ./cmd/...

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
    rm -f bin/coverage.out bin/coverage.html
    mkdir -p bin
    go test -tags=integration -count=1 -race -buildvcs=false -coverprofile=bin/coverage.out ./...
    go tool cover -html=bin/coverage.out -o=bin/coverage.html
    printf 'Coverage written to %s and %s\n' "bin/coverage.out" "bin/coverage.html"

# Generate merged coverage plus HTML report (alias for test-coverage)
[group('test')]
test-coverage-html: test-coverage

# Run a single Go test by name (e.g. `just test-go-one TestClassify`)
[group('test')]
test-go-one name:
    @go test -v -count=1 -race -run "{{name}}" ./...

# ==================================================================================== #
# LINT-GO - golangci-lint
# ==================================================================================== #

# ▪ Run golangci-lint with the repo's .golangci.yml
[group('lint')]
lint-go:
    @golangci-lint run ./...

# Run golangci-lint with auto-fix where supported
[group('lint-fix')]
lint-go-fix:
    @golangci-lint run --fix ./...

# Run all Go static analysis tools (vet + lint + test)
[group('lint')]
verify-go:
    @go vet ./...
    @golangci-lint run ./...
    @go test -count=1 -race -buildvcs=false ./...

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
# INTERNAL
# ==================================================================================== #

[private]
_ensure-devtools:
    @just setup-devtools
