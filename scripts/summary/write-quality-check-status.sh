#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0
#
# Write quality check status report to GitHub Step Summary
# Usage: write-quality-check-status.sh "Name|enabled|result|deprecated" ...

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"

main() {
  {
    printf "## Pull Request Check Status\n\n"

    # First pass: collect deprecated linters that are enabled
    local DEPRECATED_LIST=""
    local name enabled result deprecated
    for linter in "$@"; do
      IFS='|' read -r name enabled result deprecated <<<"$linter"
      if [[ "$enabled" == "true" && "$deprecated" == "true" ]]; then
        DEPRECATED_LIST="$DEPRECATED_LIST$name|"
      fi
    done

    # Show deprecation warning if any deprecated linters are enabled
    if [[ -n "$DEPRECATED_LIST" ]]; then
      printf "> **Warning:**\n"
      printf "> **DEPRECATED:** The following linters will be removed in version 3.0.0:\n"
      printf ">\n"

      local DEPRECATED_ARRAY
      IFS='|' read -ra DEPRECATED_ARRAY <<<"$DEPRECATED_LIST"
      for name in "${DEPRECATED_ARRAY[@]}"; do
        if [[ -z "$name" ]]; then continue; fi
        printf "> - **%s** - Migrate to \`linters.devbasecheck: true\`\n" "$name"
      done

      printf ">\n"
      printf "> Please update your workflow to use \`linters.devbasecheck: true\` and disable deprecated linters.\n\n"
    fi

    # Status table
    printf "### Quality Check Results\n"
    printf "| Check | Status |\n"
    printf "|-------|--------|\n"

    local FAILED=false
    for linter in "$@"; do
      IFS='|' read -r name enabled result deprecated <<<"$linter"

      # Add deprecated marker to name if applicable
      local DISPLAY_NAME="$name"
      if [[ "$enabled" == "true" && "$deprecated" == "true" ]]; then
        DISPLAY_NAME="$name ⚠️ DEPRECATED"
      fi

      if [[ "$enabled" != "true" ]]; then
        printf "| %s | 🔸 Disabled |\n" "$name"
      elif [[ "$result" == "success" ]]; then
        printf "| %s | ✓ Pass |\n" "$DISPLAY_NAME"
      elif [[ "$result" == "skipped" ]]; then
        printf "| %s | − Skipped |\n" "$DISPLAY_NAME"
      else
        printf "| %s | ✗ Fail |\n" "$DISPLAY_NAME"
        FAILED=true
      fi
    done

    printf "\n"

    if [[ "$FAILED" == "true" ]]; then
      printf "### ✗ Some checks failed\n"
      printf "Please review the failures above and fix any issues.\n"
      printf "Note: Individual linter failures are shown above. This status job always succeeds to provide summary.\n"
    else
      printf "### ✓ All enabled checks passed\n"
    fi
  } >>"$(ci_summary_file)"

  # Do NOT exit 1 on failure - this job provides summary only
  exit 0
}

main "$@"
