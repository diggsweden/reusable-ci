#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Create GitHub Release with artifacts
#
# Required env: TAG_NAME, REPOSITORY
# Optional env: RELEASE_NAME, DRAFT, MAKE_LATEST, ATTACH_ARTIFACTS, RELEASE_NOTES_FILE, ARTIFACT_NAME

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"

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
  ARGS+=("$TAG_NAME" "--title" "$RELEASE_NAME")

  if [[ "$DRAFT" == "true" ]]; then ARGS+=("--draft"); fi
  if ci_is_prerelease "$TAG_NAME"; then ARGS+=("--prerelease"); fi
  if [[ "$MAKE_LATEST" != "true" ]]; then ARGS+=("--latest=false"); fi

  if [[ -f "$RELEASE_NOTES_FILE" && -s "$RELEASE_NOTES_FILE" ]]; then
    ARGS+=("--notes-file" "$RELEASE_NOTES_FILE")
  fi
}

collect_pattern_artifacts() {
  if [[ -z "$ATTACH_ARTIFACTS" ]]; then return; fi

  local pattern
  local PATTERNS
  IFS=',' read -ra PATTERNS <<<"$ATTACH_ARTIFACTS"
  for pattern in "${PATTERNS[@]}"; do
    pattern=$(printf "%s" "$pattern" | xargs)
    for file in $pattern; do
      add_file "$file"
    done
  done
}

collect_release_artifacts() {
  if [[ ! -d "./release-artifacts" ]]; then return; fi

  local file basename
  while IFS= read -r -d '' file; do
    basename=$(basename "$file")
    add_file "$file"
    add_file "${basename}.asc"
  done < <(ci_find_release_artifacts)
}

collect_sbom_artifacts() {
  local sbom_zip
  sbom_zip=$(ci_sbom_zip_name "$PROJECT_NAME" "$VERSION")
  if [[ -f "$sbom_zip" ]]; then
    printf "Adding SBOM ZIP: %s\n" "$sbom_zip"
    add_file_with_signature "$sbom_zip"
  else
    printf "::warning::SBOM ZIP not found: %s\n" "$sbom_zip"
  fi
}

collect_checksum_artifacts() {
  if [[ -s "$CI_CHECKSUMS_FILE" ]]; then
    add_file_with_signature "$CI_CHECKSUMS_FILE"
  else
    printf "::warning::No %s or file is empty - skipping\n" "$CI_CHECKSUMS_FILE"
  fi
}

collect_remaining_signatures() {
  for sig in *.asc; do
    add_file "$sig"
  done
}

main() {
  readonly TAG_NAME="${TAG_NAME:?TAG_NAME is required}"
  readonly REPOSITORY="${REPOSITORY:?REPOSITORY is required}"
  readonly RELEASE_NAME="${RELEASE_NAME:-$TAG_NAME}"
  readonly DRAFT="${DRAFT:-false}"
  readonly MAKE_LATEST="${MAKE_LATEST:-true}"
  readonly ATTACH_ARTIFACTS="${ATTACH_ARTIFACTS:-}"
  readonly RELEASE_NOTES_FILE="${RELEASE_NOTES_FILE:-release-notes.md}"
  readonly ARTIFACT_NAME="${ARTIFACT_NAME:-$(basename "$REPOSITORY")}"

  readonly PROJECT_NAME="$ARTIFACT_NAME"
  readonly VERSION="${TAG_NAME#v}"

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

main "$@"
