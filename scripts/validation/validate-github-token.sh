#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 The Reusable CI Authors
# SPDX-License-Identifier: CC0-1.0
set -euo pipefail

readonly TOKEN="${1:?Usage: $0 <token> <repository>}"
readonly REPOSITORY="${2:?Usage: $0 <token> <repository>}"

RESPONSE=$(curl -s -o /dev/null -w "%{http_code}" \
  -H "Authorization: token $TOKEN" \
  "https://api.github.com/repos/${REPOSITORY}")

if [[ "$RESPONSE" != "200" ]]; then
  printf "::error::Token is invalid or lacks permissions\n"
  printf "HTTP Response: %s\n" "$RESPONSE"
  printf "Ensure the token has 'repo' scope\n"
  exit 1
fi

printf "✓ GitHub token validated\n"
