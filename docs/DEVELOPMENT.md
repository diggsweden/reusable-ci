# Development Guide

## Prerequisites - Linux

1. Install [mise](https://mise.jdx.dev/) (manages linting tools):

   ```bash
   curl https://mise.run | sh
   ```

2. Activate mise in your shell:

   ```bash
   # For bash - add to ~/.bashrc
   eval "$(mise activate bash)"

   # For zsh - add to ~/.zshrc
   eval "$(mise activate zsh)"

   # For fish - add to ~/.config/fish/config.fish
   mise activate fish | source
   ```

   Then restart your terminal.
3. Install pipx (needed for reuse license linting):

   ```bash
   # Debian/Ubuntu
   sudo apt install pipx
   ```

4. Install project tools:

   ```bash
   mise install
   ```

5. Run quality checks:

   ```bash
   just verify
   ```

## Prerequisites - macOS

1. Install [mise](https://mise.jdx.dev/) (manages linting tools):

   ```bash
   brew install mise
   ```

2. Activate mise in your shell:

   ```bash
   # For zsh - add to ~/.zshrc
   eval "$(mise activate zsh)"

   # For bash - add to ~/.bashrc
   eval "$(mise activate bash)"

   # For fish - add to ~/.config/fish/config.fish
   mise activate fish | source
   ```

   Then restart your terminal.
3. Install newer bash than macOS default:

   ```bash
   brew install bash
   ```

4. Install pipx (needed for reuse license linting):

   ```bash
   brew install pipx
   ```

5. Install project tools:

   ```bash
   mise install
   ```

6. Run quality checks:

   ```bash
   just verify
   ```

## Available Commands

Run `just` to see all available commands.

## Workflow Refactor Checklist

When changing workflows or workflow helper scripts:

1. Keep public workflow contracts stable unless the change is explicitly versioned.
2. Follow `docs/workflow-design-policy.md` for workflow structure and script extraction rules.
3. Run `actionlint .github/workflows/*.yml`.
4. Parse workflow YAML and check reusable-workflow input compatibility.
5. Run `bash -n` for touched helper scripts.
6. Add or update Bats tests when helper scripts are added or changed.
7. Run `bats tests/validate/changelog-permissions.bats` when changing release/changelog permissions; it checks every local call boundary and the consumer examples (requires Python 3 and the project's `yq`).

## Secret Mapping Migration Release Preparation

The upcoming release is v3.0.0. Its supported caller contract requires explicit mappings with canonical secret names for all consumers. `secrets: inherit` and retired secret names are unsupported; no grace period is offered in v3.

Before publishing v3.0.0:

1. Make the pinned revision available in the remote repository before consumers use it. The migration baseline is `747ac6d4ff82d409a48c125266939a15df9a3270`, containing the canonical secret names, OpenGrep suppression-counting fix, and changelog PR-read permissions.
2. Keep `uses:` and `reusable-ci-ref` aligned in `README.md`, `examples/`, and the reference/publishing guides when moving to a release containing that baseline.
3. Update [Passing Secrets](reference.md#passing-secrets) and the [Migration Guide](migration.md) with that upgrade target and the v3.0.0 requirements. Do not offer inheritance suppressions as a supported v3 migration path.
4. Validate the pinned examples against the called workflows' accepted inputs and secrets. Verify explicit mappings and secret forwarding with representative consumer PR and release runs, including optional secrets being absent. Retain regression coverage for OpenGrep's general suppression-counting behavior.

Document these requirements in the v3.0.0 breaking-change notes. GitHub's native `inherit` syntax is not disabled by named secret declarations; any additional enforcement must be explicit and must not be backported as a v2 compatibility break.
