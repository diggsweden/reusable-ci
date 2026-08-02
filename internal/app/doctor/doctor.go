// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package doctor implements `reusable-ci doctor` — a setup-validation
// pass for repositories that adopt reusable-ci. The goal is the same
// as `kubectl version --short` or `terraform validate`: a single
// command that says "your setup is correct" or "here's specifically
// what's wrong and how to fix it."
//
// The check set is deliberately small and grows with adoption pain
// points. Each check is independent; one failure does not block the
// others from running, so the operator gets a complete punch-list.
package doctor

import (
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"
)

// Check names. Each constant is the human-facing identifier of one
// diagnostic; the same Name is reused across every Check that
// reports on that aspect (the not-found, wrong-kind, and OK paths
// for one check all carry the same Name so operator-facing summaries
// group them correctly).
const (
	checkArtifactsYMLPresent = "artifacts.yml present"
	//nolint:gosec // G101 false positive: this is a human-readable check name; the word "token" refers to the GHA OIDC id-token permission, not a credential literal.
	checkWorkflowIDToken      = "workflow id-token permission"
	checkWorkflowsPinReusable = "workflows pin reusable-ci to a tag"
)

// Severity categorises a Check result.
type Severity string

// Recognised Severity values. Ordering: ok < warn < fail (a fail
// short-circuits the overall exit code; warn doesn't).
const (
	SeverityOK   Severity = "ok"
	SeverityWarn Severity = "warn"
	SeverityFail Severity = "fail"
)

// Check is the result of one diagnostic. Name identifies the check
// in operator-facing output; Message describes what was observed;
// Remediation is the actionable fix when Severity != ok.
type Check struct {
	Name        string   `json:"name"`
	Severity    Severity `json:"severity"`
	Message     string   `json:"message"`
	Remediation string   `json:"remediation,omitempty"`
}

// Input drives Run.
type Input struct {
	// Root is the repository root. Empty defaults to the process cwd.
	Root string
	// ArtifactsPath overrides the .reusable-ci/artifacts.yml lookup.
	ArtifactsPath string
	// RepoSlug is the "owner/repo" this binary belongs to, used by the
	// workflow-pin check to spot `uses: <slug>@main`. Empty → derived from
	// the binary's own Go module path at runtime, so a fork that renames its
	// module checks its own slug without a rebuild. Overridable via the
	// --reusable-ci-repo flag / REUSABLE_CI_REPO_SLUG for vendored or mirrored
	// setups.
	RepoSlug string

	// KeylessAvailable reports whether the detected forge can drive Sigstore
	// keyless signing (provider.Capabilities.KeylessOIDC). The CLI resolves it
	// from the active provider; it drives the (non-failing) signing-method
	// recommendation. False off a keyless-capable forge (or when undetectable
	// locally) simply suppresses the recommendation.
	KeylessAvailable bool
}

// Run executes every check and returns the result list. The caller
// formats and decides exit policy. Any error returned from Run is a
// runtime problem (e.g. cwd unreadable), not a check failure — those
// are encoded in the returned slice.
func Run(in Input) ([]Check, error) {
	root := in.Root
	if root == "" {
		wd, err := os.Getwd()
		if err != nil {
			return nil, fmt.Errorf("doctor: resolve cwd: %w", err)
		}

		root = wd
	}

	artifactsPath := in.ArtifactsPath
	if artifactsPath == "" {
		artifactsPath = filepath.Join(root, ".reusable-ci", "artifacts.yml")
	}

	checks := make([]Check, 0, 8)
	checks = append(checks, checkArtifactsExists(artifactsPath))

	cfg, parseCheck, parsed := parseArtifacts(artifactsPath)
	checks = append(checks, parseCheck)

	if !parsed {
		// Without a parsed config the remaining sign-aware checks
		// have nothing to act on — but we still flag missing pieces
		// the operator should fix first.
		return checks, nil
	}

	repoSlug := in.RepoSlug
	if repoSlug == "" {
		repoSlug = deriveSelfRepoSlug()
	}

	checks = append(checks,
		checkSignBlock(cfg.Sign),
		checkAllowedSignersIfRequired(root, cfg.Artifacts),
		checkWorkflowPermissions(root, cfg.Sign),
		checkWorkflowVersionRefs(root, repoSlug),
	)

	if rec, ok := signMethodRecommendation(cfg, in.KeylessAvailable); ok {
		checks = append(checks, rec)
	}

	if c, ok := checkGitSigningSSHAllowlist(root, cfg); ok {
		checks = append(checks, c)
	}

	return checks, nil
}

// signMethodRecommendation returns a non-failing advisory (and true) when the
// repo signs artifacts with gpg, the detected forge supports keyless, and no
// artifact publishes to a PGP-native registry — i.e. when sigstore-keyless
// would be a strict improvement (no key on the runner) with no downside. It
// stays silent (false) otherwise: keyless unavailable, already on
// sigstore/kms, or a Maven Central publish that genuinely needs a PGP .asc.
//
// Severity is OK: gpg is a valid choice, so this is guidance, not a warning,
// and never changes the exit code.
func signMethodRecommendation(cfg *config.Config, keylessAvailable bool) (Check, bool) {
	if cfg.Sign.EffectiveMethod() != domainrelease.SignMethodGPG ||
		!keylessAvailable ||
		requiresPGPSignature(cfg.Artifacts) {
		return Check{}, false
	}

	return Check{
		Name:     "signing method recommendation",
		Severity: SeverityOK,
		Message: "sign.method=gpg, but this forge supports keyless (sigstore) signing — " +
			"keyless needs no key on the runner. Consider sign.method: sigstore " +
			"(see docs/verification.md#choosing-a-method); keep gpg if you later publish to a PGP-native registry.",
	}, true
}

// checkGitSigningSSHAllowlist fires only when git-signing.method=ssh. SSH-
// signed release tags/commits are verified with `ssh-keygen -Y verify`, whose
// principal is the committer/tagger email matched against
// .reusable-ci/allowed_signers (this is what `validate tag signature` does).
// So the committer email the release passes MUST appear in that file, and the
// file must exist — otherwise the signed tag cannot be verified. doctor can
// confirm the file's presence (it cannot see the CI-supplied committer email),
// and reminds the operator of the coupling. Returns ok=false for the gpg
// default so no noise appears there.
func checkGitSigningSSHAllowlist(root string, cfg *config.Config) (Check, bool) {
	if cfg.GitSigning.EffectiveMethod() != config.GitSignSSH {
		return Check{}, false
	}

	name := "ssh git-signing allowlist"
	allowedSigners := filepath.Join(root, ".reusable-ci", "allowed_signers")

	if !regularFileExists(allowedSigners) {
		return Check{
			Name:     name,
			Severity: SeverityWarn,
			Message: "git-signing.method=ssh, but .reusable-ci/allowed_signers is missing — " +
				"SSH-signed release tags/commits verify against it (the committer email is the principal)",
			Remediation: "commit .reusable-ci/allowed_signers mapping the release identity " +
				"(the committer-email passed to the release) to its SSH public key; " +
				"see docs/verification.md#release-authorisation",
		}, true
	}

	return Check{
		Name:     name,
		Severity: SeverityOK,
		Message: ".reusable-ci/allowed_signers present — ensure the configured committer-email is one of " +
			"its principals so SSH-signed tag verification (validate tag signature) passes",
	}, true
}

// requiresPGPSignature reports whether any artifact publishes to a registry
// that mandates a detached OpenPGP signature (Maven Central), where gpg is the
// correct backend and the keyless recommendation must not fire.
func requiresPGPSignature(artifacts []config.Artifact) bool {
	for _, a := range artifacts {
		for _, target := range a.PublishTo {
			if target == config.PublishMavenCentral {
				return true
			}
		}
	}

	return false
}

// Report is the machine-readable doctor result emitted by FormatJSON
// when --json is passed: the resolved environment plus the check list and
// a failure count, so a CI gate can parse "is this repo set up correctly?"
// programmatically instead of scraping the human text.
type Report struct {
	Environment Environment `json:"environment"`
	Checks      []Check     `json:"checks"`
	Failures    int         `json:"failures"`
}

// FormatJSON writes the report as indented JSON (one document, trailing
// newline) to w. The structured counterpart to FormatEnvironment +
// FormatText, selected by the global --json / --format=json flag.
func FormatJSON(w io.Writer, report Report) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	body, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		return fmt.Errorf("doctor: marshal report: %w", err)
	}

	if _, err := w.Write(append(body, '\n')); err != nil {
		return fmt.Errorf("doctor: write report: %w", err)
	}

	return nil
}

// CountFailures returns how many checks have SeverityFail — the basis
// for both the report's Failures field and the command's exit code.
func CountFailures(checks []Check) int {
	failures := 0

	for _, c := range checks {
		if c.Severity == SeverityFail {
			failures++
		}
	}

	return failures
}

// FormatText writes a human-readable check list to w. Lines are
// prefixed with [OK]/[WARN]/[FAIL]; remediation hints are indented
// underneath the failing check.
func FormatText(w io.Writer, checks []Check) {
	for _, c := range checks {
		_, _ = fmt.Fprintf(w, "[%s] %s: %s\n", strings.ToUpper(string(c.Severity)), c.Name, c.Message)

		if c.Remediation != "" {
			_, _ = fmt.Fprintf(w, "       fix: %s\n", c.Remediation)
		}
	}
}

// Environment is the resolved CI runtime the doctor report prints up
// front: which forge API the binary will call, which runner conventions
// it will emit, and which optional forge features are available. The CLI
// resolves these (env detection + provider) and hands them in; this
// layer only formats, so it stays free of env reads and platform
// branches.
type Environment struct {
	Provider     string                `json:"provider"`  // forge display name, e.g. "Forgejo"
	ForgeAPI     string                `json:"forge_api"` // provider.Platform string, e.g. "forgejo"
	Runner       string                `json:"runner"`    // provider.RunnerKind string, e.g. "github" / "forgejo"
	Capabilities provider.Capabilities `json:"capabilities"`
}

// FormatEnvironment writes the resolved-runtime + capability matrix
// block. Capabilities that are unavailable carry a one-line hint on how
// findings/behaviour degrade, so a missing feature (e.g. Forgejo has no
// SARIF ingestion) is visible before it surprises an adopter.
func FormatEnvironment(w io.Writer, env Environment) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	_, _ = fmt.Fprintf(w, "Environment:\n")
	_, _ = fmt.Fprintf(w, "  provider (forge API): %s (%s)\n", env.Provider, env.ForgeAPI)
	_, _ = fmt.Fprintf(w, "  runner conventions:   %s\n", env.Runner)
	_, _ = fmt.Fprintf(w, "Capabilities:\n")

	rows := []struct {
		name string
		on   bool
		hint string
	}{
		{"SARIF / Code Scanning upload", env.Capabilities.SARIFUpload, "findings degrade to the step-summary + uploaded artifact"},
		{"SLSA build provenance", env.Capabilities.Attestation, "no attestation API; provenance is not published on this forge"},
		{"keyless OIDC signing", env.Capabilities.KeylessOIDC, "use key-based signing and pass --oidc-issuer explicitly"},
		{"release asset upload", env.Capabilities.ReleaseAssets, "release assets cannot be attached on this forge"},
	}

	for _, row := range rows {
		mark := "yes"
		if !row.on {
			mark = "no"
		}

		_, _ = fmt.Fprintf(w, "  [%3s] %s\n", mark, row.name)

		if !row.on {
			_, _ = fmt.Fprintf(w, "        → %s\n", row.hint)
		}
	}
}

// ExitCode returns 0 when every check is ok or warn; non-zero when
// any check is fail. Warnings are advisory and do NOT change the
// exit code — they exist so an operator can fix lower-priority
// items at their own pace.
func ExitCode(checks []Check) int {
	for _, c := range checks {
		if c.Severity == SeverityFail {
			return int(errs.ExitCodeValidation)
		}
	}

	return 0
}

func checkArtifactsExists(path string) Check {
	info, err := os.Stat(path)
	if err != nil {
		return Check{
			Name:     checkArtifactsYMLPresent,
			Severity: SeverityFail,
			Message:  path + " not found",
			Remediation: "create .reusable-ci/artifacts.yml; see examples/ for ecosystem templates " +
				"(go-cli, maven-app, openbao-kms, sigstore-keyless)",
		}
	}

	if info.IsDir() {
		return Check{
			Name:        checkArtifactsYMLPresent,
			Severity:    SeverityFail,
			Message:     path + " is a directory, expected a file",
			Remediation: "rename or remove the directory and create artifacts.yml in its place",
		}
	}

	return Check{
		Name:     checkArtifactsYMLPresent,
		Severity: SeverityOK,
		Message:  path,
	}
}

func parseArtifacts(path string) (*config.Config, Check, bool) {
	body, err := os.ReadFile(path) //nolint:gosec // operator-supplied repo path; trusted scope.
	if err != nil {
		return nil, Check{
			Name:        "artifacts.yml parses",
			Severity:    SeverityFail,
			Message:     fmt.Sprintf("read %s: %v", path, err),
			Remediation: "ensure the file is readable by the user running doctor",
		}, false
	}

	cfg, err := config.Parse(body)
	if err != nil {
		return nil, Check{
			Name:        "artifacts.yml parses",
			Severity:    SeverityFail,
			Message:     err.Error(),
			Remediation: "validate against .reusable-ci/artifacts.schema.json or run `reusable-ci config parse-artifacts --file " + path + "`",
		}, false
	}

	if err := config.Validate(cfg); err != nil {
		return cfg, Check{
			Name:        "artifacts.yml validates",
			Severity:    SeverityFail,
			Message:     err.Error(),
			Remediation: "see docs/artifacts-reference.md for the per-field schema",
		}, true
	}

	return cfg, Check{
		Name:     "artifacts.yml parses + validates",
		Severity: SeverityOK,
		Message:  fmt.Sprintf("%d artifact(s), %d container(s), sign.method=%s", len(cfg.Artifacts), len(cfg.Containers), cfg.Sign.EffectiveMethod()),
	}, true
}

func checkSignBlock(sign config.SignConfig) Check {
	if err := sign.Validate(); err != nil {
		return Check{
			Name:        "sign block valid",
			Severity:    SeverityFail,
			Message:     err.Error(),
			Remediation: "see docs/verification.md#signing-methods-for-release-artefacts for the per-method invariants",
		}
	}

	return Check{
		Name:     "sign block valid",
		Severity: SeverityOK,
		Message:  fmt.Sprintf("method=%s", sign.EffectiveMethod()),
	}
}

func checkAllowedSignersIfRequired(root string, artifacts []config.Artifact) Check {
	required := false

	for _, a := range artifacts {
		if a.RequireAuthorization {
			required = true

			break
		}
	}

	if !required {
		return Check{
			Name:     "release-authorization allowlist (n/a)",
			Severity: SeverityOK,
			Message:  "no artifact has require-authorization: true",
		}
	}

	ssh := filepath.Join(root, ".reusable-ci", "allowed_signers")
	gpg := filepath.Join(root, ".reusable-ci", "allowed_gpg_keys.asc")

	if regularFileExists(ssh) || regularFileExists(gpg) {
		return Check{
			Name:     "release-authorization allowlist present",
			Severity: SeverityOK,
			Message:  "found .reusable-ci/allowed_signers and/or allowed_gpg_keys.asc",
		}
	}

	return Check{
		Name:     "release-authorization allowlist present",
		Severity: SeverityFail,
		Message:  "require-authorization is true but neither .reusable-ci/allowed_signers nor allowed_gpg_keys.asc exists",
		Remediation: "commit .reusable-ci/allowed_signers (SSH OpenSSH format) and/or " +
			".reusable-ci/allowed_gpg_keys.asc (armored GPG public keys of authorised signers); see docs/verification.md#release-authorisation",
	}
}

func checkWorkflowPermissions(root string, sign config.SignConfig) Check {
	if sign.EffectiveMethod() != domainrelease.SignMethodSigstore {
		return Check{
			Name:     "workflow id-token permission (n/a)",
			Severity: SeverityOK,
			Message:  "sign.method != sigstore; OIDC token not required",
		}
	}

	// Sigstore-keyless needs the workflow to grant the runner an OIDC
	// id-token, and the grant is forge-specific: GitHub uses
	// `permissions: id-token: write` under .github/workflows; Forgejo IGNORES
	// `permissions` and instead uses `enable-openid-connect` under
	// .forgejo/workflows (https://forgejo.org/docs/v15.0/user/actions/security-openid-connect/).
	// A repo driven by this forge-neutral tool may target either, and doctor
	// usually runs locally (forge undetectable), so accept whichever
	// convention is present. Full expression evaluation is out of scope; this
	// catches the common "operator forgot the grant" case.
	conventions := []struct{ dir, marker, hint string }{
		{filepath.Join(".github", "workflows"), "id-token: write", ".github/workflows/ grants `permissions: id-token: write`"},
		{filepath.Join(".forgejo", "workflows"), "enable-openid-connect", ".forgejo/workflows/ sets `enable-openid-connect`"},
	}

	anyDir := false

	for _, conv := range conventions {
		dir := filepath.Join(root, conv.dir)
		if _, statErr := os.Stat(dir); statErr != nil {
			continue
		}

		anyDir = true

		found, walkErr := anyWorkflowContains(dir, conv.marker)
		if walkErr != nil {
			return Check{
				Name:     checkWorkflowIDToken,
				Severity: SeverityWarn,
				Message:  fmt.Sprintf("could not scan %s: %v", dir, walkErr),
				Remediation: "manually verify a caller workflow grants the OIDC id-token " +
					"(GitHub: `permissions: id-token: write`; Forgejo: `enable-openid-connect`)",
			}
		}

		if found {
			return Check{
				Name:     checkWorkflowIDToken,
				Severity: SeverityOK,
				Message:  "a workflow grants the OIDC id-token (" + conv.hint + ")",
			}
		}
	}

	return missingIDTokenCheck(anyDir)
}

// missingIDTokenCheck builds the failure when no workflow grants the OIDC
// id-token: distinguishing "no workflows dir at all" from "dir present but no
// grant", with remediation covering both the GitHub and Forgejo conventions.
func missingIDTokenCheck(anyDir bool) Check {
	remediation := "grant the OIDC id-token in the caller workflow — " +
		"GitHub: `permissions: id-token: write` under .github/workflows/; " +
		"Forgejo: `enable-openid-connect` under .forgejo/workflows/; " +
		"see examples/sigstore-keyless/release-workflow.yml"

	if !anyDir {
		return Check{
			Name:        checkWorkflowIDToken,
			Severity:    SeverityFail,
			Message:     "no .github/workflows/ or .forgejo/workflows/ but sign.method=sigstore needs a workflow granting the OIDC id-token",
			Remediation: remediation,
		}
	}

	return Check{
		Name:        checkWorkflowIDToken,
		Severity:    SeverityFail,
		Message:     "sign.method=sigstore but no workflow grants the OIDC id-token",
		Remediation: remediation,
	}
}

// anyWorkflowGrantsIDToken walks workflowsDir and returns true when
// at least one *.yml / *.yaml file contains the literal substring
// `id-token: write`. Unreadable workflows are skipped (a permission
// scan is best-effort, not authoritative).
// anyWorkflowContains reports whether any *.yml/*.yaml under workflowsDir
// contains marker (the forge-specific OIDC-grant substring).
func anyWorkflowContains(workflowsDir, marker string) (bool, error) {
	found := false

	walkErr := filepath.WalkDir(workflowsDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() {
			return nil
		}

		if !strings.HasSuffix(path, ".yml") && !strings.HasSuffix(path, ".yaml") {
			return nil
		}

		body, err := os.ReadFile(path) //nolint:gosec // walked workflow path under a known dir.
		if err != nil {
			return nil //nolint:nilerr // unreadable workflow shouldn't fail the whole check.
		}

		if strings.Contains(string(body), marker) {
			found = true
		}

		return nil
	})

	return found, walkErr
}

func checkWorkflowVersionRefs(root, repoSlug string) Check {
	if repoSlug == "" {
		// We couldn't determine our own module slug, so we can't tell which
		// `uses:` lines reference this project. Surface it rather than
		// silently passing a security-relevant check.
		return Check{
			Name:        checkWorkflowsPinReusable,
			Severity:    SeverityWarn,
			Message:     "could not determine this binary's repo slug; workflow-pin check skipped",
			Remediation: "set REUSABLE_CI_REPO_SLUG (or --reusable-ci-repo) to your owner/repo so the @main/@master pin check can run",
		}
	}

	// The uses:@ref pin matters on any Actions-style forge, so scan both
	// GitHub's .github/workflows and Forgejo's .forgejo/workflows (same
	// uses: syntax). GitLab's include: pinning is a separate mechanism.
	var (
		scanned  bool
		floating []string
	)

	for _, rel := range actionsWorkflowDirRels() {
		dir := filepath.Join(root, rel)
		if _, statErr := os.Stat(dir); statErr != nil {
			continue
		}

		scanned = true

		found, walkErr := findFloatingReusableCIRefs(dir, repoSlug)
		if walkErr != nil {
			return Check{
				Name:     checkWorkflowsPinReusable,
				Severity: SeverityWarn,
				Message:  fmt.Sprintf("could not scan %s: %v", dir, walkErr),
			}
		}

		floating = append(floating, found...)
	}

	if !scanned {
		// No Actions workflows at all is a valid setup (e.g. a GitLab-only
		// project) — nothing to pin, so don't warn.
		return Check{
			Name:     checkWorkflowsPinReusable,
			Severity: SeverityOK,
			Message:  ".github/workflows/ and .forgejo/workflows/ absent; no Actions workflows to scan",
		}
	}

	if len(floating) > 0 {
		return Check{
			Name:        checkWorkflowsPinReusable,
			Severity:    SeverityWarn,
			Message:     "found @main/@master references in " + strings.Join(floating, ", "),
			Remediation: "pin to a tag (e.g. @v3.0.0) so a re-tagged upstream main can't change your CI without a PR",
		}
	}

	return Check{
		Name:     checkWorkflowsPinReusable,
		Severity: SeverityOK,
		Message:  "no @main / @master references found",
	}
}

// actionsWorkflowDirRels are the Actions-style workflow directories this
// tool scans, relative to repo root: GitHub's .github/workflows and
// Forgejo's .forgejo/workflows. Both use the same uses:@ref / Actions YAML;
// Forgejo just lives elsewhere. GitLab's include: pinning is checked
// separately. Shared so the id-token and pin checks never silently skip a
// Forgejo repo.
func actionsWorkflowDirRels() []string {
	return []string{
		filepath.Join(".github", "workflows"),
		filepath.Join(".forgejo", "workflows"),
	}
}

// findFloatingReusableCIRefs walks workflowsDir and returns the
// basenames of every YAML file that references repoSlug (this binary's
// own owner/repo) via @main or @master rather than a tagged version.
func findFloatingReusableCIRefs(workflowsDir, repoSlug string) ([]string, error) {
	var floating []string

	walkErr := filepath.WalkDir(workflowsDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return err //nolint:wrapcheck // walker error type expected upstream.
		}

		if !strings.HasSuffix(path, ".yml") && !strings.HasSuffix(path, ".yaml") {
			return nil
		}

		body, err := os.ReadFile(path) //nolint:gosec // walked path under .github/workflows.
		if err != nil {
			return nil //nolint:nilerr // unreadable workflow shouldn't fail the whole check.
		}

		if hasFloatingReusableCIRef(body, repoSlug) {
			floating = append(floating, filepath.Base(path))
		}

		return nil
	})

	return floating, walkErr
}

// hasFloatingReusableCIRef reports whether body contains a
// `uses: ...<repoSlug>...@main` or `@master` line.
func hasFloatingReusableCIRef(body []byte, repoSlug string) bool {
	for _, line := range strings.Split(string(body), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "uses:") {
			continue
		}

		if strings.Contains(trimmed, repoSlug) && (strings.Contains(trimmed, "@main") || strings.Contains(trimmed, "@master")) {
			return true
		}
	}

	return false
}

// deriveSelfRepoSlug returns the "owner/repo" slug of this binary's own
// module (e.g. "diggsweden/reusable-ci"), read from the build's module path
// at runtime. A fork that renames its Go module gets its own slug for free;
// callers override via REUSABLE_CI_REPO_SLUG when the running repo differs
// from the module path (vendoring, mirrors). Returns "" when the module path
// is unavailable, in which case the pin check degrades to a skip rather than
// asserting someone else's identity.
func deriveSelfRepoSlug() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}

	return repoSlugFromModulePath(bi.Main.Path)
}

// repoSlugFromModulePath turns a Go module path into the "owner/repo" slug
// used in workflow `uses:` lines: it drops the host segment and any trailing
// "/vN" major-version element. "github.com/diggsweden/reusable-ci" →
// "diggsweden/reusable-ci"; "" for paths too short to carry a slug.
func repoSlugFromModulePath(modPath string) string {
	parts := strings.Split(modPath, "/")
	if n := len(parts); n > 0 {
		if last := parts[n-1]; len(last) > 1 && last[0] == 'v' && allDigits(last[1:]) {
			parts = parts[:n-1]
		}
	}

	if len(parts) < 2 {
		return ""
	}

	return parts[len(parts)-2] + "/" + parts[len(parts)-1]
}

// allDigits reports whether value is non-empty and all ASCII digits.
func allDigits(value string) bool {
	for i := range len(value) {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}

	return value != ""
}

func regularFileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}

	return info.Mode().IsRegular()
}
