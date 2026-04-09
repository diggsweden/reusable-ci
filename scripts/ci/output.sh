#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Shared CI output helpers
#
# Usage: source this file from other scripts
#   source "$(dirname "$0")/../ci/output.sh"
#
# Functions:
#   ci_output <key> <value>       Write key=value to workflow outputs
#   ci_output_file                Return the output file path
#   ci_output_multiline <key>     Write multiline value from stdin to workflow outputs
#   ci_summary                    Append stdin to step summary
#   ci_summary_file               Return the summary file path
#   ci_json_bool <value>          Print "true" or "false" for JSON booleans
#   ci_log_error <message>        Log an error (platform-aware)
#   ci_log_warning <message>      Log a warning (platform-aware)
#   ci_status_icon <result>       Print status icon (✓/−/✗) for CI results
#   ci_normalize_result <value>   Normalize result to known status (success/failure/cancelled/skipped)
#   ci_bool_status <value>        Print "✓ Enabled" or "⊘ Disabled"
#   ci_test_status <skip>         Print test status markdown list item
#   ci_is_semver_tag <ref>         Check if ref matches semantic version format
#   ci_is_snapshot <ref>           Check if ref is a SNAPSHOT (case-insensitive)
#   ci_is_prerelease <ref>         Check if ref has a prerelease suffix
#   ci_find_release_artifacts [d]  Find release artifacts by standard extensions
#   ci_sbom_zip_name <name> <ver>  Generate canonical SBOM ZIP filename
#   ci_gpg_sign <key-id> <file>    GPG detach-sign a file (produces .asc)
#   ci_json_value <json> <key>    Extract a string value from compact JSON
#   ci_release_url <version>      Build platform-aware release URL
#   ci_packages_url [repo]        Build platform-aware packages URL
#   ci_docs_url <repo> <path>     Build platform-aware docs/blob URL
#
# Constants:
#   CI_SEMVER_TAG_REGEX            Canonical semver tag regex (with capture groups)
#   CI_PRERELEASE_TAG_REGEX        Prerelease detection regex for full tag names
#   CI_PRERELEASE_SUFFIX_REGEX     Prerelease suffix validation regex
#   CI_CHECKSUMS_FILE              Canonical checksums filename

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "$SCRIPT_DIR/env.sh"

# Write a key=value pair to workflow outputs
ci_output() {
  local key="$1"
  local value="$2"
  local output_file
  output_file="$(ci_output_file)"

  if [[ "$output_file" != "/dev/null" ]]; then
    printf '%s=%s\n' "$key" "$value" >>"$output_file"
  fi
}

# Return the output file path (for scripts that redirect stdout in bulk)
ci_output_file() {
  printf '%s' "${CI_OUTPUT:-/dev/null}"
}

# Write a multiline value to workflow outputs (reads from stdin)
ci_output_multiline() {
  local key="$1"
  local output_file
  output_file="$(ci_output_file)"
  local delimiter="EOF_${RANDOM}_${RANDOM}"

  if [[ "$output_file" != "/dev/null" ]]; then
    printf '%s<<%s\n' "$key" "$delimiter" >>"$output_file"
    cat >>"$output_file"
    printf '%s\n' "$delimiter" >>"$output_file"
  fi
}

# Append content to step summary (reads from stdin)
ci_summary() {
  local summary_file
  summary_file="$(ci_summary_file)"
  cat >>"$summary_file"
}

# Return the summary file path (for scripts that use >> directly)
ci_summary_file() {
  printf '%s' "${CI_SUMMARY:-/dev/null}"
}

# Print JSON boolean: "true" or "false"
ci_json_bool() {
  if [[ "$1" == "true" ]]; then printf 'true'; else printf 'false'; fi
}

# Log an error message (platform-aware annotation)
ci_log_error() {
  if [[ "${CI_PLATFORM:-local}" == "github" ]]; then
    printf '::error::%s\n' "$1"
  else
    printf 'ERROR: %s\n' "$1" >&2
  fi
}

# Log a warning message (platform-aware annotation)
ci_log_warning() {
  if [[ "${CI_PLATFORM:-local}" == "github" ]]; then
    printf '::warning::%s\n' "$1"
  else
    printf 'WARNING: %s\n' "$1" >&2
  fi
}

# Print a status icon for CI result values
# success → ✓, skipped → −, anything else → ✗
ci_status_icon() {
  case "$1" in
  success) printf '✓' ;;
  skipped) printf '−' ;;
  *) printf '✗' ;;
  esac
}

# Normalize a CI result value to a known status
# Unknown values default to "skipped"
ci_normalize_result() {
  case "${1:-skipped}" in
  success | failure | cancelled | skipped)
    printf '%s' "$1"
    ;;
  *)
    printf 'skipped'
    ;;
  esac
}

# Print boolean-dependent status label: "✓ Enabled" or "⊘ Disabled"
ci_bool_status() {
  if [[ "$1" == "true" ]]; then printf '✓ Enabled'; else printf '⊘ Disabled'; fi
}

# Print test execution status as a markdown list item
ci_test_status() {
  if [[ "$1" == "true" ]]; then
    printf '%s **Tests:** %s\n' '-' '⊘ Skipped'
  else
    printf '%s **Tests:** %s\n' '-' '✓ Executed'
  fi
}

# Canonical semantic version tag regex (requires v prefix)
# Capture groups: 1=MAJOR, 2=MINOR, 3=PATCH, 4=full-prerelease, 5=prerelease-id
CI_SEMVER_TAG_REGEX='^v([0-9]+)\.([0-9]+)\.([0-9]+)(-([a-zA-Z0-9.\-]+))?$'

# Check if a ref name matches semantic version tag format (vMAJOR.MINOR.PATCH[-prerelease])
ci_is_semver_tag() {
  [[ "$1" =~ $CI_SEMVER_TAG_REGEX ]]
}

# Check if a ref name is a SNAPSHOT release (case-insensitive)
ci_is_snapshot() {
  [[ "${1,,}" == *-snapshot ]]
}

# Prerelease detection regex (matches prerelease suffix in full tag names)
# Identifiers: alpha, beta, rc, dev, snapshot/SNAPSHOT
CI_PRERELEASE_TAG_REGEX='-(alpha|beta|rc|dev|snapshot|SNAPSHOT)'

# Prerelease suffix validation regex (validates extracted prerelease identifier)
# Allows optional numeric suffix: alpha.1, beta.2, rc.3
CI_PRERELEASE_SUFFIX_REGEX='^(alpha|beta|rc|snapshot|SNAPSHOT|dev)(\.[0-9]+)?$'

# Check if a tag name indicates a prerelease version
# Sets BASH_REMATCH[1] to the matched identifier (alpha, beta, etc.)
ci_is_prerelease() {
  [[ "$1" =~ $CI_PRERELEASE_TAG_REGEX ]]
}

# Find release artifacts by standard extensions, excluding build intermediates
# Outputs null-delimited paths suitable for: while read -r -d '' file; do
ci_find_release_artifacts() {
  local dir="${1:-./release-artifacts}"
  find "$dir" -type f \
    \( -name "*.jar" -o -name "*.tgz" -o -name "*.tar.gz" -o -name "*.zip" -o -name "*.war" \) \
    ! -name "original-*.jar" -print0
}

# Generate the canonical SBOM ZIP filename
ci_sbom_zip_name() {
  local project_name="$1"
  local version="$2"
  printf '%s-%s-sboms.zip' "$project_name" "$version"
}

# Canonical checksums filename
CI_CHECKSUMS_FILE="checksums.sha256"

# GPG detach-sign a file, producing an armored .asc signature
ci_gpg_sign() {
  local key_id="$1"
  local file="$2"
  gpg --armor --detach-sign --default-key "$key_id" "$file"
}

# Extract a string value from compact JSON by key name.
# Searches for "key":"value" anywhere in the JSON string.
# Prints "skipped" if key not found or json is empty.
ci_json_value() {
  local json="$1" key="$2"
  if [[ -z "$json" ]]; then
    printf 'skipped'
    return
  fi
  local rest
  rest="${json#*\""$key"\":\"}"
  if [[ "$rest" == "$json" ]]; then
    printf 'skipped'
    return
  fi
  printf '%s' "${rest%%\"*}"
}

# Build a platform-aware URL to a release page
# Requires CI_PLATFORM, CI_SERVER_URL, CI_REPO from env.sh
ci_release_url() {
  local version="$1"
  case "${CI_PLATFORM:-}" in
  github) printf '%s/%s/releases/tag/%s' "$CI_SERVER_URL" "$CI_REPO" "$version" ;;
  gitlab) printf '%s/%s/-/releases/%s' "$CI_SERVER_URL" "$CI_REPO" "$version" ;;
  *) printf '(release: %s)' "$version" ;;
  esac
}

# Build a platform-aware URL to the packages page
# Requires CI_PLATFORM, CI_SERVER_URL from env.sh
ci_packages_url() {
  local repo="${1:-$CI_REPO}"
  case "${CI_PLATFORM:-}" in
  github) printf '%s/%s/packages' "$CI_SERVER_URL" "$repo" ;;
  gitlab) printf '%s/%s/-/packages' "$CI_SERVER_URL" "$repo" ;;
  *) printf '(packages)' ;;
  esac
}

# Build a platform-aware URL to a file in the default branch
# Requires CI_PLATFORM, CI_SERVER_URL from env.sh
ci_docs_url() {
  local repo="$1"
  local path="$2"
  case "${CI_PLATFORM:-}" in
  github) printf '%s/%s/blob/main/%s' "$CI_SERVER_URL" "$repo" "$path" ;;
  gitlab) printf '%s/%s/-/blob/main/%s' "$CI_SERVER_URL" "$repo" "$path" ;;
  *) printf '(docs: %s)' "$path" ;;
  esac
}
