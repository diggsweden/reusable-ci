#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"
source "$SCRIPT_DIR/../ci/env.sh"
source "$SCRIPT_DIR/../ci/manifest.sh"

main() {
  local run_time short_sha
  local commitlint_result licenselint_result dependencyreview_result
  local sastopengrep_result megalint_result publiccodelint_result devbasecheck_result swift_result
  local quality_stage_result_json

  run_time=$(date -u '+%Y-%m-%d %H:%M:%S UTC')
  short_sha="${CI_COMMIT:0:7}"
  quality_stage_result_json="$(ci_json_input "${QUALITY_STAGE_RESULT_JSON:-}" "${QUALITY_STAGE_RESULT_PATH:-}")"

  commitlint_result="$(ci_json_value "$quality_stage_result_json" commitlint)"
  licenselint_result="$(ci_json_value "$quality_stage_result_json" licenselint)"
  dependencyreview_result="$(ci_json_value "$quality_stage_result_json" dependencyreview)"
  sastopengrep_result="$(ci_json_value "$quality_stage_result_json" sastopengrep)"
  megalint_result="$(ci_json_value "$quality_stage_result_json" megalint)"
  publiccodelint_result="$(ci_json_value "$quality_stage_result_json" publiccodelint)"
  devbasecheck_result="$(ci_json_value "$quality_stage_result_json" devbasecheck)"
  swift_result="$(ci_json_value "$quality_stage_result_json" swift)"

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
| OpenGrep SAST | $(ci_status_icon "$sastopengrep_result") |
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
