# Neutral target fixtures

This repository owns the neutral-v2 interoperability corpus under this
directory. The canonical accepted contracts are:

- `neutral-targets-v2-compose.json`
- `neutral-targets-v2-github.json`

`neutral-v2-corpus.json` records the accepted and malformed cases with the
SHA-256 of each local file. Tests verify those hashes before evaluating the
contract behavior, so fixture and expectation changes must update the manifest
in the same commit. Repository history plus the local hash manifest is the
complete provenance record.

Current cross-repository acceptance evidence is recorded in
[`docs/testing.md`](../../../docs/testing.md).

`historical-empty-interfaces.json` preserves the previously accepted GitHub
fixture as a negative regression: `interfaces: {}` is not an absent interface
and must be rejected; portable producers omit the field or use `null`.
