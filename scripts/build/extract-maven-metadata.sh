#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Extract Maven project metadata (groupId, artifactId, version)
# and write to CI outputs for downstream steps.
#
# Outputs (via ci_output):
#   VERSION       Project version
#   IS_SNAPSHOT   "true" if version ends with -SNAPSHOT
#   GROUP_ID      Maven groupId
#   ARTIFACT_ID   Maven artifactId

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"

main() {
  VERSION=$(mvn help:evaluate -Dexpression=project.version -q -DforceStdout)
  ci_output "VERSION" "$VERSION"

  if [[ "$VERSION" == *-SNAPSHOT ]]; then
    ci_output "IS_SNAPSHOT" "true"
  else
    ci_output "IS_SNAPSHOT" "false"
  fi

  GROUP_ID=$(mvn help:evaluate -Dexpression=project.groupId -q -DforceStdout)
  ARTIFACT_ID=$(mvn help:evaluate -Dexpression=project.artifactId -q -DforceStdout)
  ci_output "GROUP_ID" "$GROUP_ID"
  ci_output "ARTIFACT_ID" "$ARTIFACT_ID"

  printf "Project: %s:%s:%s\n" "$GROUP_ID" "$ARTIFACT_ID" "$VERSION"
}

main "$@"
