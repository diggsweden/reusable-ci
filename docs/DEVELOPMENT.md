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

The supported caller contract requires new integrations to use explicit secret mappings. Existing `secrets: inherit` callers remain supported during the current major version; support ends with the next major release.

Before publishing the migration release:

1. Commit and make available a revision containing both the callable secret declarations and the OpenGrep suppression-counting fix.
2. Update the migration examples in `README.md`, `examples/`, and the reference/publishing guides so `uses:` and `reusable-ci-ref` select that same revision. The current `73d5d61ddca95f965193ce7a57bdeb7a3ae10899` pin contains the declarations only, so it is not a complete upgrade target for consumers using temporary suppressions.
3. Update [Passing Secrets](reference.md#passing-secrets) with that upgrade target, keeping the next-major deadline and the temporary suppression instructions together.
4. Validate the pinned examples against the called workflows' accepted inputs and secrets. Verify the scanner accepts explicit mappings and targeted suppressions while still reporting unsuppressed inheritance. Exercise secret forwarding with representative consumer PR and release runs, including optional secrets being absent.

Before publishing the next major release, make explicit mappings a requirement in its breaking-change notes. GitHub's native `inherit` syntax is not disabled by named secret declarations; any additional enforcement must be explicit and must not be backported as a current-major compatibility break.
