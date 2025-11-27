#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0
set -euo pipefail

readonly TAG_NAME="${1:?Usage: $0 <tag-name> <actor> [authorized-devs]}"
readonly ACTOR="${2:?Usage: $0 <tag-name> <actor> [authorized-devs]}"
readonly AUTHORIZED_DEVS="${3:-}"

if [[ "${TAG_NAME}" == *-SNAPSHOT ]]; then
  printf "− SNAPSHOT release - authorization check skipped\n"
  exit 0
fi

if [[ -z "$AUTHORIZED_DEVS" ]]; then
  printf "::warning::AUTHORIZED_RELEASE_DEVELOPERS secret not configured\n"
  printf "All users with tag push access can create releases\n"
  printf "✓ Authorization check passed (no restrictions configured)\n"
  exit 0
fi

if printf ",%s," "$AUTHORIZED_DEVS" | grep -q ",$ACTOR,"; then
  printf "✓ User '%s' is authorized to create production releases\n" "$ACTOR"
else
  printf "::error::User '%s' is not authorized to create non-SNAPSHOT releases\n" "$ACTOR"
  printf "Only the following users can create production releases:\n"
  printf "%s\n" "$AUTHORIZED_DEVS" | tr ',' '\n' | sed 's/^/  - /'
  printf "\n"
  printf "If you need to create a release, please:\n"
  printf "1. Contact one of the authorized developers\n"
  printf "2. Or create a SNAPSHOT release instead (tag with -SNAPSHOT suffix)\n"
  exit 1
fi
