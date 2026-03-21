#!/usr/bin/env bats

# shellcheck disable=SC1090,SC2016,SC2030,SC2031,SC2119,SC2120,SC2155
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
#
# SPDX-License-Identifier: CC0-1.0

bats_require_minimum_version 1.13.0

load "${BATS_TEST_DIRNAME}/../libs/bats-support/load.bash"
load "${BATS_TEST_DIRNAME}/../libs/bats-assert/load.bash"
load "${BATS_TEST_DIRNAME}/../libs/bats-file/load.bash"
load "${BATS_TEST_DIRNAME}/../test_helper.bash"

# =============================================================================
# Setup / Teardown
# =============================================================================

setup() {
  common_setup
  setup_github_env

  # Create mock trivy binary
  create_mock_trivy_clean
  use_mock_path

  # Default: no git worktree support needed
  export CI_PR_BASE_REF=""
  export SCAN_PATH="$TEST_DIR"
}

teardown() {
  common_teardown
}

# =============================================================================
# Helper Functions
# =============================================================================

run_scan_dependencies() {
  run --separate-stderr "$SCRIPTS_DIR/security/scan-dependencies.sh" "$@"
  debug_output
}

# Create a mock trivy that reports no vulnerabilities
create_mock_trivy_clean() {
  create_mock_binary "trivy" '
OUTFILE=""
FORMAT=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --output) OUTFILE="$2"; shift 2 ;;
    --format) FORMAT="$2"; shift 2 ;;
    --severity|--scanners) shift 2 ;;
    fs) shift ;;
    --version) printf "Version: 0.62.1\n"; exit 0 ;;
    *) shift ;;
  esac
done

if [[ "$FORMAT" == "json" ]] && [[ -n "$OUTFILE" ]]; then
  printf '"'"'{"SchemaVersion":2,"Results":[]}\n'"'"' > "$OUTFILE"
elif [[ "$FORMAT" == "sarif" ]] && [[ -n "$OUTFILE" ]]; then
  printf '"'"'{"version":"2.1.0","$schema":"https://raw.githubusercontent.com/oasis-tcs/sarif-spec/master/Schemata/sarif-schema-2.1.0.json","runs":[]}\n'"'"' > "$OUTFILE"
fi
'
}

# Create a mock trivy that reports vulnerabilities
create_mock_trivy_with_vulns() {
  local vuln_json="$1"
  create_mock_binary "trivy" '
OUTFILE=""
FORMAT=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    --output) OUTFILE="$2"; shift 2 ;;
    --format) FORMAT="$2"; shift 2 ;;
    --severity|--scanners) shift 2 ;;
    fs) shift ;;
    --version) printf "Version: 0.62.1\n"; exit 0 ;;
    *) shift ;;
  esac
done

if [[ "$FORMAT" == "json" ]] && [[ -n "$OUTFILE" ]]; then
  cat > "$OUTFILE" <<'"'"'VULN_EOF'"'"'
'"$vuln_json"'
VULN_EOF
elif [[ "$FORMAT" == "sarif" ]] && [[ -n "$OUTFILE" ]]; then
  printf '"'"'{"version":"2.1.0","runs":[]}\n'"'"' > "$OUTFILE"
fi
'
}

# JSON with vulnerabilities for testing
VULNS_JSON_TWO='{"SchemaVersion":2,"Results":[{"Vulnerabilities":[{"VulnerabilityID":"CVE-2024-0001","Severity":"CRITICAL","PkgName":"example-lib","InstalledVersion":"1.0.0","FixedVersion":"1.0.1"},{"VulnerabilityID":"CVE-2024-0002","Severity":"HIGH","PkgName":"other-lib","InstalledVersion":"2.0.0","FixedVersion":"2.0.1"}]}]}'

VULNS_JSON_ONE='{"SchemaVersion":2,"Results":[{"Vulnerabilities":[{"VulnerabilityID":"CVE-2024-0001","Severity":"CRITICAL","PkgName":"example-lib","InstalledVersion":"1.0.0","FixedVersion":"1.0.1"}]}]}'

VULNS_JSON_EMPTY='{"SchemaVersion":2,"Results":[]}'

# =============================================================================
# Default Behavior Tests
# =============================================================================

@test "scan-dependencies succeeds with no vulnerabilities" {
  run_scan_dependencies

  assert_success
  assert_output --partial "No new vulnerabilities found"
}

@test "scan-dependencies uses critical severity by default" {
  run_scan_dependencies

  assert_success
  assert_output --partial "Severity threshold: critical"
}

@test "scan-dependencies defaults to diff scan mode" {
  run_scan_dependencies

  assert_success
  assert_output --partial "Scan mode: diff"
}

@test "scan-dependencies writes step summary" {
  run_scan_dependencies

  assert_success
  assert_summary_contains "Dependency Vulnerability Scan"
  assert_summary_contains "Trivy"
}

@test "scan-dependencies generates SARIF file" {
  run_scan_dependencies

  assert_success
  assert_file_exists "$TEST_DIR/trivy-dependency-results.sarif"
}

# =============================================================================
# Vulnerability Detection Tests (Full Scan)
# =============================================================================

@test "scan-dependencies fails when vulnerabilities found in full mode" {
  export SCAN_MODE="full"
  create_mock_trivy_with_vulns "$VULNS_JSON_TWO"
  use_mock_path

  run_scan_dependencies

  assert_failure
  assert_output --partial "Found 2 new vulnerabilities"
}

@test "scan-dependencies fails with single vulnerability" {
  export SCAN_MODE="full"
  create_mock_trivy_with_vulns "$VULNS_JSON_ONE"
  use_mock_path

  run_scan_dependencies

  assert_failure
  assert_output --partial "Found 1 new vulnerability"
}

@test "scan-dependencies succeeds when no vulnerabilities in full mode" {
  export SCAN_MODE="full"
  create_mock_trivy_with_vulns "$VULNS_JSON_EMPTY"
  use_mock_path

  run_scan_dependencies

  assert_success
  assert_output --partial "No new vulnerabilities found"
}

# =============================================================================
# Severity Mapping Tests
# =============================================================================

@test "scan-dependencies maps critical severity" {
  export FAIL_ON_SEVERITY="critical"
  run_scan_dependencies

  assert_success
  assert_summary_contains "CRITICAL"
}

@test "scan-dependencies maps high severity" {
  export FAIL_ON_SEVERITY="high"
  run_scan_dependencies

  assert_success
  assert_summary_contains "CRITICAL,HIGH"
}

@test "scan-dependencies maps moderate severity" {
  export FAIL_ON_SEVERITY="moderate"
  run_scan_dependencies

  assert_success
  assert_summary_contains "CRITICAL,HIGH,MEDIUM"
}

@test "scan-dependencies maps low severity" {
  export FAIL_ON_SEVERITY="low"
  run_scan_dependencies

  assert_success
  assert_summary_contains "CRITICAL,HIGH,MEDIUM,LOW"
}

@test "scan-dependencies warns on unknown severity and defaults to CRITICAL" {
  export FAIL_ON_SEVERITY="unknown"
  run_scan_dependencies

  assert_success
  assert_stderr_contains "Unknown severity"
  assert_summary_contains "CRITICAL"
}

# =============================================================================
# Diff Mode Tests
# =============================================================================

@test "scan-dependencies falls back to full scan when no base ref" {
  export SCAN_MODE="diff"
  export CI_PR_BASE_REF=""

  run_scan_dependencies

  assert_success
  assert_output --partial "no base ref"
  assert_summary_contains "full (no base ref)"
}

@test "scan-dependencies uses full mode when explicitly set" {
  export SCAN_MODE="full"

  run_scan_dependencies

  assert_success
  assert_summary_contains "full"
}

# =============================================================================
# Summary Content Tests
# =============================================================================

@test "scan-dependencies summary shows scanner info" {
  run_scan_dependencies

  assert_success
  assert_summary_contains "Scanner"
  assert_summary_contains "Trivy"
}

@test "scan-dependencies summary shows fail threshold" {
  export FAIL_ON_SEVERITY="high"
  run_scan_dependencies

  assert_success
  assert_summary_contains "high"
}

@test "scan-dependencies summary shows zero new vulnerabilities" {
  run_scan_dependencies

  assert_success
  assert_summary_contains "**0**"
  assert_summary_contains "No new vulnerabilities found"
}

@test "scan-dependencies summary shows vulnerability count on failure" {
  export SCAN_MODE="full"
  create_mock_trivy_with_vulns "$VULNS_JSON_TWO"
  use_mock_path

  run_scan_dependencies

  assert_failure
  assert_summary_contains "**2**"
  assert_summary_contains "New Vulnerabilities"
}

# =============================================================================
# SARIF Output Tests
# =============================================================================

@test "scan-dependencies SARIF output is valid JSON" {
  run_scan_dependencies

  assert_success
  local sarif_file="$TEST_DIR/trivy-dependency-results.sarif"
  assert_file_exists "$sarif_file"

  # Basic JSON structure check
  run head -1 "$sarif_file"
  assert_output --partial "version"
}

# =============================================================================
# Scan Path Tests
# =============================================================================

@test "scan-dependencies uses SCAN_PATH when set" {
  local custom_dir="$TEST_DIR/custom-project"
  mkdir -p "$custom_dir"
  export SCAN_PATH="$custom_dir"

  run_scan_dependencies

  assert_success
  assert_output --partial "Scan path: $custom_dir"
}

@test "scan-dependencies defaults scan path to current directory" {
  export SCAN_PATH="."

  run_scan_dependencies

  assert_success
  assert_output --partial "Scan path: ."
}

# =============================================================================
# Trivy Installation Tests
# =============================================================================

@test "scan-dependencies skips install when trivy is available" {
  run_scan_dependencies

  assert_success
  assert_output --partial "Trivy already installed"
}

# =============================================================================
# Error Output Tests
# =============================================================================

@test "scan-dependencies error message uses GitHub annotation format" {
  export SCAN_MODE="full"
  create_mock_trivy_with_vulns "$VULNS_JSON_ONE"
  use_mock_path

  run_scan_dependencies

  assert_failure
  assert_output --partial "::error::"
}

@test "scan-dependencies success does not produce error annotations" {
  run_scan_dependencies

  assert_success
  refute_output --partial "::error::"
}
