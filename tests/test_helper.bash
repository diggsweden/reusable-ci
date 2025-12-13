#!/usr/bin/env bash
# shellcheck disable=SC2016,SC2154,SC2164,SC2268
# SPDX-FileCopyrightText: 2025 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0
#
# Shared test helper functions for BATS tests
# Inspired by devbase-core and devbase-justkit patterns
#
# Usage: load "${BATS_TEST_DIRNAME}/test_helper.bash"
#
# Shellcheck disabled:
#   SC2016 - Expressions don't expand in single quotes (intentional in mock scripts)
#   SC2154 - Variables like $output/$stderr are set by bats, not this script
#   SC2164 - cd without || exit is fine in test helpers (bats handles failures)
#   SC2268 - x-prefix in comparisons is a common bats pattern for empty checks
#
# Features:
#   - Git repository helpers (init, commits, remotes)
#   - GitHub Actions environment simulation
#   - Mock binary creation and path management
#   - File creation helpers
#   - Assertion helpers for stderr and output
#   - Debug output helpers
#   - Stub helpers for external commands (gh, curl, etc.)

# =============================================================================
# Common Setup/Teardown Helpers
# =============================================================================

# Standard test setup - creates temp dir and sets SCRIPTS_DIR
# Usage: common_setup
common_setup() {
  TEST_DIR="$(temp_make)"
  export TEST_DIR
  export SCRIPTS_DIR="${BATS_TEST_DIRNAME}/../scripts"
  cd "$TEST_DIR"
}

# Standard test teardown - returns to test dir and cleans up
# Usage: common_teardown
common_teardown() {
  cd "${BATS_TEST_DIRNAME}"
  # Use safe_temp_del which handles git's write-protected objects
  safe_temp_del "$TEST_DIR"
}

# Safely delete a temp directory, handling git's write-protected objects
# This wraps temp_del but makes files writable first to avoid interactive prompts
# SAFETY: Only deletes directories under /tmp or $BATS_TMPDIR
# Usage: safe_temp_del <path>
safe_temp_del() {
  local path="$1"
  [[ -z "$path" ]] && return 0
  [[ ! -d "$path" ]] && return 0

  # Resolve to absolute path
  local abs_path
  abs_path="$(cd "$path" 2>/dev/null && pwd)" || return 0

  # SAFETY: Only allow deletion in /tmp or BATS_TMPDIR
  local allowed_base="${BATS_TMPDIR:-/tmp}"
  if [[ "$abs_path" != /tmp/* && "$abs_path" != "$allowed_base"/* ]]; then
    echo "ERROR: safe_temp_del refuses to delete '$abs_path' - not in /tmp or BATS_TMPDIR" >&2
    return 1
  fi

  # Extra safety: refuse to delete if path is too short (e.g., /tmp itself)
  if [[ "${#abs_path}" -lt 10 ]]; then
    echo "ERROR: safe_temp_del refuses to delete '$abs_path' - path too short" >&2
    return 1
  fi

  # Make all files writable to avoid rm prompting on git objects
  chmod -R u+w "$abs_path" 2>/dev/null || true
  temp_del "$abs_path"
}

# Setup for tests that need GitHub Actions environment
# Usage: common_setup_with_github_env
common_setup_with_github_env() {
  common_setup
  setup_github_env
}

# Setup for tests that need git repository
# Usage: common_setup_with_git
common_setup_with_git() {
  common_setup
  init_git_repo
}

# Setup for tests that need isolated git repository
# Usage: common_setup_with_isolated_git
common_setup_with_isolated_git() {
  common_setup
  init_isolated_git_repo
}

# Setup with isolated HOME environment (no git)
# Usage: common_setup_isolated
common_setup_isolated() {
  common_setup
  setup_isolated_home
}

# =============================================================================
# Isolated Environment Setup
# =============================================================================

# Setup isolated HOME and XDG directories in TEST_DIR
# Usage: setup_isolated_home
# Sets: HOME, XDG_DATA_HOME, XDG_CONFIG_HOME
setup_isolated_home() {
  export HOME="${TEST_DIR}/home"
  export XDG_DATA_HOME="${HOME}/.local/share"
  export XDG_CONFIG_HOME="${HOME}/.config"
  mkdir -p "$HOME"
  mkdir -p "$XDG_DATA_HOME"
  mkdir -p "$XDG_CONFIG_HOME"
}

# =============================================================================
# Git Repository Setup Helpers
# =============================================================================

# Initialize a minimal git repository for testing
# Usage: init_git_repo
init_git_repo() {
  export GIT_CONFIG_NOSYSTEM=1
  git init -q
  git config user.email "test@example.com"
  git config user.name "Test User"
  # Make git objects writable so temp_del can clean up
  git config core.sharedRepository 0644
  echo "initial" >file.txt
  git add file.txt
  git commit -q -m "Initial commit"
}

# Initialize git repo with isolated HOME and config
# Usage: init_isolated_git_repo
init_isolated_git_repo() {
  setup_isolated_home
  export GIT_CONFIG_NOSYSTEM=1
  export GIT_CONFIG_GLOBAL="${HOME}/.gitconfig"
  init_git_repo
}

# Add a commit to the repository
# Usage: add_commit [message]
add_commit() {
  local msg="${1:-Change}"
  echo "$msg" >>file.txt
  git add file.txt
  git commit -q -m "$msg"
}

# Create a bare remote repository and link it
# Usage: init_remote_repo
# Sets REMOTE_DIR variable
init_remote_repo() {
  REMOTE_DIR="$(temp_make)"
  export REMOTE_DIR

  local current_dir
  current_dir=$(pwd)

  cd "$REMOTE_DIR"
  git init -q --bare --shared=0644

  cd "$current_dir"
  git remote add origin "$REMOTE_DIR"
  git push -q -u origin main 2>/dev/null || git push -q -u origin master 2>/dev/null || true
}

# =============================================================================
# GitHub Actions Environment Simulation
# =============================================================================

# Setup GitHub Actions environment variables
# Usage: setup_github_env
setup_github_env() {
  export GITHUB_OUTPUT="$TEST_DIR/github_output"
  export GITHUB_STEP_SUMMARY="$TEST_DIR/step_summary.md"
  export GITHUB_ENV="$TEST_DIR/github_env"
  touch "$GITHUB_OUTPUT"
  touch "$GITHUB_STEP_SUMMARY"
  touch "$GITHUB_ENV"
}

# Read a value from GITHUB_OUTPUT
# Usage: get_github_output <key>
get_github_output() {
  local key="$1"
  grep "^${key}=" "$GITHUB_OUTPUT" | cut -d'=' -f2-
}

# =============================================================================
# Mock Binary Helpers
# =============================================================================

# Create a mock binary in TEST_DIR/bin
# Usage: create_mock_binary <name> <script_content>
create_mock_binary() {
  local name="$1"
  local content="$2"

  mkdir -p "${TEST_DIR}/bin"
  cat >"${TEST_DIR}/bin/${name}" <<SCRIPT
#!/usr/bin/env bash
${content}
SCRIPT
  chmod +x "${TEST_DIR}/bin/${name}"
}

# Add mock binaries directory to PATH
# Usage: use_mock_path
use_mock_path() {
  export PATH="${TEST_DIR}/bin:${PATH}"
}

# Create a mock curl that returns a specific HTTP code
# Usage: mock_curl_response <http_code>
mock_curl_response() {
  local http_code="$1"
  create_mock_binary "curl" "printf '%s' '${http_code}'"
}

# =============================================================================
# File Creation Helpers
# =============================================================================

# Create a test file with content
# Usage: create_test_file <path> <content>
create_test_file() {
  local path="$1"
  local content="$2"
  local dir
  dir=$(dirname "$path")

  mkdir -p "$dir"
  printf '%s\n' "$content" >"$path"
}

# Create a test file with heredoc content
# Usage: create_test_file_heredoc <path> <<'EOF' ... EOF
create_test_file_heredoc() {
  local path="$1"
  local dir
  dir=$(dirname "$path")

  mkdir -p "$dir"
  cat >"$path"
}

# =============================================================================
# Assertion Helpers
# =============================================================================

# Assert that stderr contains a substring
# Usage: assert_stderr_contains <substring>
assert_stderr_contains() {
  local substring="$1"
  if [[ "$stderr" != *"$substring"* ]]; then
    printf "Expected stderr to contain: %s\n" "$substring" >&2
    printf "Actual stderr: %s\n" "$stderr" >&2
    return 1
  fi
}

# Assert that either stdout or stderr contains a substring
# Usage: assert_output_or_stderr_contains <substring>
assert_output_or_stderr_contains() {
  local substring="$1"
  if [[ "$output" == *"$substring"* ]] || [[ "$stderr" == *"$substring"* ]]; then
    return 0
  fi
  printf "Expected output or stderr to contain: %s\n" "$substring" >&2
  printf "Actual output: %s\n" "$output" >&2
  printf "Actual stderr: %s\n" "$stderr" >&2
  return 1
}

# =============================================================================
# Debug Helpers
# =============================================================================

# Standard debug output for failed tests
# Usage: debug_output (call after 'run' command)
debug_output() {
  [ "x$BATS_TEST_COMPLETED" = "x" ] && echo "o:'${output}' e:'${stderr}'"
}

# Debug with custom prefix
# Usage: debug_with_prefix <prefix>
debug_with_prefix() {
  local prefix="$1"
  [ "x$BATS_TEST_COMPLETED" = "x" ] && echo "${prefix}: o:'${output}' e:'${stderr}'"
}

# =============================================================================
# Cleanup Helpers
# =============================================================================

# Cleanup remote directory if it exists
# Usage: cleanup_remote
cleanup_remote() {
  if [[ -n "${REMOTE_DIR:-}" ]]; then
    safe_temp_del "$REMOTE_DIR"
  fi
}

# =============================================================================
# Stub Helpers (for bats-mock integration)
# =============================================================================

# Create repeated stub that always returns the same result
# Usage: stub_repeated <command> <behavior>
# Example: stub_repeated curl "echo '200'"
stub_repeated() {
  local cmd="$1"
  local behavior="$2"

  create_mock_binary "$cmd" "$behavior"
  use_mock_path
}

# Create stub for gh command with specific API responses
# Usage: stub_gh_api <endpoint_pattern> <response> [exit_code]
stub_gh_api() {
  local pattern="$1"
  local response="$2"
  local exit_code="${3:-0}"

  create_mock_binary "gh" "
case \"\$*\" in
  *\"$pattern\"*)
    printf '%s' '$response'
    exit $exit_code
    ;;
  *)
    exit 1
    ;;
esac
"
  use_mock_path
}

# Create a mock gh command that handles multiple endpoints
# Usage: create_gh_mock <<'SCRIPT' ... SCRIPT
create_gh_mock() {
  local script
  script=$(cat)
  create_mock_binary "gh" "$script"
  use_mock_path
}

# Create mock for mvn command
# Usage: mock_mvn_success
mock_mvn_success() {
  create_mock_binary "mvn" 'printf "BUILD SUCCESS\n"'
  use_mock_path
}

# Create mock for mvn command that fails
# Usage: mock_mvn_failure [message]
mock_mvn_failure() {
  local msg="${1:-BUILD FAILURE}"
  create_mock_binary "mvn" "printf '%s\n' '$msg'; exit 1"
  use_mock_path
}

# Create mock for npm command
# Usage: mock_npm_success
mock_npm_success() {
  create_mock_binary "npm" 'printf "npm success\n"'
  use_mock_path
}

# Create mock for npm command that fails
# Usage: mock_npm_failure [message]
mock_npm_failure() {
  local msg="${1:-npm ERR!}"
  create_mock_binary "npm" "printf '%s\n' '$msg'; exit 1"
  use_mock_path
}

# Create mock for gradle wrapper
# Usage: mock_gradlew_success
mock_gradlew_success() {
  cat >"${TEST_DIR}/gradlew" <<'SCRIPT'
#!/usr/bin/env bash
printf "BUILD SUCCESSFUL\n"
SCRIPT
  chmod +x "${TEST_DIR}/gradlew"
}

# Create mock for gradle wrapper that fails
# Usage: mock_gradlew_failure [message]
mock_gradlew_failure() {
  local msg="${1:-BUILD FAILED}"
  cat >"${TEST_DIR}/gradlew" <<SCRIPT
#!/usr/bin/env bash
printf '%s\n' '$msg'
exit 1
SCRIPT
  chmod +x "${TEST_DIR}/gradlew"
}

# =============================================================================
# GitHub Output Helpers
# =============================================================================

# Assert that GITHUB_OUTPUT contains a specific key=value
# Usage: assert_github_output <key> <expected_value>
assert_github_output() {
  local key="$1"
  local expected="$2"
  local actual

  actual=$(get_github_output "$key")
  if [[ "$actual" != "$expected" ]]; then
    printf "Expected GITHUB_OUTPUT[%s] = '%s'\n" "$key" "$expected" >&2
    printf "Actual GITHUB_OUTPUT[%s] = '%s'\n" "$key" "$actual" >&2
    return 1
  fi
}

# Assert that GITHUB_OUTPUT contains a key (any value)
# Usage: assert_github_output_exists <key>
assert_github_output_exists() {
  local key="$1"

  if ! grep -q "^${key}=" "$GITHUB_OUTPUT"; then
    printf "Expected GITHUB_OUTPUT to contain key '%s'\n" "$key" >&2
    printf "Actual GITHUB_OUTPUT contents:\n" >&2
    cat "$GITHUB_OUTPUT" >&2
    return 1
  fi
}

# Assert GITHUB_STEP_SUMMARY contains text
# Usage: assert_summary_contains <text>
assert_summary_contains() {
  local text="$1"

  if [[ ! -f "$GITHUB_STEP_SUMMARY" ]] || ! grep -q "$text" "$GITHUB_STEP_SUMMARY"; then
    printf "Expected GITHUB_STEP_SUMMARY to contain: %s\n" "$text" >&2
    printf "Actual contents:\n" >&2
    cat "$GITHUB_STEP_SUMMARY" 2>/dev/null || printf "(file not found)\n" >&2
    return 1
  fi
}

# Read GITHUB_STEP_SUMMARY contents
# Usage: get_summary
get_summary() {
  cat "$GITHUB_STEP_SUMMARY"
}

# =============================================================================
# Artifact Creation Helpers
# =============================================================================

# Create a mock JAR artifact
# Usage: create_jar_artifact <dir> <name> [content]
create_jar_artifact() {
  local dir="$1"
  local name="$2"
  local content="${3:-mock jar content}"

  mkdir -p "$dir"
  printf '%s' "$content" >"$dir/$name"
}

# Create release artifacts directory with common files
# Usage: create_release_artifacts [project_name] [version]
create_release_artifacts() {
  local project="${1:-myapp}"
  local version="${2:-1.0.0}"

  mkdir -p release-artifacts
  printf 'jar content' >"release-artifacts/${project}-${version}.jar"
}

# Create SBOM artifacts
# Usage: create_sbom_artifacts [project_name] [version]
create_sbom_artifacts() {
  local project="${1:-myapp}"
  local version="${2:-1.0.0}"

  mkdir -p sbom-artifacts
  printf '{"spdx": "content"}' >"sbom-artifacts/${project}-container-sbom.spdx.json"
}

# Create checksum file
# Usage: create_checksums_file [filename]
create_checksums_file() {
  local filename="${1:-checksums.sha256}"

  printf 'abc123def456  file1.jar\n' >"$filename"
  printf '789xyz000111  file2.jar\n' >>"$filename"
}

# =============================================================================
# Container/Dockerfile Helpers
# =============================================================================

# Create a Containerfile with specified content
# Usage: create_containerfile <content>
create_containerfile() {
  local content="$1"
  printf '%s\n' "$content" >Containerfile
}

# Create a Containerfile that rebuilds from source (for testing warnings)
# Usage: create_rebuild_containerfile <type>
# Types: maven, npm, gradle
create_rebuild_containerfile() {
  local type="$1"

  case "$type" in
  maven)
    create_containerfile "FROM maven:3
COPY . .
RUN mvn clean package"
    ;;
  npm)
    create_containerfile "FROM node:18
COPY . .
RUN npm run build"
    ;;
  gradle)
    create_containerfile "FROM gradle:8
COPY . .
RUN gradle build"
    ;;
  esac
}

# Create a Containerfile that uses pre-built artifacts (best practice)
# Usage: create_artifact_containerfile
create_artifact_containerfile() {
  create_containerfile "FROM eclipse-temurin:21-jre
COPY target/*.jar /app/app.jar
CMD [\"java\", \"-jar\", \"/app/app.jar\"]"
}

# =============================================================================
# Project Structure Helpers
# =============================================================================

# Create a Maven project structure
# Usage: create_maven_project [with_jar]
create_maven_project() {
  local with_jar="${1:-false}"

  cat >pom.xml <<'EOF'
<project>
  <modelVersion>4.0.0</modelVersion>
  <groupId>com.example</groupId>
  <artifactId>myapp</artifactId>
  <version>1.0.0</version>
</project>
EOF

  if [[ "$with_jar" == "true" ]]; then
    mkdir -p target
    printf 'jar content' >target/myapp-1.0.0.jar
  fi
}

# Create an NPM project structure
# Usage: create_npm_project [with_dist]
create_npm_project() {
  local with_dist="${1:-false}"

  cat >package.json <<'EOF'
{
  "name": "myapp",
  "version": "1.0.0",
  "scripts": {
    "build": "echo build"
  }
}
EOF

  if [[ "$with_dist" == "true" ]]; then
    mkdir -p dist
    printf 'built content' >dist/index.js
  fi
}

# Create a Gradle project structure
# Usage: create_gradle_project [with_jar]
create_gradle_project() {
  local with_jar="${1:-false}"

  cat >build.gradle <<'EOF'
plugins {
    id 'java'
}
group = 'com.example'
version = '1.0.0'
EOF

  # Create gradle wrapper script
  cat >gradlew <<'EOF'
#!/usr/bin/env bash
echo "Gradle wrapper"
EOF
  chmod +x gradlew

  if [[ "$with_jar" == "true" ]]; then
    mkdir -p build/libs
    printf 'jar content' >build/libs/myapp-1.0.0.jar
  fi
}

# =============================================================================
# Script Runner Helpers
# =============================================================================

# Create a standard script runner function
# Usage: In test file, call: create_script_runner "validation/validate-auth.sh"
#        Then use: run_script "arg1" "arg2"
# Note: This is a template - copy and customize in each test file

# Generic script runner with debug output
# Usage: run_script <script_path> [args...]
run_script() {
  local script_path="$1"
  shift
  run --separate-stderr "$SCRIPTS_DIR/$script_path" "$@"
  debug_output
}

# =============================================================================
# Mock Syft Helper (for SBOM tests)
# =============================================================================

# Create a mock syft command that generates fake SBOMs
# Usage: create_mock_syft
create_mock_syft() {
  create_mock_binary "syft" '
OUTPUT_FORMAT=""
TARGET=""
while [[ $# -gt 0 ]]; do
  case "$1" in
    -o) OUTPUT_FORMAT="$2"; shift 2 ;;
    *) TARGET="$1"; shift ;;
  esac
done

case "$OUTPUT_FORMAT" in
  spdx-json)
    printf "{\"spdxVersion\": \"SPDX-2.3\", \"name\": \"mock-sbom\", \"target\": \"%s\"}\n" "$TARGET"
    ;;
  cyclonedx-json)
    printf "{\"bomFormat\": \"CycloneDX\", \"specVersion\": \"1.4\", \"target\": \"%s\"}\n" "$TARGET"
    ;;
  *)
    printf "Mock syft output for: %s\n" "$TARGET"
    ;;
esac
'
  create_mock_binary "curl" 'exit 0'
  use_mock_path
}

# =============================================================================
# Go/Rust/Python Project Helpers
# =============================================================================

# Create a Go project structure
# Usage: create_go_project
create_go_project() {
  cat >go.mod <<'EOF'
module github.com/example/myapp

go 1.21
EOF
}

# Create a Rust project structure
# Usage: create_rust_project
create_rust_project() {
  cat >Cargo.toml <<'EOF'
[package]
name = "myapp"
version = "1.0.0"
EOF
}

# Create a Python project structure
# Usage: create_python_project [type]
# Types: pyproject (default), requirements, setup
create_python_project() {
  local type="${1:-pyproject}"

  case "$type" in
  pyproject)
    cat >pyproject.toml <<'EOF'
[project]
name = "myapp"
version = "1.0.0"
EOF
    ;;
  requirements)
    echo "requests==2.28.0" >requirements.txt
    ;;
  setup)
    cat >setup.py <<'EOF'
from setuptools import setup
setup(name="myapp", version="1.0.0")
EOF
    ;;
  esac
}
