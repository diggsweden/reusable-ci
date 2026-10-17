#!/usr/bin/env bash
# SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
# SPDX-License-Identifier: CC0-1.0

# Publish a Gradle project from source to one target with Gradle's own
# maven-publish and signing plugins. Expects to run in the Gradle working
# directory.
#
# github-packages: runs publishAllPublicationsToGitHubPackagesRepository
#   against the project's repository named GitHubPackages.
# maven-central: publishes into a local bundle repository added by an init
#   script, then uploads the bundle with the Central Portal Publisher API and
#   waits for validation. A release cannot be undone, so it is AUTOMATIC only
#   when the caller says the release passed the authorized-releasers check;
#   otherwise USER_MANAGED, released by a person in the Portal. A -SNAPSHOT
#   tag deploys to the Portal's snapshot repository instead, which keeps
#   snapshots for a limited time (the namespace must enable snapshots).
# PUBLISH_TASKS replaces either default with the project's own tasks, for
#   example a third-party plugin that uploads by itself.
#
# Credentials reach Gradle only as ORG_GRADLE_PROJECT_* variables in the
# environment of the gradlew process, and are checked before Gradle starts.
#
# Environment:
#   PUBLISH_TARGET          github-packages or maven-central
#   PUBLISH_TASKS           Optional override for the default publish task
#   GITHUB_ACTOR            github-packages: registry username
#   GITHUB_TOKEN            github-packages: registry token
#   MAVEN_CENTRAL_USERNAME  maven-central: Central Portal token username
#   MAVEN_CENTRAL_PASSWORD  maven-central: Central Portal token password
#   RELEASE_GPG_PASSPHRASE  maven-central: passphrase of the imported key
#   GPG_FINGERPRINT         maven-central: fingerprint of the imported key
#   GPG_KEY_ID              maven-central: key id of the imported key
#   CENTRAL_PUBLISHING_TYPE Optional, AUTOMATIC or USER_MANAGED (default)
#   RELEASE_TAG             Optional, the release tag; -SNAPSHOT selects the
#                           Central snapshot repository
#   CENTRAL_WAIT_SECONDS    Optional, how long to wait for validation (900)
#   CENTRAL_POLL_INTERVAL   Optional, seconds between status checks (10)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
source "$SCRIPT_DIR/../ci/output.sh"

readonly CENTRAL_API="https://central.sonatype.com/api/v1/publisher"
readonly CENTRAL_SNAPSHOTS_URL="${CENTRAL_SNAPSHOTS_URL:-https://central.sonatype.com/repository/maven-snapshots/}"

require_env() {
  local name
  for name in "$@"; do
    if [[ -z "${!name:-}" ]]; then
      ci_log_error "Missing ${name} for Gradle publish to ${PUBLISH_TARGET}"
      exit 1
    fi
  done
}

has_override() {
  local override="${PUBLISH_TASKS:-}"
  [[ -n "${override//[[:space:]]/}" ]]
}

# Runs gradlew with the given arguments. No daemon: a daemon left running
# would keep this job's credentials.
run_gradle() {
  ./gradlew "$@" --no-daemon
}

run_override_tasks() {
  # Split on all whitespace, newlines included, like build-gradle-app.yml.
  local -a tasks
  read -r -d '' -a tasks <<<"$PUBLISH_TASKS" || true
  printf "Tasks: %s\n" "${tasks[*]}"
  run_gradle "${tasks[@]}"
}

export_github_packages_credentials() {
  require_env GITHUB_ACTOR GITHUB_TOKEN
  export ORG_GRADLE_PROJECT_githubActor="$GITHUB_ACTOR"
  export ORG_GRADLE_PROJECT_githubToken="$GITHUB_TOKEN"
}

export_maven_central_credentials() {
  require_env MAVEN_CENTRAL_USERNAME MAVEN_CENTRAL_PASSWORD \
    RELEASE_GPG_PASSPHRASE GPG_FINGERPRINT GPG_KEY_ID

  # Gradle signs in memory through Bouncy Castle, which reads only RFC 4880
  # packets, so the key is re-exported from the keyring rather than passed
  # through as the raw secret. The passphrase goes in on stdin, not argv.
  local signing_key
  signing_key="$(gpg --batch --pinentry-mode loopback --passphrase-fd 0 \
    --armor --export-secret-keys "$GPG_FINGERPRINT" <<<"$RELEASE_GPG_PASSPHRASE")"
  if [[ -z "$signing_key" ]]; then
    ci_log_error "Could not export the signing key ${GPG_FINGERPRINT} from the keyring"
    exit 1
  fi

  # Gradle's signing plugin matches the 8-character short key id.
  local short_key_id="${GPG_KEY_ID: -8}"

  export ORG_GRADLE_PROJECT_mavenCentralUsername="$MAVEN_CENTRAL_USERNAME"
  export ORG_GRADLE_PROJECT_mavenCentralPassword="$MAVEN_CENTRAL_PASSWORD"
  # Read by the build's useInMemoryPgpKeys(...) call. The signingInMemoryKey*
  # spelling serves third-party plugins used through PUBLISH_TASKS.
  export ORG_GRADLE_PROJECT_signingKeyId="$short_key_id"
  export ORG_GRADLE_PROJECT_signingKey="$signing_key"
  export ORG_GRADLE_PROJECT_signingPassword="$RELEASE_GPG_PASSPHRASE"
  export ORG_GRADLE_PROJECT_signingInMemoryKeyId="$short_key_id"
  export ORG_GRADLE_PROJECT_signingInMemoryKey="$signing_key"
  export ORG_GRADLE_PROJECT_signingInMemoryKeyPassword="$RELEASE_GPG_PASSPHRASE"
}

# Calls the Portal API. The token goes to curl on stdin as a config line, so
# it never appears in the process list.
central_api() {
  local token
  token="$(printf '%s:%s' "$MAVEN_CENTRAL_USERNAME" "$MAVEN_CENTRAL_PASSWORD" | base64 -w0)"
  printf 'header = "Authorization: Bearer %s"\n' "$token" |
    curl --silent --show-error --fail-with-body --config - "$@"
}

# Writes an init script that adds one Maven repository to every project that
# applies maven-publish, so the project declares no Central repository. Name,
# URL and optional credentials come from CENTRAL_REPOSITORY_* variables.
write_central_init_script() {
  cat >"$1" <<'EOF'
import org.gradle.api.publish.PublishingExtension

allprojects {
    pluginManager.withPlugin("maven-publish") {
        extensions.configure<PublishingExtension> {
            repositories {
                maven {
                    name = System.getenv("CENTRAL_REPOSITORY_NAME")
                    url = uri(System.getenv("CENTRAL_REPOSITORY_URL"))
                    System.getenv("CENTRAL_REPOSITORY_USERNAME")?.let { user ->
                        credentials {
                            username = user
                            password = System.getenv("CENTRAL_REPOSITORY_PASSWORD")
                        }
                    }
                }
            }
        }
    }
}
EOF
}

# Publishes every maven-publish publication into a local directory laid out
# as a Maven repository, which is the bundle layout the Portal expects.
build_central_bundle() {
  local bundle_dir="$1" init_script="$2"

  write_central_init_script "$init_script"
  printf "Tasks: publishAllPublicationsToCentralBundleRepository\n"
  CENTRAL_REPOSITORY_NAME="CentralBundle" CENTRAL_REPOSITORY_URL="$bundle_dir" \
    run_gradle --init-script "$init_script" publishAllPublicationsToCentralBundleRepository

  # Repository metadata belongs to the repository, not to a deployment.
  find "$bundle_dir" -name 'maven-metadata.xml*' -delete

  if [[ -z "$(find "$bundle_dir" -type f -print -quit)" ]]; then
    ci_log_error "Gradle published nothing. Apply maven-publish and define a publication."
    exit 1
  fi
  if [[ -z "$(find "$bundle_dir" -type f -name '*.asc' -print -quit)" ]]; then
    ci_log_error "The publications are not signed. Apply signing and call useInMemoryPgpKeys(...)."
    exit 1
  fi
}

# Waits until the Portal has validated the deployment. A failed status
# request is retried until the deadline, because the upload already worked
# and a re-run would upload a second deployment. FAILED or the deadline
# stops the job, so a release is never reported for an unvalidated bundle.
wait_for_validation() {
  local deployment_id="$1"
  local wait_seconds="${CENTRAL_WAIT_SECONDS:-900}"
  local interval="${CENTRAL_POLL_INTERVAL:-10}"
  local deadline=$((SECONDS + wait_seconds)) status state

  while true; do
    state=""
    if status="$(central_api --request POST "${CENTRAL_API}/status?id=${deployment_id}")"; then
      state="$(jq -r '.deploymentState // ""' <<<"$status" 2>/dev/null || true)"
    else
      ci_log_warning "Status request for deployment ${deployment_id} failed, retrying: ${status}"
    fi
    case "$state" in
    VALIDATED | PUBLISHING | PUBLISHED)
      printf "Deployment %s is %s\n" "$deployment_id" "$state"
      return 0
      ;;
    FAILED)
      ci_log_error "Central Portal rejected deployment ${deployment_id}: $(jq -c '.errors // {}' <<<"$status")"
      exit 1
      ;;
    esac
    if ((SECONDS >= deadline)); then
      break
    fi
    printf "Deployment %s is %s, waiting...\n" "$deployment_id" "${state:-unknown}"
    sleep "$interval"
  done

  ci_log_error "Deployment ${deployment_id} was not validated within ${wait_seconds}s. Check it in the Central Portal before re-running, since a re-run uploads a new deployment."
  exit 1
}

write_summary() {
  local target="$1" detail="$2"
  {
    printf "## Published Gradle project 🚀\n\n"
    case "$target" in
    github-packages) printf -- "- **GitHub Packages:** [packages](%s)\n" "$(ci_packages_url)" ;;
    maven-central) printf -- "- **Maven Central:** %s\n" "$detail" ;;
    esac
  } | ci_summary
}

publish_maven_central() {
  local publishing_type="${CENTRAL_PUBLISHING_TYPE:-USER_MANAGED}"
  case "$publishing_type" in
  AUTOMATIC | USER_MANAGED) ;;
  *)
    ci_log_error "Unknown CENTRAL_PUBLISHING_TYPE '${publishing_type}' (want AUTOMATIC or USER_MANAGED)"
    exit 1
    ;;
  esac

  local work_dir bundle_dir deployment_id
  work_dir="$(mktemp -d)"
  # Expanded now: the EXIT trap runs after this function's locals are gone.
  # shellcheck disable=SC2064
  trap "rm -rf '${work_dir}'" EXIT
  bundle_dir="$work_dir/bundle"
  mkdir -p "$bundle_dir"

  build_central_bundle "$bundle_dir" "$work_dir/central-bundle.init.gradle.kts"
  (cd "$bundle_dir" && zip -qr "$work_dir/central-bundle.zip" .)

  printf "Uploading bundle to the Central Portal as %s...\n" "$publishing_type"
  if ! deployment_id="$(central_api --request POST \
    --form "bundle=@${work_dir}/central-bundle.zip" \
    "${CENTRAL_API}/upload?publishingType=${publishing_type}")"; then
    ci_log_error "Central Portal upload failed: ${deployment_id}"
    exit 1
  fi
  if [[ ! "$deployment_id" =~ ^[A-Za-z0-9-]+$ ]]; then
    ci_log_error "Unexpected Central Portal upload response: ${deployment_id}"
    exit 1
  fi
  ci_output "deployment-id" "$deployment_id"

  wait_for_validation "$deployment_id"

  if [[ "$publishing_type" == "AUTOMATIC" ]]; then
    write_summary maven-central "deployment \`${deployment_id}\` validated; the Portal releases it automatically"
  else
    write_summary maven-central "deployment \`${deployment_id}\` validated; release it under [Deployments](https://central.sonatype.com/publishing/deployments)"
  fi
}

# Snapshots go straight to the Portal's snapshot repository with the token as
# credentials. They need no bundle, validation or release step.
publish_maven_central_snapshot() {
  local work_dir
  work_dir="$(mktemp -d)"
  # Expanded now: the EXIT trap runs after this function's locals are gone.
  # shellcheck disable=SC2064
  trap "rm -rf '${work_dir}'" EXIT

  write_central_init_script "$work_dir/central-snapshots.init.gradle.kts"
  printf "Tasks: publishAllPublicationsToCentralSnapshotsRepository\n"
  CENTRAL_REPOSITORY_NAME="CentralSnapshots" CENTRAL_REPOSITORY_URL="$CENTRAL_SNAPSHOTS_URL" \
    CENTRAL_REPOSITORY_USERNAME="$MAVEN_CENTRAL_USERNAME" CENTRAL_REPOSITORY_PASSWORD="$MAVEN_CENTRAL_PASSWORD" \
    run_gradle --init-script "$work_dir/central-snapshots.init.gradle.kts" \
    publishAllPublicationsToCentralSnapshotsRepository

  write_summary maven-central "snapshot deployed to the [Central snapshot repository](${CENTRAL_SNAPSHOTS_URL}), kept for a limited time"
}

main() {
  case "${PUBLISH_TARGET:-}" in
  github-packages) export_github_packages_credentials ;;
  maven-central) export_maven_central_credentials ;;
  *)
    ci_log_error "Unknown Gradle publish target '${PUBLISH_TARGET:-}' (want github-packages or maven-central)"
    exit 1
    ;;
  esac

  printf "Publishing to %s via Gradle...\n" "$PUBLISH_TARGET"

  if has_override; then
    run_override_tasks
  elif [[ "$PUBLISH_TARGET" == "github-packages" ]]; then
    # The named-repository task publishes to that destination only; the bare
    # `publish` task would push every publication to every repository.
    printf "Tasks: publishAllPublicationsToGitHubPackagesRepository\n"
    run_gradle publishAllPublicationsToGitHubPackagesRepository
    write_summary github-packages ""
  elif ci_is_snapshot "${RELEASE_TAG:-}"; then
    publish_maven_central_snapshot
  else
    publish_maven_central
  fi

  printf "✓ Published to %s\n" "$PUBLISH_TARGET"
}

main "$@"
