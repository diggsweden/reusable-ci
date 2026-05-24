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

// Recognised Platform values.
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

// Recognised RefType values.
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

// Provider is the always-available base: every platform adapter
// implements it. Use cases that need richer capabilities depend on the
// role-specific interfaces below — `local.Provider` only implements
// this base + RepoMetadataFetcher, so use cases that need a missing
// role surface a typed error at the CLI boundary rather than a runtime
// "method returns ErrUnsupported" trap deep in the call stack.
type Provider interface {
	// Name returns which platform this provider talks to. Used by
	// use cases that gate platform-only features (e.g. SLSA L3).
	Name() Platform

	// ResolveContext extracts the EventContext from the platform's
	// native environment.
	ResolveContext(ctx context.Context) (*EventContext, error)
}

// RepoMetadataFetcher reads repo-level metadata for OCI label
// population. All three adapters implement it — `local` returns a
// zero-valued struct since there is no API to query (best-effort
// semantics, never load-bearing for a release).
//
// repo is "owner/repo" on GitHub, "group/project/path" on GitLab.
type RepoMetadataFetcher interface {
	FetchRepoMetadata(ctx context.Context, repo string) (*RepoMetadata, error)
}

// TokenValidator probes a release-bot token's API access.
// Implemented by github and gitlab; not by local (no API to probe).
//
// ValidateToken uses the lowest-cost authenticated GET ("repos/<repo>"
// on GitHub, "projects/<enc-path>" on GitLab) with the explicit token
// passed in (not the adapter's env-stored one). Token-format checks
// (ghp_/ghs_/github_pat_/glpat_) are caller-side — this method only
// verifies "the token works against the API right now".
//
// ValidateBotPermissions probes the bot user's API access.
type TokenValidator interface {
	ValidateToken(ctx context.Context, token, repo string) error
	ValidateBotPermissions(ctx context.Context, repo string) (*BotPermissions, error)
}

// ReleaseCreator creates a release on the platform. Implemented by
// github and gitlab; not by local.
//
//   - github: in-process via go-github (delete-and-recreate of existing
//     draft / prerelease tags, --notes-file forwarding, asset list).
//   - gitlab: POST /api/v4/projects/{enc-path}/releases + per-asset
//     links via /releases/{tag}/assets/links.
type ReleaseCreator interface {
	CreateRelease(ctx context.Context, repo string, spec ReleaseSpec) error
}

// ReleaseAssetUploader attaches a single file to an existing release,
// overwriting any existing asset with the same basename. Today only
// github implements this; the GitLab release-link API will land
// alongside the rest of GitLab CI support.
type ReleaseAssetUploader interface {
	UploadReleaseAsset(ctx context.Context, tag, file string) error
}

// SARIFUploader posts a SARIF report to the platform's code-scanning
// surface. Today only github implements this — GitLab has its own
// security-report shape that Trivy/OpenGrep emit directly via the
// GitLab SAST format, so no GitLab implementation is needed.
//
// Callers gate fatality: missing token / file → skip without hitting
// this interface; transport / auth failures → propagate.
type SARIFUploader interface {
	UploadSARIF(ctx context.Context, up SARIFUpload) error
}
