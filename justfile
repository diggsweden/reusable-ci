# SPDX-FileCopyrightText: 2025 The Reusable CI Authors
#
# SPDX-License-Identifier: CC0-1.0

# Quality checks and automation for reusable-ci
# Run 'just' to see available commands

# Terminal colors
red := '\033[0;31m'
green := '\033[0;32m'
yellow := '\033[0;33m'
nc := '\033[0m'

# Unicode symbols (works across platforms)
checkmark := '✓'
missing := '✗'

# Default recipe - show help
default:
    @echo "Available commands:"
    @just --list --unsorted | grep -v "default"

# Run all quality verifications
verify: verify-deps lint lint-license lint-commit _summary

# Check tool dependencies
verify-deps:
    @echo "Checking required tools..."
    @echo "========================="
    @missing_tools=""; \
    if command -v mise >/dev/null 2>&1; then \
        printf "{{green}}{{checkmark}}{{nc}} mise $(mise version 2>/dev/null | cut -d' ' -f1)\n"; \
    else \
        printf "{{red}}{{missing}}{{nc}} mise\n"; \
        missing_tools="$missing_tools mise"; \
    fi; \
    if command -v just >/dev/null 2>&1; then \
        printf "{{green}}{{checkmark}}{{nc}} just $(just --version 2>/dev/null | cut -d' ' -f2)\n"; \
    else \
        printf "{{red}}{{missing}}{{nc}} just\n"; \
        missing_tools="$missing_tools just"; \
    fi; \
    if command -v git >/dev/null 2>&1; then \
        printf "{{green}}{{checkmark}}{{nc}} git $(git --version 2>/dev/null | cut -d' ' -f3)\n"; \
    else \
        printf "{{red}}{{missing}}{{nc}} git\n"; \
        missing_tools="$missing_tools git"; \
    fi; \
    if command -v reuse >/dev/null 2>&1; then \
        printf "{{green}}{{checkmark}}{{nc}} reuse $(reuse --version 2>/dev/null | head -1 | cut -d' ' -f3)\n"; \
    else \
        printf "{{red}}{{missing}}{{nc}} reuse\n"; \
        missing_tools="$missing_tools reuse"; \
    fi; \
    if command -v rumdl >/dev/null 2>&1; then \
        printf "{{green}}{{checkmark}}{{nc}} rumdl $(rumdl --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1)\n"; \
    else \
        printf "{{red}}{{missing}}{{nc}} rumdl\n"; \
        missing_tools="$missing_tools rumdl"; \
    fi; \
    if command -v yamlfmt >/dev/null 2>&1; then \
        printf "{{green}}{{checkmark}}{{nc}} yamlfmt $(yamlfmt -version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1)\n"; \
    else \
        printf "{{red}}{{missing}}{{nc}} yamlfmt\n"; \
        missing_tools="$missing_tools yamlfmt"; \
    fi; \
    if command -v actionlint >/dev/null 2>&1; then \
        printf "{{green}}{{checkmark}}{{nc}} actionlint $(actionlint -version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1)\n"; \
    else \
        printf "{{red}}{{missing}}{{nc}} actionlint\n"; \
        missing_tools="$missing_tools actionlint"; \
    fi; \
    if command -v gitleaks >/dev/null 2>&1; then \
        gitleaks_ver=$(gitleaks version 2>&1 | head -1); \
        if echo "$gitleaks_ver" | grep -q "build process"; then \
            mise_ver=$(grep gitleaks .mise.toml 2>/dev/null | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -1); \
            printf "{{green}}{{checkmark}}{{nc}} gitleaks ${mise_ver:-installed}\n"; \
        else \
            printf "{{green}}{{checkmark}}{{nc}} gitleaks $gitleaks_ver\n"; \
        fi; \
    else \
        printf "{{red}}{{missing}}{{nc}} gitleaks\n"; \
        missing_tools="$missing_tools gitleaks"; \
    fi; \
    if command -v conform >/dev/null 2>&1; then \
        printf "{{green}}{{checkmark}}{{nc}} conform $(conform version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+-alpha\.[0-9]+' | head -1)\n"; \
    else \
        printf "{{red}}{{missing}}{{nc}} conform\n"; \
        missing_tools="$missing_tools conform"; \
    fi; \
    if command -v shfmt >/dev/null 2>&1; then \
        printf "{{green}}{{checkmark}}{{nc}} shfmt $(shfmt --version 2>/dev/null)\n"; \
    else \
        printf "{{red}}{{missing}}{{nc}} shfmt\n"; \
        missing_tools="$missing_tools shfmt"; \
    fi; \
    if command -v shellcheck >/dev/null 2>&1; then \
        printf "{{green}}{{checkmark}}{{nc}} shellcheck $(shellcheck --version 2>/dev/null | grep version: | cut -d' ' -f2)\n"; \
    else \
        printf "{{red}}{{missing}}{{nc}} shellcheck\n"; \
        missing_tools="$missing_tools shellcheck"; \
    fi; \
    echo ""; \
    if [ -n "$missing_tools" ]; then \
        printf '%b{{missing}} Missing tools detected!%b\n\n' "{{red}}" "{{nc}}"; \
        printf '%bTo fix:%b\n' "{{yellow}}" "{{nc}}"; \
        printf '1. Install: %bmise install%b\n' "{{green}}" "{{nc}}"; \
        printf '2. Activate: %beval "$(mise activate bash)"%b\n' "{{green}}" "{{nc}}"; \
        exit 1; \
    else \
        printf '%b{{checkmark}} All required tools installed!%b\n' "{{green}}" "{{nc}}"; \
    fi

# Run all linters
lint: lint-markdown lint-yaml lint-actions lint-shell lint-secrets
    @echo "LINT_PASS" > /tmp/just_lint_status

# Lint markdown files with rumdl (Rust)
# linter-name: Markdown
# linter-tools: rumdl
# Lint markdown files with rumdl (Rust)
lint-markdown:
    @printf '%b\n************ MARKDOWN LINTING (RUMDL) ***********%b\n\n' "{{yellow}}" "{{nc}}"
    @rumdl check . \
    && echo "{{green}}{{checkmark}} Markdown linting passed{{nc}}" \
    || { echo "{{red}}{{missing}} Markdown linting failed - run 'just lint-markdown-fix' to fix{{nc}}"; exit 1; }
    @printf '\n'

# linter-name: YAML Formatting
# linter-tools: yamlfmt
# Lint YAML files with yamlfmt (Go)
lint-yaml:
    @printf '%b\n************ YAML LINTING (YAMLFMT) ***********%b\n\n' "{{yellow}}" "{{nc}}"
    @yamlfmt -conf .yamlfmt -lint . \
    && echo "{{green}}{{checkmark}} YAML linting passed{{nc}}" \
    || { echo "{{red}}{{missing}} YAML linting failed - run 'just lint-yaml-fix' to fix{{nc}}"; exit 1; }
    @printf '\n'

# linter-name: GitHub Actions
# linter-tools: actionlint
# Lint GitHub Actions with actionlint (Go)
lint-actions:
    @printf '%b\n************ GITHUB ACTIONS LINTING (ACTIONLINT) ***********%b\n\n' "{{yellow}}" "{{nc}}"
    @actionlint -shellcheck= -pyflakes= -ignore 'unknown permission scope "attestations"' \
    && echo "{{green}}{{checkmark}} GitHub Actions linting passed{{nc}}" \
    || { echo "{{red}}{{missing}} GitHub Actions linting failed{{nc}}"; exit 1; }
    @printf '\n'

# linter-name: Shell Scripts
# linter-tools: shellcheck, shfmt
# Lint shell scripts with shfmt and shellcheck
lint-shell:
    #!/usr/bin/env bash
    set -euo pipefail
    printf '%b\n************ SHELL SCRIPT LINTING ***********%b\n\n' "{{yellow}}" "{{nc}}"
    
    # Check if we have shell scripts to lint
    if [ -z "$(find scripts -type f -name '*.sh' 2>/dev/null | head -1)" ]; then
        printf 'No shell scripts found, skipping\n'
        printf '{{green}}{{checkmark}} Shell linting passed{{nc}}\n\n'
        exit 0
    fi
    
    echo "Running shellcheck on all shell scripts..."
    if command -v shellcheck >/dev/null 2>&1; then
        find scripts -type f -name "*.sh" | xargs shellcheck || exit 1
        printf '{{green}}{{checkmark}} Shellcheck passed{{nc}}\n'
    else
        printf '{{yellow}}⚠ shellcheck not found, skipping{{nc}}\n'
    fi
    
    echo ""
    echo "Running shfmt on all shell scripts..."
    if command -v shfmt >/dev/null 2>&1; then
        shfmt -d -i 2 scripts || { printf '{{red}}{{missing}} Shell formatting failed - run just lint-shell-fix to fix{{nc}}\n'; exit 1; }
        printf '{{green}}{{checkmark}} Shell formatting passed{{nc}}\n'
    else
        printf '{{yellow}}⚠ shfmt not found, skipping{{nc}}\n'
    fi
    
    printf '\n'

# linter-name: Secret Scanning
# linter-tools: gitleaks
# Check for secrets with gitleaks (Go) - only scan commits different from main
lint-secrets:
    @printf '%b\n************ SECRET SCANNING (GITLEAKS) ***********%b\n\n' "{{yellow}}" "{{nc}}"
    @# Get the default branch (usually main or master)
    @default_branch=$(git symbolic-ref refs/remotes/origin/HEAD 2>/dev/null | sed 's@^refs/remotes/origin/@@' || echo "main"); \
    current_branch=$(git rev-parse --abbrev-ref HEAD 2>/dev/null); \
    if [ "$current_branch" = "$default_branch" ]; then \
        echo "On default branch, scanning all commits..."; \
        gitleaks detect --no-banner; \
    else \
        echo "Scanning commits different from $default_branch..."; \
        gitleaks detect --no-banner --log-opts="$default_branch..HEAD"; \
    fi \
    && echo "{{green}}{{checkmark}} No secrets found{{nc}}" \
    || { echo "{{red}}{{missing}} Secret scanning failed{{nc}}"; exit 1; }
    @printf '\n'

# Fix all auto-fixable issues
lint-fix: lint-markdown-fix lint-yaml-fix lint-shell-fix
    @echo "{{green}}{{checkmark}} All auto-fixable issues resolved{{nc}}"
    @echo "Note: Some issues may require manual fixes"

# Fix markdown issues with rumdl
lint-markdown-fix:
    @printf '%b\n************ FIXING MARKDOWN (RUMDL) ***********%b\n\n' "{{yellow}}" "{{nc}}"
    @rumdl check --fix . \
    && echo "{{green}}{{checkmark}} Markdown files fixed{{nc}}" \
    || { echo "{{red}}{{missing}} Failed to fix markdown files{{nc}}"; exit 1; }
    @printf '\n'

# Fix YAML formatting with yamlfmt
lint-yaml-fix:
    @printf '%b\n************ FIXING YAML (YAMLFMT) ***********%b\n\n' "{{yellow}}" "{{nc}}"
    @yamlfmt -conf .yamlfmt . \
    && echo "{{green}}{{checkmark}} YAML files formatted{{nc}}" \
    || { echo "{{red}}{{missing}} Failed to format YAML files{{nc}}"; exit 1; }
    @printf '\n'

# Fix shell script formatting
lint-shell-fix:
    @printf '%b************ FIXING SHELL SCRIPTS ***********%b\n\n' "{{yellow}}" "{{nc}}"
    @shfmt -w -i 2 scripts \
    && echo "{{green}}{{checkmark}} Shell scripts formatted{{nc}}" \
    || { echo "{{red}}{{missing}} Failed to format shell scripts{{nc}}"; exit 1; }
    @printf '\n'

# linter-name: License Headers
# linter-tools: reuse
# Check licenses with REUSE
lint-license:
    @printf '%b************ LICENSE HEALTH (REUSE) ***********%b\n\n' "{{yellow}}" "{{nc}}"
    @command -v reuse >/dev/null 2>&1 || { \
        echo "Error: reuse not installed."; \
        echo "Install with: apt install reuse"; \
        exit 1; \
    }
    @reuse lint \
    && echo "LICENSE_PASS" > /tmp/just_license_status \
    || echo "LICENSE_FAIL" > /tmp/just_license_status
    @printf '\n\n'

# linter-name: Commit Messages
# linter-tools: conform
# Check commits with conform
lint-commit:
    @printf '%b************ COMMIT HEALTH (CONFORM) ***********%b\n\n' "{{yellow}}" "{{nc}}"
    @currentBranch=$(git branch --show-current); \
    default_branch=$(git symbolic-ref refs/remotes/origin/HEAD 2>/dev/null | sed 's@^refs/remotes/origin/@@'); \
    if [ -z "$default_branch" ]; then default_branch="main"; fi; \
    if [ "$(git rev-list --count ${default_branch}..)" = "0" ]; then \
        printf "%s" "{{green}} No commits found in current branch: {{yellow}}$currentBranch{{nc}}, compared to: {{yellow}}${default_branch}{{nc}} {{nc}}"; \
        echo "COMMIT_SKIP" > /tmp/just_commit_status; \
    else \
        conform enforce --base-branch=${default_branch} \
        && echo "COMMIT_PASS" > /tmp/just_commit_status \
        || echo "COMMIT_FAIL" > /tmp/just_commit_status; \
    fi
    @printf '\n\n'

# Print summary (internal)
_summary:
    @printf '%b********* CODE QUALITY RUN SUMMARY ******%b\n\n' "{{yellow}}" "{{nc}}"
    @if [ -f /tmp/just_lint_status ] && grep -q "FAIL" /tmp/just_lint_status; then \
        printf '{{missing}} {{red}}Lint check failed, see logs above.{{nc}}\n'; \
    elif [ -f /tmp/just_lint_status ] && grep -q "PASS" /tmp/just_lint_status; then \
        printf '{{green}}{{checkmark}}{{checkmark}} Lint check passed{{nc}}\n'; \
    fi
    @if [ -f /tmp/just_license_status ] && grep -q "FAIL" /tmp/just_license_status; then \
        printf '{{missing}} {{red}}License check failed, see logs and fix problems.{{nc}}\n'; \
    elif [ -f /tmp/just_license_status ] && grep -q "PASS" /tmp/just_license_status; then \
        printf '{{green}}{{checkmark}}{{checkmark}} License check passed{{nc}}\n'; \
    fi
    @if [ -f /tmp/just_commit_status ] && grep -q "FAIL" /tmp/just_commit_status; then \
        printf '{{missing}} {{red}}Commit check failed, see logs (std out) and fix problems.{{nc}}\n'; \
    elif [ -f /tmp/just_commit_status ] && grep -q "SKIP" /tmp/just_commit_status; then \
        printf '{{yellow}}{{checkmark}}{{checkmark}} Commit check skipped, no new commits found in current branch{{nc}}\n'; \
    elif [ -f /tmp/just_commit_status ] && grep -q "PASS" /tmp/just_commit_status; then \
        printf '{{green}}{{checkmark}}{{checkmark}} Commit check passed{{nc}}\n'; \
    fi
    @printf "\n"
