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

## Deprecate scalar inputs on release-dev-orchestrator at v3.0

v2.9.x adds `artifacts-config` to `release-dev-orchestrator.yml`,
mirroring `release-orchestrator.yml`'s shape so dev releases support
multi-container projects via a per-container matrix.

The legacy single-container inputs are retained for backwards
compatibility:

- `container-file`
- `working-directory` (for the dev-context-driven container build only;
  it remains valid for callers using artifacts-config too)
- `project-type` (still required for the umbrella linter inference;
  that role survives v3.0 — only its single-container build-stage
  routing role is replaced by artifacts-config)

Plan:

- v2.9.x — both paths supported. New callers use `artifacts-config`;
  existing single-container callers see no change.
- v2.10.x — emit `::warning::` annotations from `compose-dev-interface`
  whenever `artifacts-config` is empty, nudging callers to migrate.
- v3.0 — remove the legacy single-container path. `artifacts-config`
  becomes required (single-container projects declare a one-entry
  artifacts.yml).

Migration for a single-container caller is small: replace the
`container-file: Containerfile` + `working-directory: .` inputs with a
~5-line `.github/artifacts.yml` that declares one container. The
existing `examples/cargo-app/` artifacts.yml is the template.
