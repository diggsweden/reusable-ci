// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package container

// DefaultDigestsDir is the canonical directory where per-arch digest
// marker files are written before `container manifest merge` assembles
// them into a manifest list. Mirrors the workflow's $DIGESTS_DIR
// fallback.
//
// Single source of truth: the CLI flag's --digests-dir Value and the
// app-layer empty-fallback in container/manifest.go both reference
// this constant.
const DefaultDigestsDir = "/tmp/digests"
