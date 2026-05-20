#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

run_gradle_tasks() {
  local skip_tests="$1"
  shift

  if [[ "$skip_tests" = "true" ]]; then
    ./gradlew "$@" -x test
  else
    ./gradlew "$@"
  fi
}

main() {
  readonly SKIP_TESTS="${SKIP_TESTS:-false}"
  readonly GRADLE_TASKS="${GRADLE_TASKS:?GRADLE_TASKS is required}"

  printf "Running Gradle tasks: %s\n" "$GRADLE_TASKS"
  # shellcheck disable=SC2086
  run_gradle_tasks "$SKIP_TESTS" $GRADLE_TASKS
}

main "$@"
