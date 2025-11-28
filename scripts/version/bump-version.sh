#!/bin/bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
#
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

log() {
  printf "%s\n" "$1"
}

log_success() {
  printf "✓ %s\n" "$1"
}

log_error() {
  printf "::error::%s\n" "$1"
}

update_or_add_property() {
  local file="$1"
  local key="$2"
  local value="$3"
  local separator="${4:- = }"

  if grep -q "^${key}" "$file"; then
    sed -i "s/^${key}.*/${key}${separator}${value}/" "$file"
    log "Updated ${key} to ${value}"
  else
    printf "%s%s%s\n" "$key" "$separator" "$value" >> "$file"
    log "Added ${key}${separator}${value}"
  fi
}

increment_version_code() {
  local file="$1"

  if grep -q '^versionCode=' "$file"; then
    local current_code
    current_code=$(grep '^versionCode=' "$file" | cut -d'=' -f2 | tr -d ' ')
    local new_code=$((current_code + 1))
    sed -i "s/^versionCode=.*/versionCode=$new_code/" "$file"
    log "Incremented versionCode: ${current_code} → ${new_code}"
  else
    printf "versionCode=1\n" >> "$file"
    log "Added versionCode=1"
  fi
}

PROJECT_TYPE="${1}"
VERSION="${2}"
WORKING_DIR="${3:-.}"
GRADLE_VERSION_FILE="${4:-gradle.properties}"

log "Bumping version to ${VERSION} for ${PROJECT_TYPE} project in ${WORKING_DIR}"

case "$PROJECT_TYPE" in
maven)
  cd "$WORKING_DIR"
  log "Updating Maven version to ${VERSION}"
  # shellcheck disable=SC2086
  mvn ${MAVEN_CLI_OPTS:-} versions:set -DnewVersion="$VERSION" -DgenerateBackupPoms=false -DprocessAllModules=true -DskipTests
  log_success "Maven version updated (including all sub-modules)"
  ;;

npm)
  cd "$WORKING_DIR"
  log "Updating NPM version to ${VERSION}"
  npm version "$VERSION" --no-git-tag-version --allow-same-version
  log_success "NPM version updated"
  ;;

gradle | gradle-android)
  cd "$WORKING_DIR"
  log "Version file: ${GRADLE_VERSION_FILE}"

  if [[ ! -f "$GRADLE_VERSION_FILE" ]]; then
    log_error "Gradle version file not found: ${GRADLE_VERSION_FILE}"
    exit 1
  fi

  update_or_add_property "$GRADLE_VERSION_FILE" "versionName" "$VERSION" "="
  increment_version_code "$GRADLE_VERSION_FILE"

  log_success "Gradle version updated"
  cat "$GRADLE_VERSION_FILE"
  ;;

xcode-ios)
  cd "$WORKING_DIR"
  log "Updating Xcode version to ${VERSION}"

  XCCONFIG_FILE="${XCODE_VERSION_FILE:-versions.xcconfig}"

  if [[ ! -f "$XCCONFIG_FILE" ]]; then
    log "Creating ${XCCONFIG_FILE}"
    printf "MARKETING_VERSION = %s\n" "$VERSION" > "$XCCONFIG_FILE"
    log_success "Created ${XCCONFIG_FILE} with MARKETING_VERSION = ${VERSION}"
  else
    update_or_add_property "$XCCONFIG_FILE" "MARKETING_VERSION" "$VERSION" " = "
    log_success "Xcode version updated"
    cat "$XCCONFIG_FILE"
  fi
  ;;

*)
  log_error "Unknown project type: ${PROJECT_TYPE}"
  exit 1
  ;;
esac
