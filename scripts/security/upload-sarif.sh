#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Upload a SARIF file to GitHub Code Scanning via the REST API.
#
# Uses a dedicated token (GitHub App or fine-grained PAT with
# code_scanning_alerts:write) instead of the workflow GITHUB_TOKEN,
# so workflows never need security-events:write — satisfying
# OpenSSF Scorecard Token-Permissions.
#
# The API requires SARIF content to be gzipped and base64-encoded.
# See: https://docs.github.com/en/rest/code-scanning/code-scanning#upload-an-analysis-as-sarif-data
#
# Usage: bash upload-sarif.sh
#
# Environment variables (required):
#   SARIF_FILE            Path to the SARIF file to upload
#   SARIF_UPLOAD_TOKEN    Token with code_scanning_alerts:write (skips gracefully if empty)
#   GITHUB_REPOSITORY     owner/repo (set automatically by GitHub Actions)
#   GITHUB_SHA            Commit SHA (set automatically by GitHub Actions)
#   GITHUB_REF            Full git ref, e.g. refs/heads/main (set automatically by GitHub Actions)
#
# Environment variables (optional):
#   SARIF_CATEGORY        Category label shown in the Code Scanning tab (e.g. "dependency-review")
#   GITHUB_API_URL        API base URL (default: https://api.github.com)

set -euo pipefail

# =============================================================================
# Platform-aware logging (self-contained — no dependency on ci/output.sh)
# =============================================================================

_log_notice() {
  if [[ "${GITHUB_ACTIONS:-}" == "true" ]]; then
    printf '::notice::%s\n' "$1"
  else
    printf 'NOTICE: %s\n' "$1"
  fi
}

_log_error() {
  if [[ "${GITHUB_ACTIONS:-}" == "true" ]]; then
    printf '::error::%s\n' "$1"
  else
    printf 'ERROR: %s\n' "$1" >&2
  fi
}

# =============================================================================
# Validation
# =============================================================================

if [[ -z "${SARIF_UPLOAD_TOKEN:-}" ]]; then
  _log_notice "SARIF upload to Code Scanning skipped — SARIF_UPLOAD_TOKEN secret is not configured"
  exit 0
fi

if [[ -z "${SARIF_FILE:-}" ]]; then
  _log_error "SARIF_FILE environment variable is required"
  exit 1
fi

if [[ ! -f "$SARIF_FILE" ]]; then
  _log_notice "SARIF file not found: ${SARIF_FILE} — skipping upload"
  exit 0
fi

if [[ -z "${GITHUB_REPOSITORY:-}" ]]; then
  _log_error "GITHUB_REPOSITORY environment variable is required"
  exit 1
fi

if [[ -z "${GITHUB_SHA:-}" ]]; then
  _log_error "GITHUB_SHA environment variable is required"
  exit 1
fi

if [[ -z "${GITHUB_REF:-}" ]]; then
  _log_error "GITHUB_REF environment variable is required"
  exit 1
fi

API_URL="${GITHUB_API_URL:-https://api.github.com}"
SARIF_CATEGORY="${SARIF_CATEGORY:-}"

# =============================================================================
# Upload
# =============================================================================

if [[ -n "$SARIF_CATEGORY" ]]; then
  printf "Uploading %s to Code Scanning [%s] (%s @ %s)\n" "$SARIF_FILE" "$SARIF_CATEGORY" "$GITHUB_REPOSITORY" "${GITHUB_SHA:0:7}"
else
  printf "Uploading %s to Code Scanning (%s @ %s)\n" "$SARIF_FILE" "$GITHUB_REPOSITORY" "${GITHUB_SHA:0:7}"
fi

sarif_b64="$(gzip -c "$SARIF_FILE" | base64 | tr -d '\n')"

# Build JSON payload with jq (proper escaping) and pipe via stdin to
# avoid ARG_MAX limits — large SARIF base64 strings exceed shell limits.
jq_args=(--arg sha "$GITHUB_SHA" --arg ref "$GITHUB_REF" --arg sarif "$sarif_b64")
# shellcheck disable=SC2016 # $sha, $ref, $sarif are jq variables, not shell
jq_filter='{commit_sha: $sha, ref: $ref, sarif: $sarif}'

if [[ -n "$SARIF_CATEGORY" ]]; then
  jq_args+=(--arg tool "$SARIF_CATEGORY")
  # shellcheck disable=SC2016
  jq_filter='{commit_sha: $sha, ref: $ref, sarif: $sarif, tool_name: $tool}'
fi

response="$(jq -n "${jq_args[@]}" "$jq_filter" | curl -s -w "\n%{http_code}" -X POST \
  -H "Authorization: token ${SARIF_UPLOAD_TOKEN}" \
  -H "Accept: application/vnd.github+json" \
  "${API_URL}/repos/${GITHUB_REPOSITORY}/code-scanning/sarifs" \
  -d @-)"

http_code="$(echo "$response" | tail -1)"
body="$(echo "$response" | sed '$d')"

if [[ "$http_code" -ge 200 && "$http_code" -lt 300 ]]; then
  printf "✓ SARIF uploaded successfully (HTTP %s)\n" "$http_code"
else
  _log_error "SARIF upload failed (HTTP ${http_code}): ${body}"
  exit 1
fi
