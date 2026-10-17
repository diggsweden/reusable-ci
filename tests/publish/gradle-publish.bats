#!/usr/bin/env bats

# shellcheck disable=SC1090,SC2016,SC2030,SC2031,SC2119,SC2120,SC2155
# SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

bats_require_minimum_version 1.13.0

load "${BATS_TEST_DIRNAME}/../libs/bats-support/load.bash"
load "${BATS_TEST_DIRNAME}/../libs/bats-assert/load.bash"
load "${BATS_TEST_DIRNAME}/../libs/bats-file/load.bash"
load "${BATS_TEST_DIRNAME}/../test_helper.bash"

readonly DEPLOYMENT_ID="e3b0c442-98fc-1c14-9afb-f4c8996fb924"

setup() {
  common_setup_with_github_env
  mkdir -p "$TEST_DIR/bin"

  # Fake gradlew records its arguments and the Gradle project properties it
  # received. With CENTRAL_BUNDLE_DIR set it publishes a signed artifact
  # there, as the bundle repository would; FAKE_UNSIGNED leaves out the
  # signature and FAKE_EMPTY publishes nothing.
  cat >"$TEST_DIR/gradlew" <<'SCRIPT'
#!/usr/bin/env bash
if [[ "$*" == "-q properties --no-daemon" ]]; then
  printf 'group: se.digg\nversion: %s\n' "${FAKE_VERSION:-1.0.0}"
  exit 0
fi
printf '%s\n' "$*" >"$TEST_DIR/gradlew.args"
env | grep '^ORG_GRADLE_PROJECT_' | sort >"$TEST_DIR/gradlew.env" || true
env | grep '^CENTRAL_REPOSITORY_' | sort >"$TEST_DIR/gradlew.repo" || true
if [[ "${CENTRAL_REPOSITORY_NAME:-}" == "CentralBundle" && -z "${FAKE_EMPTY:-}" ]]; then
  dir="$CENTRAL_REPOSITORY_URL/se/digg/lib/1.0.0"
  mkdir -p "$dir"
  printf 'jar' >"$dir/lib-1.0.0.jar"
  printf 'md5' >"$dir/lib-1.0.0.jar.md5"
  [[ -n "${FAKE_UNSIGNED:-}" ]] || printf 'sig' >"$dir/lib-1.0.0.jar.asc"
  printf 'meta' >"$CENTRAL_REPOSITORY_URL/se/digg/lib/maven-metadata.xml"
fi
SCRIPT
  chmod +x "$TEST_DIR/gradlew"

  # Fake gpg checks that the passphrase arrives on stdin, not in argv.
  cat >"$TEST_DIR/bin/gpg" <<'SCRIPT'
#!/usr/bin/env bash
passphrase="$(cat)"
[[ "$passphrase" == "s3cret" ]] || exit 2
case "$*" in
  *"--passphrase-fd 0"*"--export-secret-keys ABCDEF0123456789"*) printf "ARMORED-KEY\n" ;;
  *) exit 3 ;;
esac
SCRIPT

  # Fake Central Portal. Checks that the token arrives on stdin as a curl
  # config line, keeps the uploaded bundle and answers status requests
  # with FAKE_STATE.
  cat >"$TEST_DIR/bin/curl" <<'SCRIPT'
#!/usr/bin/env bash
config="$(cat)"
expected="$(printf 'central-user:central-pass' | base64 -w0)"
[[ "$config" == "header = \"Authorization: Bearer ${expected}\"" ]] || exit 22
printf '%s\n' "$*" >>"$TEST_DIR/curl.calls"
url="${!#}"
case "$url" in
  https://central.sonatype.com/api/v1/publisher/upload\?publishingType=*)
    if [[ -n "${FAKE_UPLOAD_FAILURE:-}" ]]; then
      printf '%s' "$FAKE_UPLOAD_FAILURE"
      exit 22
    fi
    for arg in "$@"; do
      [[ "$arg" == bundle=@* ]] && cp "${arg#bundle=@}" "$TEST_DIR/bundle.zip"
    done
    printf '%s' "${FAKE_UPLOAD_RESPONSE:-e3b0c442-98fc-1c14-9afb-f4c8996fb924}"
    ;;
  https://central.sonatype.com/api/v1/publisher/status\?id=*)
    failures="$(cat "$TEST_DIR/status.failures" 2>/dev/null || printf 0)"
    if ((failures < ${FAKE_STATUS_FAILURES:-0})); then
      printf '%s' "$((failures + 1))" >"$TEST_DIR/status.failures"
      printf 'Bad Gateway'
      exit 22
    fi
    # FAKE_STATES steps through one state per call and then stays on the last.
    read -ra states <<<"${FAKE_STATES:-${FAKE_STATE:-VALIDATED}}"
    calls="$(cat "$TEST_DIR/status.calls" 2>/dev/null || printf 0)"
    printf '%s' "$((calls + 1))" >"$TEST_DIR/status.calls"
    index=$((calls < ${#states[@]} ? calls : ${#states[@]} - 1))
    printf '{"deploymentState":"%s","errors":{"pom":["missing license"]}}' "${states[$index]}"
    ;;
  *) exit 22 ;;
esac
SCRIPT
  chmod +x "$TEST_DIR/bin/gpg" "$TEST_DIR/bin/curl"
  use_mock_path

  unset PUBLISH_TASKS GITHUB_ACTOR GITHUB_TOKEN MAVEN_CENTRAL_USERNAME \
    MAVEN_CENTRAL_PASSWORD RELEASE_GPG_PASSPHRASE GPG_FINGERPRINT GPG_KEY_ID
  export CENTRAL_POLL_INTERVAL=0
  # Keeps a test that never reaches its end state from hanging the suite.
  export CENTRAL_WAIT_SECONDS=5
}

teardown() {
  common_teardown
}

set_github_env() {
  export PUBLISH_TARGET="github-packages"
  export GITHUB_ACTOR="octocat"
  export GITHUB_TOKEN="ghs_token"
}

set_central_env() {
  export PUBLISH_TARGET="maven-central"
  export MAVEN_CENTRAL_USERNAME="central-user"
  export MAVEN_CENTRAL_PASSWORD="central-pass"
  export RELEASE_GPG_PASSPHRASE="s3cret"
  export GPG_FINGERPRINT="ABCDEF0123456789"
  export GPG_KEY_ID="1122334455667788"
}

@test "gradle-publish runs the GitHubPackages task with registry credentials" {
  set_github_env

  run_script "publish/gradle-publish.sh"

  assert_success
  assert_file_contains "$TEST_DIR/gradlew.args" "^publishAllPublicationsToGitHubPackagesRepository --no-daemon$"
  run cat "$TEST_DIR/gradlew.env"
  assert_output "ORG_GRADLE_PROJECT_githubActor=octocat
ORG_GRADLE_PROJECT_githubToken=ghs_token"
  assert_file_not_exists "$TEST_DIR/curl.calls"
}

@test "gradle-publish builds the Central bundle with an init script and signing properties" {
  set_central_env

  run_script "publish/gradle-publish.sh"

  assert_success
  assert_file_contains "$TEST_DIR/gradlew.args" "^--init-script .*central-bundle.init.gradle.kts publishAllPublicationsToCentralBundleRepository --no-daemon$"
  run cat "$TEST_DIR/gradlew.env"
  assert_output "ORG_GRADLE_PROJECT_mavenCentralPassword=central-pass
ORG_GRADLE_PROJECT_mavenCentralUsername=central-user
ORG_GRADLE_PROJECT_signingInMemoryKey=ARMORED-KEY
ORG_GRADLE_PROJECT_signingInMemoryKeyId=55667788
ORG_GRADLE_PROJECT_signingInMemoryKeyPassword=s3cret
ORG_GRADLE_PROJECT_signingKey=ARMORED-KEY
ORG_GRADLE_PROJECT_signingKeyId=55667788
ORG_GRADLE_PROJECT_signingPassword=s3cret"
}

@test "gradle-publish uploads the bundle as USER_MANAGED without repository metadata" {
  set_central_env

  run_script "publish/gradle-publish.sh"

  assert_success
  assert_output --partial "Deployment ${DEPLOYMENT_ID} is VALIDATED"
  run unzip -Z1 "$TEST_DIR/bundle.zip"
  assert_output --partial "se/digg/lib/1.0.0/lib-1.0.0.jar.asc"
  refute_output --partial "maven-metadata.xml"
}

@test "gradle-publish waits for an AUTOMATIC deployment to be published" {
  set_central_env
  export CENTRAL_PUBLISHING_TYPE="AUTOMATIC"
  export FAKE_STATES="VALIDATING VALIDATED PUBLISHING PUBLISHED"

  run_script "publish/gradle-publish.sh"

  assert_success
  assert_output --partial "Deployment ${DEPLOYMENT_ID} is PUBLISHING, waiting"
  assert_output --partial "Deployment ${DEPLOYMENT_ID} is PUBLISHED"
}

@test "gradle-publish fails when an AUTOMATIC deployment fails while publishing" {
  set_central_env
  export CENTRAL_PUBLISHING_TYPE="AUTOMATIC"
  export FAKE_STATES="VALIDATED PUBLISHING FAILED"

  run_script "publish/gradle-publish.sh"

  assert_failure
  assert_output --partial "Central Portal rejected deployment ${DEPLOYMENT_ID}"
}

@test "gradle-publish warns when a validated AUTOMATIC deployment is still publishing at the deadline" {
  set_central_env
  export CENTRAL_PUBLISHING_TYPE="AUTOMATIC"
  export FAKE_STATE="PUBLISHING"
  export CENTRAL_WAIT_SECONDS=0

  run_script "publish/gradle-publish.sh"

  assert_success
  assert_output --partial "is validated but still PUBLISHING"
}

@test "gradle-publish fails when the Portal rejects the deployment" {
  set_central_env
  export FAKE_STATE="FAILED"

  run_script "publish/gradle-publish.sh"

  assert_failure
  assert_output --partial "Central Portal rejected deployment ${DEPLOYMENT_ID}"
  assert_output --partial "missing license"
  refute_output --partial "Published to"
}

@test "gradle-publish fails when validation outlasts the wait" {
  set_central_env
  export FAKE_STATE="VALIDATING"
  export CENTRAL_WAIT_SECONDS=0

  run_script "publish/gradle-publish.sh"

  assert_failure
  assert_output --partial "was not validated within 0s"
  refute_output --partial "Published to"
}

@test "gradle-publish retries a failed status request" {
  set_central_env
  export FAKE_STATUS_FAILURES=1

  run_script "publish/gradle-publish.sh"

  assert_success
  assert_output --partial "Status request for deployment ${DEPLOYMENT_ID} failed, retrying"
  assert_output --partial "is VALIDATED"
}

@test "gradle-publish shows the Portal error when the upload fails" {
  set_central_env
  export FAKE_UPLOAD_FAILURE="Invalid token"

  run_script "publish/gradle-publish.sh"

  assert_failure
  assert_output --partial "Central Portal upload failed: Invalid token"
}

@test "gradle-publish uploads as AUTOMATIC when asked" {
  set_central_env
  export CENTRAL_PUBLISHING_TYPE="AUTOMATIC"
  export FAKE_STATE="PUBLISHED"

  run_script "publish/gradle-publish.sh"

  assert_success
  assert_file_contains "$TEST_DIR/curl.calls" "publishingType=AUTOMATIC"
}

@test "gradle-publish rejects an unknown publishing type" {
  set_central_env
  export CENTRAL_PUBLISHING_TYPE="SOMETIMES"

  run_script "publish/gradle-publish.sh"

  assert_failure
  assert_output --partial "Unknown CENTRAL_PUBLISHING_TYPE 'SOMETIMES'"
  assert_file_not_exists "$TEST_DIR/curl.calls"
}

@test "gradle-publish deploys a SNAPSHOT version to the Central snapshot repository" {
  set_central_env
  export RELEASE_TAG="v1.2.0-SNAPSHOT"
  export FAKE_VERSION="1.2.0-SNAPSHOT"

  run_script "publish/gradle-publish.sh"

  assert_success
  assert_file_contains "$TEST_DIR/gradlew.args" "^--init-script .*central-snapshots.init.gradle.kts publishAllPublicationsToCentralSnapshotsRepository --no-daemon$"
  run cat "$TEST_DIR/gradlew.repo"
  assert_output "CENTRAL_REPOSITORY_NAME=CentralSnapshots
CENTRAL_REPOSITORY_PASSWORD=central-pass
CENTRAL_REPOSITORY_URL=https://central.sonatype.com/repository/maven-snapshots/
CENTRAL_REPOSITORY_USERNAME=central-user"
  assert_file_not_exists "$TEST_DIR/curl.calls"
}

@test "gradle-publish refuses a SNAPSHOT tag with a release version" {
  set_central_env
  export RELEASE_TAG="v1.2.0-SNAPSHOT"
  export FAKE_VERSION="1.2.0"

  run_script "publish/gradle-publish.sh"

  assert_failure
  assert_output --partial "SNAPSHOT tag v1.2.0-SNAPSHOT has release version 1.2.0"
  assert_file_not_exists "$TEST_DIR/curl.calls"
  assert_file_not_exists "$TEST_DIR/gradlew.args"
}

@test "gradle-publish sends a SNAPSHOT version under a production tag to the snapshot repository" {
  set_central_env
  export RELEASE_TAG="v1.2.0"
  export FAKE_VERSION="1.2.0-SNAPSHOT"

  run_script "publish/gradle-publish.sh"

  assert_success
  assert_file_contains "$TEST_DIR/gradlew.args" "publishAllPublicationsToCentralSnapshotsRepository"
  assert_file_not_exists "$TEST_DIR/curl.calls"
}

@test "gradle-publish fails when the project version cannot be read" {
  set_central_env
  export FAKE_VERSION="unspecified"

  run_script "publish/gradle-publish.sh"

  assert_failure
  assert_output --partial "Could not read the project version from Gradle"
}

@test "gradle-publish gives the bundle repository no credentials" {
  set_central_env

  run_script "publish/gradle-publish.sh"

  assert_success
  assert_file_not_contains "$TEST_DIR/gradlew.repo" "CENTRAL_REPOSITORY_USERNAME"
}

@test "gradle-publish runs multi-line publish-tasks in full" {
  set_github_env
  export PUBLISH_TASKS=":lib:publishX
:cli:publishX"

  run_script "publish/gradle-publish.sh"

  assert_success
  assert_file_contains "$TEST_DIR/gradlew.args" "^:lib:publishX :cli:publishX --no-daemon$"
}

@test "gradle-publish refuses an unsigned bundle before upload" {
  set_central_env
  export FAKE_UNSIGNED=1

  run_script "publish/gradle-publish.sh"

  assert_failure
  assert_output --partial "The publications are not signed"
  assert_file_not_exists "$TEST_DIR/curl.calls"
}

@test "gradle-publish refuses an empty bundle before upload" {
  set_central_env
  export FAKE_EMPTY=1

  run_script "publish/gradle-publish.sh"

  assert_failure
  assert_output --partial "Gradle published nothing"
  assert_file_not_exists "$TEST_DIR/curl.calls"
}

@test "gradle-publish rejects an unexpected upload response" {
  set_central_env
  export FAKE_UPLOAD_RESPONSE="<html>error</html>"

  run_script "publish/gradle-publish.sh"

  assert_failure
  assert_output --partial "Unexpected Central Portal upload response"
}

@test "gradle-publish runs publish-tasks instead of the Central upload" {
  set_central_env
  export PUBLISH_TASKS="publishAndReleaseToMavenCentral  --stacktrace"

  run_script "publish/gradle-publish.sh"

  assert_success
  assert_file_contains "$TEST_DIR/gradlew.args" "^publishAndReleaseToMavenCentral --stacktrace --no-daemon$"
  assert_file_not_exists "$TEST_DIR/curl.calls"
}

@test "gradle-publish ignores a blank publish-tasks override" {
  set_github_env
  export PUBLISH_TASKS="   "

  run_script "publish/gradle-publish.sh"

  assert_success
  assert_file_contains "$TEST_DIR/gradlew.args" "^publishAllPublicationsToGitHubPackagesRepository --no-daemon$"
}

@test "gradle-publish fails before Gradle when a Central secret is missing" {
  set_central_env
  unset MAVEN_CENTRAL_PASSWORD

  run_script "publish/gradle-publish.sh"

  assert_failure
  assert_output --partial "Missing MAVEN_CENTRAL_PASSWORD"
  assert_file_not_exists "$TEST_DIR/gradlew.args"
}

@test "gradle-publish fails before Gradle when the registry token is missing" {
  set_github_env
  unset GITHUB_TOKEN

  run_script "publish/gradle-publish.sh"

  assert_failure
  assert_output --partial "Missing GITHUB_TOKEN"
  assert_file_not_exists "$TEST_DIR/gradlew.args"
}

@test "gradle-publish fails when the signing key cannot be exported" {
  set_central_env
  export GPG_FINGERPRINT="UNKNOWN"

  run_script "publish/gradle-publish.sh"

  assert_failure
  assert_file_not_exists "$TEST_DIR/gradlew.args"
}

@test "gradle-publish rejects an unknown target" {
  export PUBLISH_TARGET="npmjs"

  run_script "publish/gradle-publish.sh"

  assert_failure
  assert_output --partial "Unknown Gradle publish target 'npmjs'"
  assert_file_not_exists "$TEST_DIR/gradlew.args"
}

@test "gradle-publish fails when Gradle fails" {
  set_github_env
  printf '#!/usr/bin/env bash\nexit 1\n' >"$TEST_DIR/gradlew"

  run_script "publish/gradle-publish.sh"

  assert_failure
  refute_output --partial "Published to"
}
