<!--
SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government

SPDX-License-Identifier: CC0-1.0
-->

# Example: nanolinter PR lint gate on GitLab CI

The GitLab counterpart of enabling `lint-engine: nanolinter` on a GitHub PR: instead
of a reusable workflow, a GitLab pipeline `include:`s the **`nanolinter`
CI/CD Catalog component**.

`.gitlab-ci.yml` here pulls in `nanolinter` and selects a flavour image. The
component runs `nanolinter verify` against your project's `nanolinter.toml`
verify plan — the project lint gate, which bundles the whole check toolchain
(opengrep SAST, osv-scanner, secrets, trivy-fs, ecosystem linters) baked into the
flavour image, so no `just`/justfile is required. The job fails on blocking
findings, the security SARIF is kept as an artifact, and it is converted to a
GitLab SAST report published as `artifacts:reports:sast`.

> The SARIF → GitLab-SAST conversion runs `reusable-ci security report
> to-gitlab-sast`, which the component installs at runtime (cosign-verified).
> Findings land on the merge-request **Security tab** — its display needs GitLab
> Ultimate, but producing the report is harmless on any tier. Set
> `enable-gitlab-sast: false` to keep the SARIF as a plain artifact only.

## Use it

```yaml
include:
  - component: $CI_SERVER_FQDN/diggsweden/reusable-ci/nanolinter@1.0.0
    inputs:
      nanolinter-image: "codefloe.com/itiquette/nanolinter:java"   # flavour for your stack
```

The component's inputs and defaults are documented on its Catalog page (GitLab
renders them from the component `spec:`). Pin `@<version>` to a release.
