#!/usr/bin/env bats

# shellcheck disable=SC1090,SC2016,SC2030,SC2031,SC2119,SC2120,SC2155
# SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

bats_require_minimum_version 1.13.0

load "${BATS_TEST_DIRNAME}/../libs/bats-support/load.bash"
load "${BATS_TEST_DIRNAME}/../libs/bats-assert/load.bash"
load "${BATS_TEST_DIRNAME}/../libs/bats-file/load.bash"
load "${BATS_TEST_DIRNAME}/../test_helper.bash"

setup() {
  common_setup_with_github_env
  export OPENGREP_CONFIG="p/default"
  export OPENGREP_TARGET_PATH="."
  export OPENGREP_TEST_JSON='{"version":"1.18.0","results":[],"errors":[],"paths":{"scanned":["src/app.js"]}}'
  export OPENGREP_TEST_SARIF='{"version":"2.1.0","runs":[]}'
  export OPENGREP_TEST_TEXT='No findings'
  export OPENGREP_TEST_GITLAB_SAST='{"version":"15.0.4","scan":{"start_time":"2026-01-01T00:00:00","end_time":"2026-01-01T00:00:00","analyzer":{"id":"opengrep","name":"Opengrep","version":"1.18.0","vendor":{"name":"Opengrep"}},"scanner":{"id":"opengrep","name":"Opengrep","version":"1.18.0","vendor":{"name":"Opengrep"}},"status":"success","type":"sast"},"vulnerabilities":[]}'
  export OPENGREP_TEST_EXIT_CODE="0"

  create_mock_binary "opengrep" '
if [[ "${1:-}" == "--version" ]]; then
  printf "opengrep %s\n" "${OPENGREP_TEST_VERSION:-1.18.0}"
  exit 0
fi
json_file=""
sarif_file=""
text_file=""
gitlab_sast_file=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --json-output) json_file="$2"; shift 2 ;;
    --sarif-output) sarif_file="$2"; shift 2 ;;
    --text-output) text_file="$2"; shift 2 ;;
    --gitlab-sast-output) gitlab_sast_file="$2"; shift 2 ;;
    *) shift ;;
  esac
done
printf "%s" "$OPENGREP_TEST_JSON" > "$json_file"
printf "%s" "$OPENGREP_TEST_SARIF" > "$sarif_file"
printf "%s" "$OPENGREP_TEST_TEXT" > "$text_file"
printf "%s" "$OPENGREP_TEST_GITLAB_SAST" > "$gitlab_sast_file"
exit "$OPENGREP_TEST_EXIT_CODE"
'
  use_mock_path
}

teardown() {
  common_teardown
}

@test "writes OpenGrep outputs and succeeds with no findings" {
  export OPENGREP_FAIL_ON_SEVERITY="high"

  run_script "security/run-opengrep.sh"

  assert_success
  assert_file_exist "$TEST_DIR/opengrep-results.json"
  assert_file_exist "$TEST_DIR/opengrep-results.sarif"
  assert_file_exist "$TEST_DIR/opengrep-results.gitlab-sast.json"
  assert_summary_contains "OpenGrep SAST"
  assert_summary_contains 'Passed with `0` findings.'
  assert_summary_contains "| Security / Code Scanning | SARIF generated, upload not configured |"
  assert_summary_contains "test-owner/test-repo/actions/runs/12345"
  assert_summary_contains 'Configure CODE_SCANNING_TOKEN to publish results in Security / Code Scanning.'
  run get_github_output "opengrep-result"
  assert_output "success"
}

@test "fails when warning findings meet medium threshold" {
  export OPENGREP_FAIL_ON_SEVERITY="medium"
  export OPENGREP_TEST_JSON='{"version":"1.18.0","results":[{"check_id":"rule.warning","extra":{"severity":"WARNING"}},{"check_id":"rule.info","extra":{"severity":"INFO"}}],"errors":[]}'
  export OPENGREP_TEST_TEXT='warning finding'

  run_script "security/run-opengrep.sh"

  assert_failure
  assert_summary_contains 'Blocked by findings meeting threshold `medium`.'
  assert_summary_contains "| Findings | 2 |"
  assert_summary_contains "| WARNING | 1 |"
  run get_github_output "opengrep-result"
  assert_output "failure"
}

@test "ignores suppressed errors but still counts active warnings" {
  export OPENGREP_FAIL_ON_SEVERITY="high"
  export OPENGREP_TEST_JSON='{"results":[{"check_id":"rule.error","extra":{"severity":"ERROR","is_ignored":true}},{"check_id":"rule.warning","extra":{"severity":"WARNING","is_ignored":false}}]}'

  run_script "security/run-opengrep.sh"

  assert_success
  assert_summary_contains "| Findings | 1 |"
  assert_summary_contains "| ERROR | 0 |"
  assert_summary_contains "| WARNING | 1 |"
  run get_github_output "opengrep-result"
  assert_output "success"
}

@test "only suppressed findings pass even the low threshold" {
  export OPENGREP_FAIL_ON_SEVERITY="low"
  export OPENGREP_TEST_JSON='{"results":[{"check_id":"rule.error","extra":{"severity":"ERROR","is_ignored":true}},{"check_id":"rule.warning","extra":{"severity":"WARNING","is_ignored":true}},{"check_id":"rule.info","extra":{"severity":"INFO","is_ignored":true}}]}'

  run_script "security/run-opengrep.sh"

  assert_success
  assert_summary_contains 'Passed with `0` findings.'
  run get_github_output "opengrep-findings-total"
  assert_output "0"
  # Keep the original report for auditing; only counts and gating are filtered.
  run jq '.results | length' "$TEST_DIR/opengrep-results.json"
  assert_output "3"
}

@test "active errors still fail with formatted JSON and suppressed findings" {
  export OPENGREP_FAIL_ON_SEVERITY="high"
  export OPENGREP_TEST_JSON='{
    "results": [
      {"check_id": "ignored", "extra": {"severity": "ERROR", "is_ignored": true}},
      {"check_id": "active", "extra": {"severity": "ERROR"}}
    ]
  }'

  run_script "security/run-opengrep.sh"

  assert_failure
  assert_summary_contains "| Findings | 1 |"
  assert_summary_contains "| ERROR | 1 |"
  run get_github_output "opengrep-result"
  assert_output "failure"
}

@test "metadata that resembles a finding is not counted" {
  export OPENGREP_FAIL_ON_SEVERITY="high"
  export OPENGREP_TEST_JSON='{"results":[{"check_id":"rule.info","extra":{"severity":"INFO","metadata":{"check_id":"example","severity":"ERROR"}}}]}'

  run_script "security/run-opengrep.sh"

  assert_success
  assert_summary_contains "| Findings | 1 |"
  assert_summary_contains "| ERROR | 0 |"
  assert_summary_contains "| INFO | 1 |"
}

@test "invalid JSON cannot report a successful scan" {
  export OPENGREP_TEST_JSON='not JSON'

  run_script "security/run-opengrep.sh"

  assert_failure
  run get_github_output "opengrep-result"
  refute_output "success"
}
