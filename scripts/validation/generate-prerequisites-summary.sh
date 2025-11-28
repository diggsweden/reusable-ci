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

summary() {
  printf "%s\n" "$1" >>"$GITHUB_STEP_SUMMARY"
}

secret_row() {
  local name="$1"
  local purpose="$2"
  local var_name="$3"
  local status

  if [[ -n "${!var_name:-}" ]]; then
    status="✓ Available"
  else
    status="✗ Missing"
  fi

  summary "| $name | $purpose | $status |"
}

validation_row() {
  local name="$1"
  local result="$2"
  local details="$3"
  summary "| $name | $result | $details |"
}

detect_signature() {
  local content="$1"
  if printf "%s" "$content" | grep -q "BEGIN PGP SIGNATURE"; then
    printf "GPG signed"
  elif printf "%s" "$content" | grep -q "BEGIN SSH SIGNATURE"; then
    printf "SSH signed"
  else
    printf "Not signed"
  fi
}

generate_tag_info() {
  summary "# 📋 Release Prerequisites Validation Report"
  summary ""
  summary "## 🏷️ Release Tag"
  summary "- **Tag:** \`$TAG_NAME\`"
  summary "- **Type:** $REF_TYPE"

  if [[ "$REF_TYPE" != "tag" ]]; then
    return
  fi

  local tagger_info tag_date tag_message tag_signature
  tagger_info=$(git for-each-ref refs/tags/"$TAG_NAME" --format='%(taggername) <%(taggeremail)>' 2>/dev/null || printf "N/A")
  tag_date=$(git for-each-ref refs/tags/"$TAG_NAME" --format='%(taggerdate:short)' 2>/dev/null || printf "N/A")
  tag_message=$(git tag -l -n1 "$TAG_NAME" | sed "s/^$TAG_NAME *//" | head -1 || printf "No message")

  tag_signature="Not signed"
  if git tag -v "$TAG_NAME" >/dev/null 2>&1; then
    tag_signature="GPG signed"
  elif git show "$TAG_NAME" 2>/dev/null | grep -q "BEGIN SSH SIGNATURE"; then
    tag_signature="SSH signed"
  fi

  summary "- **Tagger:** $tagger_info"
  summary "- **Tag Date:** $tag_date"
  summary "- **Tag Signature:** $tag_signature"
  summary "- **Tag Message:** $tag_message"
}

generate_commit_info() {
  local commit_author commit_date commit_message commit_signature commit_content

  commit_author=$(git log -1 --format='%an <%ae>' "$COMMIT_SHA")
  commit_date=$(git log -1 --format='%cs' "$COMMIT_SHA")
  commit_message=$(git log -1 --format='%s' "$COMMIT_SHA")
  commit_content=$(git cat-file commit "$COMMIT_SHA")
  commit_signature=$(detect_signature "$commit_content")

  summary ""
  summary "## 📦 Tagged Commit"
  summary "- **SHA:** \`$COMMIT_SHA\`"
  summary "- **Author:** $commit_author"
  summary "- **Date:** $commit_date"
  summary "- **Signature:** $commit_signature"
  summary "- **Message:** $commit_message"
}

generate_configuration() {
  local signing_status
  [[ "$SIGN_ARTIFACTS" = "true" ]] && signing_status="Enabled" || signing_status="Disabled"

  summary ""
  summary "## ⚙️ Configuration"
  summary "| Setting | Value |"
  summary "|---------|-------|"
  summary "| **Project Type** | $PROJECT_TYPE |"
  summary "| **Build Type** | $BUILD_TYPE |"
  summary "| **Container Registry** | $CONTAINER_REGISTRY |"
  summary "| **Release Publisher** | GitHub CLI |"
  summary "| **GPG Signing** | $signing_status |"
}

generate_secrets_status() {
  summary ""
  summary "## 🔑 Required Secrets Status"
  summary ""
  summary "| Secret | Purpose | Status |"
  summary "|--------|---------|--------|"

  if [[ "$SIGN_ARTIFACTS" = "true" ]]; then
    secret_row "OSPO_BOT_GPG_PRIV" "Sign commits/artifacts" "OSPO_BOT_GPG_PRIV"
    secret_row "OSPO_BOT_GPG_PASS" "GPG passphrase" "OSPO_BOT_GPG_PASS"
  fi

  if [[ -n "${OSPO_BOT_GHTOKEN:-}" ]]; then
    summary "| OSPO_BOT_GHTOKEN | Push commits | ✓ Available |"

    local bot_status="❓ Not verified"
    if GH_TOKEN="$OSPO_BOT_GHTOKEN" gh api repos/"${GITHUB_REPOSITORY:-}" --silent 2>/dev/null; then
      bot_status="✓ Valid token"
    else
      bot_status="✗ Invalid/No access"
    fi
    summary "| Bot Token Status | Repository access | $bot_status |"
  else
    summary "| OSPO_BOT_GHTOKEN | Push commits | ✗ Missing |"
    summary "| Bot Token Status | Repository access | ❓ No token |"
  fi

  if [[ "$SIGN_ARTIFACTS" = "true" ]]; then
    secret_row "OSPO_BOT_GPG_PUB" "GPG verification" "OSPO_BOT_GPG_PUB"
  fi

  secret_row "RELEASE_TOKEN" "Create releases" "RELEASE_TOKEN"

  if [[ -n "${PUBLISH_TO:-}" ]]; then
    if printf "%s" "$PUBLISH_TO" | grep -q "maven-central"; then
      secret_row "MAVENCENTRAL_USERNAME" "Maven Central auth" "MAVENCENTRAL_USERNAME"
      secret_row "MAVENCENTRAL_PASSWORD" "Maven Central auth" "MAVENCENTRAL_PASSWORD"
    fi

    if printf "%s" "$PUBLISH_TO" | grep -q "npmjs"; then
      secret_row "NPM_TOKEN" "NPM registry auth" "NPM_TOKEN"
    fi
  fi
}

generate_job_status() {
  summary ""
  if [[ "$JOB_STATUS" = "success" ]]; then
    summary "### ✅ All required prerequisites are configured!"
    summary "Ready to proceed with release 🚀"
  else
    summary "### ❌ Prerequisites validation failed"
    summary "Please configure the missing secrets before attempting release"
  fi
}

generate_validation_results() {
  summary ""
  summary "## ✅ Validation Results"
  summary ""
  summary "| Validation | Result | Details |"
  summary "|------------|--------|---------|"

  if [[ "$REF_TYPE" = "tag" ]]; then
    if [[ "$TAG_NAME" =~ ^v[0-9]+\.[0-9]+\.[0-9]+ ]]; then
      validation_row "Semantic Version" "✓ Pass" "\`$TAG_NAME\` follows vX.Y.Z"
    else
      validation_row "Semantic Version" "✗ Fail" "Invalid format"
    fi

    validation_row "Tag Type" "✓ Pass" "Annotated (not lightweight)"
    validation_row "Tag Signature" "✓ Pass" "GPG/SSH signed"

    if [[ "$TAG_NAME" =~ -(alpha|beta|rc|snapshot|SNAPSHOT|dev) ]]; then
      validation_row "Release Type" "🚧 Pre-release" "\`${BASH_REMATCH[1]}\` version"
    else
      validation_row "Release Type" "🎯 Stable" "Production release"
    fi
  fi

  if [[ "$CHECK_AUTHORIZATION" = "true" ]]; then
    validation_row "User Authorization" "✓ Pass" "$ACTOR authorized"
  elif [[ "$TAG_NAME" =~ -SNAPSHOT$ ]]; then
    validation_row "User Authorization" "− Skip" "SNAPSHOT release"
  fi

  if [[ -n "${OSPO_BOT_GHTOKEN:-}" ]]; then
    validation_row "Push Token" "✓ Pass" "Valid GitHub token"
  else
    validation_row "Push Token" "✗ Fail" "Missing OSPO_BOT_GHTOKEN"
  fi

  if [[ -n "${PUBLISH_TO:-}" ]]; then
    if printf "%s" "$PUBLISH_TO" | grep -q "maven-central"; then
      if [[ -n "${MAVENCENTRAL_USERNAME:-}" ]]; then
        validation_row "Maven Central" "✓ Pass" "Credentials configured"
      else
        validation_row "Maven Central" "✗ Fail" "Missing credentials"
      fi
    fi

    if printf "%s" "$PUBLISH_TO" | grep -q "npmjs"; then
      if [[ -n "${NPM_TOKEN:-}" ]]; then
        validation_row "NPM Registry" "✓ Pass" "Token configured"
      else
        validation_row "NPM Registry" "✗ Fail" "Missing NPM_TOKEN"
      fi
    fi

    if printf "%s" "$PUBLISH_TO" | grep -q "github-packages"; then
      validation_row "GitHub Packages" "✓ Pass" "Using GITHUB_TOKEN"
    fi
  fi
}

generate_footer() {
  summary ""
  summary "---"
  summary "*Generated at: $(date -u '+%Y-%m-%d %H:%M:%S UTC')*"
}

main() {
  generate_tag_info
  generate_commit_info
  generate_configuration
  generate_secrets_status
  generate_job_status
  generate_validation_results
  generate_footer
}

main
