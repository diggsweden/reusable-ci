#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"
source "$SCRIPT_DIR/../ci/env.sh"

add_status_row() {
  printf "| %s | %s |\n" "$1" "$(ci_status_icon "$2")" >>"$(ci_summary_file)"
}

write_overview() {
  local release_time="$1"

  {
    printf "# Release Summary\n\n"
    printf "## Release Overview\n"
    printf "| Property | Value |\n"
    printf "|----------|-------|\n"
    printf "| **Version** | \`%s\` |\n" "$RELEASE_VERSION"
    printf "| **Branch** | \`%s\` |\n" "$RELEASE_BRANCH"
    printf "| **Commit** | \`%s\` |\n" "$RELEASE_COMMIT"
    printf "| **Released By** | @%s |\n" "$RELEASE_ACTOR"
    printf "| **Released At** | %s |\n\n" "$release_time"
    printf "## Job Status\n"
    printf "| Job | Status |\n"
    printf "|-----|--------|\n"
  } >>"$(ci_summary_file)"
}

write_resources() {
  cat >>"$(ci_summary_file)" <<EOF

## Resources
- [GitHub Release]($CI_SERVER_URL/$CI_REPO/releases/tag/$RELEASE_VERSION)
- [Packages]($CI_SERVER_URL/$CI_REPO/packages)
- [Workflow Run]($CI_RUN_URL)

EOF
}

main() {
  local release_time
  local prepare_stage_result
  local build_maven_result build_npm_result build_gradle_result build_gradleandroid_result build_xcodeios_result
  local publish_github_result publish_central_result publish_appstore_result publish_googleplay_result publish_containers_result

  release_time=$(date -u '+%Y-%m-%d %H:%M:%S UTC')
  prepare_stage_result="$(ci_json_value "$PREPARE_STAGE_RESULT_JSON" version-bump)"
  build_maven_result="$(ci_json_value "$BUILD_STAGE_RESULT_JSON" maven)"
  build_npm_result="$(ci_json_value "$BUILD_STAGE_RESULT_JSON" npm)"
  build_gradle_result="$(ci_json_value "$BUILD_STAGE_RESULT_JSON" gradle)"
  build_gradleandroid_result="$(ci_json_value "$BUILD_STAGE_RESULT_JSON" gradleandroid)"
  build_xcodeios_result="$(ci_json_value "$BUILD_STAGE_RESULT_JSON" xcodeios)"
  publish_github_result="$(ci_json_value "$PUBLISH_STAGE_RESULT_JSON" githubpackages)"
  publish_central_result="$(ci_json_value "$PUBLISH_STAGE_RESULT_JSON" mavencentral)"
  publish_appstore_result="$(ci_json_value "$PUBLISH_STAGE_RESULT_JSON" appleappstore)"
  publish_googleplay_result="$(ci_json_value "$PUBLISH_STAGE_RESULT_JSON" googleplay)"
  publish_containers_result="$(ci_json_value "$PUBLISH_STAGE_RESULT_JSON" containers)"

  write_overview "$release_time"
  add_status_row "Version Bump" "$prepare_stage_result"
  add_status_row "Build Maven" "$build_maven_result"
  add_status_row "Build NPM" "$build_npm_result"
  add_status_row "Build Gradle" "$build_gradle_result"
  add_status_row "Build Gradle Android" "$build_gradleandroid_result"
  add_status_row "Build Xcode" "$build_xcodeios_result"
  add_status_row "Publish GitHub" "$publish_github_result"
  add_status_row "Publish Maven Central" "$publish_central_result"
  add_status_row "Publish Apple App Store" "$publish_appstore_result"
  add_status_row "Publish Google Play" "$publish_googleplay_result"
  add_status_row "Containers" "$publish_containers_result"
  add_status_row "GitHub Release" "$CREATE_RELEASE_RESULT"
  write_resources
}

main "$@"
