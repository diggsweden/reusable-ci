# Example: plan-driven release build fan-out on GitLab CI

How reusable-ci fans the release **build stage** out over a project's artifacts
on GitLab — the counterpart of GitHub's `strategy: matrix` over the plan.

## Why a generated child pipeline

GitHub drives the per-artifact build matrix natively:
`strategy: matrix.artifact: ${{ fromJson(plan).targets.maven.items }}`. GitLab
cannot expand a matrix from a value computed at runtime, so the fan-out is
**materialised**: a prepare job generates a child pipeline from the *same* plan
JSON, and a trigger job runs it. This is the GitLab provider's *rendering* of the
shared plan contract — not a second plan model or a meta-DSL.

It is small precisely because each ecosystem build is now **one thin job**
(`reusable-ci build <eco> run`, packaged as the `build-<eco>` Catalog
components). The generator emits one `include:` per running plan item; it
synthesises no job bodies.

## The flow

1. **`generate-build-pipeline`** runs `config parse-artifacts` → `plan release`
   (which emits the typed build-stage plan) → `plan gitlab-build-pipeline`, which
   writes `build-pipeline.yml`: one `include:` of the matching `build-<eco>`
   component per artifact, with per-item inputs (job-name, working-directory,
   build-type, version).
2. **`build`** triggers that child pipeline
   (`trigger:{include:{artifact: build-pipeline.yml}}`), running the actual
   per-artifact builds.

```sh
reusable-ci plan gitlab-build-pipeline \
  --component-ref 1.0.0 --version "$CI_COMMIT_TAG" --output build-pipeline.yml
```

A generated `build-pipeline.yml` looks like:

```yaml
stages:
  - build
include:
  - component: $CI_SERVER_FQDN/diggsweden/reusable-ci/build-maven@1.0.0
    inputs:
      build-type: lib
      job-name: build-maven-lib-core
      working-directory: core
      version: "1.2.3"
  - component: $CI_SERVER_FQDN/diggsweden/reusable-ci/build-go@1.0.0
    inputs:
      job-name: build-go-cli
      working-directory: "."
      version: "1.2.3"
```

`xcode-ios` / `gradle-android` targets are specialised (macOS / signing) and are
not emitted here; they degrade away on GitLab until dedicated components exist.
