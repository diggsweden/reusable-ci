#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

main() {
  printf "Built artifacts:\n"
  find build -type f \( -name "*.ipa" -o -name "*.xcarchive" \) -ls || printf "No artifacts found\n"
}

main "$@"
