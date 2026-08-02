# TODO

## Multi-artifact version-bump race — implemented, pending a CI release run

The fix is in place but **not yet validated by a real release**:

- `version-bump.yml` no longer creates the tag — the `bump-version` job ends at
  `commit-push` and emits an informational `bump-sha`. Signing setup + cleanup
  stay (the bump commit is signed).
- `release-prepare-stage.yml` has a single `tag-release` job (`needs:
  version-bump`) that checks out the branch (HEAD now carries every leg's bump),
  sets up signing, runs `reusable-ci version tag-release` once, and is the source
  of the stage's `release-sha`. Runs on the no-bump path too.

This makes the tag point at the commit containing *all* bumps and the
`release-sha` deterministic, with no Go change.

**Remaining:**

1. **CI validation** — release-critical workflow change; the new job's signing
   setup + tag creation need a real release run (multi-artifact and no-bump
   paths) before it can be trusted. actionlint and the Go workflow-contract test
   pass, but neither exercises the live ceremony.
2. **Contract note** — `version-bump.yml`'s output was renamed `release-sha` →
   `bump-sha`, and a direct (non-orchestrator) caller of `version-bump.yml` no
   longer gets a tag. Fine for the orchestrator path; flag for any external
   direct callers.
3. **Maintainability follow-up** — the GPG/SSH signing setup is duplicated
   between `version-bump.yml` and the new `tag-release` job. Extract a shared
   composite action (the forgejo-ci pattern) once the flow is CI-proven.
