#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Validates that a git tag is annotated and cryptographically signed
# Usage: validate-tag-signature.sh <tag-name> <github-repository> [ospo-bot-gpg-pub]

set -uo pipefail

TAG_NAME="${1:-}"
GITHUB_REPOSITORY="${2:-}"
OSPO_BOT_GPG_PUB="${3:-}"

if [[ -z "$TAG_NAME" ]]; then
  printf "::error::Usage: validate-tag-signature.sh <tag-name> <github-repository>\n"
  exit 1
fi

printf "## Validating Release Tag Security\n"

# Check if tag is annotated using git cat-file
# For annotated tags: git cat-file -t <tag> returns "tag"
# For lightweight tags: git cat-file -t <tag> returns "commit"
TAG_TYPE=$(git cat-file -t "$TAG_NAME" 2>/dev/null || printf "unknown")

printf "Tag '%s' object type: %s\n" "$TAG_NAME" "$TAG_TYPE"

if [[ "$TAG_TYPE" != "tag" ]]; then
  printf "✗ Tag '%s' is a lightweight tag (not annotated)\n" "$TAG_NAME"
  printf "📝 Requirement: Use annotated tags for releases\n"
  printf "💡 Example: git tag -a v1.0.0 -m 'Release v1.0.0'\n"
  if [[ -n "$GITHUB_REPOSITORY" ]]; then
    printf "📚 https://github.com/%s/blob/main/.github/WORKFLOWS.md#tag-requirements\n" "${GITHUB_REPOSITORY}"
  fi
  exit 1
fi

printf "✓ Tag '%s' is annotated\n" "$TAG_NAME"

# Check if tag has any signature (GPG or SSH)
printf "Checking tag signature...\n"
TAG_CONTENT=$(git cat-file tag "$TAG_NAME")

HAS_GPG_SIG=false
HAS_SSH_SIG=false

if printf "%s" "$TAG_CONTENT" | grep -q "BEGIN PGP SIGNATURE"; then
  HAS_GPG_SIG=true
  printf "✓ Tag has a GPG signature\n"
fi

if printf "%s" "$TAG_CONTENT" | grep -q "BEGIN SSH SIGNATURE"; then
  HAS_SSH_SIG=true
  printf "✓ Tag has an SSH signature\n"
fi

if [[ "$HAS_GPG_SIG" == "false" ]] && [[ "$HAS_SSH_SIG" == "false" ]]; then
  printf "::error::✗ Tag '%s' is not signed\n" "$TAG_NAME"
  printf "\n"
  printf "Release tags must be cryptographically signed.\n"
  printf "Create with: git tag -s v1.0.0 -m \"Release v1.0.0\"\n"
  printf "Learn more: https://docs.github.com/en/authentication/managing-commit-signature-verification\n"
  exit 1
fi

# Try to verify the signature (might fail if we don't have the key)
if [[ "$HAS_GPG_SIG" == "true" ]]; then
  # Import GPG keys for verification (if available)
  if [[ -n "$OSPO_BOT_GPG_PUB" ]]; then
    printf "%s" "$OSPO_BOT_GPG_PUB" | gpg --import 2>/dev/null || true
  fi

  if git tag -v "$TAG_NAME" 2>/dev/null; then
    printf "✓ GPG signature verification successful\n"
    SIGNER=$(git tag -v "$TAG_NAME" 2>&1 | grep "Good signature from" | sed 's/.*Good signature from "\(.*\)".*/\1/' || printf "Unknown")
    printf "   Signed by: %s\n" "$SIGNER"
  else
    printf "ℹ️ GPG signature present (verification requires signer's public key)\n"
  fi
fi

if [[ "$HAS_SSH_SIG" == "true" ]]; then
  printf "ℹ️ SSH signature present\n"
  printf "   GitHub will show 'Verified' if the SSH key is uploaded to the signer's account\n"
fi

# Display tag information
printf "\n"
printf "### Tag Security Summary:\n"
printf "✓ Tag is annotated (not lightweight)\n"
printf "✓ Tag is cryptographically signed\n"
printf "✓ Release security requirements met\n"
printf "\n"
printf "### Tag Information:\n"
printf "Tagged commit: %s\n" "$(git rev-list -n 1 "$TAG_NAME")"
printf "Tagger: %s\n" "$(git for-each-ref "refs/tags/$TAG_NAME" --format='%(taggername) <%(taggeremail)>')"
printf "Tag date: %s\n" "$(git for-each-ref "refs/tags/$TAG_NAME" --format='%(taggerdate:iso8601)')"
printf "\n"
printf "Tag message:\n"
git tag -l -n999 "$TAG_NAME" | sed 's/^[^ ]* */  /'
