# Documentation Index

This index separates end-user documentation by purpose. The examples and docs
target the v3 Go-backed workflow contract.

## Tutorials

Learn by starting from a working setup.

- [Examples](../examples/README.md) - Start from a working Maven, NPM, Gradle, Android, Go, Cargo, or monorepo setup.

## How-To Guides

Solve a specific task.

- [Publishing](publishing.md) - Configure Maven Central, GitHub Packages, containers, App Store Connect, and Google Play.
- [Runtime Images](runtime-images.md) - Override runtime images for reproducibility, mirroring, or branch testing.
- [Verification](verification.md) - Verify release assets, checksums, signatures, attestations, and SBOMs.
- [SBOM](sbom.md) - Configure SBOM layers and understand release/snapshot defaults.
- [Forgejo](forgejo.md) - Adopting on Forgejo: the release-ci middle layer, consumer kit, and platform notes.
- [Threat Model](threat-model.md) - What reusable-ci defends against, what it deliberately doesn't, and where adopter controls layer on top.
- [Open Questions](open-questions.md) - Behaviours found by the test review that need a decision: duplicated container SBOM scans, an asymmetric `--output`, signing zero packages, an empty revision label.

## Reference

Look up exact fields, defaults, commands, and requirements.

- [Artifacts Reference](artifacts-reference.md) - Complete `artifacts.yml` schema, defaults, and validation constraints.
- [Components Reference](components.md) - Reusable workflow components and direct-use caveats.
- [CLI Reference](cli-reference.md) - Generated `reusable-ci` command surface.
- [Secrets, Permissions, and Validation](reference.md) - Required secrets, permissions, and validation matrices.

## Explanation

Understand the design and tradeoffs.

- [Artifact and Image Flows](flows.md) - How a declared artifact becomes a released one: run artifacts between jobs, and the image ledger from build to signature to final tag. Starts jargon-free and layers up.
- [Workflow Architecture](workflows.md) - Orchestrator, stage, and component interaction model.
- [Ecosystem Support](ecosystems.md) - Artifact-first vs container-first ecosystem model and current support status.
- [Providers and Runners](providers.md) - Forge detection, the runner/forge-API axes, overrides, and the per-forge capability matrix (GitHub, GitLab, Forgejo, local).
- [CLI Black Box](cli-black-box.md) - The CLI-as-black-box contract between workflows and the binary.
- [Signing Convergence](signing-convergence.md) - How the signing backends and verification paths converge across forges.

### Architecture Decisions

Records of decisions that shaped the codebase, kept as written rather than
edited when things move. Newer decisions append an update note instead.

- [ADR 0001 — Forgejo shell home](adr/0001-forgejo-shell-home.md)
- [ADR 0002 — Signer trust boundary](adr/0002-signer-trust-boundary.md)
- [ADR 0003 — CLI verb lexicon](adr/0003-cli-verb-lexicon.md) - the bar a guard test has to clear to earn its keep.
- [ADR 0004 — Package layering](adr/0004-package-layering.md) - the hexagonal layering, and the guard that enforces it.

## Maintainer And Historical Docs

- [Development](DEVELOPMENT.md) - Local development, testing, and branch verification for this repository.
- [Testing](testing.md) - Go test helpers and testing conventions.
- [Scripts](scripts.md) - Remaining bootstrap scripts and the workflow surface moved to the Go binary.
- [Workflow Design Policy](workflow-design-policy.md) - Design rules for maintainers extending the workflow graph.
- [GitLab Support Plan](gitlabsupportplan.md) - The single GitLab planning doc: architecture, capability/orchestration model, and the work still ahead.
