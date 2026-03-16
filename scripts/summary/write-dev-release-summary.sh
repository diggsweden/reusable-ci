#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"
source "$SCRIPT_DIR/../ci/env.sh"

main() {
  local build_time short_sha container_status npm_status container_icon npm_icon
  local container_image npm_package_name npm_package_version

  build_time=$(date -u '+%Y-%m-%d %H:%M:%S UTC')
  short_sha="${RELEASE_SHA:0:7}"
  container_status="$(ci_json_value "$PUBLISH_STAGE_RESULT_JSON" container)"
  npm_status="$(ci_json_value "$PUBLISH_STAGE_RESULT_JSON" npm)"
  container_image="$(ci_json_value "$DEV_ARTIFACTS_JSON" container_image)"
  npm_package_name="$(ci_json_value "$DEV_ARTIFACTS_JSON" npm_package_name)"
  npm_package_version="$(ci_json_value "$DEV_ARTIFACTS_JSON" npm_package_version)"
  container_icon="$(ci_status_icon "$container_status")"
  npm_icon="$(ci_status_icon "$npm_status")"

  printf '================================================\n'
  printf 'Generating Dev Release Summary\n'
  printf '================================================\n'
  printf 'Project Type: %s\n' "$PROJECT_TYPE"
  printf 'Branch: %s\n' "$RELEASE_REF"
  printf 'Commit: %s\n' "$short_sha"
  printf 'Container Image: %s\n' "${container_image:-none}"
  printf 'NPM Package: %s@%s\n\n' "${npm_package_name:-none}" "${npm_package_version:-none}"

  cat >>"$(ci_summary_file)" <<EOF
# Dev Release Summary

## Build Information
| Property | Value |
|----------|-------|
| **Project Type** | \`$PROJECT_TYPE\` |
| **Branch** | \`$RELEASE_REF\` |
| **Commit** | \`$short_sha\` |
| **Built By** | @$RELEASE_ACTOR |
| **Built At** | $build_time |

## Job Status
| Job | Status |
|-----|--------|
| Build Container | $container_icon |
EOF

  if [[ "$PROJECT_TYPE" == 'npm' ]]; then
    cat >>"$(ci_summary_file)" <<EOF
| Publish NPM Package | $npm_icon |
EOF
  fi

  cat >>"$(ci_summary_file)" <<EOF

## Published Artifacts
EOF

  if [[ -n "$container_image" && "$container_status" == 'success' ]]; then
    cat >>"$(ci_summary_file)" <<EOF

### Container Image
\`\`\`
$container_image
\`\`\`

\`\`\`bash
docker pull $container_image
docker run $container_image
\`\`\`
EOF
  else
    cat >>"$(ci_summary_file)" <<EOF

### Container Image
Not published
EOF
  fi

  if [[ "$PROJECT_TYPE" == 'npm' ]]; then
    if [[ -n "$npm_package_name" && -n "$npm_package_version" && "$npm_status" == 'success' ]]; then
      cat >>"$(ci_summary_file)" <<EOF

### NPM Package
\`\`\`
$npm_package_name@$npm_package_version
\`\`\`

\`\`\`bash
npm install $npm_package_name@$npm_package_version
npm install $npm_package_name@dev
\`\`\`
EOF
    else
      cat >>"$(ci_summary_file)" <<EOF

### NPM Package
Not published
EOF
    fi
  fi

  cat >>"$(ci_summary_file)" <<EOF

## Resources
- [Packages]($CI_SERVER_URL/$RELEASE_REPOSITORY/packages)
- [Workflow Run]($CI_SERVER_URL/$RELEASE_REPOSITORY/actions/runs/$CI_RUN_ID)

These are development artifacts tagged with \`dev\` and are not intended for production use.
EOF

  printf '✓ Dev release summary generated successfully\n'
}

main "$@"
