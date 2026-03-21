#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"

decode_export_options() {
  local export_options_base64="$1"
  local export_options_var="$2"

  if [[ -z "$export_options_base64" ]]; then
    ci_log_error "Export options not found in variable $export_options_var"
    exit 1
  fi

  printf "%s" "$export_options_base64" | base64 --decode >export-options.plist
}

main() {
  readonly EXPORT_OPTIONS_BASE64="${EXPORT_OPTIONS_BASE64:-}"
  readonly EXPORT_OPTIONS_VAR="${EXPORT_OPTIONS_VAR:?EXPORT_OPTIONS_VAR is required}"

  decode_export_options "$EXPORT_OPTIONS_BASE64" "$EXPORT_OPTIONS_VAR"

  xcodebuild -exportArchive \
    -archivePath build/app.xcarchive \
    -exportPath build/export \
    -exportOptionsPlist export-options.plist |
    xcbeautify
}

main "$@"
