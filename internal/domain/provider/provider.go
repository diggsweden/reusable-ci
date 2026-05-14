// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package provider defines the cross-platform provider port.
//
// Domain code depends only on this package. Adapters import it to
// implement the interface; the composition root in cli/deps wires the
// chosen adapter into use cases.
package provider

import "context"

// Platform identifies the CI provider the binary is running under.
type Platform string

const (
	PlatformGitHub Platform = "github"
	PlatformGitLab Platform = "gitlab"
	PlatformLocal  Platform = "local"
)

// String satisfies fmt.Stringer for ergonomic logging.
func (p Platform) String() string { return string(p) }

// IsValid reports whether the value is one of the known platforms.
func (p Platform) IsValid() bool {
	switch p {
	case PlatformGitHub, PlatformGitLab, PlatformLocal:
		return true
	}
	return false
}

// RefType describes whether the current ref is a branch, tag, or pull/merge
// request. Adapters resolve this from their native context (GITHUB_REF_TYPE
// / CI_COMMIT_TAG presence / etc.).
type RefType string

const (
	RefTypeBranch RefType = "branch"
	RefTypeTag    RefType = "tag"
	RefTypePR     RefType = "pr"
	RefTypeOther  RefType = "other"
)

// EventContext is the platform-portable description of "what is the current
// build running against". Each adapter resolves this from its native env
// variables (GITHUB_REF / GITHUB_SHA on GHA; CI_COMMIT_REF_NAME /
// CI_COMMIT_TAG / CI_COMMIT_SHA on GitLab).
type EventContext struct {
	Platform  Platform
	RefName   string // "main", "v1.2.3", etc.
	RefType   RefType
	SHA       string // full commit SHA
	ShortSHA  string // 7-char short SHA
	Branch    string // best-effort branch name (PR head branch on PR refs)
	PRNumber  string // empty when not a PR
	EventName string // "push", "pull_request", "schedule", "merge_request_event", …
	Repo      string // "owner/repo" or "group/project/path"
	RepoURL   string // canonical web URL
}

// RepoMetadata is what adapter.FetchRepoMetadata returns. Used by the
// container OCI-label assembly.
type RepoMetadata struct {
	Description string
	HTMLURL     string
	LicenseSPDX string
}

// BotPermissions reports the result of probing the configured release
// bot's permissions. Each probe is best-effort:
//
//	UserAccessible:     /user (or equivalent) returned 2xx
//	RepoAccessible:     /repos/<repo> returned 2xx — fatal when false
//	BranchesAccessible: /repos/<repo>/branches returned 2xx — warn-only
//
// Adapters that can't perform a probe (e.g. local) return false for
// all three. Use cases decide which fields are fatal vs warning.
type BotPermissions struct {
	UserAccessible     bool
	RepoAccessible     bool
	BranchesAccessible bool
}

// ReleaseSpec describes a release to create on the platform. Domain
// code populates this from the use-case inputs + asset collection;
// adapters consume it without further policy decisions.
//
// Asset paths are platform-local file paths the adapter uploads
// (gh release create supports passing them directly; the GitLab
// adapter uploads them via /assets/links).
type ReleaseSpec struct {
	Tag        string
	Name       string // display name (defaults to Tag at use-case layer)
	NotesFile  string // path; empty → no --notes-file
	Draft      bool
	Prerelease bool
	MakeLatest bool
	Assets     []string // file paths to attach
}

// SARIFUpload is the payload for UploadSARIF. The adapter handles
// the platform-specific encoding (gzip+base64 + JSON wrapper on
// GitHub Code Scanning; not supported on GitLab/local). Raw SARIF
// content is what use-case code reads off disk — adapters compress
// and wrap as needed.
type SARIFUpload struct {
	// Repository is "owner/repo" on GitHub (the only platform that
	// implements this today).
	Repository string

	// SHA is the commit SHA the SARIF results pertain to.
	SHA string

	// Ref is the full git ref (refs/heads/main, refs/pull/N/merge).
	Ref string

	// SARIF is the raw SARIF JSON body (uncompressed, undeflated).
	// Adapters apply the platform's required encoding.
	SARIF []byte

	// Category is the tool-name surfaced in the Code Scanning UI.
	// Empty → no category in the request.
	Category string

	// Token is the code-scanning-alerts:write token. Adapters that
	// don't accept a token in their request shape ignore this.
	Token string
}

// Provider is the cross-platform port.
//
// Implementations live under internal/adapters/{github,gitlab,local}.
type Provider interface {
	// Name returns which platform this provider talks to.
	// Used by use cases that need to gate platform-only features
	// (e.g. SLSA L3 attestation).
	Name() Platform

	// ResolveContext extracts the EventContext from the platform's
	// native environment.
	ResolveContext(ctx context.Context) (*EventContext, error)

	// FetchRepoMetadata queries the provider's REST API for repo-level
	// metadata used to populate OCI labels (description, html_url, SPDX
	// license id). When the API call fails or the relevant field is
	// missing, a zero-valued field is returned (not an error) — OCI
	// labels are best-effort, never load-bearing for a release.
	//
	// repo is "owner/repo" on GitHub, "group/project/path" on GitLab.
	FetchRepoMetadata(ctx context.Context, repo string) (*RepoMetadata, error)

	// ValidateToken does the lowest-cost API call ("repos/<repo>" on
	// GitHub, "projects/<enc-path>" on GitLab) using the explicit token
	// passed in (not the adapter's env-stored one). Returns nil when the
	// API call succeeds, an error otherwise. Format checks (ghp_/ghs_/
	// github_pat_/glpat_) are caller-side responsibilities — this method
	// only verifies "the token works against the API right now".
	ValidateToken(ctx context.Context, token, repo string) error

	// ValidateBotPermissions probes the bot user's API access.
	// Returns a typed BotPermissions struct that the use case renders.
	ValidateBotPermissions(ctx context.Context, repo string) (*BotPermissions, error)

	// CreateRelease creates a release on the platform. Implementations:
	//   - github: shells out to `gh release create`, mirroring the
	//     existing bash semantics (delete-and-recreate of existing draft
	//     / prerelease tags, --notes-file forwarding, asset list).
	//   - gitlab: POST /api/v4/projects/{enc-path}/releases + per-asset
	//     links via /releases/{tag}/assets/links.
	//   - local: returns "not supported in local mode".
	CreateRelease(ctx context.Context, repo string, spec ReleaseSpec) error

	// UploadSARIF posts a SARIF report to the platform's code-scanning
	// surface. Implementations:
	//   - github: POSTs gzip+base64-encoded SARIF to
	//     /repos/{repo}/code-scanning/sarifs.
	//   - gitlab: returns errs.ErrUnsupported (GitLab has its own
	//     security-report shape — Trivy/OpenGrep emit it directly
	//     via the GitLab SAST format).
	//   - local: returns errs.ErrUnsupported.
	//
	// Callers gate fatality: missing token / file → skip without
	// hitting this method; transport / auth failures → propagate.
	UploadSARIF(ctx context.Context, up SARIFUpload) error
}
