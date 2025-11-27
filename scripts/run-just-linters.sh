#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
#
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

RED='\033[0;31m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m'

CROSSMARK='✗'

OVERALL_STATUS=0
TOTAL_START=$(date +%s.%N 2>/dev/null || date +%s)
PASS_COUNT=0
FAIL_COUNT=0

print_separator() {
  printf "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━\n"
}

print_header() {
  local text="$1"
  print_separator
  printf '%s%s%s\n' "${BLUE}" "$text" "${NC}"
  print_separator
}

calculate_duration() {
  local start="$1"
  local end="$2"

  if command -v bc >/dev/null 2>&1; then
    printf "%s - %s\n" "$end" "$start" | bc 2>/dev/null || printf "0"
  else
    printf "%d" "$((end - start))"
  fi
}

discover_linters() {
  if ! command -v just >/dev/null 2>&1; then
    printf '%s%s Error: '\''just'\'' command not found%s\n' "${RED}" "$CROSSMARK" "${NC}"
    exit 1
  fi

  local linters
  linters=$(just --summary 2>/dev/null | tr ' ' '\n' | grep '^lint-' | grep -v '\-fix$' || true)

  if [[ -z "$linters" ]]; then
    printf "%s%s No lint-* tasks found in justfile%s\n" "${RED}" "$CROSSMARK" "${NC}"
    printf "\n"
    printf "Expected tasks named: lint-java, lint-markdown, lint-yaml, etc.\n"
    printf "Note: Tasks ending with -fix are automatically excluded (e.g., lint-yaml-fix)\n"
    exit 1
  fi

  printf "%s" "$linters"
}

init_github_summary() {
  local linter_count="$1"

  [ -z "${GITHUB_STEP_SUMMARY:-}" ] && return

  cat <<EOF >>"$GITHUB_STEP_SUMMARY"
# 🔍 Just+Mise Linting Results

**Linters Run:** ${linter_count}  
**Started:** $(date -u +"%Y-%m-%d %H:%M:%S UTC")

---

## Individual Linter Results

| Linter | Tools | Status | Duration | Details |
|--------|-------|--------|----------|---------|
EOF
}

add_failed_linters_header() {
  [ -z "${GITHUB_STEP_SUMMARY:-}" ] && return

  if ! grep -q "^## ❌ Failed Linters" "$GITHUB_STEP_SUMMARY" 2>/dev/null; then
    cat <<EOF >>"$GITHUB_STEP_SUMMARY"

---

## ❌ Failed Linters

EOF
  fi
}

extract_linter_metadata_from_justfile() {
  local task_name="$1"
  local metadata_key="$2"

  grep -B5 "^lint-${task_name}:" justfile 2>/dev/null | grep "# ${metadata_key}:" | sed "s/# ${metadata_key}: //" || printf ""
}

add_linter_result_to_summary() {
  local display_name="$1"
  local tools="$2"
  local status_emoji="$3"
  local duration="$4"
  local exit_code="$5"

  [ -z "${GITHUB_STEP_SUMMARY:-}" ] && return

  printf "| %s | %s | %s | %.2fs | " "$display_name" "$tools" "$status_emoji" "$duration" >>"$GITHUB_STEP_SUMMARY"

  if [[ "$exit_code" -ne 0 ]]; then
    printf "[View errors below](#-failed-linters) |\n" >>"$GITHUB_STEP_SUMMARY"
  else
    printf "Success |\n" >>"$GITHUB_STEP_SUMMARY"
  fi
}

add_error_details_to_summary() {
  local linter_name="$1"
  local exit_code="$2"
  local duration="$3"
  local output_file="$4"

  [ -z "${GITHUB_STEP_SUMMARY:-}" ] && return

  add_failed_linters_header

  cat <<EOF >>"$GITHUB_STEP_SUMMARY"

<details>
<summary><b>❌ ${linter_name}</b> - Click to expand error details</summary>

<br>

**Exit code:** ${exit_code}  
**Duration:** ${duration}s

### Output:
\`\`\`
$(cat "$output_file")
\`\`\`

</details>

EOF
}

run_linter() {
  local task="$1"
  local linter_name="${task#lint-}"

  print_header "🔧 Running ${task}"
  printf "\n"

  local start
  start=$(date +%s.%N 2>/dev/null || date +%s)

  local output_file
  output_file=$(mktemp)

  local exit_code=0
  local status
  local status_emoji

  if just "$task" >"$output_file" 2>&1; then
    status="${GREEN}✅ PASS${NC}"
    status_emoji="✅ Pass"
    PASS_COUNT=$((PASS_COUNT + 1))
  else
    exit_code=$?
    status="${RED}❌ FAIL${NC}"
    status_emoji="❌ Fail"
    FAIL_COUNT=$((FAIL_COUNT + 1))
    OVERALL_STATUS=1
  fi

  local end
  end=$(date +%s.%N 2>/dev/null || date +%s)

  local duration
  duration=$(calculate_duration "$start" "$end")

  local display_name
  local tools

  display_name=$(extract_linter_metadata_from_justfile "$linter_name" "linter-name")
  tools=$(extract_linter_metadata_from_justfile "$linter_name" "linter-tools")

  if [[ -z "$display_name" ]]; then
    display_name="$linter_name"
  fi

  if [[ -z "$tools" ]]; then
    tools="-"
  fi

  cat "$output_file"
  printf "\n"
  printf "⏱️  Duration: %ss\n" "$duration"
  printf '%s\n' "$status"
  printf "\n"

  add_linter_result_to_summary "$display_name" "$tools" "$status_emoji" "$duration" "$exit_code"

  if [[ $exit_code -ne 0 ]]; then
    add_error_details_to_summary "$display_name" "$exit_code" "$duration" "$output_file"
  fi

  rm -f "$output_file"
}

finalize_github_summary() {
  [ -z "${GITHUB_STEP_SUMMARY:-}" ] && return

  cat <<EOF >>"$GITHUB_STEP_SUMMARY"

---

### Summary

**Total Duration:** ${TOTAL_DURATION}s  
**Pass:** ✅ ${PASS_COUNT} | **Fail:** ❌ ${FAIL_COUNT}

EOF

  if [[ $OVERALL_STATUS -eq 0 ]]; then
    printf "### ✅ All linters passed successfully!\n" >>"$GITHUB_STEP_SUMMARY"
  else
    printf "### ⚠️ Please fix the failing linters before merging.\n" >>"$GITHUB_STEP_SUMMARY"
  fi
}

print_console_summary() {
  print_header "📊 SUMMARY"
  printf "\n"
  printf "Total linters: %s\n" "${LINTER_COUNT}"
  printf '%s✅ Passed: %s%s\n' "${GREEN}" "${PASS_COUNT}" "${NC}"
  printf '%s❌ Failed: %s%s\n' "${RED}" "${FAIL_COUNT}" "${NC}"
  printf "⏱️  Total time: %ss\n" "${TOTAL_DURATION}"
  printf "\n"

  if [[ $OVERALL_STATUS -eq 0 ]]; then
    printf '%s✨ All checks passed! ✨%s\n' "${GREEN}" "${NC}"
  else
    printf "%s⚠️  Some checks failed. Please review the output above.%s\n" "${RED}" "${NC}"
  fi
}

main() {
  print_header "🔍 Discovering linters from justfile..."

  local lint_tasks
  lint_tasks=$(discover_linters)

  LINTER_COUNT=$(printf "%s" "$lint_tasks" | wc -l)
  printf "Found %s linters: %s\n" "${LINTER_COUNT}" "$(printf "%s" "$lint_tasks" | tr '\n' ' ')"
  printf "\n"

  init_github_summary "$LINTER_COUNT"

  while IFS= read -r task; do
    run_linter "$task"
  done <<<"$lint_tasks"

  local total_end
  total_end=$(date +%s.%N 2>/dev/null || date +%s)
  TOTAL_DURATION=$(calculate_duration "$TOTAL_START" "$total_end")

  print_console_summary
  finalize_github_summary

  exit $OVERALL_STATUS
}

main "$@"
