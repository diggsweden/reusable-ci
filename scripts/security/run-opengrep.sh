#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/env.sh"
source "$SCRIPT_DIR/../ci/output.sh"
source "$SCRIPT_DIR/../ci/install-opengrep.sh"

readonly DEFAULT_OPENGREP_CONFIG="p/default"
readonly DEFAULT_OPENGREP_FAIL_ON_SEVERITY="high"
readonly DEFAULT_OPENGREP_TARGET_PATH="."
readonly DEFAULT_OPENGREP_JSON_FILE="opengrep-results.json"
readonly DEFAULT_OPENGREP_SARIF_FILE="opengrep-results.sarif"
readonly DEFAULT_OPENGREP_TEXT_FILE="opengrep-results.txt"
readonly DEFAULT_OPENGREP_GITLAB_SAST_FILE="opengrep-results.gitlab-sast.json"

trim_whitespace() {
  local value="${1:-}"
  value="${value#"${value%%[![:space:]]*}"}"
  value="${value%"${value##*[![:space:]]}"}"
  printf '%s' "$value"
}

normalize_fail_on_severity() {
  case "${1,,}" in
  none | off | never)
    printf 'none'
    ;;
  low | info)
    printf 'low'
    ;;
  medium | moderate | warning)
    printf 'medium'
    ;;
  high | critical | error)
    printf 'high'
    ;;
  *)
    ci_log_error "Unsupported OPENGREP_FAIL_ON_SEVERITY value: $1"
    return 1
    ;;
  esac
}

build_config_args() {
  local raw_configs="$1"
  local config trimmed

  CONFIG_ARGS=()
  IFS=',' read -r -a raw_list <<<"$raw_configs"
  for config in "${raw_list[@]}"; do
    trimmed="$(trim_whitespace "$config")"
    [[ -z "$trimmed" ]] && continue
    CONFIG_ARGS+=(--config "$trimmed")
  done

  if [[ ${#CONFIG_ARGS[@]} -eq 0 ]]; then
    ci_log_error "At least one OpenGrep config is required"
    return 1
  fi
}

count_occurrences() {
  local file="$1"
  local needle="$2"
  local count="0"

  if [[ -f "$file" ]]; then
    count="$(grep -o "$needle" "$file" 2>/dev/null | wc -l | tr -d '[:space:]')"
  fi

  printf '%s' "${count:-0}"
}

has_findings_meeting_threshold() {
  local threshold="$1"
  local findings_total="$2"
  local error_total="$3"
  local warning_total="$4"

  case "$threshold" in
  none) printf 'false' ;;
  low)
    if [[ "$findings_total" -gt 0 ]]; then printf 'true'; else printf 'false'; fi
    ;;
  medium)
    if [[ "$error_total" -gt 0 || "$warning_total" -gt 0 ]]; then printf 'true'; else printf 'false'; fi
    ;;
  high)
    if [[ "$error_total" -gt 0 ]]; then printf 'true'; else printf 'false'; fi
    ;;
  *)
    printf 'false'
    ;;
  esac
}

code_scanning_label() {
  case "${CI_PLATFORM:-local}" in
  github)
    if [[ "${HAS_SARIF_UPLOAD_TOKEN:-false}" == "true" || -n "${SARIF_UPLOAD_TOKEN:-}" ]]; then
      printf 'SARIF generated, upload configured'
    else
      printf 'SARIF generated, upload not configured'
    fi
    ;;
  gitlab)
    printf 'GitLab SAST artifact generated'
    ;;
  *)
    printf 'Portable artifacts only'
    ;;
  esac
}

workflow_run_label() {
  if [[ -n "${CI_RUN_URL:-}" ]]; then
    printf '[View workflow run](%s)' "$CI_RUN_URL"
  else
    printf 'n/a'
  fi
}

artifacts_label() {
  printf '%s' "\`opengrep-results.sarif\`, \`opengrep-results.json\`, \`opengrep-results.txt\`, \`opengrep-results.gitlab-sast.json\`"
}

code_scanning_note() {
  case "${CI_PLATFORM:-local}" in
  github)
    if [[ "${HAS_SARIF_UPLOAD_TOKEN:-false}" == "true" || -n "${SARIF_UPLOAD_TOKEN:-}" ]]; then
      printf 'SARIF will be uploaded to Security / Code Scanning after the scan step completes.'
    else
      printf 'SARIF is still generated and saved as a workflow artifact. Configure SARIF_UPLOAD_TOKEN to publish results in Security / Code Scanning.'
    fi
    ;;
  gitlab)
    printf 'A GitLab SAST report is generated alongside the portable artifacts.'
    ;;
  *)
    printf 'Portable artifacts are generated without platform-native upload.'
    ;;
  esac
}

write_failure_summary() {
  local config="$1"
  local target_path="$2"
  local scan_exit="$3"

  {
    printf "## OpenGrep SAST\n\n"
    printf 'OpenGrep exited with status %s before a complete result set was produced.\n\n' "$scan_exit"
    printf "| Property | Value |\n"
    printf "|----------|-------|\n"
    printf "| Rules | %s |\n" "$config"
    printf "| Target | %s |\n" "$target_path"
    printf "| Security / Code Scanning | %s |\n" "$(code_scanning_label)"
    printf "| Workflow Run | %s |\n" "$(workflow_run_label)"
    printf "| Artifacts | %s |\n" "$(artifacts_label)"
    printf "| Scan Result | failure |\n"
  } >>"$(ci_summary_file)"
}

write_scan_summary() {
  local config="$1"
  local target_path="$2"
  local fail_on_severity="$3"
  local findings_total="$4"
  local error_total="$5"
  local warning_total="$6"
  local info_total="$7"
  local threshold_failure="$8"
  local text_file="$9"
  local scan_result summary_line

  if [[ "$threshold_failure" == "true" ]]; then
    scan_result="failure"
    summary_line="Blocked by findings meeting threshold \`${fail_on_severity}\`."
  elif [[ "$findings_total" -gt 0 ]]; then
    scan_result="success"
    summary_line="Completed with findings below threshold \`${fail_on_severity}\`."
  else
    scan_result="success"
    summary_line="Passed with \`0\` findings."
  fi

  {
    printf "## OpenGrep SAST\n\n"
    printf "%s\n\n" "$summary_line"
    printf "| Property | Value |\n"
    printf "|----------|-------|\n"
    printf '| Rules | %s |\n' "$config"
    printf '| Target | %s |\n' "$target_path"
    printf "| Findings | %s |\n" "$findings_total"
    printf "| Fail Threshold | %s |\n" "$fail_on_severity"
    printf "| Security / Code Scanning | %s |\n" "$(code_scanning_label)"
    printf "| Workflow Run | %s |\n" "$(workflow_run_label)"
    printf "| Artifacts | %s |\n" "$(artifacts_label)"
    printf "| Scan Result | %s |\n" "$scan_result"
    printf "\n%s\n" "$(code_scanning_note)"

    if [[ "$findings_total" -gt 0 ]]; then
      printf "\n| Severity | Count |\n"
      printf "|----------|-------|\n"
      printf "| ERROR | %s |\n" "$error_total"
      printf "| WARNING | %s |\n" "$warning_total"
      printf "| INFO | %s |\n" "$info_total"
    fi

    if [[ "$findings_total" -gt 0 && -f "$text_file" ]]; then
      printf "\n<details>\n<summary>OpenGrep findings excerpt</summary>\n\n<pre>\n"
      sed -n '1,120p' "$text_file"
      printf "\n</pre>\n</details>\n"
    fi
  } >>"$(ci_summary_file)"
}

main() {
  local config fail_on_severity target_path
  local json_file sarif_file text_file gitlab_sast_file
  local scan_exit findings_total error_total warning_total info_total threshold_failure

  config="${OPENGREP_CONFIG:-$DEFAULT_OPENGREP_CONFIG}"
  fail_on_severity="$(normalize_fail_on_severity "${OPENGREP_FAIL_ON_SEVERITY:-$DEFAULT_OPENGREP_FAIL_ON_SEVERITY}")"
  target_path="${OPENGREP_TARGET_PATH:-$DEFAULT_OPENGREP_TARGET_PATH}"
  json_file="${OPENGREP_JSON_FILE:-$DEFAULT_OPENGREP_JSON_FILE}"
  sarif_file="${OPENGREP_SARIF_FILE:-$DEFAULT_OPENGREP_SARIF_FILE}"
  text_file="${OPENGREP_TEXT_FILE:-$DEFAULT_OPENGREP_TEXT_FILE}"
  gitlab_sast_file="${OPENGREP_GITLAB_SAST_FILE:-$DEFAULT_OPENGREP_GITLAB_SAST_FILE}"

  build_config_args "$config"
  install_opengrep

  local -a scan_args
  scan_args=(
    scan
    --quiet
    --disable-version-check
    --exclude .github-shared
    --taint-intrafile
    --dataflow-traces
    --json-output "$json_file"
    --sarif-output "$sarif_file"
    --text-output "$text_file"
    --gitlab-sast-output "$gitlab_sast_file"
  )
  scan_args+=("${CONFIG_ARGS[@]}")
  scan_args+=("$target_path")

  printf "Running OpenGrep with config '%s' on '%s'...\n" "$config" "$target_path"
  set +e
  PYTHONWARNINGS="ignore:RequestsDependencyWarning" opengrep "${scan_args[@]}"
  scan_exit=$?
  set -e

  if [[ "$scan_exit" -ne 0 ]]; then
    write_failure_summary "$config" "$target_path" "$scan_exit"
    exit "$scan_exit"
  fi

  findings_total="$(count_occurrences "$json_file" '"check_id":')"
  error_total="$(count_occurrences "$json_file" '"severity":"ERROR"')"
  warning_total="$(count_occurrences "$json_file" '"severity":"WARNING"')"
  info_total="$(count_occurrences "$json_file" '"severity":"INFO"')"
  threshold_failure="$(has_findings_meeting_threshold "$fail_on_severity" "$findings_total" "$error_total" "$warning_total")"

  write_scan_summary "$config" "$target_path" "$fail_on_severity" "$findings_total" "$error_total" "$warning_total" "$info_total" "$threshold_failure" "$text_file"

  ci_output "opengrep-result" "$(if [[ "$threshold_failure" == "true" ]]; then printf 'failure'; else printf 'success'; fi)"
  ci_output "opengrep-findings-total" "$findings_total"
  ci_output "opengrep-findings-error" "$error_total"
  ci_output "opengrep-findings-warning" "$warning_total"
  ci_output "opengrep-findings-info" "$info_total"
  ci_output "opengrep-fail-threshold" "$fail_on_severity"

  if [[ "$threshold_failure" == "true" ]]; then
    ci_log_error "OpenGrep found findings meeting fail threshold '${fail_on_severity}'"
    exit 1
  fi

  printf "OpenGrep completed successfully with %s findings\n" "$findings_total"
}

main "$@"
