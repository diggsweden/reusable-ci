#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 The Reusable CI Authors
# SPDX-License-Identifier: CC0-1.0

# Generate Development Release Summary
#
# Purpose: Creates a GitHub Actions job summary for dev releases showing
# published artifacts (container images, NPM packages) with usage instructions.
#
# Usage:
#   generate-dev-summary.sh \
#     <project-type> \
#     <branch> \
#     <commit-sha> \
#     <actor> \
#     <repository> \
#     <container-image> \
#     <container-status> \
#     <npm-package-name> \
#     <npm-package-version> \
#     <npm-status>
#
# Arguments:
#   project-type: Project type (maven, npm, gradle)
#   branch: Branch name
#   commit-sha: Commit SHA
#   actor: User who triggered the workflow
#   repository: GitHub repository (org/repo)
#   container-image: Full container image URL (empty if not built)
#   container-status: Container job status (success, failure, skipped)
#   npm-package-name: NPM package name (empty if not published)
#   npm-package-version: NPM package version (empty if not published)
#   npm-status: NPM publish job status (success, failure, skipped)

set -euo pipefail

PROJECT_TYPE="${1:-unknown}"
BRANCH="${2:-unknown}"
COMMIT_SHA="${3:-unknown}"
ACTOR="${4:-unknown}"
REPOSITORY="${5:-unknown}"
CONTAINER_IMAGE="${6:-}"
CONTAINER_STATUS="${7:-skipped}"
NPM_PACKAGE_NAME="${8:-}"
NPM_PACKAGE_VERSION="${9:-}"
NPM_STATUS="${10:-skipped}"

SHORT_SHA="${COMMIT_SHA:0:7}"
BUILD_TIME=$(date -u '+%Y-%m-%d %H:%M:%S UTC')

# Status icons
get_status_icon() {
  case "$1" in
  success) printf "✓" ;;
  failure) printf "✗" ;;
  skipped) printf "−" ;;
  *) printf "?" ;;
  esac
}

CONTAINER_ICON=$(get_status_icon "$CONTAINER_STATUS")
NPM_ICON=$(get_status_icon "$NPM_STATUS")

printf "================================================\n"
printf "Generating Dev Release Summary\n"
printf "================================================\n"
printf "Project Type: %s\n" "$PROJECT_TYPE"
printf "Branch: %s\n" "$BRANCH"
printf "Commit: %s\n" "$SHORT_SHA"
printf "Container Image: %s\n" "${CONTAINER_IMAGE:-none}"
printf "NPM Package: %s@%s\n" "${NPM_PACKAGE_NAME:-none}" "${NPM_PACKAGE_VERSION:-none}"
printf "\n"

# Generate summary
cat >>"$GITHUB_STEP_SUMMARY" <<EOF
# Dev Release Summary

## Build Information
| Property | Value |
|----------|-------|
| **Project Type** | \`$PROJECT_TYPE\` |
| **Branch** | \`$BRANCH\` |
| **Commit** | \`$SHORT_SHA\` |
| **Built By** | @$ACTOR |
| **Built At** | $BUILD_TIME |

## Job Status
| Job | Status |
|-----|--------|
| Build Container | $CONTAINER_ICON |
EOF

# Add NPM status if NPM project
if [ "$PROJECT_TYPE" = "npm" ]; then
  cat >>"$GITHUB_STEP_SUMMARY" <<EOF
| Publish NPM Package | $NPM_ICON |
EOF
fi

# Close job status table
cat >>"$GITHUB_STEP_SUMMARY" <<EOF

## Published Artifacts
EOF

# Container artifact
if [ -n "$CONTAINER_IMAGE" ] && [ "$CONTAINER_STATUS" = "success" ]; then
  cat >>"$GITHUB_STEP_SUMMARY" <<EOF

### 🐳 Container Image
\`\`\`
$CONTAINER_IMAGE
\`\`\`

**Pull and run:**
\`\`\`bash
docker pull $CONTAINER_IMAGE
docker run $CONTAINER_IMAGE
\`\`\`
EOF
else
  cat >>"$GITHUB_STEP_SUMMARY" <<EOF

### 🐳 Container Image
✗ Not published
EOF
fi

# NPM artifact
if [ -n "$NPM_PACKAGE_NAME" ] && [ -n "$NPM_PACKAGE_VERSION" ] && [ "$NPM_STATUS" = "success" ]; then
  cat >>"$GITHUB_STEP_SUMMARY" <<EOF

### 📦 NPM Package
\`\`\`
$NPM_PACKAGE_NAME@$NPM_PACKAGE_VERSION
\`\`\`

**Install specific version:**
\`\`\`bash
npm install $NPM_PACKAGE_NAME@$NPM_PACKAGE_VERSION
\`\`\`

**Install latest dev version:**
\`\`\`bash
npm install $NPM_PACKAGE_NAME@dev
\`\`\`
EOF
elif [ "$PROJECT_TYPE" = "npm" ]; then
  cat >>"$GITHUB_STEP_SUMMARY" <<EOF

### 📦 NPM Package
✗ Not published
EOF
fi

# Resources section
cat >>"$GITHUB_STEP_SUMMARY" <<EOF

## Resources
- [Packages](https://github.com/$REPOSITORY/packages)
- [Workflow Run](https://github.com/$REPOSITORY/actions/runs/$GITHUB_RUN_ID)

---
**Note:** These are development artifacts tagged with \`dev\`. Not for production use.
EOF

printf "✓ Dev release summary generated successfully\n\n"
