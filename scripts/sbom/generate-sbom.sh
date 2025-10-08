#!/bin/bash
# SPDX-FileCopyrightText: 2025 The Reusable CI Authors
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

echo "================================================"
echo "SBOM Generation Script"
echo "================================================"
echo "Working directory: $(pwd)"
echo "Project type: $PROJECT_TYPE"
echo "Requested layers: $LAYERS"
echo ""

# Install Syft if not available
install_syft() {
  if ! command -v syft &>/dev/null; then
    echo "Installing Syft SBOM generator..."
    curl -sSfL https://raw.githubusercontent.com/anchore/syft/main/install.sh | sh -s -- -b /tmp
    export PATH="/tmp:$PATH"
    echo "✅ Syft installed successfully"
  else
    echo "✅ Syft already installed: $(syft version --output json | grep -o '"version":"[^"]*"' | cut -d'"' -f4)"
  fi
  echo ""
}

# Generate both SPDX and CycloneDX SBOMs for a target
generate_dual_sboms() {
  local scan_target=$1
  local name=$2
  local version=$3
  local layer_name=$4
  local custom_basename=$5

  local spdx_file cdx_file

  if [ -n "$custom_basename" ]; then
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
  if [ -f "$spdx_file" ] && [ -s "$spdx_file" ]; then
    echo "   ✅ $spdx_file"
  else
    echo "   ❌ Failed to generate: $spdx_file" >&2
    return 1
  fi

  if [ -f "$cdx_file" ] && [ -s "$cdx_file" ]; then
    echo "   ✅ $cdx_file"
  else
    echo "   ❌ Failed to generate: $cdx_file" >&2
    return 1
  fi
}

# Auto-detect project type from build files
detect_project_type() {
  if [ "$PROJECT_TYPE" = "auto" ]; then
    if [ -f "pom.xml" ]; then
      PROJECT_TYPE="maven"
    elif [ -f "package.json" ]; then
      PROJECT_TYPE="npm"
    elif [ -f "build.gradle" ] || [ -f "build.gradle.kts" ]; then
      PROJECT_TYPE="gradle"
    else
      PROJECT_TYPE="unknown"
    fi
    echo "Auto-detected project type: $PROJECT_TYPE"
  fi
}

# Extract version from project files
get_version() {
  if [ -n "$VERSION" ]; then
    echo "$VERSION"
    return
  fi

  local detected_version=""

  case "$PROJECT_TYPE" in
  maven)
    if [ -f "pom.xml" ]; then
      if detected_version=$(mvn help:evaluate -Dexpression=project.version -q -DforceStdout 2>&1); then
        echo "$detected_version"
        return
      else
        echo "⚠️  Warning: Could not extract version from pom.xml" >&2
      fi
    fi
    ;;
  npm)
    if [ -f "package.json" ]; then
      if detected_version=$(node -p "require('./package.json').version" 2>&1); then
        echo "$detected_version"
        return
      else
        echo "⚠️  Warning: Could not extract version from package.json" >&2
      fi
    fi
    ;;
  gradle)
    if [ -f "build.gradle" ]; then
      if detected_version=$(grep -oP "version\s*=\s*['\"]?\K[^'\"]*" build.gradle 2>&1 | head -1); then
        echo "$detected_version"
        return
      else
        echo "⚠️  Warning: Could not extract version from build.gradle" >&2
      fi
    fi
    ;;
  esac

  echo "unknown"
}

# Extract project name from project files
get_project_name() {
  if [ -n "$PROJECT_NAME" ]; then
    echo "$PROJECT_NAME"
    return
  fi

  local detected_name=""

  case "$PROJECT_TYPE" in
  maven)
    if [ -f "pom.xml" ]; then
      if detected_name=$(mvn help:evaluate -Dexpression=project.artifactId -q -DforceStdout 2>&1); then
        echo "$detected_name"
        return
      else
        echo "⚠️  Warning: Could not extract artifactId from pom.xml" >&2
      fi
    fi
    ;;
  npm)
    if [ -f "package.json" ]; then
      if detected_name=$(node -p "require('./package.json').name" 2>&1 | sed 's/@.*\///'); then
        echo "$detected_name"
        return
      else
        echo "⚠️  Warning: Could not extract name from package.json" >&2
      fi
    fi
    ;;
  gradle)
    if [ -f "settings.gradle" ]; then
      if detected_name=$(grep -oP "rootProject.name\s*=\s*['\"]?\K[^'\"]*" settings.gradle 2>&1); then
        echo "$detected_name"
        return
      else
        echo "⚠️  Warning: Could not extract name from settings.gradle" >&2
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

  echo "📦 Generating Source layer SBOMs..."

  if [ -f "pom.xml" ]; then
    echo "   Scanning pom.xml for Maven dependencies..."
    generate_dual_sboms "pom.xml" "$name" "$version" "pom" "${name}-${version}-pom"
  elif [ -f "package.json" ]; then
    echo "   Scanning package.json for NPM dependencies..."
    generate_dual_sboms "." "$name" "$version" "package" "${name}-${version}-package"
  elif [ -f "build.gradle" ] || [ -f "build.gradle.kts" ]; then
    echo "   Scanning build.gradle for Gradle dependencies..."
    generate_dual_sboms "." "$name" "$version" "gradle" "${name}-${version}-gradle"
  else
    echo "   ⚠️  No build manifest found (pom.xml, package.json, build.gradle)"
  fi
  echo ""
}

# Generate Artifact layer SBOMs (Maven JARs, NPM tar archives, Gradle JARs, etc.)
generate_artifact_layer() {
  local name=$1
  local version=$2
  local layer_name

  echo "📦 Generating Artifact layer SBOMs..."

  # Handle different project types
  case "$PROJECT_TYPE" in
  maven)
    echo "   Project type: Maven (looking for JAR files)"

    # Maven JARs can be in multiple locations:
    # 1. target/ directory (from local build or JReleaser workflow)
    # 2. ./release-artifacts/ (downloaded from GitHub CLI workflow artifact)
    local artifacts=()

    # Collect all JARs from release-artifacts (common in GitHub CLI release workflows)
    if [ -d "./release-artifacts" ]; then
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
    if [ ${#artifacts[@]} -eq 0 ] && [ -d "target" ]; then
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

    if [ ${#artifacts[@]} -gt 0 ]; then
      for artifact in "${artifacts[@]}"; do
        echo "   Scanning JAR file: $artifact"
        local jar_basename
        jar_basename=$(basename "$artifact" .jar)
        generate_dual_sboms "$artifact" "$name" "$version" "jar" "${jar_basename}-jar"
      done
    else
      echo "   ⚠️  No JAR files found in target/ or ./release-artifacts/ directories"
      echo "      Searched for: ${name}.jar or ${name}-*.jar"
    fi
    ;;

  npm)
    echo "   Project type: NPM (looking for tar archive files)"

    # NPM tar archives can be in multiple locations:
    # 1. Current directory (from npm pack)
    # 2. ./release-artifacts/ (downloaded from workflow artifact)
    local artifacts=()

    # Collect all tgz files from release-artifacts (common in release workflows)
    if [ -d "./release-artifacts" ]; then
      while IFS= read -r -d '' artifact; do
        artifacts+=("$artifact")
      done < <(find ./release-artifacts -maxdepth 1 -name "*.tgz" -type f -print0 2>/dev/null)
    fi

    # Fallback to current directory if no artifacts found yet
    if [ ${#artifacts[@]} -eq 0 ]; then
      while IFS= read -r -d '' artifact; do
        artifacts+=("$artifact")
      done < <(find . -maxdepth 1 -name "*.tgz" -type f -print0 2>/dev/null)
    fi

    if [ ${#artifacts[@]} -gt 0 ]; then
      for artifact in "${artifacts[@]}"; do
        echo "   Scanning NPM tar archive: $artifact"
        local tgz_basename
        tgz_basename=$(basename "$artifact" .tgz)
        generate_dual_sboms "$artifact" "$name" "$version" "tararchive" "${tgz_basename}-tararchive"
      done
    else
      echo "   ⚠️  No NPM tar archive (.tgz) found"
      echo "      Searched in: current directory and ./release-artifacts/"
    fi
    ;;

  gradle)
    echo "   Project type: Gradle (looking for JAR files)"

    # Gradle builds to build/libs/
    if [ ! -d "build/libs" ]; then
      echo "   ⚠️  No build/libs/ directory found, skipping JAR layer"
      echo ""
      return
    fi

    local artifacts=()
    while IFS= read -r -d '' artifact; do
      artifacts+=("$artifact")
    done < <(find build/libs -maxdepth 1 -type f -name "${name}-*.jar" \
      ! -name "*-sources.jar" \
      ! -name "*-javadoc.jar" \
      -print0 2>/dev/null)

    if [ ${#artifacts[@]} -gt 0 ]; then
      for artifact in "${artifacts[@]}"; do
        echo "   Scanning JAR file: $artifact"
        local jar_basename
        jar_basename=$(basename "$artifact" .jar)
        generate_dual_sboms "$artifact" "$name" "$version" "jar" "${jar_basename}-jar"
      done
    else
      echo "   ⚠️  No JAR files found in build/libs/ directory"
    fi
    ;;

  *)
    echo "   ⚠️  Unknown project type: $PROJECT_TYPE"
    echo "   Skipping artifact layer SBOM generation"
    ;;
  esac

  echo ""
}

# Generate Container layer SBOMs
generate_container_layer() {
  local name=$1
  local version=$2

  echo "📦 Generating Container layer SBOMs..."

  if [ -z "$CONTAINER_IMAGE" ]; then
    echo "   ⚠️  No container image specified, skipping container layer"
    echo ""
    return
  fi

  echo "   Scanning container image: $CONTAINER_IMAGE"
  generate_dual_sboms "$CONTAINER_IMAGE" "$name" "$version" "container" "${name}-${version}-container"
  echo ""
}

# Main execution
main() {
  install_syft
  detect_project_type

  local version
  local project_name
  version=$(get_version)
  project_name=$(get_project_name)

  echo "Project Information:"
  echo "  Name: $project_name"
  echo "  Version: $version"
  echo "  Type: $PROJECT_TYPE"
  echo ""

  # Parse requested layers (comma-separated)
  # Supported layer names:
  #   Layer 1 (Source):   source
  #   Layer 2 (Artifact): artifact
  #   Layer 3 (Runtime):  containerimage
  IFS=',' read -ra LAYER_ARRAY <<<"$LAYERS"

  for layer in "${LAYER_ARRAY[@]}"; do
    # Trim whitespace
    layer=$(echo "$layer" | xargs)

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
      echo "⚠️  Unknown layer type: $layer"
      echo "   Valid options: source, artifact, containerimage"
      echo ""
      return 1
      ;;
    esac
  done

  echo "================================================"
  echo "SBOM Generation Complete"
  echo "================================================"
  echo ""

  # Summary statistics
  local sbom_count
  sbom_count=$(find . -maxdepth 1 -name '*-sbom.*.json' -type f 2>/dev/null | wc -l)

  if [ "$sbom_count" -gt 0 ]; then
    echo "✅ Successfully generated $sbom_count SBOM files"
    echo ""
    echo "Generated files:"
    find . -maxdepth 1 -name '*-sbom.*.json' -type f -exec ls -lh {} +
    echo ""

    # Create ZIP archive with all SBOMs (if requested)
    if [ "$CREATE_ZIP" = "true" ]; then
      echo "📦 Creating SBOM ZIP archive..."
      local sbom_zip="${project_name}-${version}-sboms.zip"

      # Add all SBOM files to ZIP
      find . -maxdepth 1 -name '*-sbom.*.json' -type f -exec zip "$sbom_zip" {} +

      if [ -f "$sbom_zip" ]; then
        echo "✅ Created SBOM ZIP: $sbom_zip"
        echo ""
        echo "ZIP contents:"
        unzip -l "$sbom_zip"
      else
        echo "⚠️ Failed to create SBOM ZIP"
      fi
      echo ""
    fi
  else
    echo "❌ No SBOM files generated"
    return 1
  fi
  echo ""
}

# Run main function
main
