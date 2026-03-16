#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

main() {
  readonly BUILD_MODULE="${BUILD_MODULE:?BUILD_MODULE is required}"

  printf "Built artifacts:\n"
  find "$BUILD_MODULE/build/outputs" -type f \( -name "*.apk" -o -name "*.aab" \) -ls || printf "No artifacts found\n"
}

main "$@"
