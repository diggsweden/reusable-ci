#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
# SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

# Ensure buildah + podman are available for `reusable-ci container build`:
# buildah builds the image (daemonless, the forge-neutral replacement for
# docker/build-push-action) and podman runs the load-mode smoke tests (it reads
# buildah's container storage without a docker daemon).
#
# On GitHub-hosted ubuntu runners both are pre-installed, so this is a fast
# no-op. On other runners (Forgejo, self-hosted) it installs them from the
# distro packages — making the dependency explicit and the build verb genuinely
# forge-portable rather than silently assuming the GitHub runner image.
#
# Usage: source this file then call install_buildah, or run it directly.

# shellcheck source=install-common.sh
source "$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)/install-common.sh"

# ci_pkg_install installs the named packages with the host's package manager
# (apt/apk/dnf/yum cover the common GitHub + Forgejo + self-hosted runner
# images), prefixing sudo when not root. Fails closed with an actionable
# message on an unsupported distro.
ci_pkg_install() {
  local sudo=()
  if [ "$(id -u)" -ne 0 ]; then
    sudo=(sudo)
  fi

  if command -v apt-get &>/dev/null; then
    "${sudo[@]}" apt-get update -qq && "${sudo[@]}" apt-get install -y -qq "$@"
  elif command -v apk &>/dev/null; then
    "${sudo[@]}" apk add --no-cache "$@"
  elif command -v dnf &>/dev/null; then
    "${sudo[@]}" dnf install -y -q "$@"
  elif command -v yum &>/dev/null; then
    "${sudo[@]}" yum install -y -q "$@"
  else
    printf 'ERROR: no supported package manager (apt/apk/dnf/yum); bake %s into the runner image\n' "$*" >&2
    return 1
  fi
}

install_buildah() {
  if command -v buildah &>/dev/null && command -v podman &>/dev/null; then
    ci_print_already_installed "buildah/podman" "$(buildah --version 2>/dev/null | head -1)"
    return 0
  fi

  printf 'Installing buildah + podman...\n'
  if ! ci_pkg_install buildah podman; then
    printf 'ERROR: failed to install buildah/podman\n' >&2
    return 1
  fi

  ci_require_command buildah buildah || return 1
  ci_require_command podman podman || return 1
  ci_print_installed "buildah/podman" "$(buildah --version 2>/dev/null | head -1)"
}

if [[ "${BASH_SOURCE[0]}" == "$0" ]]; then
  install_buildah "$@"
fi
