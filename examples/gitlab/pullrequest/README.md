<!--
SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government

SPDX-License-Identifier: CC0-1.0
-->

# Example: PR quality orchestration on GitLab CI

The GitLab counterpart of GitHub's **pull-request orchestrator**. On GitHub a
reusable workflow fans the PR quality checks out with `strategy: matrix`; on
GitLab the quality job set is *fixed* (lint ∥ scan ∥ test), so it is a **static
graph** — the consumer pipeline `include:`s the Catalog components and wires the
`stages:`/`rules:`. No generated child pipeline; this is the idiomatic shape
(see [`gitlabsupportplan.md`](../../../docs/gitlabsupportplan.md), "Orchestration
model").

`.gitlab-ci.yml` here:

- **Scopes to PR context** with `workflow:rules` — runs on merge requests and on
  the default branch (the GitLab equivalent of GitHub `on: pull_request` + push
  to the protected branch).
- **Composes the quality stage** by including the `nanolinter` component, which
  runs `nanolinter verify` against your project's `nanolinter.toml` verify plan
  (the flavour image bakes the tools — no `just`/justfile needed). Add further
  quality components (scan, test, license) to the same `quality` stage as they
  ship; each is one more `include:`.

## The gate

The **pipeline status is the gate**: if any quality job fails, the pipeline — and
the merge request — goes red. There is no separate "status" aggregation job as on
GitHub, because GitLab fails the pipeline on any failed job natively. nanolinter
also publishes its findings to the merge-request **Security tab** via
`artifacts:reports:sast` (display needs GitLab Ultimate; producing the report is
tier-agnostic).

## The aggregated PR summary panel

GitHub's orchestrator renders one aggregated PR summary by reading every sibling
job's result via `toJson(needs)`. GitLab has no equivalent — a job cannot see its
siblings' statuses — so each job records its own outcome with `report job-result`
and the summary job collects the records, feeding the *same* shared
`report stage-result` aggregator. See
[`examples/gitlab/stage-summary/`](../stage-summary/) for that flow
end-to-end. This example keeps the gate minimal (pipeline status + Security tab);
add a summary stage following that example to render the aggregated panel.

## Use it

```yaml
workflow:
  rules:
    - if: $CI_PIPELINE_SOURCE == "merge_request_event"
    - if: $CI_COMMIT_BRANCH == $CI_DEFAULT_BRANCH

stages:
  - quality

include:
  - component: $CI_SERVER_FQDN/diggsweden/reusable-ci/nanolinter@1.0.0
    inputs:
      stage: quality
      nanolinter-image: "codefloe.com/itiquette/nanolinter:java"   # flavour for your stack
```

Pin `@<version>` to a release. Component inputs are documented on each Catalog
page (GitLab renders them from the component `spec:`).
