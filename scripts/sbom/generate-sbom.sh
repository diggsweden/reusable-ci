#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Generate Software Bill of Materials (SBOMs) for different project types and layers
# Supports: maven, npm, gradle, go, rust, python
# Layers: source (dependency manifests), artifact (built binaries), containerimage

set -uo pipefail

# renovate: datasource=github-releases depName=anchore/syft
readonly SYFT_VERSION="v1.37.0"

PROJECT_TYPE="${1:-auto}"
LAYERS="${2:-source}"
VERSION="${3:-}"
PROJECT_NAME="${4:-}"
WORKING_DIR="${5:-.}"
CONTAINER_IMAGE="${6:-}"
CREATE_ZIP="${7:-false}"

cd "$WORKING_DIR" || exit 1

#
# Utility functions
#
log_header() {
  printf "================================================\n"
  printf "%s\n" "$1"
  printf "================================================\n"
}

log_section() {
  printf "📦 %s\n" "$1"
}

log_info() {
  printf "   %s\n" "$1"
}

log_success() {
  printf "   ✅ %s\n" "$1"
}

log_warning() {
  printf "   ⚠️  %s\n" "$1"
}

log_error() {
  printf "   ❌ %s\n" "$1" >&2
}

#
# Syft installation
#
install_syft() {
  if command -v syft &>/dev/null; then
    printf "✅ Syft already installed: %s\n\n" "$(syft version --output json 2>/dev/null | grep -o '"version":"[^"]*"' | cut -d'"' -f4)"
    return
  fi

  printf "Installing Syft SBOM generator (version: %s)...\n" "${SYFT_VERSION}"
  curl -sSfL https://raw.githubusercontent.com/anchore/syft/main/install.sh | sh -s -- -b /tmp "${SYFT_VERSION}"
  export PATH="/tmp:$PATH"
  printf "✅ Syft installed successfully\n\n"
}

#
# SBOM generation core
#
generate_dual_sboms() {
  local scan_target="$1"
  local name="$2"
  local version="$3"
  local layer_name="$4"
  local custom_basename="$5"

  local spdx_file cdx_file

  if [[ -n "$custom_basename" ]]; then
    spdx_file="${custom_basename}-sbom.spdx.json"
    cdx_file="${custom_basename}-sbom.cyclonedx.json"
  else
    spdx_file="${name}-${version}-${layer_name}-sbom.spdx.json"
    cdx_file="${name}-${version}-${layer_name}-sbom.cyclonedx.json"
  fi

  syft "$scan_target" -o spdx-json >"$spdx_file"
  syft "$scan_target" -o cyclonedx-json >"$cdx_file"

  if [[ -f "$spdx_file" && -s "$spdx_file" ]]; then
    log_success "$spdx_file"
  else
    log_error "Failed to generate: $spdx_file"
    return 1
  fi

  if [[ -f "$cdx_file" && -s "$cdx_file" ]]; then
    log_success "$cdx_file"
  else
    log_error "Failed to generate: $cdx_file"
    return 1
  fi
}

#
# Project detection
#
detect_project_type() {
  [[ "$PROJECT_TYPE" != "auto" ]] && return

  if [[ -f "pom.xml" ]]; then
    PROJECT_TYPE="maven"
  elif [[ -f "package.json" ]]; then
    PROJECT_TYPE="npm"
  elif [[ -f "build.gradle" || -f "build.gradle.kts" ]]; then
    PROJECT_TYPE="gradle"
  elif [[ -f "go.mod" ]]; then
    PROJECT_TYPE="go"
  elif [[ -f "Cargo.toml" ]]; then
    PROJECT_TYPE="rust"
  elif [[ -f "pyproject.toml" || -f "requirements.txt" || -f "setup.py" ]]; then
    PROJECT_TYPE="python"
  else
    PROJECT_TYPE="unknown"
  fi

  printf "Auto-detected project type: %s\n" "$PROJECT_TYPE"
}

#
# Version extraction per project type
#
get_version_maven() { mvn help:evaluate -Dexpression=project.version -q -DforceStdout 2>/dev/null; }
get_version_npm() { node -p "require('./package.json').version" 2>/dev/null; }
get_version_gradle() { grep -oP "version\s*=\s*['\"]?\K[^'\"]*" build.gradle 2>/dev/null | head -1; }
get_version_go() {
  local v
  v=$(grep -oP '^module\s+\S+/v\K[0-9]+' go.mod 2>/dev/null | head -1) && printf "%s.0.0" "$v"
}
get_version_rust() { grep -oP '^version\s*=\s*"\K[^"]+' Cargo.toml 2>/dev/null | head -1; }
get_version_python() {
  grep -oP '^version\s*=\s*"\K[^"]+' pyproject.toml 2>/dev/null | head -1 ||
    grep -oP "version\s*=\s*['\"]?\K[^'\"]*" setup.py 2>/dev/null | head -1
}

get_version() {
  [[ -n "$VERSION" ]] && {
    printf "%s" "$VERSION"
    return
  }

  local detected
  case "$PROJECT_TYPE" in
  maven) detected=$(get_version_maven) ;;
  npm) detected=$(get_version_npm) ;;
  gradle) detected=$(get_version_gradle) ;;
  go) detected=$(get_version_go) ;;
  rust) detected=$(get_version_rust) ;;
  python) detected=$(get_version_python) ;;
  esac

  printf "%s" "${detected:-unknown}"
}

#
# Name extraction per project type
#
get_name_maven() { mvn help:evaluate -Dexpression=project.artifactId -q -DforceStdout 2>/dev/null; }
get_name_npm() { node -p "require('./package.json').name" 2>/dev/null | sed 's/@.*\///'; }
get_name_gradle() { grep -oP "rootProject.name\s*=\s*['\"]?\K[^'\"]*" settings.gradle 2>/dev/null; }
get_name_go() { grep -oP '^module\s+\K\S+' go.mod 2>/dev/null | head -1 | xargs basename 2>/dev/null; }
get_name_rust() { grep -oP '^name\s*=\s*"\K[^"]+' Cargo.toml 2>/dev/null | head -1; }
get_name_python() {
  grep -oP '^name\s*=\s*"\K[^"]+' pyproject.toml 2>/dev/null | head -1 ||
    grep -oP "name\s*=\s*['\"]?\K[^'\"]*" setup.py 2>/dev/null | head -1
}

get_project_name() {
  [[ -n "$PROJECT_NAME" ]] && {
    printf "%s" "$PROJECT_NAME"
    return
  }

  local detected
  case "$PROJECT_TYPE" in
  maven) detected=$(get_name_maven) ;;
  npm) detected=$(get_name_npm) ;;
  gradle) detected=$(get_name_gradle) ;;
  go) detected=$(get_name_go) ;;
  rust) detected=$(get_name_rust) ;;
  python) detected=$(get_name_python) ;;
  esac

  printf "%s" "${detected:-$(basename "$(pwd)")}"
}

#
# Artifact collection helpers
#
find_artifacts() {
  local dir="$1"
  shift
  [[ -d "$dir" ]] && find "$dir" -type f "$@" -print0 2>/dev/null
}

collect_artifacts() {
  local -n arr=$1
  shift
  while IFS= read -r -d '' artifact; do
    arr+=("$artifact")
  done < <("$@")
}

scan_artifacts() {
  local name="$1"
  local version="$2"
  local layer_type="$3"
  local ext="$4"
  shift 4
  local artifacts=("$@")

  if [[ ${#artifacts[@]} -gt 0 ]]; then
    for artifact in "${artifacts[@]}"; do
      log_info "Scanning: $artifact"
      local basename
      basename=$(basename "$artifact" ".$ext")
      generate_dual_sboms "$artifact" "$name" "$version" "$layer_type" "${basename}-${layer_type}"
    done
    return 0
  fi
  return 1
}

#
# Source layer generation
#
generate_source_layer() {
  local name="$1" version="$2"

  log_section "Generating Source layer SBOMs..."

  case "$PROJECT_TYPE" in
  maven)
    if [[ -f "pom.xml" ]]; then
      log_info "Scanning pom.xml..."
      generate_dual_sboms "pom.xml" "$name" "$version" "pom" "${name}-${version}-pom"
    else
      log_warning "No pom.xml found"
    fi
    ;;
  npm)
    if [[ -f "package.json" ]]; then
      log_info "Scanning package.json..."
      generate_dual_sboms "." "$name" "$version" "package" "${name}-${version}-package"
    else
      log_warning "No package.json found"
    fi
    ;;
  gradle)
    if [[ -f "build.gradle" || -f "build.gradle.kts" ]]; then
      log_info "Scanning build.gradle..."
      generate_dual_sboms "." "$name" "$version" "gradle" "${name}-${version}-gradle"
    else
      log_warning "No build.gradle found"
    fi
    ;;
  go)
    if [[ -f "go.mod" ]]; then
      log_info "Scanning go.mod..."
      generate_dual_sboms "." "$name" "$version" "gomod" "${name}-${version}-gomod"
    else
      log_warning "No go.mod found"
    fi
    ;;
  rust)
    if [[ -f "Cargo.toml" ]]; then
      log_info "Scanning Cargo.toml..."
      generate_dual_sboms "." "$name" "$version" "cargo" "${name}-${version}-cargo"
    else
      log_warning "No Cargo.toml found"
    fi
    ;;
  python)
    if [[ -f "pyproject.toml" ]]; then
      log_info "Scanning pyproject.toml..."
      generate_dual_sboms "." "$name" "$version" "pyproject" "${name}-${version}-pyproject"
    elif [[ -f "requirements.txt" ]]; then
      log_info "Scanning requirements.txt..."
      generate_dual_sboms "." "$name" "$version" "requirements" "${name}-${version}-requirements"
    elif [[ -f "setup.py" ]]; then
      log_info "Scanning setup.py..."
      generate_dual_sboms "." "$name" "$version" "setup" "${name}-${version}-setup"
    else
      log_warning "No Python dependency file found"
    fi
    ;;
  *)
    log_warning "Unsupported project type: $PROJECT_TYPE"
    ;;
  esac
  printf "\n"
}

#
# Artifact layer generation
#
generate_artifact_layer_maven() {
  local name="$1" version="$2"
  local artifacts=()

  # Search for JAR files matching the artifact name (excluding sources, javadoc, tests, and original artifacts)
  collect_artifacts artifacts find_artifacts "./release-artifacts" -name "${name}-*.jar" ! -name "*-sources.jar" ! -name "*-javadoc.jar" ! -name "*-tests.jar" ! -name "original-*.jar"

  [[ ${#artifacts[@]} -eq 0 ]] && collect_artifacts artifacts find_artifacts "target" -name "${name}-*.jar" ! -name "*-sources.jar" ! -name "*-javadoc.jar" ! -name "*-tests.jar" ! -name "original-*.jar"

  scan_artifacts "$name" "$version" "jar" "jar" "${artifacts[@]}" || log_warning "No JAR files found"
}

generate_artifact_layer_npm() {
  local name="$1" version="$2"
  local artifacts=()

  collect_artifacts artifacts find_artifacts "./release-artifacts" -name "*.tgz"
  [[ ${#artifacts[@]} -eq 0 ]] && collect_artifacts artifacts find_artifacts "." -name "*.tgz"

  scan_artifacts "$name" "$version" "tararchive" "tgz" "${artifacts[@]}" || log_warning "No NPM tarball found"
}

generate_artifact_layer_gradle() {
  local name="$1" version="$2"
  local artifacts=()

  [[ ! -d "build/libs" ]] && {
    log_warning "No build/libs/ directory found"
    return
  }

  collect_artifacts artifacts find_artifacts "build/libs" -name "${name}-*.jar" ! -name "*-sources.jar" ! -name "*-javadoc.jar"

  scan_artifacts "$name" "$version" "jar" "jar" "${artifacts[@]}" || log_warning "No JAR files found"
}

generate_artifact_layer_go() {
  local name="$1" version="$2"
  local artifacts=()

  collect_artifacts artifacts find_artifacts "./release-artifacts" -executable
  [[ ${#artifacts[@]} -eq 0 ]] && collect_artifacts artifacts find_artifacts "." -name "$name"

  if ! scan_artifacts "$name" "$version" "binary" "" "${artifacts[@]}"; then
    log_warning "No Go binary found"
    log_info "Note: Source layer SBOM from go.mod is usually sufficient"
  fi
}

generate_artifact_layer_rust() {
  local name="$1" version="$2"
  local artifacts=()

  collect_artifacts artifacts find_artifacts "./release-artifacts" -executable
  [[ ${#artifacts[@]} -eq 0 ]] && collect_artifacts artifacts find_artifacts "target/release" -executable ! -name "*.d"

  if ! scan_artifacts "$name" "$version" "binary" "" "${artifacts[@]}"; then
    log_warning "No Rust binary found"
    log_info "Note: Source layer SBOM from Cargo.toml is usually sufficient"
  fi
}

generate_artifact_layer_python() {
  local name="$1" version="$2"
  local artifacts=()

  collect_artifacts artifacts find_artifacts "./release-artifacts" \( -name "*.whl" -o -name "*.tar.gz" \)
  [[ ${#artifacts[@]} -eq 0 ]] && collect_artifacts artifacts find_artifacts "dist" \( -name "*.whl" -o -name "*.tar.gz" \)

  if [[ ${#artifacts[@]} -gt 0 ]]; then
    for artifact in "${artifacts[@]}"; do
      log_info "Scanning: $artifact"
      local pkg_basename
      pkg_basename=$(basename "$artifact")
      pkg_basename="${pkg_basename%.whl}"
      pkg_basename="${pkg_basename%.tar.gz}"
      generate_dual_sboms "$artifact" "$name" "$version" "wheel" "${pkg_basename}-wheel"
    done
  else
    log_warning "No Python wheel/sdist found"
    log_info "Note: Source layer SBOM is usually sufficient"
  fi
}

generate_artifact_layer() {
  local name="$1" version="$2"

  log_section "Generating Artifact layer SBOMs..."
  log_info "Project type: $PROJECT_TYPE"

  case "$PROJECT_TYPE" in
  maven) generate_artifact_layer_maven "$name" "$version" ;;
  npm) generate_artifact_layer_npm "$name" "$version" ;;
  gradle) generate_artifact_layer_gradle "$name" "$version" ;;
  go) generate_artifact_layer_go "$name" "$version" ;;
  rust) generate_artifact_layer_rust "$name" "$version" ;;
  python) generate_artifact_layer_python "$name" "$version" ;;
  *) log_warning "Unknown project type: $PROJECT_TYPE" ;;
  esac
  printf "\n"
}

#
# Container layer generation
#
generate_container_layer() {
  local name="$1" version="$2"

  log_section "Generating Container layer SBOMs..."

  if [[ -z "$CONTAINER_IMAGE" ]]; then
    log_warning "No container image specified, skipping"
    printf "\n"
    return
  fi

  log_info "Scanning container: $CONTAINER_IMAGE"
  generate_dual_sboms "$CONTAINER_IMAGE" "$name" "$version" "container" "${name}-${version}-container"
  printf "\n"
}

#
# Summary and ZIP creation
#
generate_summary() {
  local project_name="$1" version="$2"

  log_header "SBOM Generation Complete"

  local sbom_count
  sbom_count=$(find . -maxdepth 1 -name '*-sbom.*.json' -type f 2>/dev/null | wc -l)

  if [[ "$sbom_count" -eq 0 ]]; then
    printf "❌ No SBOM files generated\n"
    return 1
  fi

  printf "✅ Successfully generated %s SBOM files\n\n" "$sbom_count"
  printf "Generated files:\n"
  find . -maxdepth 1 -name '*-sbom.*.json' -type f -exec ls -lh {} +
  printf "\n"

  if [[ "$CREATE_ZIP" = "true" ]]; then
    local sbom_zip="${project_name}-${version}-sboms.zip"
    printf "📦 Creating SBOM ZIP archive...\n"
    find . -maxdepth 1 -name '*-sbom.*.json' -type f -exec zip "$sbom_zip" {} +

    if [[ -f "$sbom_zip" ]]; then
      printf "✅ Created: %s\n\n" "$sbom_zip"
      unzip -l "$sbom_zip"
    else
      log_warning "Failed to create SBOM ZIP"
    fi
    printf "\n"
  fi
}

#
# Main
#
main() {
  log_header "SBOM Generation Script"
  printf "Working directory: %s\n" "$(pwd)"
  printf "Project type: %s\n" "$PROJECT_TYPE"
  printf "Requested layers: %s\n\n" "$LAYERS"

  install_syft
  detect_project_type

  local version project_name
  version=$(get_version)
  project_name=$(get_project_name)

  printf "Project Information:\n"
  printf "  Name: %s\n" "$project_name"
  printf "  Version: %s\n" "$version"
  printf "  Type: %s\n\n" "$PROJECT_TYPE"

  IFS=',' read -ra layer_array <<<"$LAYERS"
  for layer in "${layer_array[@]}"; do
    layer=$(printf "%s" "$layer" | xargs)
    case "$layer" in
    source) generate_source_layer "$project_name" "$version" ;;
    artifact) generate_artifact_layer "$project_name" "$version" ;;
    containerimage) generate_container_layer "$project_name" "$version" ;;
    *)
      log_warning "Unknown layer: $layer (valid: source, artifact, containerimage)"
      return 1
      ;;
    esac
  done

  generate_summary "$project_name" "$version"
}

main
