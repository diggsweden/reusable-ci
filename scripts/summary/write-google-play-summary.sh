#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

print_next_step() {
  local track="$1"
  local percentage="$2"

  case "$track" in
  internal)
    printf "2. Build will be available to internal testers within minutes\n"
    ;;
  alpha | beta)
    printf "2. Build will be available to %s testers after review\n" "$track"
    ;;
  production)
    if [ -n "$percentage" ]; then
      printf "2. Staged rollout to %s of users will begin after review\n" "$percentage"
    else
      printf "2. Full production release will begin after review\n"
    fi
    ;;
  esac
}

calculate_percentage() {
  local user_fraction="$1"

  if [ -n "$user_fraction" ]; then
    awk -v user_fraction="$user_fraction" 'BEGIN { printf "%.0f%%", user_fraction * 100 }'
  fi
}

write_summary() {
  # shellcheck disable=SC2153
  local percentage="$1"

  {
    printf "## Google Play Upload Summary\n"
    printf "\n"
    printf "### Upload Details\n"
    printf "| Property | Value |\n"
    printf "|----------|-------|\n"
    printf "| **AAB File** | \`%s\` |\n" "$(basename "${AAB_FILE}")"
    printf "| **Package** | \`%s\` |\n" "${PACKAGE_NAME}"
    printf "| **Track** | %s |\n" "${TRACK}"
    printf "| **Status** | %s |\n" "${STATUS}"

    if [ -n "${RELEASE_NAME:-}" ]; then
      printf "| **Release Name** | %s |\n" "${RELEASE_NAME}"
    fi

    if [ -n "${USER_FRACTION:-}" ]; then
      printf "| **Staged Rollout** | %s |\n" "$percentage"
    fi

    if [ "${PRIORITY}" != "0" ]; then
      printf "| **Update Priority** | %s |\n" "${PRIORITY}"
    fi

    printf "| **Upload Status** | Uploaded |\n"

    printf "\n"
    printf "### Next Steps\n"
    printf "1. Check [Google Play Console](https://play.google.com/console) for upload status\n"
    print_next_step "${TRACK}" "$percentage"

    if [ "${STATUS}" = "draft" ]; then
      printf "3. Release is saved as draft - manually publish from Play Console when ready\n"
    fi

    printf "\n"
    printf "*Upload completed at %s*\n" "$(date -u '+%Y-%m-%d %H:%M:%S UTC')"
  } >>"$GITHUB_STEP_SUMMARY"
}

main() {
  readonly AAB_FILE="${AAB_FILE:?AAB_FILE is required}"
  readonly PACKAGE_NAME="${PACKAGE_NAME:?PACKAGE_NAME is required}"
  readonly TRACK="${TRACK:?TRACK is required}"
  readonly STATUS="${STATUS:?STATUS is required}"
  readonly RELEASE_NAME="${RELEASE_NAME:-}"
  readonly USER_FRACTION="${USER_FRACTION:-}"
  readonly PRIORITY="${PRIORITY:?PRIORITY is required}"
  local percentage
  percentage=$(calculate_percentage "${USER_FRACTION:-}")
  write_summary "$percentage"
}

main "$@"
