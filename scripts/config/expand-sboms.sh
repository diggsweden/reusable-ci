#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0
#
# Expand an `sboms` enum value to the list of CISA layers it represents.
#
# Accepted input:
#   all                 → all three layers
#   none                → empty
#   <layer>[,<layer>…]  → deduped list of the listed layers
#
# Valid layer tokens: build, analyzed-artifact, analyzed-container.
# `all` and `none` are shortcuts and are NOT composable with other tokens.
# Whitespace around commas is tolerated. Duplicates are deduped silently.
#
# Usage: expand-sboms.sh [--format json|comma] [--exclude <layer>] <value>
#   --format json  (default)   Emit a JSON array, e.g. ["build","analyzed-artifact"]
#   --format comma             Emit a comma-list, e.g. build,analyzed-artifact
#                              (empty when the expansion is empty; no "none" literal)
#   --exclude <layer>          Drop <layer> from the expansion before emitting
#
# Prints the result to stdout; errors to stderr with a non-zero exit.

set -euo pipefail

readonly VALID_LAYERS="build analyzed-artifact analyzed-container"

err() {
  printf "expand-sboms: %s\n" "$1" >&2
}

# Emit a deduped array of layer tokens for a given enum value.
# Writes one token per line so the caller can reshape as JSON or comma-list.
_emit_layers() {
  local input="$1"
  input="${input//[[:space:]]/}"

  if [[ -z "$input" ]]; then
    err "value required (expected: all | none | comma-list of build,analyzed-artifact,analyzed-container)"
    return 1
  fi

  if [[ "$input" =~ (^,|,$|,,) ]]; then
    err "empty token in '$input' (check for leading, trailing or duplicate commas)"
    return 1
  fi

  case "$input" in
  all)
    printf 'build\nanalyzed-artifact\nanalyzed-container\n'
    return 0
    ;;
  none)
    return 0
    ;;
  esac

  local -a parts
  IFS=',' read -ra parts <<<"$input"

  local -A seen=()
  local token
  for token in "${parts[@]}"; do
    case "$token" in
    build | analyzed-artifact | analyzed-container)
      if [[ -z "${seen[$token]:-}" ]]; then
        printf '%s\n' "$token"
        seen[$token]=1
      fi
      ;;
    all | none)
      err "'$token' is a shortcut and cannot be combined with other values"
      return 1
      ;;
    *)
      err "unknown token '$token' (valid: $VALID_LAYERS, or shortcuts all|none)"
      return 1
      ;;
    esac
  done
}

main() {
  local format="json"
  local -a excludes=()

  while [[ $# -gt 0 ]]; do
    case "$1" in
    --format)
      [[ $# -ge 2 ]] || {
        err "--format requires an argument"
        exit 1
      }
      format="$2"
      shift 2
      ;;
    --exclude)
      [[ $# -ge 2 ]] || {
        err "--exclude requires an argument"
        exit 1
      }
      excludes+=("$2")
      shift 2
      ;;
    --)
      shift
      break
      ;;
    -*)
      err "unknown flag: $1"
      exit 1
      ;;
    *)
      break
      ;;
    esac
  done

  case "$format" in
  json | comma) ;;
  *)
    err "--format must be json or comma, got: $format"
    exit 1
    ;;
  esac

  if [[ $# -lt 1 ]]; then
    err "missing argument"
    printf "Usage: %s [--format json|comma] [--exclude <layer>] <value>\n" "$(basename "$0")" >&2
    exit 1
  fi

  # Capture _emit_layers output eagerly so its non-zero exit propagates (process
  # substitution would swallow the exit status and leave us with an empty list).
  local raw
  raw=$(_emit_layers "$1")

  local -a layers=()
  local layer
  while IFS= read -r layer; do
    [[ -z "$layer" ]] && continue
    local skip=0 excl
    for excl in "${excludes[@]}"; do
      [[ "$layer" == "$excl" ]] && {
        skip=1
        break
      }
    done
    ((skip)) || layers+=("$layer")
  done <<<"$raw"

  case "$format" in
  json)
    local first=1 out="["
    for layer in "${layers[@]}"; do
      if ((first)); then
        out+="\"$layer\""
        first=0
      else
        out+=",\"$layer\""
      fi
    done
    out+="]"
    printf "%s\n" "$out"
    ;;
  comma)
    local IFS=','
    printf "%s\n" "${layers[*]}"
    ;;
  esac
}

main "$@"
