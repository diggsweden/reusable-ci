#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# CI platform environment abstraction
#
# Maps platform-specific env vars to generic CI_* names so scripts
# work across CI systems without direct platform coupling.
#
# Usage: source this file from other scripts
#   source "$(dirname "$0")/../ci/env.sh"
#
# Exported variables:
#   CI_COMMIT      Commit SHA being built
#   CI_REPO        Repository identifier (org/repo)
#   CI_RUN_ID      Unique run/pipeline identifier
#   CI_ACTOR       User or bot that triggered the run
#   CI_BRANCH      Source branch (PR head branch, empty on non-PR)
#   CI_REF_NAME    Short ref name (tag or branch, no refs/ prefix)
#   CI_REF         Full git ref (e.g. refs/heads/main)
#   CI_SERVER_URL  Base URL of the CI server
#   CI_RUN_URL     Direct URL to the current run/pipeline

[[ -n "${_CI_ENV_LOADED:-}" ]] && return 0
_CI_ENV_LOADED=1

if [[ "${GITHUB_ACTIONS:-}" == "true" ]]; then
  CI_COMMIT="${GITHUB_SHA:-}"
  CI_REPO="${GITHUB_REPOSITORY:-}"
  CI_RUN_ID="${GITHUB_RUN_ID:-}"
  CI_ACTOR="${GITHUB_ACTOR:-}"
  CI_BRANCH="${GITHUB_HEAD_REF:-}"
  CI_REF_NAME="${GITHUB_REF_NAME:-}"
  CI_REF="${GITHUB_REF:-}"
  CI_SERVER_URL="https://github.com"
  CI_RUN_URL="https://github.com/${GITHUB_REPOSITORY:-}/actions/runs/${GITHUB_RUN_ID:-}"

elif [[ "${GITLAB_CI:-}" == "true" ]]; then
  # GitLab CI mappings — future task
  :
fi

export CI_COMMIT CI_REPO CI_RUN_ID CI_ACTOR CI_BRANCH CI_REF_NAME CI_REF CI_SERVER_URL CI_RUN_URL
