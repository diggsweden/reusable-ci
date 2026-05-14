// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package git holds the plain DTOs that cross the git-port boundary.
// Adapters (internal/adapters/git) implement the port and produce/consume
// these values; use cases (internal/app/{version,summary,...}) reference
// them in their consumer-defined interfaces without importing the
// adapter. Mirrors the precedent set by internal/domain/provider for the
// provider port.
package git

// CommitInput captures the bits a use case needs to drive `git commit`.
type CommitInput struct {
	Message     string
	AuthorName  string
	AuthorEmail string
	Signoff     bool
}

// CommitInfo is the result of a CommitInfo lookup. Author is "Name <email>";
// Date is YYYY-MM-DD; Message is the commit subject line; Body is the raw
// `git cat-file commit` output (used by callers that need to detect GPG /
// SSH signatures in the trailer block).
type CommitInfo struct {
	Author  string
	Date    string
	Message string
	Body    string
}
