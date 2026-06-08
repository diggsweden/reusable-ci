#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
# SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

ci_install_dir() {
  local tool="$1"
  printf '%s/%s-bin' "${CI_TEMP_DIR:-/tmp}" "$tool"
}

ci_prepend_path() {
  local dir="$1"
  export PATH="${dir}:$PATH"
  # Persist to later workflow steps too — without this a tool installed in one
  # step (reusable-ci, syft, trivy, …) is not on PATH in a subsequent step,
  # since a step's `export PATH` does not survive into the next step. Guarded:
  # skipped when GITHUB_PATH is unset/empty (e.g. the non-root nanolinter lint
  # sets it empty because it can't write the runner file-command files).
  if [[ -n "${GITHUB_PATH:-}" ]]; then
    printf '%s\n' "$dir" >>"$GITHUB_PATH"
  fi
}

ci_require_command() {
  local cmd="$1"
  local label="$2"
  if ! command -v "$cmd" &>/dev/null; then
    printf 'ERROR: %s binary not found after install\n' "$label" >&2
    return 1
  fi
}

ci_print_already_installed() {
  local label="$1"
  local version="$2"
  printf '%s already installed: %s\n\n' "$label" "$version"
}

ci_print_installed() {
  local label="$1"
  local version="$2"
  printf '%s installed successfully: %s\n\n' "$label" "$version"
}
