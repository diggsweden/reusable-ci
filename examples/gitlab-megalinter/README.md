<!--
SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government

SPDX-License-Identifier: CC0-1.0
-->

# Example: MegaLinter PR lint gate on GitLab CI

The GitLab counterpart of setting `lint-engine: megalinter` on a GitHub PR:
instead of a reusable workflow, a GitLab pipeline `include:`s the **`megalinter`
CI/CD Catalog component**. MegaLinter is the heavier, governance-recognised
alternative to nanolinter — pick one engine, not both.

`.gitlab-ci.yml` here pulls in `megalinter` and selects a flavour image. The
component runs MegaLinter (reading the project's own `.mega-linter.yml`) as the
lint gate; the job fails on blocking findings, the security SARIF is kept as an
artifact, and it is converted to a GitLab SAST report published as
`artifacts:reports:sast`.

> The SARIF → GitLab-SAST conversion runs `reusable-ci security report
> to-gitlab-sast`, which the component installs at runtime (cosign-verified —
> use a MegaLinter image that ships cosign for the conversion to run, else the
> SARIF is kept as a plain artifact). Findings land on the merge-request
> **Security tab** — its display needs GitLab Ultimate, but producing the report
> is harmless on any tier. Set `enable-gitlab-sast: false` to keep the SARIF as a
> plain artifact only.

## Use it

```yaml
include:
  - component: $CI_SERVER_FQDN/diggsweden/reusable-ci/megalinter@1.0.0
    inputs:
      megalinter-image: "oxsecurity/megalinter-java:v8"   # flavour for your stack
```

The component's inputs and defaults are documented on its Catalog page (GitLab
renders them from the component `spec:`). Pin `@<version>` to a release.
