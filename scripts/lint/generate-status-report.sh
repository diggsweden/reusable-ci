#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 The Reusable CI Authors
# SPDX-License-Identifier: CC0-1.0
#
# Generate linter status report for GitHub Step Summary
# Usage: generate-status-report.sh "Name|enabled|result|deprecated" ...

set -euo pipefail

echo "## Pull Request Check Status" >> "$GITHUB_STEP_SUMMARY"
echo "" >> "$GITHUB_STEP_SUMMARY"

# First pass: collect deprecated linters that are enabled
DEPRECATED_LIST=""
for linter in "$@"; do
  IFS='|' read -r name enabled result deprecated <<< "$linter"
  if [[ "$enabled" == "true" && "$deprecated" == "true" ]]; then
    DEPRECATED_LIST="$DEPRECATED_LIST$name|"
  fi
done

# Show deprecation warning if any deprecated linters are enabled
if [[ -n "$DEPRECATED_LIST" ]]; then
  echo "> [!WARNING]" >> "$GITHUB_STEP_SUMMARY"
  echo "> **DEPRECATED:** The following linters will be removed in version 3.0.0:" >> "$GITHUB_STEP_SUMMARY"
  echo ">" >> "$GITHUB_STEP_SUMMARY"
  
  IFS='|' read -ra DEPRECATED_ARRAY <<< "$DEPRECATED_LIST"
  for name in "${DEPRECATED_ARRAY[@]}"; do
    [[ -z "$name" ]] && continue
    echo "> - **$name** - Migrate to \`linters.justmiselint: true\`" >> "$GITHUB_STEP_SUMMARY"
  done
  
  echo ">" >> "$GITHUB_STEP_SUMMARY"
  echo "> Please update your workflow to use \`linters.justmiselint: true\` and disable deprecated linters." >> "$GITHUB_STEP_SUMMARY"
  echo "" >> "$GITHUB_STEP_SUMMARY"
fi

# Status table
echo "### Linter Results" >> "$GITHUB_STEP_SUMMARY"
echo "| Linter | Status |" >> "$GITHUB_STEP_SUMMARY"
echo "|--------|--------|" >> "$GITHUB_STEP_SUMMARY"

FAILED=false
for linter in "$@"; do
  IFS='|' read -r name enabled result deprecated <<< "$linter"
  
  # Add deprecated marker to name if applicable
  DISPLAY_NAME="$name"
  if [[ "$enabled" == "true" && "$deprecated" == "true" ]]; then
    DISPLAY_NAME="$name ⚠️ DEPRECATED"
  fi
  
  if [[ "$enabled" != "true" ]]; then
    echo "| $name | 🔸 Disabled |" >> "$GITHUB_STEP_SUMMARY"
  elif [[ "$result" == "success" ]]; then
    echo "| $DISPLAY_NAME | ✓ Pass |" >> "$GITHUB_STEP_SUMMARY"
  elif [[ "$result" == "skipped" ]]; then
    echo "| $DISPLAY_NAME | − Skipped |" >> "$GITHUB_STEP_SUMMARY"
  else
    echo "| $DISPLAY_NAME | ✗ Fail |" >> "$GITHUB_STEP_SUMMARY"
    FAILED=true
  fi
done

echo "" >> "$GITHUB_STEP_SUMMARY"

if [[ "$FAILED" == "true" ]]; then
  echo "### ✗ Some checks failed" >> "$GITHUB_STEP_SUMMARY"
  echo "Please review the failures above and fix any issues." >> "$GITHUB_STEP_SUMMARY"
  echo "Note: Individual linter failures are shown above. This status job always succeeds to provide summary." >> "$GITHUB_STEP_SUMMARY"
else
  echo "### ✓ All enabled checks passed" >> "$GITHUB_STEP_SUMMARY"
fi

# Do NOT exit 1 on failure - this job provides summary only
exit 0
