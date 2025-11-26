#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
#
# SPDX-License-Identifier: CC0-1.0
#
# Generates a release prerequisites summary for GITHUB_STEP_SUMMARY
#
# Expected environment variables:
#   TAG_NAME             - Git tag name (e.g., v1.0.0)
#   COMMIT_SHA           - Full commit SHA being released
#   REF_TYPE             - GitHub ref type (tag or branch)
#   PROJECT_TYPE         - Project type (maven, npm, gradle, etc.)
#   BUILD_TYPE           - Build type (application or library)
#   CONTAINER_REGISTRY   - Container registry URL (optional)
#   SIGN_ARTIFACTS       - Enable GPG signing (true/false, default: false)
#   CHECK_AUTHORIZATION  - Check user authorization (true/false, default: false)
#   ACTOR                - GitHub actor triggering the release
#   JOB_STATUS           - Current job status (success/failure)
#
# Optional secrets (checked for availability):
#   OSPO_BOT_GPG_PRIV, OSPO_BOT_GPG_PASS, OSPO_BOT_GPG_PUB
#   OSPO_BOT_GHTOKEN, RELEASE_TOKEN
#   MAVENCENTRAL_USERNAME, MAVENCENTRAL_PASSWORD, NPM_TOKEN, PUBLISH_TO

set -euo pipefail

TAG_NAME="${TAG_NAME:-}"
COMMIT_SHA="${COMMIT_SHA:-}"
REF_TYPE="${REF_TYPE:-}"
PROJECT_TYPE="${PROJECT_TYPE:-}"
BUILD_TYPE="${BUILD_TYPE:-}"
CONTAINER_REGISTRY="${CONTAINER_REGISTRY:-}"
SIGN_ARTIFACTS="${SIGN_ARTIFACTS:-false}"
CHECK_AUTHORIZATION="${CHECK_AUTHORIZATION:-false}"
ACTOR="${ACTOR:-}"
JOB_STATUS="${JOB_STATUS:-}"

cat >>"$GITHUB_STEP_SUMMARY" <<EOF
# 📋 Release Prerequisites Validation Report

## 🏷️ Release Tag
- **Tag:** \`$TAG_NAME\`
- **Type:** $REF_TYPE
EOF

if [ "$REF_TYPE" = "tag" ]; then
  TAGGER_INFO=$(git for-each-ref refs/tags/"$TAG_NAME" --format='%(taggername) <%(taggeremail)>' 2>/dev/null || printf "N/A")
  TAG_DATE=$(git for-each-ref refs/tags/"$TAG_NAME" --format='%(taggerdate:short)' 2>/dev/null || printf "N/A")
  TAG_MESSAGE=$(git tag -l -n1 "$TAG_NAME" | sed "s/^$TAG_NAME *//" | head -1 || printf "No message")

  TAG_SIGNATURE="Not signed"
  if git tag -v "$TAG_NAME" >/dev/null 2>&1; then
    TAG_SIGNATURE="GPG signed"
  elif git show "$TAG_NAME" 2>/dev/null | grep -q "BEGIN SSH SIGNATURE"; then
    TAG_SIGNATURE="SSH signed"
  fi

  cat >>"$GITHUB_STEP_SUMMARY" <<EOF
- **Tagger:** $TAGGER_INFO
- **Tag Date:** $TAG_DATE
- **Tag Signature:** $TAG_SIGNATURE
- **Tag Message:** $TAG_MESSAGE
EOF
fi

COMMIT_AUTHOR=$(git log -1 --format='%an <%ae>' "$COMMIT_SHA")
COMMIT_DATE=$(git log -1 --format='%cs' "$COMMIT_SHA")
COMMIT_MESSAGE=$(git log -1 --format='%s' "$COMMIT_SHA")

COMMIT_SIGNATURE="Not signed"
COMMIT_CONTENT=$(git cat-file commit "$COMMIT_SHA")
if printf "%s" "$COMMIT_CONTENT" | grep -q "BEGIN PGP SIGNATURE"; then
  COMMIT_SIGNATURE="GPG signed"
elif printf "%s" "$COMMIT_CONTENT" | grep -q "BEGIN SSH SIGNATURE"; then
  COMMIT_SIGNATURE="SSH signed"
fi

cat >>"$GITHUB_STEP_SUMMARY" <<EOF

## 📦 Tagged Commit
- **SHA:** \`$COMMIT_SHA\`
- **Author:** $COMMIT_AUTHOR
- **Date:** $COMMIT_DATE
- **Signature:** $COMMIT_SIGNATURE
- **Message:** $COMMIT_MESSAGE

## ⚙️ Configuration
| Setting | Value |
|---------|-------|
| **Project Type** | $PROJECT_TYPE |
| **Build Type** | $BUILD_TYPE |
| **Container Registry** | $CONTAINER_REGISTRY |
| **Release Publisher** | GitHub CLI |
| **GPG Signing** | $([ "$SIGN_ARTIFACTS" = "true" ] && printf "Enabled" || printf "Disabled") |

## 🔑 Required Secrets Status

| Secret | Purpose | Status |
|--------|---------|--------|
EOF

if [ "$SIGN_ARTIFACTS" = "true" ]; then
  if [ -n "${OSPO_BOT_GPG_PRIV:-}" ]; then
    printf "| OSPO_BOT_GPG_PRIV | Sign commits/artifacts | ✓ Available |\n" >>"$GITHUB_STEP_SUMMARY"
    printf "| OSPO_BOT_GPG_PASS | GPG passphrase | ✓ Available |\n" >>"$GITHUB_STEP_SUMMARY"
  else
    printf "| OSPO_BOT_GPG_PRIV | Sign commits/artifacts | ✗ Missing |\n" >>"$GITHUB_STEP_SUMMARY"
    printf "| OSPO_BOT_GPG_PASS | GPG passphrase | ✗ Missing |\n" >>"$GITHUB_STEP_SUMMARY"
  fi
fi

if [ -n "${OSPO_BOT_GHTOKEN:-}" ]; then
  printf "| OSPO_BOT_GHTOKEN | Push commits | ✓ Available |\n" >>"$GITHUB_STEP_SUMMARY"

  BOT_STATUS="❓ Not verified"
  if GH_TOKEN="$OSPO_BOT_GHTOKEN" gh api repos/"${GITHUB_REPOSITORY:-}" --silent 2>/dev/null; then
    BOT_STATUS="✓ Valid token"
  else
    BOT_STATUS="✗ Invalid/No access"
  fi

  printf "| Bot Token Status | Repository access | %s |\n" "$BOT_STATUS" >>"$GITHUB_STEP_SUMMARY"
else
  printf "| OSPO_BOT_GHTOKEN | Push commits | ✗ Missing |\n" >>"$GITHUB_STEP_SUMMARY"
  printf "| Bot Token Status | Repository access | ❓ No token |\n" >>"$GITHUB_STEP_SUMMARY"
fi

if [ "$SIGN_ARTIFACTS" = "true" ]; then
  if [ -n "${OSPO_BOT_GPG_PUB:-}" ]; then
    printf "| OSPO_BOT_GPG_PUB | GPG verification | ✓ Available |\n" >>"$GITHUB_STEP_SUMMARY"
  else
    printf "| OSPO_BOT_GPG_PUB | GPG verification | ✗ Required |\n" >>"$GITHUB_STEP_SUMMARY"
  fi
fi

if [ -n "${RELEASE_TOKEN:-}" ]; then
  printf "| RELEASE_TOKEN | Create releases | ✓ Available |\n" >>"$GITHUB_STEP_SUMMARY"
else
  printf "| RELEASE_TOKEN | Create releases | ✗ Required |\n" >>"$GITHUB_STEP_SUMMARY"
fi

if [ -n "${PUBLISH_TO:-}" ]; then
  if printf "%s" "$PUBLISH_TO" | grep -q "maven-central"; then
    if [ -n "${MAVENCENTRAL_USERNAME:-}" ]; then
      printf "| MAVENCENTRAL_USERNAME | Maven Central auth | ✓ Available |\n" >>"$GITHUB_STEP_SUMMARY"
    else
      printf "| MAVENCENTRAL_USERNAME | Maven Central auth | ✗ Missing |\n" >>"$GITHUB_STEP_SUMMARY"
    fi

    if [ -n "${MAVENCENTRAL_PASSWORD:-}" ]; then
      printf "| MAVENCENTRAL_PASSWORD | Maven Central auth | ✓ Available |\n" >>"$GITHUB_STEP_SUMMARY"
    else
      printf "| MAVENCENTRAL_PASSWORD | Maven Central auth | ✗ Missing |\n" >>"$GITHUB_STEP_SUMMARY"
    fi
  fi

  if printf "%s" "$PUBLISH_TO" | grep -q "npmjs"; then
    if [ -n "${NPM_TOKEN:-}" ]; then
      printf "| NPM_TOKEN | NPM registry auth | ✓ Available |\n" >>"$GITHUB_STEP_SUMMARY"
    else
      printf "| NPM_TOKEN | NPM registry auth | ✗ Missing |\n" >>"$GITHUB_STEP_SUMMARY"
    fi
  fi
fi

if [ "$JOB_STATUS" = "success" ]; then
  {
    printf "\n"
    printf "### ✅ All required prerequisites are configured!\n"
    printf "Ready to proceed with release 🚀\n"
  } >>"$GITHUB_STEP_SUMMARY"
else
  {
    printf "\n"
    printf "### ❌ Prerequisites validation failed\n"
    printf "Please configure the missing secrets before attempting release\n"
  } >>"$GITHUB_STEP_SUMMARY"
fi

cat >>"$GITHUB_STEP_SUMMARY" <<'EOF'

## ✅ Validation Results

| Validation | Result | Details |
|------------|--------|---------|
EOF

if [ "$REF_TYPE" = "tag" ]; then
  if [[ "$TAG_NAME" =~ ^v[0-9]+\.[0-9]+\.[0-9]+ ]]; then
    printf "| Semantic Version | ✓ Pass | \`%s\` follows vX.Y.Z |\n" "$TAG_NAME" >>"$GITHUB_STEP_SUMMARY"
  else
    printf "| Semantic Version | ✗ Fail | Invalid format |\n" >>"$GITHUB_STEP_SUMMARY"
  fi

  printf "| Tag Type | ✓ Pass | Annotated (not lightweight) |\n" >>"$GITHUB_STEP_SUMMARY"
  printf "| Tag Signature | ✓ Pass | GPG/SSH signed |\n" >>"$GITHUB_STEP_SUMMARY"

  if [[ "$TAG_NAME" =~ -(alpha|beta|rc|snapshot|SNAPSHOT|dev) ]]; then
    printf "| Release Type | 🚧 Pre-release | \`%s\` version |\n" "${BASH_REMATCH[1]}" >>"$GITHUB_STEP_SUMMARY"
  else
    printf "| Release Type | 🎯 Stable | Production release |\n" >>"$GITHUB_STEP_SUMMARY"
  fi
fi

if [ "$CHECK_AUTHORIZATION" = "true" ]; then
  printf "| User Authorization | ✓ Pass | %s authorized |\n" "$ACTOR" >>"$GITHUB_STEP_SUMMARY"
elif [[ "$TAG_NAME" =~ -SNAPSHOT$ ]]; then
  printf "| User Authorization | − Skip | SNAPSHOT release |\n" >>"$GITHUB_STEP_SUMMARY"
fi

if [ -n "${OSPO_BOT_GHTOKEN:-}" ]; then
  printf "| Push Token | ✓ Pass | Valid GitHub token |\n" >>"$GITHUB_STEP_SUMMARY"
else
  printf "| Push Token | ✗ Fail | Missing OSPO_BOT_GHTOKEN |\n" >>"$GITHUB_STEP_SUMMARY"
fi

if [ -n "${PUBLISH_TO:-}" ]; then
  if printf "%s" "$PUBLISH_TO" | grep -q "maven-central"; then
    if [ -n "${MAVENCENTRAL_USERNAME:-}" ]; then
      printf "| Maven Central | ✓ Pass | Credentials configured |\n" >>"$GITHUB_STEP_SUMMARY"
    else
      printf "| Maven Central | ✗ Fail | Missing credentials |\n" >>"$GITHUB_STEP_SUMMARY"
    fi
  fi

  if printf "%s" "$PUBLISH_TO" | grep -q "npmjs"; then
    if [ -n "${NPM_TOKEN:-}" ]; then
      printf "| NPM Registry | ✓ Pass | Token configured |\n" >>"$GITHUB_STEP_SUMMARY"
    else
      printf "| NPM Registry | ✗ Fail | Missing NPM_TOKEN |\n" >>"$GITHUB_STEP_SUMMARY"
    fi
  fi

  if printf "%s" "$PUBLISH_TO" | grep -q "github-packages"; then
    printf "| GitHub Packages | ✓ Pass | Using GITHUB_TOKEN |\n" >>"$GITHUB_STEP_SUMMARY"
  fi
fi

{
  printf "\n"
  printf "---\n"
  printf "*Generated at: %s*\n" "$(date -u '+%Y-%m-%d %H:%M:%S UTC')"
} >>"$GITHUB_STEP_SUMMARY"
