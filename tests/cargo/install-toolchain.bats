#!/usr/bin/env bats

# shellcheck disable=SC1090,SC2030,SC2031,SC2155
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

bats_require_minimum_version 1.13.0

load "${BATS_TEST_DIRNAME}/../libs/bats-support/load.bash"
load "${BATS_TEST_DIRNAME}/../libs/bats-assert/load.bash"
load "${BATS_TEST_DIRNAME}/../libs/bats-file/load.bash"
load "${BATS_TEST_DIRNAME}/../test_helper.bash"

setup() {
  common_setup
  # Mock rustup; record args for assertions.
  create_mock_binary "rustup" 'printf "%s\n" "rustup $*" >> "$TEST_DIR/rustup.log"
case "$1" in
  show)
    if [[ "${MOCK_ACTIVE_TOOLCHAIN_PRESENT:-false}" == "true" ]]; then
      printf "1.94.0\n"
      exit 0
    fi
    exit 1
    ;;
  *) exit 0 ;;
esac'
  use_mock_path
}

teardown() {
  common_teardown
}

@test "uses rust-toolchain.toml when present and toolchain already active" {
  printf "[toolchain]\nchannel = \"1.94\"\n" > rust-toolchain.toml
  export MOCK_ACTIVE_TOOLCHAIN_PRESENT="true"

  WORKDIR="." TOOLCHAIN="stable" \
    run bash "${SCRIPTS_DIR}/cargo/install-toolchain.sh"

  assert_success
  assert_file_contains "$TEST_DIR/rustup.log" "rustup show active-toolchain"
  refute [ -e "$TEST_DIR/rustup.log" ] && grep -q "rustup default" "$TEST_DIR/rustup.log" || true
}

@test "rust-toolchain.toml present but not yet active triggers install" {
  printf "[toolchain]\nchannel = \"1.94\"\n" > rust-toolchain.toml
  # Default mock returns failure for `rustup show active-toolchain`.

  WORKDIR="." TOOLCHAIN="stable" \
    run bash "${SCRIPTS_DIR}/cargo/install-toolchain.sh"

  assert_success
  assert_file_contains "$TEST_DIR/rustup.log" "rustup toolchain install"
}

@test "legacy rust-toolchain file is honoured the same way" {
  printf "1.94\n" > rust-toolchain
  export MOCK_ACTIVE_TOOLCHAIN_PRESENT="true"

  WORKDIR="." TOOLCHAIN="stable" \
    run bash "${SCRIPTS_DIR}/cargo/install-toolchain.sh"

  assert_success
  assert_file_contains "$TEST_DIR/rustup.log" "rustup show active-toolchain"
}

@test "no pinned file falls back to TOOLCHAIN with --profile minimal" {
  WORKDIR="." TOOLCHAIN="1.94" \
    run bash "${SCRIPTS_DIR}/cargo/install-toolchain.sh"

  assert_success
  assert_file_contains "$TEST_DIR/rustup.log" "rustup toolchain install 1.94 --profile minimal"
  assert_file_contains "$TEST_DIR/rustup.log" "rustup default 1.94"
}

@test "fallback path adds --component when COMPONENTS is set" {
  WORKDIR="." TOOLCHAIN="1.94" COMPONENTS="rustfmt clippy" \
    run bash "${SCRIPTS_DIR}/cargo/install-toolchain.sh"

  assert_success
  run grep -F -- "--component rustfmt clippy" "$TEST_DIR/rustup.log"
  assert_success
}

@test "pinned-file path runs rustup component add for each component" {
  printf "[toolchain]\nchannel = \"1.94\"\n" > rust-toolchain.toml
  export MOCK_ACTIVE_TOOLCHAIN_PRESENT="true"

  WORKDIR="." COMPONENTS="rustfmt clippy" \
    run bash "${SCRIPTS_DIR}/cargo/install-toolchain.sh"

  assert_success
  assert_file_contains "$TEST_DIR/rustup.log" "rustup component add rustfmt"
  assert_file_contains "$TEST_DIR/rustup.log" "rustup component add clippy"
}

@test "honours WORKDIR for pinned-file detection" {
  mkdir -p crates/foo
  printf "[toolchain]\nchannel = \"1.94\"\n" > crates/foo/rust-toolchain.toml
  export MOCK_ACTIVE_TOOLCHAIN_PRESENT="true"

  WORKDIR="crates/foo" \
    run bash "${SCRIPTS_DIR}/cargo/install-toolchain.sh"

  assert_success
  assert_file_contains "$TEST_DIR/rustup.log" "rustup show active-toolchain"
}
