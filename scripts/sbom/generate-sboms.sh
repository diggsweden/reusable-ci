#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Generate Software Bill of Materials (SBOMs) for different project types and layers
# Supports: maven, npm, gradle, go, rust, python
# Layers: source (dependency manifests), analyzed-artifact (built binaries), analyzed-container

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"
source "$SCRIPT_DIR/../ci/install-syft.sh"

readonly VALID_PROJECT_TYPES="auto maven npm gradle gradle-android xcode-ios python go rust"

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

# Construct the basename used by analyzed-* SBOM filenames. Injects a short
# commit SHA for traceability when run inside a git repo; otherwise falls
# back to a SHA-less form so test fixtures without git work the same.
_analyzed_basename() {
  local file_basename="$1" layer_type="$2"
  local sha
  if sha=$(git rev-parse --short HEAD 2>/dev/null); then
    printf "%s-%s-%s" "$file_basename" "$sha" "$layer_type"
  else
    printf "%s-%s" "$file_basename" "$layer_type"
  fi
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
      generate_dual_sboms "$artifact" "$name" "$version" "$layer_type" "$(_analyzed_basename "$basename" "$layer_type")"
    done
    return 0
  fi
  return 1
}

#
# Artifact layer generation
#
generate_artifact_layer_maven() {
  local name="$1" version="$2"
  local artifacts=()
  local jar_patterns=(-name "${name}-*.jar" -o -name "${name}.jar")
  local jar_excludes=(! -name "*-sources.jar" ! -name "*-javadoc.jar" ! -name "*-tests.jar" ! -name "original-*.jar")

  # Search for JAR files matching the artifact name (excluding sources, javadoc, tests, and original artifacts)
  collect_artifacts artifacts find_artifacts "./release-artifacts" \( "${jar_patterns[@]}" \) "${jar_excludes[@]}"

  [[ ${#artifacts[@]} -eq 0 ]] && collect_artifacts artifacts find_artifacts "./release-artifacts/target" \( "${jar_patterns[@]}" \) "${jar_excludes[@]}"

  [[ ${#artifacts[@]} -eq 0 ]] && collect_artifacts artifacts find_artifacts "target" \( "${jar_patterns[@]}" \) "${jar_excludes[@]}"

  scan_artifacts "$name" "$version" "analyzed-jar" "jar" "${artifacts[@]}" || log_warning "No JAR files found"
}

generate_artifact_layer_npm() {
  local name="$1" version="$2"
  local artifacts=()

  collect_artifacts artifacts find_artifacts "./release-artifacts" -name "*.tgz"
  [[ ${#artifacts[@]} -eq 0 ]] && collect_artifacts artifacts find_artifacts "." -name "*.tgz"

  scan_artifacts "$name" "$version" "analyzed-tararchive" "tgz" "${artifacts[@]}" || log_warning "No NPM tarball found"
}

generate_artifact_layer_gradle() {
  local name="$1" version="$2"
  local artifacts=()

  [[ ! -d "build/libs" ]] && {
    log_warning "No build/libs/ directory found"
    return
  }

  collect_artifacts artifacts find_artifacts "build/libs" -name "${name}-*.jar" ! -name "*-sources.jar" ! -name "*-javadoc.jar"

  scan_artifacts "$name" "$version" "analyzed-jar" "jar" "${artifacts[@]}" || log_warning "No JAR files found"
}

generate_artifact_layer_go() {
  local name="$1" version="$2"
  local artifacts=()

  collect_artifacts artifacts find_artifacts "./release-artifacts" -executable
  [[ ${#artifacts[@]} -eq 0 ]] && collect_artifacts artifacts find_artifacts "." -name "$name"

  if ! scan_artifacts "$name" "$version" "analyzed-binary" "" "${artifacts[@]}"; then
    log_warning "No Go binary found"
    log_info "Note: Source layer SBOM from go.mod is usually sufficient"
  fi
}

generate_artifact_layer_rust() {
  local name="$1" version="$2"
  local artifacts=()

  collect_artifacts artifacts find_artifacts "./release-artifacts" -executable
  [[ ${#artifacts[@]} -eq 0 ]] && collect_artifacts artifacts find_artifacts "target/release" -executable ! -name "*.d"

  if ! scan_artifacts "$name" "$version" "analyzed-binary" "" "${artifacts[@]}"; then
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
      generate_dual_sboms "$artifact" "$name" "$version" "analyzed-wheel" "$(_analyzed_basename "$pkg_basename" "analyzed-wheel")"
    done
  else
    log_warning "No Python wheel/sdist found"
    log_info "Note: Source layer SBOM is usually sufficient"
  fi
}

# Build the Build-layer SBOM output filename. Appends a short commit SHA for traceability
build_layer_filename() {
  local name="$1" version="$2"
  local sha
  if sha=$(git rev-parse --short HEAD 2>/dev/null); then
    printf "%s-%s-%s-build-sbom.cyclonedx.json" "$name" "$version" "$sha"
  else
    printf "%s-%s-build-sbom.cyclonedx.json" "$name" "$version"
  fi
}

generate_build_layer() {
  local name="$1" version="$2"

  log_section "Generating Build layer SBOMs..."
  log_info "Project type: $PROJECT_TYPE"

  case "$PROJECT_TYPE" in
  maven) generate_build_layer_maven "$name" "$version" ;;
  npm) generate_build_layer_npm "$name" "$version" ;;
  gradle) generate_build_layer_gradle "$name" "$version" ;;
  rust) generate_build_layer_cargo "$name" "$version" ;;
  go) log_warning "Build SBOM not implemented for project type: go" ;;
  python) log_warning "Build SBOM not implemented for project type: python" ;;
  *) log_warning "Build SBOM not supported for project type: $PROJECT_TYPE" ;;
  esac
  printf "\n"
}

# Locate the aggregate CISA Build SBOM (bom.json) produced by the stack's
# cyclonedx plugin inside the download-artifact tree. Picks the path with the
# fewest directory separators on the assumption that monorepo/per-module BOMs
# live deeper than the project-root aggregate. Works whether the consumer's
# working-directory was `.` or a subdir — the artifact upload preserves the
# working-directory prefix, so fixed-path lookups fail on subdir projects.
#
# TODO(option B): once builders upload only the aggregate bom.json per
# artefact, this whole depth-sort becomes `find ... -name bom.json | head -1`.
#
# Usage: _find_build_bom <include-pattern>... [-- <exclude-pattern>...]
# All patterns are find-style -path globs. Patterns containing whitespace are
# supported because arguments are passed positionally (no word-splitting).
_find_build_bom() {
  local root="./release-artifacts"
  [[ -d "$root" ]] || root="."

  local includes=() excludes=()
  local in_excludes=0
  local arg
  for arg in "$@"; do
    if [[ "$arg" == "--" ]]; then
      in_excludes=1
      continue
    fi
    if ((in_excludes)); then
      excludes+=("$arg")
    else
      includes+=("$arg")
    fi
  done

  # Sentinel -false lets every include be prepended with -o uniformly.
  local find_args=(-type f \( -false)
  local pattern
  for pattern in "${includes[@]}"; do
    find_args+=(-o -path "$pattern")
  done
  find_args+=(\))
  for pattern in "${excludes[@]}"; do
    find_args+=(! -path "$pattern")
  done

  # Print depth + path, sort numerically (depth first, then path for a stable
  # tie-break across filesystems), take the smallest-depth entry.
  find "$root" "${find_args[@]}" -printf '%d\t%p\n' 2>/dev/null |
    sort -k1,1n -k2,2 |
    head -1 |
    cut -f2-
}

_emit_build_sbom() {
  local bom_file="$1" name="$2" version="$3" stack="$4" hint="$5"
  if [[ -z "$bom_file" ]]; then
    log_warning "No ${stack} Build SBOM found${hint:+ - ${hint}}"
    return 0
  fi
  local output_file
  output_file=$(build_layer_filename "$name" "$version")
  cp "$bom_file" "$output_file"
  log_success "$output_file"
}

generate_build_layer_maven() {
  local name="$1" version="$2"
  local bom_file
  bom_file=$(_find_build_bom '*/target/bom.json')
  _emit_build_sbom "$bom_file" "$name" "$version" "Maven" \
    "run cyclonedx-maven-plugin during build"
}

generate_build_layer_npm() {
  local name="$1" version="$2"
  local bom_file
  # Exclude node_modules — vendored packages may ship their own bom.json and
  # would otherwise hijack pickup when sorted by depth.
  bom_file=$(_find_build_bom '*/bom.json' -- '*/node_modules/*')
  _emit_build_sbom "$bom_file" "$name" "$version" "npm" \
    "run @cyclonedx/cyclonedx-npm during build"
}

generate_build_layer_gradle() {
  local name="$1" version="$2"
  local bom_file
  # Plugin 3.x writes to build/reports/bom.json; older versions used
  # build/reports/cyclonedx/bom.json. Match both for forward/back compat.
  bom_file=$(
    _find_build_bom \
      '*/build/reports/bom.json' \
      '*/build/reports/cyclonedx/bom.json'
  )
  _emit_build_sbom "$bom_file" "$name" "$version" "Gradle" \
    "run cyclonedx-gradle-plugin during build"
}

generate_build_layer_cargo() {
  local name="$1" version="$2"
  local bom_file
  # cargo-cyclonedx writes bom.json at each crate root (workspace-aware);
  # pick the aggregate (root-crate) BOM over deeper per-crate ones. Exclude
  # compile artifacts under target/.
  bom_file=$(_find_build_bom '*/bom.json' -- '*/target/*')
  _emit_build_sbom "$bom_file" "$name" "$version" "Cargo" \
    "run cargo-cyclonedx during build"
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
  generate_dual_sboms "$CONTAINER_IMAGE" "$name" "$version" "analyzed-container" "$(_analyzed_basename "${name}-${version}" "analyzed-container")"
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
    local sbom_zip
    sbom_zip=$(ci_sbom_zip_name "$project_name" "$version")
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
usage() {
  cat <<USAGE
Usage: $(basename "$0") [flags]

Generate CISA-layered SBOMs (SPDX + CycloneDX) for a project.

Flags:
  --project-type <type>     Project type (maven|npm|gradle|gradle-android|go|rust|python|auto). Default: auto
  --layers <csv>            Comma-list of layers (build|analyzed-artifact|analyzed-container). Default: build
  --version <ver>           Project version (overrides auto-detect)
  --name <name>             Project name (overrides auto-detect)
  --working-dir <dir>       cd to this dir before running. Default: .
  --container-image <ref>   Container image ref for analyzed-container layer
  --create-zip              Bundle the generated SBOMs into a release-ready ZIP
  -h, --help                Show this help
USAGE
}

main() {
  local PROJECT_TYPE="auto"
  local LAYERS="build"
  local VERSION=""
  local PROJECT_NAME=""
  local WORKING_DIR="."
  local CONTAINER_IMAGE=""
  local CREATE_ZIP="false"

  while [[ $# -gt 0 ]]; do
    case "$1" in
    --project-type)
      [[ $# -ge 2 ]] || {
        printf "Error: --project-type requires an argument\n" >&2
        exit 1
      }
      PROJECT_TYPE="$2"
      printf "%s" "$VALID_PROJECT_TYPES" | grep -qw "$PROJECT_TYPE" || {
        printf "Error: invalid --project-type '%s' (valid: %s)\n" "$PROJECT_TYPE" "$VALID_PROJECT_TYPES" >&2
        exit 1
      }
      shift 2
      ;;
    --layers)
      [[ $# -ge 2 ]] || {
        printf "Error: --layers requires an argument\n" >&2
        exit 1
      }
      LAYERS="$2"
      shift 2
      ;;
    --version)
      [[ $# -ge 2 ]] || {
        printf "Error: --version requires an argument\n" >&2
        exit 1
      }
      VERSION="$2"
      shift 2
      ;;
    --name)
      [[ $# -ge 2 ]] || {
        printf "Error: --name requires an argument\n" >&2
        exit 1
      }
      PROJECT_NAME="$2"
      shift 2
      ;;
    --working-dir)
      [[ $# -ge 2 ]] || {
        printf "Error: --working-dir requires an argument\n" >&2
        exit 1
      }
      WORKING_DIR="$2"
      shift 2
      ;;
    --container-image)
      [[ $# -ge 2 ]] || {
        printf "Error: --container-image requires an argument\n" >&2
        exit 1
      }
      CONTAINER_IMAGE="$2"
      shift 2
      ;;
    --create-zip)
      CREATE_ZIP="true"
      shift
      ;;
    -h | --help)
      usage
      exit 0
      ;;
    *)
      printf "Error: unknown flag: %s\n\n" "$1" >&2
      usage >&2
      exit 1
      ;;
    esac
  done

  cd "$WORKING_DIR" || exit 1

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

  local layer_array
  IFS=',' read -ra layer_array <<<"$LAYERS"
  local layer
  for layer in "${layer_array[@]}"; do
    layer=$(printf "%s" "$layer" | xargs)
    case "$layer" in
    build) generate_build_layer "$project_name" "$version" ;;
    analyzed-artifact) generate_artifact_layer "$project_name" "$version" ;;
    analyzed-container) generate_container_layer "$project_name" "$version" ;;
    *)
      log_warning "Unknown layer: $layer (valid: build, analyzed-artifact, analyzed-container)"
      return 1
      ;;
    esac
  done

  generate_summary "$project_name" "$version"
}

main "$@"
