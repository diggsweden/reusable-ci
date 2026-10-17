#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"

# Matches a user against the comma-separated list literally, so names such
# as release-bot[bot] are not read as a pattern.
is_listed() {
  printf ",%s," "$AUTHORIZED_DEVS" | grep -qF ",$1,"
}

main() {
  readonly TAG_NAME="${1:?Usage: $0 <tag-name> <actor> [authorized-devs]}"
  readonly ACTOR="${2:?Usage: $0 <tag-name> <actor> [authorized-devs]}"
  readonly AUTHORIZED_DEVS="${3:-}"

  if ci_is_snapshot "$TAG_NAME"; then
    printf "− SNAPSHOT release - authorization check skipped\n"
    exit 0
  fi

  # A release to Maven Central cannot be undone, so it needs a named list of
  # releasers rather than everyone with tag push access.
  if [[ -z "$AUTHORIZED_DEVS" && "${REQUIRE_AUTHORIZED_DEVELOPERS:-false}" == "true" ]]; then
    ci_log_error "AUTHORIZED_RELEASE_DEVELOPERS secret not configured; it is required for production Maven Central releases"
    exit 1
  fi

  if [[ -z "$AUTHORIZED_DEVS" ]]; then
    ci_log_warning "AUTHORIZED_RELEASE_DEVELOPERS secret not configured"
    printf "All users with tag push access can create releases\n"
    printf "✓ Authorization check passed (no restrictions configured)\n"
    exit 0
  fi

  if is_listed "$ACTOR"; then
    printf "✓ User '%s' is authorized to create production releases\n" "$ACTOR"
    # Only a match against a configured list counts as authorized; it lets
    # publishers release irreversibly. Re-runs are judged in the publish
    # jobs, which also run on "Re-run failed jobs" while this step does not.
    ci_output "authorized" "true"
  else
    ci_log_error "User '$ACTOR' is not authorized to create non-SNAPSHOT releases"
    printf "Only the following users can create production releases:\n"
    printf "%s\n" "$AUTHORIZED_DEVS" | tr ',' '\n' | sed 's/^/  - /'
    printf "\n"
    printf "If you need to create a release, please:\n"
    printf "1. Contact one of the authorized developers\n"
    printf "2. Or create a SNAPSHOT release instead (tag with -SNAPSHOT suffix)\n"
    exit 1
  fi
}

main "$@"
