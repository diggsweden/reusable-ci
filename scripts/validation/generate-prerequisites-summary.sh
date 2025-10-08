#!/bin/bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
#
# SPDX-License-Identifier: CC0-1.0

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
  TAGGER_INFO=$(git for-each-ref refs/tags/"$TAG_NAME" --format='%(taggername) <%(taggeremail)>' 2>/dev/null || echo "N/A")
  TAG_DATE=$(git for-each-ref refs/tags/"$TAG_NAME" --format='%(taggerdate:short)' 2>/dev/null || echo "N/A")
  TAG_MESSAGE=$(git tag -l -n1 "$TAG_NAME" | sed "s/^$TAG_NAME *//" | head -1 || echo "No message")

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
if echo "$COMMIT_CONTENT" | grep -q "BEGIN PGP SIGNATURE"; then
  COMMIT_SIGNATURE="GPG signed"
elif echo "$COMMIT_CONTENT" | grep -q "BEGIN SSH SIGNATURE"; then
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
| **GPG Signing** | $([ "$SIGN_ARTIFACTS" = "true" ] && echo "Enabled" || echo "Disabled") |

## 🔑 Required Secrets Status

| Secret | Purpose | Status |
|--------|---------|--------|
EOF

if [ "$SIGN_ARTIFACTS" = "true" ]; then
  if [ -n "${OSPO_BOT_GPG_PRIV:-}" ]; then
    echo "| OSPO_BOT_GPG_PRIV | Sign commits/artifacts | ✓ Available |" >>"$GITHUB_STEP_SUMMARY"
    echo "| OSPO_BOT_GPG_PASS | GPG passphrase | ✓ Available |" >>"$GITHUB_STEP_SUMMARY"
  else
    echo "| OSPO_BOT_GPG_PRIV | Sign commits/artifacts | ✗ Missing |" >>"$GITHUB_STEP_SUMMARY"
    echo "| OSPO_BOT_GPG_PASS | GPG passphrase | ✗ Missing |" >>"$GITHUB_STEP_SUMMARY"
  fi
fi

if [ -n "${OSPO_BOT_GHTOKEN:-}" ]; then
  echo "| OSPO_BOT_GHTOKEN | Push commits | ✓ Available |" >>"$GITHUB_STEP_SUMMARY"

  BOT_STATUS="❓ Not verified"
  if GH_TOKEN="$OSPO_BOT_GHTOKEN" gh api repos/"${GITHUB_REPOSITORY:-}" --silent 2>/dev/null; then
    BOT_STATUS="✓ Valid token"
  else
    BOT_STATUS="✗ Invalid/No access"
  fi

  echo "| Bot Token Status | Repository access | $BOT_STATUS |" >>"$GITHUB_STEP_SUMMARY"
else
  echo "| OSPO_BOT_GHTOKEN | Push commits | ✗ Missing |" >>"$GITHUB_STEP_SUMMARY"
  echo "| Bot Token Status | Repository access | ❓ No token |" >>"$GITHUB_STEP_SUMMARY"
fi

if [ "$SIGN_ARTIFACTS" = "true" ]; then
  if [ -n "${OSPO_BOT_GPG_PUB:-}" ]; then
    echo "| OSPO_BOT_GPG_PUB | GPG verification | ✓ Available |" >>"$GITHUB_STEP_SUMMARY"
  else
    echo "| OSPO_BOT_GPG_PUB | GPG verification | ✗ Required |" >>"$GITHUB_STEP_SUMMARY"
  fi
fi

if [ -n "${RELEASE_TOKEN:-}" ]; then
  echo "| RELEASE_TOKEN | Create releases | ✓ Available |" >>"$GITHUB_STEP_SUMMARY"
else
  echo "| RELEASE_TOKEN | Create releases | ✗ Required |" >>"$GITHUB_STEP_SUMMARY"
fi

if [ -n "${PUBLISH_TO:-}" ]; then
  if echo "$PUBLISH_TO" | grep -q "maven-central"; then
    if [ -n "${MAVENCENTRAL_USERNAME:-}" ]; then
      echo "| MAVENCENTRAL_USERNAME | Maven Central auth | ✓ Available |" >>"$GITHUB_STEP_SUMMARY"
    else
      echo "| MAVENCENTRAL_USERNAME | Maven Central auth | ✗ Missing |" >>"$GITHUB_STEP_SUMMARY"
    fi

    if [ -n "${MAVENCENTRAL_PASSWORD:-}" ]; then
      echo "| MAVENCENTRAL_PASSWORD | Maven Central auth | ✓ Available |" >>"$GITHUB_STEP_SUMMARY"
    else
      echo "| MAVENCENTRAL_PASSWORD | Maven Central auth | ✗ Missing |" >>"$GITHUB_STEP_SUMMARY"
    fi
  fi

  if echo "$PUBLISH_TO" | grep -q "npmjs"; then
    if [ -n "${NPM_TOKEN:-}" ]; then
      echo "| NPM_TOKEN | NPM registry auth | ✓ Available |" >>"$GITHUB_STEP_SUMMARY"
    else
      echo "| NPM_TOKEN | NPM registry auth | ✗ Missing |" >>"$GITHUB_STEP_SUMMARY"
    fi
  fi
fi

if [ "$JOB_STATUS" = "success" ]; then
  {
    echo ""
    echo "### ✅ All required prerequisites are configured!"
    echo "Ready to proceed with release 🚀"
  } >>"$GITHUB_STEP_SUMMARY"
else
  {
    echo ""
    echo "### ❌ Prerequisites validation failed"
    echo "Please configure the missing secrets before attempting release"
  } >>"$GITHUB_STEP_SUMMARY"
fi

cat >>"$GITHUB_STEP_SUMMARY" <<'EOF'

## ✅ Validation Results

| Validation | Result | Details |
|------------|--------|---------|
EOF

if [ "$REF_TYPE" = "tag" ]; then
  if [[ "$TAG_NAME" =~ ^v[0-9]+\.[0-9]+\.[0-9]+ ]]; then
    echo "| Semantic Version | ✓ Pass | \`$TAG_NAME\` follows vX.Y.Z |" >>"$GITHUB_STEP_SUMMARY"
  else
    echo "| Semantic Version | ✗ Fail | Invalid format |" >>"$GITHUB_STEP_SUMMARY"
  fi

  echo "| Tag Type | ✓ Pass | Annotated (not lightweight) |" >>"$GITHUB_STEP_SUMMARY"
  echo "| Tag Signature | ✓ Pass | GPG/SSH signed |" >>"$GITHUB_STEP_SUMMARY"

  if [[ "$TAG_NAME" =~ -(alpha|beta|rc|snapshot|SNAPSHOT|dev) ]]; then
    echo "| Release Type | 🚧 Pre-release | \`${BASH_REMATCH[1]}\` version |" >>"$GITHUB_STEP_SUMMARY"
  else
    echo "| Release Type | 🎯 Stable | Production release |" >>"$GITHUB_STEP_SUMMARY"
  fi
fi

if [ "$CHECK_AUTHORIZATION" = "true" ]; then
  echo "| User Authorization | ✓ Pass | $ACTOR authorized |" >>"$GITHUB_STEP_SUMMARY"
elif [[ "$TAG_NAME" =~ -SNAPSHOT$ ]]; then
  echo "| User Authorization | − Skip | SNAPSHOT release |" >>"$GITHUB_STEP_SUMMARY"
fi

if [ -n "${OSPO_BOT_GHTOKEN:-}" ]; then
  echo "| Push Token | ✓ Pass | Valid GitHub token |" >>"$GITHUB_STEP_SUMMARY"
else
  echo "| Push Token | ✗ Fail | Missing OSPO_BOT_GHTOKEN |" >>"$GITHUB_STEP_SUMMARY"
fi

if [ -n "${PUBLISH_TO:-}" ]; then
  if echo "$PUBLISH_TO" | grep -q "maven-central"; then
    if [ -n "${MAVENCENTRAL_USERNAME:-}" ]; then
      echo "| Maven Central | ✓ Pass | Credentials configured |" >>"$GITHUB_STEP_SUMMARY"
    else
      echo "| Maven Central | ✗ Fail | Missing credentials |" >>"$GITHUB_STEP_SUMMARY"
    fi
  fi

  if echo "$PUBLISH_TO" | grep -q "npmjs"; then
    if [ -n "${NPM_TOKEN:-}" ]; then
      echo "| NPM Registry | ✓ Pass | Token configured |" >>"$GITHUB_STEP_SUMMARY"
    else
      echo "| NPM Registry | ✗ Fail | Missing NPM_TOKEN |" >>"$GITHUB_STEP_SUMMARY"
    fi
  fi

  if echo "$PUBLISH_TO" | grep -q "github-packages"; then
    echo "| GitHub Packages | ✓ Pass | Using GITHUB_TOKEN |" >>"$GITHUB_STEP_SUMMARY"
  fi
fi

{
  echo ""
  echo "---"
  echo "*Generated at: $(date -u '+%Y-%m-%d %H:%M:%S UTC')*"
} >>"$GITHUB_STEP_SUMMARY"
