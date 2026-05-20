#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Install the Rust toolchain for sbom-cargo.yml and lint-cargo.yml.
#
# rust-toolchain.toml (or legacy rust-toolchain) inside WORKDIR is the source
# of truth when present — rustup honours the file automatically once installed.
# Otherwise the TOOLCHAIN env var is installed and set as the default. Optional
# COMPONENTS adds rustfmt/clippy on top of --profile minimal.
#
# Inputs (env):
#   WORKDIR     Working directory to inspect for a pinned toolchain file. Defaults to ".".
#   TOOLCHAIN   Toolchain channel to install when no pinned file is present. Defaults to "stable".
#   COMPONENTS  Space-separated rustup components to add. Optional.

set -euo pipefail

WORKDIR="${WORKDIR:-.}"
TOOLCHAIN="${TOOLCHAIN:-stable}"
COMPONENTS="${COMPONENTS:-}"

if [[ -f "$WORKDIR/rust-toolchain.toml" || -f "$WORKDIR/rust-toolchain" ]]; then
  # Pinned file present; rustup install honours it. show active-toolchain
  # confirms the toolchain is materialised so cache keys downstream are stable.
  (cd "$WORKDIR" && rustup show active-toolchain >/dev/null 2>&1) ||
    (cd "$WORKDIR" && rustup toolchain install)
  for component in $COMPONENTS; do
    rustup component add "$component"
  done
else
  # shellcheck disable=SC2086 # COMPONENTS is a deliberate space-separated list
  if [[ -n "$COMPONENTS" ]]; then
    rustup toolchain install "$TOOLCHAIN" --profile minimal --component $COMPONENTS
  else
    rustup toolchain install "$TOOLCHAIN" --profile minimal
  fi
  rustup default "$TOOLCHAIN"
fi
