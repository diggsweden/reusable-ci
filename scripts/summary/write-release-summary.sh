#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

add_status_row() {
  local label="$1"
  local result="$2"
  local status

  case "$result" in
  success)
    status="✓"
    ;;
  skipped)
    status="−"
    ;;
  *)
    status="✗"
    ;;
  esac

  printf "| %s | %s |\n" "$label" "$status" >>"$GITHUB_STEP_SUMMARY"
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
  } >>"$GITHUB_STEP_SUMMARY"
}

write_resources() {
  cat >>"$GITHUB_STEP_SUMMARY" <<EOF

## Resources
- [GitHub Release](https://github.com/$GITHUB_REPOSITORY/releases/tag/$RELEASE_VERSION)
- [Packages](https://github.com/$GITHUB_REPOSITORY/packages)
- [Workflow Run](https://github.com/$GITHUB_REPOSITORY/actions/runs/$GITHUB_RUN_ID)

EOF
}

main() {
  local release_time
  release_time=$(date -u '+%Y-%m-%d %H:%M:%S UTC')

  write_overview "$release_time"
  add_status_row "Version Bump" "$VERSION_AND_CHANGELOG_RESULT"
  add_status_row "Build Maven" "$BUILD_MAVEN_RESULT"
  add_status_row "Build NPM" "$BUILD_NPM_RESULT"
  add_status_row "Build Gradle" "$BUILD_GRADLE_RESULT"
  add_status_row "Build Gradle Android" "$BUILD_GRADLE_ANDROID_RESULT"
  add_status_row "Build Xcode" "$BUILD_XCODE_RESULT"
  add_status_row "Publish GitHub" "$PUBLISH_MAVEN_GITHUB_RESULT"
  add_status_row "Publish Maven Central" "$PUBLISH_MAVEN_CENTRAL_RESULT"
  add_status_row "Publish Apple App Store" "$PUBLISH_APPLE_APPSTORE_RESULT"
  add_status_row "Publish Google Play" "$PUBLISH_GOOGLE_PLAY_RESULT"
  add_status_row "Containers" "$BUILD_CONTAINERS_RESULT"
  add_status_row "GitHub Release" "$CREATE_RELEASE_RESULT"
  write_resources
}

main "$@"
