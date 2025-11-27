#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 The Reusable CI Authors
# SPDX-License-Identifier: CC0-1.0
set -euo pipefail

readonly REPOSITORY="${1:?Usage: $0 <repository>}"

printf "Validating bot token permissions...\n"

if ! gh api user --silent 2>/dev/null; then
  printf "::error::OSPO_BOT_GHTOKEN is invalid or expired\n"
  exit 1
fi

if ! gh api "repos/${REPOSITORY}" --silent 2>/dev/null; then
  printf "::error::Bot token cannot access this repository\n"
  printf "Please ensure the bot has appropriate repository access\n"
  exit 1
fi

if ! gh api "repos/${REPOSITORY}/branches" --silent 2>/dev/null; then
  printf "::warning::Bot token may have limited permissions\n"
  printf "Ensure the bot has sufficient permissions to:\n"
  printf "  - Push commits to branches\n"
  printf "  - Create and move tags\n"
  printf "  - Bypass branch protection (if enabled)\n"
fi

printf "✓ Bot token is valid and has repository access\n"
