#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Create GitHub Release with artifacts
# Usage: create-github-release.sh <tag-name> <repository> <draft> <make-latest> <attach-artifacts> [release-notes-file] [artifact-name]

set -euo pipefail

readonly TAG_NAME="${1:?Usage: $0 <tag-name> <repository> <draft> <make-latest> <attach-artifacts> [release-notes-file] [artifact-name]}"
readonly REPOSITORY="${2:?Usage: $0 <tag-name> <repository> <draft> <make-latest> <attach-artifacts> [release-notes-file] [artifact-name]}"
readonly DRAFT="${3:-false}"
readonly MAKE_LATEST="${4:-true}"
readonly ATTACH_ARTIFACTS="${5:-}"
readonly RELEASE_NOTES_FILE="${6:-release-notes.md}"
readonly ARTIFACT_NAME="${7:-$(basename "$REPOSITORY")}"

PROJECT_NAME="$ARTIFACT_NAME"
readonly PROJECT_NAME
VERSION="${TAG_NAME#v}"
readonly VERSION
readonly PRERELEASE_REGEX='-(alpha|beta|rc|dev|snapshot)'

declare -A ADDED_FILES
ARGS=()

cleanup_existing_release() {
  local tag="$1"
  local release_info is_draft is_prerelease

  if ! release_info=$(gh release view "$tag" --json isDraft,isPrerelease 2>/dev/null); then
    return 0
  fi

  is_draft=$(printf "%s" "$release_info" | jq -r '.isDraft')
  is_prerelease=$(printf "%s" "$release_info" | jq -r '.isPrerelease')

  if [[ "$is_draft" == "true" || "$is_prerelease" == "true" ]]; then
    printf "Removing existing draft/prerelease for tag: %s\n" "$tag"
    gh release delete "$tag" --yes
  else
    printf "::error::Release %s already exists and is not a draft/prerelease. Cannot overwrite.\n" "$tag"
    exit 1
  fi
}

add_file() {
  local file="$1"
  local basename

  [[ -f "$file" ]] || return 0

  basename=$(basename "$file")
  if [[ -z "${ADDED_FILES[$basename]:-}" ]]; then
    ARGS+=("$file")
    ADDED_FILES["$basename"]=1
  fi
}

add_file_with_signature() {
  local file="$1"
  add_file "$file"
  add_file "${file}.asc"
}

build_release_args() {
  ARGS+=("$TAG_NAME" "--title" "$TAG_NAME")

  [[ "$DRAFT" == "true" ]] && ARGS+=("--draft")
  [[ "$TAG_NAME" =~ $PRERELEASE_REGEX ]] && ARGS+=("--prerelease")
  [[ "$MAKE_LATEST" != "true" ]] && ARGS+=("--latest=false")

  if [[ -f "$RELEASE_NOTES_FILE" && -s "$RELEASE_NOTES_FILE" ]]; then
    ARGS+=("--notes-file" "$RELEASE_NOTES_FILE")
  fi
}

collect_pattern_artifacts() {
  [[ -z "$ATTACH_ARTIFACTS" ]] && return

  local pattern
  IFS=',' read -ra PATTERNS <<<"$ATTACH_ARTIFACTS"
  for pattern in "${PATTERNS[@]}"; do
    pattern=$(printf "%s" "$pattern" | xargs)
    for file in $pattern; do
      add_file "$file"
    done
  done
}

collect_release_artifacts() {
  [[ -d "./release-artifacts" ]] || return

  local file basename
  while IFS= read -r -d '' file; do
    basename=$(basename "$file")
    add_file "$file"
    add_file "${basename}.asc"
  done < <(find ./release-artifacts -type f \
    \( -name "*.jar" -o -name "*.tgz" -o -name "*.tar.gz" -o -name "*.zip" -o -name "*.war" \) \
    ! -name "original-*.jar" -print0)
}

collect_sbom_artifacts() {
  local sbom_zip="${PROJECT_NAME}-${VERSION}-sboms.zip"
  if [[ -f "$sbom_zip" ]]; then
    printf "Adding SBOM ZIP: %s\n" "$sbom_zip"
    add_file_with_signature "$sbom_zip"
  else
    printf "::warning::SBOM ZIP not found: %s\n" "$sbom_zip"
  fi
}

collect_checksum_artifacts() {
  add_file_with_signature "checksums.sha256"
}

collect_remaining_signatures() {
  for sig in *.asc; do
    add_file "$sig"
  done
}

main() {
  cleanup_existing_release "$TAG_NAME"

  build_release_args
  collect_pattern_artifacts
  collect_release_artifacts
  collect_sbom_artifacts
  collect_checksum_artifacts
  collect_remaining_signatures

  printf "Creating release with gh release create\n"
  gh release create "${ARGS[@]}"
}

main
