#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0
set -euo pipefail

readonly TAG_NAME="${1:?Usage: $0 <tag-name> <repository> <draft> <make-latest> <attach-artifacts> [release-notes-file]}"
readonly REPOSITORY="${2:?Usage: $0 <tag-name> <repository> <draft> <make-latest> <attach-artifacts> [release-notes-file]}"
readonly DRAFT="${3:-false}"
readonly MAKE_LATEST="${4:-true}"
readonly ATTACH_ARTIFACTS="${5:-}"
readonly RELEASE_NOTES_FILE="${6:-release-notes.md}"

PROJECT_NAME=$(basename "$REPOSITORY")

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

cleanup_existing_release "$TAG_NAME"

ARGS=()
ARGS+=("$TAG_NAME")
ARGS+=("--title" "$TAG_NAME")

if [[ "$DRAFT" == "true" ]]; then
  ARGS+=("--draft")
fi

if [[ "$TAG_NAME" =~ -(alpha|beta|rc|dev|snapshot) ]]; then
  ARGS+=("--prerelease")
fi

if [[ "$MAKE_LATEST" != "true" ]]; then
  ARGS+=("--latest=false")
fi

if [[ -f "$RELEASE_NOTES_FILE" ]] && [[ -s "$RELEASE_NOTES_FILE" ]]; then
  ARGS+=("--notes-file" "$RELEASE_NOTES_FILE")
fi

if [[ -n "$ATTACH_ARTIFACTS" ]]; then
  IFS=',' read -ra PATTERNS <<<"$ATTACH_ARTIFACTS"
  for pattern in "${PATTERNS[@]}"; do
    pattern=$(printf "%s" "$pattern" | xargs)
    for file in $pattern; do
      [[ -f "$file" ]] && ARGS+=("$file")
    done
  done
fi

declare -A ADDED_FILES

if [[ -d "./release-artifacts" ]]; then
  while IFS= read -r -d '' file; do
    BASENAME=$(basename "$file")
    ARGS+=("$file")
    ADDED_FILES[$BASENAME]=1
    if [[ -f "${BASENAME}.asc" ]]; then
      ARGS+=("${BASENAME}.asc")
      ADDED_FILES["${BASENAME}.asc"]=1
    fi
  done < <(find ./release-artifacts -type f \( -name "*.jar" -o -name "*.tgz" -o -name "*.tar.gz" -o -name "*.zip" -o -name "*.war" \) ! -name "original-*.jar" -print0)
fi

VERSION="${TAG_NAME#v}"
SBOM_ZIP="${PROJECT_NAME}-${VERSION}-sboms.zip"
if [[ -f "$SBOM_ZIP" ]]; then
  ARGS+=("$SBOM_ZIP")
  ADDED_FILES["$SBOM_ZIP"]=1
  if [[ -f "${SBOM_ZIP}.asc" ]]; then
    ARGS+=("${SBOM_ZIP}.asc")
    ADDED_FILES["${SBOM_ZIP}.asc"]=1
  fi
fi

if [[ -f "checksums.sha256" ]]; then
  ARGS+=("checksums.sha256")
  ADDED_FILES["checksums.sha256"]=1
  if [[ -f "checksums.sha256.asc" ]]; then
    ARGS+=("checksums.sha256.asc")
    ADDED_FILES["checksums.sha256.asc"]=1
  fi
fi

for sig in *.asc; do
  if [[ -f "$sig" ]]; then
    BASENAME=$(basename "$sig")
    if [[ -z "${ADDED_FILES[$BASENAME]:-}" ]]; then
      ARGS+=("$sig")
      ADDED_FILES[$BASENAME]=1
    fi
  fi
done

printf "Creating release with gh release create\n"
gh release create "${ARGS[@]}"
