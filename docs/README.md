<!--
SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government

SPDX-License-Identifier: CC0-1.0
-->

# Documentation Index

This index separates end-user documentation by purpose. The examples and docs
target the v3 Go-backed workflow contract.

## Tutorials

Learn by starting from a working setup.

- [Examples](../examples/README.md) - Start from a working Maven, NPM, Gradle, Android, Go, Cargo, or monorepo setup.

## How-To Guides

Solve a specific task.

- [Publishing](publishing.md) - Configure Maven Central, GitHub Packages, containers, App Store Connect, and Google Play.
- [Gradle Publishing Onboarding](gradle-publish-onboarding.md) - Add `maven-publish` to a Gradle or Android-library project so it can publish to Maven Central and GitHub Packages.
- [Runtime Images](runtime-images.md) - Override runtime images for reproducibility, mirroring, or branch testing.
- [Verification](verification.md) - Verify release assets, checksums, signatures, attestations, and SBOMs.
- [SBOM](sbom.md) - Configure SBOM layers and understand release/snapshot defaults.
- [Threat Model](threat-model.md) - What reusable-ci defends against, what it deliberately doesn't, and where adopter controls layer on top.

## Reference

Look up exact fields, defaults, commands, and requirements.

- [Artifacts Reference](artifacts-reference.md) - Complete `artifacts.yml` schema, defaults, and validation constraints.
- [Components Reference](components.md) - Reusable workflow components and direct-use caveats.
- [CLI Reference](cli-reference.md) - Generated `reusable-ci` command surface.
- [Secrets, Permissions, and Validation](reference.md) - Required secrets, permissions, and validation matrices.

## Explanation

Understand the design and tradeoffs.

- [Workflow Architecture](workflows.md) - Orchestrator, stage, and component interaction model.
- [Ecosystem Support](ecosystems.md) - Artifact-first vs container-first ecosystem model and current support status.
- [Providers and Runners](providers.md) - Forge detection, the runner/forge-API axes, overrides, and the per-forge capability matrix (GitHub, GitLab, Forgejo, local).

## Maintainer And Historical Docs

- [Development](DEVELOPMENT.md) - Local development, testing, and branch verification for this repository.
- [Testing](testing.md) - Go test helpers and testing conventions.
- [Scripts](scripts.md) - Remaining bootstrap scripts and why the workflow surface moved to the Go binary.
- [Workflow Design Policy](workflow-design-policy.md) - Design rules for maintainers extending the workflow graph.
- [GitLab Remaining Prep](gitlab.prep.md) - Current GitLab Catalog work still missing.
- [GitLab Support Plan](gitlabsupportplan.md) - Architecture and future plan for GitLab CI support.
