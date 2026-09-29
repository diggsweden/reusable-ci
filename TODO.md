# TODO

## Secret mapping migration release pins

Before publishing, repin the migration examples and their helper-script refs to a
published revision containing both secret declarations and the OpenGrep counting
fix. The current declaration pin predates the counting fix. Follow
[release preparation](docs/DEVELOPMENT.md#secret-mapping-migration-release-preparation).

## Private repository container provenance

The pinned SLSA generator requires an explicit `private-repository: true` opt-in,
but `publish-container.yml` does not expose or forward it. Design a consumer-facing
option for private releases that makes the public Rekor disclosure explicit, and
thread it through container configuration and the publish stage. Keep the default
non-opted-in behavior. See [Private Repository Release Notes](docs/private-repositories.md).

## Private package dependency authentication

Maven/NPM build jobs currently have only Contents read and do not configure tokens
for private dependency installation. Add scoped registry authentication and the
necessary permission forwarding before claiming support for those dependency paths.

## Gradle Maven Central publishing example

The Gradle example requests `build publish`, but the Gradle builder does not
receive Maven Central credentials. The separate Maven Central publisher is gated
on the Maven build result and downloads Maven artifacts. Resolve and exercise the
Gradle publishing path before describing that example as a complete Maven Central
release setup; explicit caller secret mappings alone do not fix it.

## Multi-artifact version-bump race condition

The `execute-version-bump` job in `release-prepare-stage.yml` uses a matrix strategy.
When multiple artifacts each run their own version-bump, they race on `git push` and
`git tag --force`. The `release-sha` output from a matrix reusable workflow call takes
the value from the last-completing matrix leg, which may not be deterministic.

Single-artifact projects (the common case) are unaffected. For multi-artifact projects,
consider serializing version-bump or consolidating it into a single job.

## Rename `reusable-ci-ref` output in orchestrator

The `parse-config` job output `reusable-ci-ref` holds the pinned commit SHA of the
scripts checkout (resolved before any tag movement). The name suggests it is the
original ref input, but it is actually a resolved SHA used only for checking out
helper scripts. Consider renaming to `reusable-ci-sha` or `scripts-ref` to make
the intent clearer.
