#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

print_validation_status() {
  local skip_validation="$1"

  if [ "$skip_validation" = "true" ]; then
    printf "| **Validation** | %s |\n" "⊘ Skipped"
  else
    printf "| **Validation** | %s |\n" "✓ Passed"
  fi
}

print_submission_step() {
  local submit_review="$1"

  if [ "$submit_review" = "true" ]; then
    printf "3. Build will be automatically submitted for App Store review\n"
  else
    printf "3. Manually submit for external testing or App Store review from App Store Connect\n"
  fi
}

write_summary() {
  # shellcheck disable=SC2153
  {
    printf "## App Store Connect Upload Summary 📱\n"
    printf "\n"
    printf "### Upload Details\n"
    printf "| Property | Value |\n"
    printf "|----------|-------|\n"
    printf "| **IPA File** | \`%s\` |\n" "$(basename "${IPA_FILE}")"
    printf "| **Platform** | %s |\n" "${PLATFORM}"
    print_validation_status "${SKIP_VALIDATION}"
    printf "| **Status** | ✓ Uploaded |\n"

    if [ -n "${REQUEST_ID:-}" ]; then
      printf "| **Request ID** | \`%s\` |\n" "${REQUEST_ID}"
    fi

    printf "\n"
    printf "### Next Steps\n"
    printf "1. Check [App Store Connect](https://appstoreconnect.apple.com) for build processing status\n"
    printf "2. Build will be available in TestFlight within 10-15 minutes after processing completes\n"
    print_submission_step "${SUBMIT_REVIEW}"
    printf "\n"
    printf "*Upload completed at %s*\n" "$(date -u '+%Y-%m-%d %H:%M:%S UTC')"
  } >>"$GITHUB_STEP_SUMMARY"
}

main() {
  readonly IPA_FILE="${IPA_FILE:?IPA_FILE is required}"
  readonly PLATFORM="${PLATFORM:?PLATFORM is required}"
  readonly SKIP_VALIDATION="${SKIP_VALIDATION:?SKIP_VALIDATION is required}"
  readonly SUBMIT_REVIEW="${SUBMIT_REVIEW:?SUBMIT_REVIEW is required}"
  readonly REQUEST_ID="${REQUEST_ID:-}"
  write_summary
}

main "$@"
