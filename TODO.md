# TODO

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

## Cargo release-side ergonomics — follow-ups

v2.9.0 ships `sbom-cargo.yml` (manifest SBOM), `lint-cargo.yml`, the
orchestrator linter sub-flags, `bump-version.sh` cargo case, and a
cargo branch in `validate-release-prerequisites.yml` (Cargo.lock +
toolchain pin checks). Remaining release-side ergonomics for cargo:

- **`publish-cratesio.yml`** — analogous to `publish-mavencentral.yml` /
  `publish-npm.yml`. Wired into `release-orchestrator.yml` via
  `publish-to: [crates-io]`. Needs API token handling, dry-run support,
  and workspace publishing order.

Shippable as a v2.9.x patch or v2.10.0 minor when a real library
caller surfaces.
