#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0
set -euo pipefail

readonly TOKEN="${1:-}"
readonly REPOSITORY="${2:?Usage: $0 <token> <repository>}"

if [[ -z "$TOKEN" ]]; then
  printf "::error::Missing RELEASE_TOKEN secret\n"
  printf "This token is required for creating GitHub releases\n"
  printf "GITHUB_TOKEN cannot be used as it lacks permissions when workflows chain\n"
  printf "Add it in Settings → Secrets → Actions\n"
  exit 1
fi

RESPONSE=$(curl -s -o /dev/null -w "%{http_code}" \
  -H "Authorization: token $TOKEN" \
  "https://api.github.com/repos/${REPOSITORY}/releases")

if [[ "$RESPONSE" != "200" ]]; then
  printf "::error::RELEASE_TOKEN is invalid or lacks permissions\n"
  exit 1
fi

printf "Release token validated\n"
