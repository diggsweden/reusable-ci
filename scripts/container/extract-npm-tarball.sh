#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

find_tarball() {
  find . -maxdepth 1 \( -name "*.tgz" -o -name "*.tar.gz" \) -type f -print -quit
}

main() {
  local tarball

  tarball=$(find_tarball)
  if [[ -n "$tarball" ]]; then
    printf "Extracting %s...\n" "$tarball"
    tar -xzf "$tarball" --strip-components=1
    rm "$tarball"
  fi
}

main "$@"
