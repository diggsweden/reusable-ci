// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package provider defines the cross-platform provider port.
//
// Domain code depends only on this package. Adapters import it to
// implement the interface; the composition root in cli/deps wires the
// chosen adapter into use cases.
package provider

import "context"

// Platform identifies the *forge API* the binary talks to — the server
// REST surface used for releases, asset upload, token/permission checks,
// repo metadata, and SARIF. It is one of two orthogonal axes; the other
// is RunnerKind (the workflow-runner conventions). The axes are genuinely
// independent: Forgejo, for instance, is its own value on both — a
// distinct forge API (PlatformForgejo) and a distinct runner dialect
// (RunnerForgejo) — and keeping them separate is what lets one binary
// serve every combination without misrouting API calls or output.
type Platform string

// Recognised Platform (forge API) values.
const (
	PlatformGitHub  Platform = "github"
	PlatformGitLab  Platform = "gitlab"
	PlatformForgejo Platform = "forgejo"
	PlatformLocal   Platform = "local"
)

// String satisfies fmt.Stringer for ergonomic logging.
func (p Platform) String() string { return string(p) }

// AllPlatforms is the canonical, ordered set of forge-API platforms — the
// single source consumers (IsValid, the --provider flag help/validation)
// derive from, so adding a forge is one edit here.
func AllPlatforms() []Platform {
	return []Platform{PlatformGitHub, PlatformGitLab, PlatformForgejo, PlatformLocal}
}

// IsValid reports whether the value is one of the known platforms.
func (p Platform) IsValid() bool {
	for _, v := range AllPlatforms() {
		if p == v {
			return true
		}
	}

	return false
}

// RunnerKind identifies the *workflow-runner conventions* the binary
// emits for — output format, $*_OUTPUT key/value writes, annotation
// vocabulary, and step-summary file. It is the second axis alongside
// Platform (forge API).
//
// GitHub Actions and Forgejo Actions are deliberately *separate* runner
// kinds, not one shared "gha-compatible" value: Forgejo itself states it
// "is not designed to be compatible" with GitHub Actions, only familiar
// (https://forgejo.org/docs/latest/user/actions/github-actions/), and the
// differences are exactly in this binary's output surface — its native
// step-output var is $FORGEJO_OUTPUT (with $GITHUB_OUTPUT as a compat alias
// since Forgejo Runner 7.0.0), it documents NO job-summary variable at all,
// and it does not render `::error::`-style annotations, `::group::` log
// folds, or job summaries (go-gitea/gitea#27898; nektos/act#1187, #1533).
// Collapsing the two would emit workflow-command noise that Forgejo drops
// on the floor.
type RunnerKind string

// Recognised RunnerKind values.
const (
	// RunnerGitHub is the GitHub Actions workflow-command dialect
	// (::error::, ::group::, $GITHUB_OUTPUT k=v, $GITHUB_STEP_SUMMARY),
	// rendered in the run UI.
	RunnerGitHub RunnerKind = "github"

	// RunnerForgejo is Forgejo/Gitea Actions: $FORGEJO_OUTPUT for step
	// outputs (file-based, like GitHub's), but no UI rendering of
	// annotations / log groups / job summaries — so the binary emits
	// plain, readable log lines instead of workflow commands and routes
	// summaries to the job log. See the RunnerKind doc above.
	RunnerForgejo RunnerKind = "forgejo"

	// RunnerGitLab is GitLab CI's dialect (section_start/section_end,
	// dotenv $CI_OUTPUT appends, $CI_SUMMARY_FILE).
	RunnerGitLab RunnerKind = "gitlab"

	// RunnerLocal is a non-CI invocation (laptop / tests): plain text,
	// no machine-readable sink.
	RunnerLocal RunnerKind = "local"
)

// String satisfies fmt.Stringer for ergonomic logging.
func (r RunnerKind) String() string { return string(r) }

// AllRunnerKinds is the canonical, ordered set of runner conventions — the
// single source consumers (IsValid, the --runner flag help/validation)
// derive from.
func AllRunnerKinds() []RunnerKind {
	return []RunnerKind{RunnerGitHub, RunnerForgejo, RunnerGitLab, RunnerLocal}
}

// IsValid reports whether the value is one of the known runner kinds.
func (r RunnerKind) IsValid() bool {
	for _, v := range AllRunnerKinds() {
		if r == v {
			return true
		}
	}

	return false
}

// Info is a forge's self-description: the human labels and
// conventions a generic command needs without branching on the forge's
// identity. Each adapter returns its own values from Describe(), so app
// code consults this instead of `switch`-ing on Platform.
type Info struct {
	DisplayName string // human name, e.g. "GitHub", "GitLab", "local"
	SetupURL    string // where to create a release token ("" = none)
	ScopesHint  string // human hint for the required token scopes
	OIDCIssuer  string // default OIDC issuer ("" = caller must supply)
}

// Describer is implemented by providers that can describe their own
// conventions. Every adapter implements it; app code depends on this
// interface rather than the Platform enum.
type Describer interface{ Describe() Info }

// Capabilities reports which optional forge features are available, so
// commands can degrade gracefully rather than calling an endpoint a
// forge does not implement (e.g. Forgejo has no SARIF ingestion).
type Capabilities struct {
	SARIFUpload   bool `json:"sarif_upload"`   // ingest SARIF into a code-scanning surface
	Attestation   bool `json:"attestation"`    // SLSA build-provenance attestation API
	KeylessOIDC   bool `json:"keyless_oidc"`   // keyless signing via a runner OIDC issuer
	ReleaseAssets bool `json:"release_assets"` // upload binary assets onto a release
	RunArtifacts  bool `json:"run_artifacts"`  // programmatic intra-run artifact store (RunArtifactUploader/Downloader)
}

// CapabilityReporter is implemented by providers that report their
// feature set. Every adapter implements it.
type CapabilityReporter interface{ Capabilities() Capabilities }

// DeriveCapabilities computes the role-backed capability bools from what p
// actually implements, so the reported feature set can never drift from what
// the requireRole gates enforce. Two capabilities are not pure role
// membership and stay explicit: keylessOIDC (a provider may implement
// SigningIdentityResolver yet report false because no public Fulcio trusts
// its issuer — Forgejo) and attestation (no role interface exists yet).
//
// RunArtifacts requires the full Uploader+Downloader pair; a half-implemented
// pair reports false rather than promising a store that cannot round-trip.
func DeriveCapabilities(p any, keylessOIDC, attestation bool) Capabilities {
	_, sarif := p.(SARIFUploader)
	_, assets := p.(ReleaseAssetUploader)
	_, upload := p.(RunArtifactUploader)
	_, download := p.(RunArtifactDownloader)

	return Capabilities{
		SARIFUpload:   sarif,
		Attestation:   attestation,
		KeylessOIDC:   keylessOIDC,
		ReleaseAssets: assets,
		RunArtifacts:  upload && download,
	}
}

// TokenAdviser is implemented by providers whose tokens have
// recognisable shapes worth advising on (e.g. GitHub classic vs
// fine-grained PATs). Optional: providers without token conventions
// (GitLab, local) simply do not implement it.
type TokenAdviser interface {
	// AdviseToken inspects a token string and returns human advice plus
	// whether the token must be refused. Empty advice with reject=false
	// means "the shape is fine; say nothing".
	AdviseToken(token string) (advice string, reject bool)
}

// TagDeleter removes a single container tag from the forge's registry,
// keeping the underlying manifest. This is deliberately a per-forge role
// (each forge's package/registry API differs) — it is NOT a generic OCI
// operation: `skopeo delete` removes the manifest by digest, which would
// destroy the promoted image when staging and final tags share a digest
// (the shared-digest "promote a verified candidate" model). The forge's
// package API is the only safe, tag-scoped delete.
//
// The method shape matches imageledger.TagDeleter, so a forge adapter
// satisfies both the role here and the ledger domain port without either
// domain importing the other.
type TagDeleter interface {
	// DeleteTag removes the tag named by ref (e.g.
	// "codeberg.org/owner/repo:staging-v1.2.3"), keeping the manifest.
	DeleteTag(ctx context.Context, ref string) error
}

// ContainerPackageLister enumerates the versions (tags) of one container
// package through the forge's package API. Like TagDeleter, this is a
// per-forge role rather than a generic OCI operation: listing tags via the
// package API is what lets base-image cleanup enumerate stale staging
// versions to delete without touching manifests by digest. Only forges with
// a package API implement it (Forgejo via the Gitea SDK today; GitHub's GHCR
// packages API could add it); callers reach it through
// Deps.RequireContainerPackageLister and degrade with a typed ErrUnsupported
// elsewhere.
type ContainerPackageLister interface {
	// ListContainerPackageVersions returns every version recorded for the
	// container package owner/name.
	ListContainerPackageVersions(ctx context.Context, owner, name string) ([]string, error)
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
	Platform Platform
	RefName  string // "main", "v1.2.3", etc.
	RefType  RefType
	SHA      string // full commit SHA
	ShortSHA string // 7-char short SHA
	Branch   string // best-effort branch name (PR head branch on PR refs)
	PRNumber string // empty when not a PR
	// EventName is the workflow trigger in the canonical vocabulary (the
	// GitHub-Actions spellings: "push", "pull_request",
	// "workflow_dispatch", "schedule", …). Adapters normalize their
	// forge's dialect (GitLab's CI_PIPELINE_SOURCE) and pass unknown
	// values through verbatim so fail-closed gates stay closed.
	EventName string
	Repo      string // "owner/repo" or "group/project/path"
	RepoURL   string // canonical web URL
}

// RepoMetadata is what adapter.FetchRepoMetadata returns. Used by the
// container OCI-label assembly.
type RepoMetadata struct {
	Description string
	HTMLURL     string
	LicenseSPDX string
	// ObjectFormat is the repository hash algorithm ("sha1" or "sha256"),
	// when the forge reports it. Empty means the forge does not expose it
	// (e.g. GitHub) — callers default to sha1. `platform checkout` reads
	// this to git-init the working tree with the right format instead of
	// the curl+sed metadata probe the shell checkout used.
	ObjectFormat string
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

// MakeLatestMode is the platform value for release "latest" handling. GitHub
// accepts "true", "false", and "legacy"; other forges may ignore it.
type MakeLatestMode string

// MakeLatest modes select how a release marks itself "latest": force true,
// force false, or defer to the platform's default (legacy).
const (
	MakeLatestTrue   MakeLatestMode = "true"
	MakeLatestFalse  MakeLatestMode = "false"
	MakeLatestLegacy MakeLatestMode = "legacy"
)

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
	MakeLatest MakeLatestMode
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

	// SARIF is the raw SARIF JSON body (uncompressed, undeflated). The
	// analysis category is carried INSIDE this body as each run's
	// automationDetails.id (set app-side by security.SetSARIFCategory) — the
	// field Code Scanning keys analyses on. Adapters apply the platform's
	// required encoding and do not handle category separately.
	SARIF []byte

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

// ReleasePublisher creates or updates a release and reconciles its assets to
// match the supplied spec. Unlike ReleaseCreator, this role never deletes the
// release object during an update; providers update release metadata in place,
// replace colliding assets by basename, upload desired assets, and remove stale
// assets no longer present in spec.Assets.
type ReleasePublisher interface {
	PublishRelease(ctx context.Context, repo string, spec ReleaseSpec) error
}

// ReleaseAssetUploader attaches a single file to an existing release,
// overwriting any existing asset with the same basename.
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

// RunArtifactUploader and RunArtifactDownloader model the ephemeral,
// per-workflow-run artifact store — the thing actions/upload-artifact and
// actions/download-artifact talk to. This is deliberately distinct from
// the permanent release-asset roles above: run artifacts are an intra-CI
// hand-off (build job → sign job), not published release assets.
//
// The interface is transport-free on purpose. Behind it each forge brings
// its own mechanism:
//   - forgejo: the in-run Actions runtime service (ACTIONS_RUNTIME_URL +
//     ACTIONS_RUNTIME_TOKEN, v3 container protocol) — same-run scope only.
//   - github: the repo REST artifacts API (cross-run reads) for download;
//     upload is runtime-only and may be unimplemented (ErrUnsupported).
//   - gitlab: intentionally absent (Capabilities.RunArtifacts=false). GitLab
//     passes intra-pipeline artifacts declaratively through the job YAML
//     (`artifacts:` + `needs:`/`dependencies:`), not a programmatic in-job
//     API, so the hand-off lives in the GitLab template, not this binary.
//     RequireRunArtifact* returns ErrUnsupported there by design.
//
// Contract, independent of transport:
//   - Exactly one artifact must match Name; zero or many is an error.
//   - Download with an empty RunID means the current run. A provider that
//     cannot reach other runs (forgejo's run-scoped runtime token) MUST
//     return ErrUnsupported for an explicit foreign RunID rather than
//     silently downloading the wrong thing.
//   - Extraction is hardened: entries that escape the destination, are
//     absolute, traverse "..", or are symlinks are rejected; per-file size
//     is capped; credentials live only in transient request headers, never
//     in argv or on disk.
type RunArtifactUploader interface {
	UploadRunArtifact(ctx context.Context, in RunArtifactUpload) (RunArtifactInfo, error)
}

// RunArtifactDownloader fetches a named run artifact into a directory.
type RunArtifactDownloader interface {
	DownloadRunArtifact(ctx context.Context, in RunArtifactDownload) (RunArtifactInfo, error)
}

// IfNoFilesPolicy selects what UploadRunArtifact does when the input
// matches no files — mirroring actions/upload-artifact's if-no-files-found.
type IfNoFilesPolicy string

const (
	// IfNoFilesError fails the upload (the safe default).
	IfNoFilesError IfNoFilesPolicy = "error"
	// IfNoFilesWarn logs and uploads nothing.
	IfNoFilesWarn IfNoFilesPolicy = "warn"
	// IfNoFilesIgnore silently uploads nothing.
	IfNoFilesIgnore IfNoFilesPolicy = "ignore"
)

// RunArtifactUpload describes one upload into the current run. Exactly one
// of Dir (upload the tree) or Files (an explicit set) is used.
type RunArtifactUpload struct {
	Name string
	Dir  string
	// Files are explicit literal files, flattened to their basenames.
	Files []string
	// Paths are glob patterns (*, ?, [set], ** and !excludes) whose matches
	// preserve directory structure relative to their common root — the
	// actions/upload-artifact `path:` contract. Mutually exclusive with Dir.
	Paths         []string
	RetentionDays int // 0 = forge default
	IfNoFiles     IfNoFilesPolicy
	// IncludeHidden uploads dotfiles/hidden entries. Off by default, matching
	// actions/upload-artifact's include-hidden-files (false) — so a stray
	// .git or .npmrc is never published unless explicitly asked for.
	IncludeHidden bool
}

// RunArtifactDownload describes a download. RunID/Repository empty mean the
// current run/repo. Either Name (one exact artifact) or Pattern (a glob over
// artifact names, downloading every match) is given.
type RunArtifactDownload struct {
	Name string
	// Pattern is a glob over artifact names (the JS download-artifact
	// `pattern:`). When set, every matching artifact downloads and Name is
	// ignored.
	Pattern string
	// MergeMultiple flattens every matched artifact's contents into Dir (the JS
	// `merge-multiple: true`); otherwise each lands in Dir/<artifact-name>/.
	MergeMultiple bool
	Dir           string
	RunID         string
	Repository    string
}

// RunArtifactInfo is the result of an upload or download: the artifact
// name, the forge's opaque id (when known), and the byte/file totals.
type RunArtifactInfo struct {
	Name      string
	ID        string
	Bytes     int64
	FileCount int
}
