#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

main() {
  readonly WORKSPACE="${WORKSPACE:-}"
  readonly PROJECT="${PROJECT:-}"
  readonly SCHEME="${SCHEME:?SCHEME is required}"
  readonly CONFIGURATION="${CONFIGURATION:?CONFIGURATION is required}"
  readonly DESTINATION="${DESTINATION:?DESTINATION is required}"
  readonly XC_CONFIG_PATH="${XC_CONFIG_PATH:-}"
  readonly BUILD_NUMBER="${BUILD_NUMBER:-}"

  local -a cmd=(xcodebuild archive)

  if [[ -n "$WORKSPACE" ]]; then
    cmd+=(-workspace "$WORKSPACE")
  elif [[ -n "$PROJECT" ]]; then
    cmd+=(-project "$PROJECT")
  fi

  cmd+=(-scheme "$SCHEME")
  cmd+=(-configuration "$CONFIGURATION")
  cmd+=(-archivePath build/app.xcarchive)
  cmd+=(-destination "$DESTINATION")
  cmd+=(-skipPackagePluginValidation)

  if [[ -n "$XC_CONFIG_PATH" ]]; then
    cmd+=(-xcconfig "$XC_CONFIG_PATH")
  fi

  if [[ -n "$BUILD_NUMBER" ]]; then
    cmd+=("CURRENT_PROJECT_VERSION=$BUILD_NUMBER")
  fi

  mkdir -p build

  printf 'Running: %s\n' "${cmd[*]}"
  "${cmd[@]}" | xcbeautify
}

main "$@"
