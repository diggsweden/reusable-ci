#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 The Reusable CI Authors
# SPDX-License-Identifier: CC0-1.0
set -euo pipefail

if [[ -z "${ANDROID_KEYSTORE_BASE64:-}" ]]; then
  printf "::error::ANDROID_KEYSTORE secret not found but enable-signing is true\n"
  exit 1
fi

printf "%s" "$ANDROID_KEYSTORE_BASE64" | base64 -d >release.keystore
printf "✓ Android keystore decoded successfully\n" >&2
printf "ANDROID_KEYSTORE_PATH=%s/release.keystore\n" "$PWD"
