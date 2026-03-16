#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"
source "$SCRIPT_DIR/../ci/env.sh"

main() {
  local run_time short_sha
  local commitlint_result licenselint_result dependencyreview_result
  local megalint_result publiccodelint_result devbasecheck_result swift_result

  run_time=$(date -u '+%Y-%m-%d %H:%M:%S UTC')
  short_sha="${CI_COMMIT:0:7}"

  commitlint_result="$(ci_json_value "$QUALITY_STAGE_RESULT_JSON" commitlint)"
  licenselint_result="$(ci_json_value "$QUALITY_STAGE_RESULT_JSON" licenselint)"
  dependencyreview_result="$(ci_json_value "$QUALITY_STAGE_RESULT_JSON" dependencyreview)"
  megalint_result="$(ci_json_value "$QUALITY_STAGE_RESULT_JSON" megalint)"
  publiccodelint_result="$(ci_json_value "$QUALITY_STAGE_RESULT_JSON" publiccodelint)"
  devbasecheck_result="$(ci_json_value "$QUALITY_STAGE_RESULT_JSON" devbasecheck)"
  swift_result="$(ci_json_value "$QUALITY_STAGE_RESULT_JSON" swift)"

  cat >>"$(ci_summary_file)" <<EOF
# Pull Request Summary

## Overview
| Property | Value |
|----------|-------|
| **Project Type** | \`$PROJECT_TYPE\` |
| **Branch** | \`$CI_BRANCH\` |
| **Commit** | \`$short_sha\` |
| **Checked By** | @$CI_ACTOR |
| **Checked At** | $run_time |

## Quality Check Status
| Check | Status |
|-------|--------|
| Commit Lint | $(ci_status_icon "$commitlint_result") |
| License Lint | $(ci_status_icon "$licenselint_result") |
| Dependency Review | $(ci_status_icon "$dependencyreview_result") |
| MegaLinter | $(ci_status_icon "$megalint_result") |
| Publiccode Lint | $(ci_status_icon "$publiccodelint_result") |
| Devbase Check | $(ci_status_icon "$devbasecheck_result") |
| Swift Lint | $(ci_status_icon "$swift_result") |

## Resources
- [Workflow Run]($CI_RUN_URL)
EOF

  printf '✓ PR summary generated successfully\n'
}

main "$@"
