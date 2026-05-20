#!/usr/bin/env bash

# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
#
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
LIBS_DIR="${SCRIPT_DIR}/libs"

declare -A BATS_LIBS_URL=(
  ["bats-support"]="https://github.com/bats-core/bats-support.git"
  ["bats-assert"]="https://github.com/bats-core/bats-assert.git"
  ["bats-file"]="https://github.com/bats-core/bats-file.git"
  ["bats-mock"]="https://github.com/jasonkarns/bats-mock.git"
)

declare -A BATS_LIBS_VERSION=(
  ["bats-support"]="v0.3.0"
  ["bats-assert"]="v2.1.0"
  ["bats-file"]="v0.4.0"
  ["bats-mock"]=""
)

ensure_libs_dir() {
  mkdir -p "$LIBS_DIR"
}

install_lib() {
  local name="$1"
  local url="${BATS_LIBS_URL[$name]}"
  local version="${BATS_LIBS_VERSION[$name]}"
  local target_dir="${LIBS_DIR}/${name}"

  if [[ -d "$target_dir" ]]; then
    echo "Updating ${name}..."
    # Stash any local changes before pulling to avoid conflicts
    git -C "$target_dir" stash --quiet 2>/dev/null || true
    git -C "$target_dir" pull --quiet
  else
    echo "Installing ${name}..."
    if [[ -n "$version" ]]; then
      git clone --depth 1 --branch "$version" "$url" "$target_dir"
    else
      git clone --depth 1 "$url" "$target_dir"
    fi
  fi
}

install_all_libs() {
  for name in "${!BATS_LIBS_URL[@]}"; do
    install_lib "$name"
  done
}

main() {
  ensure_libs_dir
  install_all_libs
  echo "BATS libraries installed in ${LIBS_DIR}"
}

main "$@"
