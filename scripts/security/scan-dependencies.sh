#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Scan project dependencies for known vulnerabilities using Trivy.
#
# Replaces GitHub's dependency-review-action with a portable scanner
# that works across CI platforms. In diff mode (default for PRs),
# only newly introduced vulnerabilities cause failure.
#
# Usage: bash scan-dependencies.sh
#
# Environment variables:
#   FAIL_ON_SEVERITY  Minimum severity to fail on: critical (default), high, moderate, low
#   SCAN_MODE         Scan mode: diff (default) or full
#   SCAN_PATH         Path to scan (default: .)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"

# shellcheck source=../ci/env.sh
source "$SCRIPT_DIR/../ci/env.sh"
# shellcheck source=../ci/output.sh
source "$SCRIPT_DIR/../ci/output.sh"
# shellcheck source=../ci/install-trivy.sh
source "$SCRIPT_DIR/../ci/install-trivy.sh"

# =============================================================================
# Configuration
# =============================================================================

FAIL_ON_SEVERITY="${FAIL_ON_SEVERITY:-critical}"
SCAN_MODE="${SCAN_MODE:-diff}"
SCAN_PATH="${SCAN_PATH:-.}"

# =============================================================================
# Functions
# =============================================================================

# Map user-facing severity to cumulative Trivy severity filter.
# "moderate" → "CRITICAL,HIGH,MEDIUM" means fail on MEDIUM and above.
map_severity() {
  local level="$1"
  case "${level,,}" in
  critical) printf 'CRITICAL' ;;
  high) printf 'CRITICAL,HIGH' ;;
  moderate) printf 'CRITICAL,HIGH,MEDIUM' ;;
  low) printf 'CRITICAL,HIGH,MEDIUM,LOW' ;;
  *)
    ci_log_warning "Unknown severity '${level}', defaulting to CRITICAL" >&2
    printf 'CRITICAL'
    ;;
  esac
}

# Extract unique vulnerability IDs from a Trivy JSON report.
# Uses grep/cut as a portable fallback; prefers jq when available.
extract_vuln_ids() {
  local json_file="$1"

  if [[ ! -f "$json_file" ]]; then
    return
  fi

  if command -v jq &>/dev/null; then
    jq -r '
      .Results[]?.Vulnerabilities[]?.VulnerabilityID // empty
    ' "$json_file" 2>/dev/null | sort -u
  else
    grep -o '"VulnerabilityID":"[^"]*"' "$json_file" 2>/dev/null |
      cut -d'"' -f4 | sort -u
  fi
}

# Count lines in input (0 if empty)
count_lines() {
  local input
  input="$(cat)"
  if [[ -z "$input" ]]; then
    printf '0'
  else
    printf '%s\n' "$input" | wc -l | tr -d ' '
  fi
}

# Build a markdown summary table from a Trivy JSON report,
# filtered to only the given vulnerability IDs.
write_summary() {
  local json_file="$1"
  local new_ids_file="$2"
  local new_count="$3"
  local severity_filter="$4"
  local mode="$5"

  {
    printf '## Dependency Vulnerability Scan\n\n'
    printf '| Setting | Value |\n'
    printf '|---------|-------|\n'
    printf '| Scanner | Trivy |\n'
    printf '| Mode | %s |\n' "$mode"
    printf '| Fail threshold | %s |\n' "$FAIL_ON_SEVERITY"
    printf '| Severity filter | %s |\n' "$severity_filter"
    printf '| New vulnerabilities | **%s** |\n\n' "$new_count"

    if [[ "$new_count" -gt 0 ]] && [[ -f "$json_file" ]]; then
      printf '### New Vulnerabilities\n\n'
      printf '| ID | Severity | Package | Installed | Fixed |\n'
      printf '|----|----------|---------|-----------|-------|\n'

      # Extract vulnerability details for new IDs
      if command -v jq &>/dev/null && [[ -f "$new_ids_file" ]]; then
        jq -r --slurpfile ids <(jq -R -s 'split("\n") | map(select(. != ""))' "$new_ids_file") '
          .Results[]?.Vulnerabilities[]? |
          select(.VulnerabilityID as $id | $ids[0] | index($id)) |
          "| \(.VulnerabilityID) | \(.Severity) | \(.PkgName) | \(.InstalledVersion) | \(.FixedVersion // "—") |"
        ' "$json_file" 2>/dev/null || true
      else
        # Fallback: list IDs only
        while IFS= read -r vid; do
          [[ -z "$vid" ]] && continue
          printf '| %s | — | — | — | — |\n' "$vid"
        done <"$new_ids_file"
      fi
      printf '\n'
    elif [[ "$new_count" -eq 0 ]]; then
      printf '> No new vulnerabilities found.\n\n'
    fi
  } | ci_summary
}

# =============================================================================
# Main
# =============================================================================

WORK_DIR="$(mktemp -d)"
trap 'rm -rf "$WORK_DIR"' EXIT

main() {
  printf "🔍 Dependency vulnerability scan\n"
  printf "   Severity threshold: %s\n" "$FAIL_ON_SEVERITY"
  printf "   Scan mode: %s\n" "$SCAN_MODE"
  printf "   Scan path: %s\n\n" "$SCAN_PATH"

  install_trivy

  local severity_filter
  severity_filter="$(map_severity "$FAIL_ON_SEVERITY")"

  local head_vulns="$WORK_DIR/head-vulns.json"
  local base_vulns="$WORK_DIR/base-vulns.json"
  local head_ids="$WORK_DIR/head-ids.txt"
  local base_ids="$WORK_DIR/base-ids.txt"
  local new_ids="$WORK_DIR/new-ids.txt"
  local new_count=0
  local mode_used="$SCAN_MODE"

  # Scan HEAD
  printf "Scanning HEAD for vulnerabilities...\n"
  trivy fs --format json --severity "$severity_filter" \
    --scanners vuln --output "$head_vulns" "$SCAN_PATH" || true

  extract_vuln_ids "$head_vulns" >"$head_ids"

  # Diff mode: scan base ref and compare
  if [[ "$SCAN_MODE" == "diff" ]] && [[ -n "${CI_PR_BASE_REF:-}" ]]; then
    printf "Diff mode: scanning base ref '%s' for comparison...\n" "$CI_PR_BASE_REF"

    local worktree_dir="$WORK_DIR/base-worktree"
    local worktree_ok=false

    if git worktree add -q "$worktree_dir" "origin/${CI_PR_BASE_REF}" 2>/dev/null; then
      worktree_ok=true
    elif git worktree add -q "$worktree_dir" "${CI_PR_BASE_REF}" 2>/dev/null; then
      worktree_ok=true
    fi

    if [[ "$worktree_ok" == "true" ]]; then
      trivy fs --format json --severity "$severity_filter" \
        --scanners vuln --output "$base_vulns" "$worktree_dir" || true

      git worktree remove -f "$worktree_dir" 2>/dev/null || true

      extract_vuln_ids "$base_vulns" >"$base_ids"

      # New = in HEAD but not in base
      comm -13 "$base_ids" "$head_ids" >"$new_ids"
      new_count="$(count_lines <"$new_ids")"
      mode_used="diff"
      printf "Base vulnerabilities: %s\n" "$(count_lines <"$base_ids")"
      printf "Head vulnerabilities: %s\n" "$(count_lines <"$head_ids")"
      printf "New vulnerabilities:  %s\n\n" "$new_count"
    else
      ci_log_warning "Could not create worktree for base ref '${CI_PR_BASE_REF}'. Falling back to full scan."
      cp "$head_ids" "$new_ids"
      new_count="$(count_lines <"$new_ids")"
      mode_used="full (worktree fallback)"
    fi
  else
    # Full scan mode or no base ref available
    if [[ "$SCAN_MODE" == "diff" ]] && [[ -z "${CI_PR_BASE_REF:-}" ]]; then
      ci_log_warning "Diff mode requested but no base ref available. Running full scan."
      mode_used="full (no base ref)"
    else
      mode_used="full"
    fi
    cp "$head_ids" "$new_ids"
    new_count="$(count_lines <"$new_ids")"
    printf "Total vulnerabilities: %s\n\n" "$new_count"
  fi

  # Generate SARIF for GitHub Code Scanning (inline PR annotations)
  local sarif_file="trivy-dependency-results.sarif"
  printf "Generating SARIF report...\n"
  trivy fs --format sarif --severity "$severity_filter" \
    --scanners vuln --output "$sarif_file" "$SCAN_PATH" || true

  # Write step summary
  write_summary "$head_vulns" "$new_ids" "$new_count" "$severity_filter" "$mode_used"

  # Exit based on results
  if [[ "$new_count" -gt 0 ]]; then
    ci_log_error "Found ${new_count} new vulnerabilit$([ "$new_count" -eq 1 ] && printf 'y' || printf 'ies') at severity ${FAIL_ON_SEVERITY} or above"
    exit 1
  else
    printf "✅ No new vulnerabilities found at severity %s or above\n" "$FAIL_ON_SEVERITY"
  fi
}

main "$@"
