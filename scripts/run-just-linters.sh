#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 The Reusable CI Authors
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
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
}

print_header() {
  local text="$1"
  print_separator
  echo -e "${BLUE}${text}${NC}"
  print_separator
}

calculate_duration() {
  local start="$1"
  local end="$2"

  if command -v bc >/dev/null 2>&1; then
    echo "$end - $start" | bc 2>/dev/null || echo "0"
  else
    echo "$((end - start))"
  fi
}

discover_linters() {
  if ! command -v just >/dev/null 2>&1; then
    echo -e "${RED}${CROSSMARK} Error: 'just' command not found${NC}"
    exit 1
  fi

  local linters
  linters=$(just --summary 2>/dev/null | tr ' ' '\n' | grep '^lint-' | grep -v '\-fix$' || true)

  if [ -z "$linters" ]; then
    echo -e "${RED}${CROSSMARK} No lint-* tasks found in justfile${NC}"
    echo ""
    echo "Expected tasks named: lint-java, lint-markdown, lint-yaml, etc."
    echo "Note: Tasks ending with -fix are automatically excluded (e.g., lint-yaml-fix)"
    exit 1
  fi

  echo "$linters"
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

get_linter_tools() {
  local linter_name="$1"

  case "$linter_name" in
  actions) echo "actionlint" ;;
  commit) echo "conform" ;;
  java) echo "checkstyle, pmd, spotbugs" ;;
  license) echo "reuse" ;;
  markdown) echo "rumdl" ;;
  secrets) echo "gitleaks" ;;
  shell) echo "shellcheck, shfmt" ;;
  yaml) echo "yamlfmt" ;;
  *) echo "-" ;;
  esac
}

add_linter_result_to_summary() {
  local linter_name="$1"
  local status_emoji="$2"
  local duration="$3"
  local exit_code="$4"

  [ -z "${GITHUB_STEP_SUMMARY:-}" ] && return

  local tools
  tools=$(get_linter_tools "$linter_name")

  printf "| %s | %s | %s | %.2fs | " "$linter_name" "$tools" "$status_emoji" "$duration" >>"$GITHUB_STEP_SUMMARY"

  if [ "$exit_code" -ne 0 ]; then
    echo "[View errors below](#-failed-linters) |" >>"$GITHUB_STEP_SUMMARY"
  else
    echo "Success |" >>"$GITHUB_STEP_SUMMARY"
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
  echo ""

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

  cat "$output_file"
  echo ""
  echo -e "⏱️  Duration: ${duration}s"
  echo -e "${status}"
  echo ""

  add_linter_result_to_summary "$linter_name" "$status_emoji" "$duration" "$exit_code"

  if [ $exit_code -ne 0 ]; then
    add_error_details_to_summary "$linter_name" "$exit_code" "$duration" "$output_file"
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

  if [ $OVERALL_STATUS -eq 0 ]; then
    echo "### ✅ All linters passed successfully!" >>"$GITHUB_STEP_SUMMARY"
  else
    echo "### ⚠️ Please fix the failing linters before merging." >>"$GITHUB_STEP_SUMMARY"
  fi
}

print_console_summary() {
  print_header "📊 SUMMARY"
  echo ""
  echo "Total linters: ${LINTER_COUNT}"
  echo -e "${GREEN}✅ Passed: ${PASS_COUNT}${NC}"
  echo -e "${RED}❌ Failed: ${FAIL_COUNT}${NC}"
  echo "⏱️  Total time: ${TOTAL_DURATION}s"
  echo ""

  if [ $OVERALL_STATUS -eq 0 ]; then
    echo -e "${GREEN}✨ All checks passed! ✨${NC}"
  else
    echo -e "${RED}⚠️  Some checks failed. Please review the output above.${NC}"
  fi
}

main() {
  print_header "🔍 Discovering linters from justfile..."

  local lint_tasks
  lint_tasks=$(discover_linters)

  LINTER_COUNT=$(echo "$lint_tasks" | wc -l)
  echo "Found ${LINTER_COUNT} linters: $(echo "$lint_tasks" | tr '\n' ' ')"
  echo ""

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
