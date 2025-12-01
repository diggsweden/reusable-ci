# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
#
# SPDX-License-Identifier: CC0-1.0

# Quality checks and automation for reusable-ci
# Run 'just' to see available commands

devtools_repo := env("DEVBASE_JUSTKIT_REPO", "https://github.com/diggsweden/devbase-justkit")
devtools_dir := env("XDG_DATA_HOME", env("HOME") + "/.local/share") + "/devbase-justkit"
lint := devtools_dir + "/linters"
colors := devtools_dir + "/utils/colors.sh"

# ==================================================================================== #
# DEFAULT - Show available recipes
# ==================================================================================== #

# Display available recipes
default:
    @printf "\033[1;36m Reusable CI\033[0m\n"
    @printf "\n"
    @printf "Quick start: \033[1;32mjust setup-devtools\033[0m | \033[1;34mjust verify\033[0m | \033[1;35mjust lint-all\033[0m\n"
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
        "{{devtools_dir}}/scripts/setup.sh" "{{devtools_repo}}" "{{devtools_dir}}"
    else
        printf "Cloning devbase-justkit to %s...\n" "{{devtools_dir}}"
        mkdir -p "$(dirname "{{devtools_dir}}")"
        git clone --depth 1 "{{devtools_repo}}" "{{devtools_dir}}"
        git -C "{{devtools_dir}}" fetch --tags --quiet
        latest=$(git -C "{{devtools_dir}}" describe --tags --abbrev=0 origin/main 2>/dev/null || echo "")
        if [[ -n "$latest" ]]; then
            git -C "{{devtools_dir}}" fetch --depth 1 origin tag "$latest" --quiet
            git -C "{{devtools_dir}}" checkout "$latest" --quiet
        fi
        printf "Installed devbase-justkit %s\n" "${latest:-main}"
    fi

# Check required tools are installed
[group('setup')]
check-tools: _ensure-devtools
    @{{devtools_dir}}/scripts/check-tools.sh --check-devtools mise git just rumdl yamlfmt actionlint gitleaks shellcheck shfmt conform reuse

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

# ▪ Run all linters with summary
[group('verify')]
verify: _ensure-devtools
    @{{devtools_dir}}/scripts/verify.sh

# ==================================================================================== #
# LINT - Code quality checks
# ==================================================================================== #

# ▪ Run all linters (override in project justfile to customize)
[group('lint')]
lint-all: _ensure-devtools lint-commits lint-secrets lint-yaml lint-markdown lint-shell lint-shell-fmt lint-actions lint-license
    #!/usr/bin/env bash
    source "{{colors}}"
    just_success "All linting checks completed"

# Validate commit messages (conform)
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

# Check license compliance (reuse)
[group('lint')]
lint-license:
    @{{lint}}/license.sh

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
# INTERNAL
# ==================================================================================== #

[private]
_ensure-devtools:
    #!/usr/bin/env bash
    if [[ ! -d "{{devtools_dir}}" ]]; then
        just setup-devtools
    fi
