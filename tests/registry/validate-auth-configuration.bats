#!/usr/bin/env bats

# shellcheck disable=SC1090,SC2016,SC2030,SC2031,SC2119,SC2120,SC2155
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

bats_require_minimum_version 1.13.0

load "${BATS_TEST_DIRNAME}/../libs/bats-support/load.bash"
load "${BATS_TEST_DIRNAME}/../libs/bats-assert/load.bash"
load "${BATS_TEST_DIRNAME}/../libs/bats-file/load.bash"
load "${BATS_TEST_DIRNAME}/../test_helper.bash"

setup() {
  common_setup_with_github_env
  cat > "${TEST_DIR}/validate-auth-stub.sh" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
printf 'use=%s\nregistry=%s\ngithub=%s\nhas_password=%s\n' "$1" "$2" "$3" "$4"
EOF
  chmod +x "${TEST_DIR}/validate-auth-stub.sh"
}

teardown() {
  common_teardown
}

@test "validate-auth-configuration reports password presence" {
  export USE_GITHUB_TOKEN="false"
  export TARGET_REGISTRY="registry.example.com"
  export GITHUB_REGISTRY="ghcr.io"
  export REGISTRY_PASSWORD="secret"
  export VALIDATE_AUTH_SCRIPT="${TEST_DIR}/validate-auth-stub.sh"

  run_script "registry/validate-auth-configuration.sh"

  assert_success
  assert_output --partial "has_password=true"
}

@test "validate-auth-configuration reports missing password" {
  export USE_GITHUB_TOKEN="true"
  export TARGET_REGISTRY="ghcr.io"
  export GITHUB_REGISTRY="ghcr.io"
  export REGISTRY_PASSWORD=""
  export VALIDATE_AUTH_SCRIPT="${TEST_DIR}/validate-auth-stub.sh"

  run_script "registry/validate-auth-configuration.sh"

  assert_success
  assert_output --partial "has_password=false"
}
