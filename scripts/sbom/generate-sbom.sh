#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Generate Software Bill of Materials (SBOMs) for different project types and layers
# This script supports Maven, NPM, and Gradle projects
# It can generate SBOMs for:
#   - Layer 1: Source (pom.xml, package.json, build.gradle)
#   - Layer 2: Artifact (JAR files, NPM tarballs, Gradle JARs)
#   - Layer 3: Container (Docker/OCI images)

set -uo pipefail

# Script arguments
PROJECT_TYPE="${1:-auto}"
LAYERS="${2:-source}"
VERSION="${3:-}"
PROJECT_NAME="${4:-}"
WORKING_DIR="${5:-.}"
CONTAINER_IMAGE="${6:-}"
CREATE_ZIP="${7:-false}" # Optional: create ZIP archive of all SBOMs

# Change to working directory
cd "$WORKING_DIR" || exit 1

printf "================================================\n"
printf "SBOM Generation Script\n"
printf "================================================\n"
printf "Working directory: %s\n" "$(pwd)"
printf "Project type: %s\n" "$PROJECT_TYPE"
printf "Requested layers: %s\n" "$LAYERS"
printf "\n"

# Syft version to install (pinned to avoid bugs in newer versions)
# renovate: datasource=github-releases depName=anchore/syft
SYFT_VERSION="v1.37.0"

# Install Syft if not available
install_syft() {
  if ! command -v syft &>/dev/null; then
    printf "Installing Syft SBOM generator (version: %s)...\n" "${SYFT_VERSION}"
    curl -sSfL https://raw.githubusercontent.com/anchore/syft/main/install.sh | sh -s -- -b /tmp "${SYFT_VERSION}"
    export PATH="/tmp:$PATH"
    printf "✅ Syft installed successfully\n"
  else
    printf "✅ Syft already installed: %s\n" "$(syft version --output json | grep -o '"version":"[^"]*"' | cut -d'"' -f4)"
  fi
  printf "\n"
}

# Generate both SPDX and CycloneDX SBOMs for a target
generate_dual_sboms() {
  local scan_target=$1
  local name=$2
  local version=$3
  local layer_name=$4
  local custom_basename=$5

  local spdx_file cdx_file

  if [[ -n "$custom_basename" ]]; then
    spdx_file="${custom_basename}-sbom.spdx.json"
    cdx_file="${custom_basename}-sbom.cyclonedx.json"
  else
    spdx_file="${name}-${version}-${layer_name}-sbom.spdx.json"
    cdx_file="${name}-${version}-${layer_name}-sbom.cyclonedx.json"
  fi

  # Generate SBOMs
  syft "$scan_target" -o spdx-json >"$spdx_file"
  syft "$scan_target" -o cyclonedx-json >"$cdx_file"

  # Validate they exist and aren't empty
  if [[ -f "$spdx_file" && -s "$spdx_file" ]]; then
    printf "   ✅ %s\n" "$spdx_file"
  else
    printf "   ❌ Failed to generate: %s\n" "$spdx_file" >&2
    return 1
  fi

  if [[ -f "$cdx_file" && -s "$cdx_file" ]]; then
    printf "   ✅ %s\n" "$cdx_file"
  else
    printf "   ❌ Failed to generate: %s\n" "$cdx_file" >&2
    return 1
  fi
}

# Auto-detect project type from build files
detect_project_type() {
  if [[ "$PROJECT_TYPE" = "auto" ]]; then
    if [[ -f "pom.xml" ]]; then
      PROJECT_TYPE="maven"
    elif [[ -f "package.json" ]]; then
      PROJECT_TYPE="npm"
    elif [[ -f "build.gradle" || -f "build.gradle.kts" ]]; then
      PROJECT_TYPE="gradle"
    else
      PROJECT_TYPE="unknown"
    fi
    printf "Auto-detected project type: %s\n" "$PROJECT_TYPE"
  fi
}

# Extract version from project files
get_version() {
  if [[ -n "$VERSION" ]]; then
    printf "%s" "$VERSION"
    return
  fi

  local detected_version=""

  case "$PROJECT_TYPE" in
  maven)
    if [[ -f "pom.xml" ]]; then
      if detected_version=$(mvn help:evaluate -Dexpression=project.version -q -DforceStdout 2>&1); then
        printf "%s" "$detected_version"
        return
      else
        printf "⚠️  Warning: Could not extract version from pom.xml\n" >&2
      fi
    fi
    ;;
  npm)
    if [[ -f "package.json" ]]; then
      if detected_version=$(node -p "require('./package.json').version" 2>&1); then
        printf "%s" "$detected_version"
        return
      else
        printf "⚠️  Warning: Could not extract version from package.json\n" >&2
      fi
    fi
    ;;
  gradle)
    if [[ -f "build.gradle" ]]; then
      if detected_version=$(grep -oP "version\s*=\s*['\"]?\K[^'\"]*" build.gradle 2>&1 | head -1); then
        printf "%s" "$detected_version"
        return
      else
        printf "⚠️  Warning: Could not extract version from build.gradle\n" >&2
      fi
    fi
    ;;
  esac

  printf "unknown"
}

# Extract project name from project files
get_project_name() {
  if [[ -n "$PROJECT_NAME" ]]; then
    printf "%s" "$PROJECT_NAME"
    return
  fi

  local detected_name=""

  case "$PROJECT_TYPE" in
  maven)
    if [[ -f "pom.xml" ]]; then
      if detected_name=$(mvn help:evaluate -Dexpression=project.artifactId -q -DforceStdout 2>&1); then
        printf "%s" "$detected_name"
        return
      else
        printf "⚠️  Warning: Could not extract artifactId from pom.xml\n" >&2
      fi
    fi
    ;;
  npm)
    if [[ -f "package.json" ]]; then
      if detected_name=$(node -p "require('./package.json').name" 2>&1 | sed 's/@.*\///'); then
        printf "%s" "$detected_name"
        return
      else
        printf "⚠️  Warning: Could not extract name from package.json\n" >&2
      fi
    fi
    ;;
  gradle)
    if [[ -f "settings.gradle" ]]; then
      if detected_name=$(grep -oP "rootProject.name\s*=\s*['\"]?\K[^'\"]*" settings.gradle 2>&1); then
        printf "%s" "$detected_name"
        return
      else
        printf "⚠️  Warning: Could not extract name from settings.gradle\n" >&2
      fi
    fi
    ;;
  esac

  basename "$(pwd)"
}

# Generate Source layer SBOMs (pom.xml, package.json, build.gradle)
generate_source_layer() {
  local name=$1
  local version=$2

  printf "📦 Generating Source layer SBOMs...\n"

  if [[ -f "pom.xml" ]]; then
    printf "   Scanning pom.xml for Maven dependencies...\n"
    generate_dual_sboms "pom.xml" "$name" "$version" "pom" "${name}-${version}-pom"
  elif [[ -f "package.json" ]]; then
    printf "   Scanning package.json for NPM dependencies...\n"
    generate_dual_sboms "." "$name" "$version" "package" "${name}-${version}-package"
  elif [[ -f "build.gradle" || -f "build.gradle.kts" ]]; then
    printf "   Scanning build.gradle for Gradle dependencies...\n"
    generate_dual_sboms "." "$name" "$version" "gradle" "${name}-${version}-gradle"
  else
    printf "   ⚠️  No build manifest found (pom.xml, package.json, build.gradle)\n"
  fi
  printf "\n"
}

# Generate Artifact layer SBOMs (Maven JARs, NPM tar archives, Gradle JARs, etc.)
generate_artifact_layer() {
  local name=$1
  local version=$2
  local layer_name

  printf "📦 Generating Artifact layer SBOMs...\n"

  # Handle different project types
  case "$PROJECT_TYPE" in
  maven)
    printf "   Project type: Maven (looking for JAR files)\n"

    # Maven JARs can be in multiple locations:
    # 1. target/ directory (from local build or JReleaser workflow)
    # 2. ./release-artifacts/ (downloaded from GitHub CLI workflow artifact)
    local artifacts=()

    # Collect all JARs from release-artifacts (common in GitHub CLI release workflows)
    if [[ -d "./release-artifacts" ]]; then
      while IFS= read -r -d '' artifact; do
        artifacts+=("$artifact")
      done < <(find ./release-artifacts -maxdepth 1 -type f \
        \( -name "${name}.jar" -o -name "${name}-*.jar" \) \
        ! -name "*-sources.jar" \
        ! -name "*-javadoc.jar" \
        ! -name "*-tests.jar" \
        ! -name "*-test.jar" \
        -print0 2>/dev/null)
    fi

    # Fallback to target/ directory if no artifacts found yet
    if [[ ${#artifacts[@]} -eq 0 && -d "target" ]]; then
      while IFS= read -r -d '' artifact; do
        artifacts+=("$artifact")
      done < <(find target -maxdepth 1 -type f \
        \( -name "${name}.jar" -o -name "${name}-*.jar" \) \
        ! -name "*-sources.jar" \
        ! -name "*-javadoc.jar" \
        ! -name "*-tests.jar" \
        ! -name "*-test.jar" \
        -print0 2>/dev/null)
    fi

    if [[ ${#artifacts[@]} -gt 0 ]]; then
      for artifact in "${artifacts[@]}"; do
        printf "   Scanning JAR file: %s\n" "$artifact"
        local jar_basename
        jar_basename=$(basename "$artifact" .jar)
        generate_dual_sboms "$artifact" "$name" "$version" "jar" "${jar_basename}-jar"
      done
    else
      printf "   ⚠️  No JAR files found in target/ or ./release-artifacts/ directories\n"
      printf "      Searched for: %s.jar or %s-*.jar\n" "${name}" "${name}"
    fi
    ;;

  npm)
    printf "   Project type: NPM (looking for tar archive files)\n"

    # NPM tar archives can be in multiple locations:
    # 1. Current directory (from npm pack)
    # 2. ./release-artifacts/ (downloaded from workflow artifact)
    local artifacts=()

    # Collect all tgz files from release-artifacts (common in release workflows)
    if [[ -d "./release-artifacts" ]]; then
      while IFS= read -r -d '' artifact; do
        artifacts+=("$artifact")
      done < <(find ./release-artifacts -maxdepth 1 -name "*.tgz" -type f -print0 2>/dev/null)
    fi

    # Fallback to current directory if no artifacts found yet
    if [[ ${#artifacts[@]} -eq 0 ]]; then
      while IFS= read -r -d '' artifact; do
        artifacts+=("$artifact")
      done < <(find . -maxdepth 1 -name "*.tgz" -type f -print0 2>/dev/null)
    fi

    if [[ ${#artifacts[@]} -gt 0 ]]; then
      for artifact in "${artifacts[@]}"; do
        printf "   Scanning NPM tar archive: %s\n" "$artifact"
        local tgz_basename
        tgz_basename=$(basename "$artifact" .tgz)
        generate_dual_sboms "$artifact" "$name" "$version" "tararchive" "${tgz_basename}-tararchive"
      done
    else
      printf "   ⚠️  No NPM tar archive (.tgz) found\n"
      printf "      Searched in: current directory and ./release-artifacts/\n"
    fi
    ;;

  gradle)
    printf "   Project type: Gradle (looking for JAR files)\n"

    # Gradle builds to build/libs/
    if [[ ! -d "build/libs" ]]; then
      printf "   ⚠️  No build/libs/ directory found, skipping JAR layer\n"
      printf "\n"
      return
    fi

    local artifacts=()
    while IFS= read -r -d '' artifact; do
      artifacts+=("$artifact")
    done < <(find build/libs -maxdepth 1 -type f -name "${name}-*.jar" \
      ! -name "*-sources.jar" \
      ! -name "*-javadoc.jar" \
      -print0 2>/dev/null)

    if [[ ${#artifacts[@]} -gt 0 ]]; then
      for artifact in "${artifacts[@]}"; do
        printf "   Scanning JAR file: %s\n" "$artifact"
        local jar_basename
        jar_basename=$(basename "$artifact" .jar)
        generate_dual_sboms "$artifact" "$name" "$version" "jar" "${jar_basename}-jar"
      done
    else
      printf "   ⚠️  No JAR files found in build/libs/ directory\n"
    fi
    ;;

  *)
    printf "   ⚠️  Unknown project type: %s\n" "$PROJECT_TYPE"
    printf "   Skipping artifact layer SBOM generation\n"
    ;;
  esac

  printf "\n"
}

# Generate Container layer SBOMs
generate_container_layer() {
  local name=$1
  local version=$2

  printf "📦 Generating Container layer SBOMs...\n"

  if [[ -z "$CONTAINER_IMAGE" ]]; then
    printf "   ⚠️  No container image specified, skipping container layer\n"
    printf "\n"
    return
  fi

  printf "   Scanning container image: %s\n" "$CONTAINER_IMAGE"
  generate_dual_sboms "$CONTAINER_IMAGE" "$name" "$version" "container" "${name}-${version}-container"
  printf "\n"
}

# Main execution
main() {
  install_syft
  detect_project_type

  local version
  local project_name
  version=$(get_version)
  project_name=$(get_project_name)

  printf "Project Information:\n"
  printf "  Name: %s\n" "$project_name"
  printf "  Version: %s\n" "$version"
  printf "  Type: %s\n" "$PROJECT_TYPE"
  printf "\n"

  # Parse requested layers (comma-separated)
  # Supported layer names:
  #   Layer 1 (Source):   source
  #   Layer 2 (Artifact): artifact
  #   Layer 3 (Runtime):  containerimage
  IFS=',' read -ra LAYER_ARRAY <<<"$LAYERS"

  for layer in "${LAYER_ARRAY[@]}"; do
    # Trim whitespace
    layer=$(printf "%s" "$layer" | xargs)

    case "$layer" in
    source)
      generate_source_layer "$project_name" "$version"
      ;;
    artifact)
      generate_artifact_layer "$project_name" "$version"
      ;;
    containerimage)
      generate_container_layer "$project_name" "$version"
      ;;
    *)
      printf "⚠️  Unknown layer type: %s\n" "$layer"
      printf "   Valid options: source, artifact, containerimage\n"
      printf "\n"
      return 1
      ;;
    esac
  done

  printf "================================================\n"
  printf "SBOM Generation Complete\n"
  printf "================================================\n"
  printf "\n"

  # Summary statistics
  local sbom_count
  sbom_count=$(find . -maxdepth 1 -name '*-sbom.*.json' -type f 2>/dev/null | wc -l)

  if [[ "$sbom_count" -gt 0 ]]; then
    printf "✅ Successfully generated %s SBOM files\n" "$sbom_count"
    printf "\n"
    printf "Generated files:\n"
    find . -maxdepth 1 -name '*-sbom.*.json' -type f -exec ls -lh {} +
    printf "\n"

    # Create ZIP archive with all SBOMs (if requested)
    if [[ "$CREATE_ZIP" = "true" ]]; then
      printf "📦 Creating SBOM ZIP archive...\n"
      local sbom_zip="${project_name}-${version}-sboms.zip"

      # Add all SBOM files to ZIP
      find . -maxdepth 1 -name '*-sbom.*.json' -type f -exec zip "$sbom_zip" {} +

      if [[ -f "$sbom_zip" ]]; then
        printf "✅ Created SBOM ZIP: %s\n" "$sbom_zip"
        printf "\n"
        printf "ZIP contents:\n"
        unzip -l "$sbom_zip"
      else
        printf "⚠️ Failed to create SBOM ZIP\n"
      fi
      printf "\n"
    fi
  else
    printf "❌ No SBOM files generated\n"
    return 1
  fi
  printf "\n"
}

# Run main function
main
