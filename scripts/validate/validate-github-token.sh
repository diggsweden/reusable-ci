#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Validates GitHub token type and permissions
# Usage: validate-github-token.sh <token> <repository>

set -euo pipefail

main() {
  local TOKEN="${1:-}"
  local REPOSITORY="${2:-}"

  if [[ -z "$TOKEN" ]]; then
    printf "::error::No GitHub token provided\n"
    printf "A fine-grained PAT (github_pat_*) with 'contents: write' permission is required.\n"
    printf "Create one at: https://github.com/settings/personal-access-tokens/new\n"
    exit 1
  fi

  if [[ -z "$REPOSITORY" ]]; then
    printf "::error::No repository provided\n"
    printf "Usage: %s <token> <repository>\n" "$0"
    exit 1
  fi

  readonly TOKEN
  readonly REPOSITORY

  if [[ "$TOKEN" == ghp_* ]]; then
    printf "::error::Classic PAT detected (ghp_*)\n"
    printf "Classic PATs have broad access and are not recommended.\n"
    printf "Please use a fine-grained PAT (github_pat_*) with 'contents: write' permission.\n"
    printf "See: https://github.com/settings/personal-access-tokens/new\n"
    exit 1
  fi

  if [[ "$TOKEN" != github_pat_* && "$TOKEN" != ghs_* ]]; then
    printf "::warning::Unknown token type. Expected fine-grained PAT (github_pat_*) or GitHub App token (ghs_*).\n"
  fi

  local RESPONSE
  RESPONSE=$(curl -s -o /dev/null -w "%{http_code}" \
    -H "Authorization: token $TOKEN" \
    "https://api.github.com/repos/${REPOSITORY}")

  if [[ "$RESPONSE" != "200" ]]; then
    printf "::error::Token is invalid or lacks permissions\n"
    printf "HTTP Response: %s\n" "$RESPONSE"
    printf "Ensure the token has 'contents: write' permission for this repository\n"
    exit 1
  fi

  printf "✓ GitHub token validated (fine-grained PAT)\n"
}

main "$@"
