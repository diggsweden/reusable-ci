# TODO

## MegaLinter lint route (`linters.megalinter`)

The PR orchestrator's lint route is `linters.nanolinter`. A second route,
`linters.megalinter`, is still to add: wire a `linters.megalinter` input + a
`nanolinter`-style planner target + a `lint-megalinter.yml` reusable workflow
that runs MegaLinter (its own container/action, not the mise toolchain). Its
execution model differs from nanolinter (which runs the consumer's `just
lint`), so it needs its own job design.

## Multi-artifact version-bump race condition

The `execute-version-bump` job in `release-prepare-stage.yml` uses a matrix strategy.
When multiple artifacts each run their own version-bump, they race on `git push` and
`git tag --force`. The `release-sha` output from a matrix reusable workflow call takes
the value from the last-completing matrix leg, which may not be deterministic.

Single-artifact projects (the common case) are unaffected. For multi-artifact projects,
consider serializing version-bump or consolidating it into a single job.
