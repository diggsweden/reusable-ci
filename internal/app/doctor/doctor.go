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
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/config"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	domain "github.com/diggsweden/reusable-ci/internal/domain/release"
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
	Name        string
	Severity    Severity
	Message     string
	Remediation string
}

// Input drives Run.
type Input struct {
	// Root is the repository root. Empty defaults to the process cwd.
	Root string
	// ArtifactsPath overrides the .reusable-ci/artifacts.yml lookup.
	ArtifactsPath string
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

	checks = append(checks,
		checkSignBlock(cfg.Sign),
		checkAllowedSignersIfRequired(root, cfg.Artifacts),
		checkWorkflowPermissions(root, cfg.Sign),
		checkWorkflowVersionRefs(root),
	)

	return checks, nil
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
	gpg := filepath.Join(root, ".reusable-ci", "allowed_gpg_fingerprints")

	if regularFileExists(ssh) || regularFileExists(gpg) {
		return Check{
			Name:     "release-authorization allowlist present",
			Severity: SeverityOK,
			Message:  "found .reusable-ci/allowed_signers and/or allowed_gpg_fingerprints",
		}
	}

	return Check{
		Name:     "release-authorization allowlist present",
		Severity: SeverityFail,
		Message:  "require-authorization is true but neither .reusable-ci/allowed_signers nor allowed_gpg_fingerprints exists",
		Remediation: "commit .reusable-ci/allowed_signers (SSH OpenSSH format) and/or " +
			".reusable-ci/allowed_gpg_fingerprints (40-char hex, one per line); see docs/verification.md#release-authorisation",
	}
}

func checkWorkflowPermissions(root string, sign config.SignConfig) Check {
	if sign.EffectiveMethod() != domain.SignMethodSigstore {
		return Check{
			Name:     "workflow id-token permission (n/a)",
			Severity: SeverityOK,
			Message:  "sign.method != sigstore; OIDC token not required",
		}
	}

	// Cheap grep across .github/workflows for `id-token: write`.
	// A full GHA expression evaluation is out of scope; this catches
	// the common case (operator forgot the permissions block).
	workflowsDir := filepath.Join(root, ".github", "workflows")

	if _, statErr := os.Stat(workflowsDir); os.IsNotExist(statErr) {
		return Check{
			Name:     checkWorkflowIDToken,
			Severity: SeverityFail,
			Message:  ".github/workflows/ absent but sign.method=sigstore requires a workflow with id-token: write",
			Remediation: "create a release workflow under .github/workflows/ that grants `permissions: id-token: write`; " +
				"see examples/sigstore-keyless/release-workflow.yml",
		}
	}

	found, walkErr := anyWorkflowGrantsIDToken(workflowsDir)
	if walkErr != nil {
		return Check{
			Name:     checkWorkflowIDToken,
			Severity: SeverityWarn,
			Message:  fmt.Sprintf("could not scan %s: %v", workflowsDir, walkErr),
			Remediation: "manually verify the caller workflow grants `permissions: id-token: write` " +
				"(required for Sigstore-keyless signing)",
		}
	}

	if found {
		return Check{
			Name:     checkWorkflowIDToken,
			Severity: SeverityOK,
			Message:  "at least one workflow under .github/workflows/ grants id-token: write",
		}
	}

	return Check{
		Name:     checkWorkflowIDToken,
		Severity: SeverityFail,
		Message:  "sign.method=sigstore but no .github/workflows/*.yml grants id-token: write",
		Remediation: "add `permissions: id-token: write` to the caller workflow that invokes the " +
			"release orchestrator; see examples/sigstore-keyless/release-workflow.yml",
	}
}

// anyWorkflowGrantsIDToken walks workflowsDir and returns true when
// at least one *.yml / *.yaml file contains the literal substring
// `id-token: write`. Unreadable workflows are skipped (a permission
// scan is best-effort, not authoritative).
func anyWorkflowGrantsIDToken(workflowsDir string) (bool, error) {
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

		body, err := os.ReadFile(path) //nolint:gosec // walked path under .github/workflows.
		if err != nil {
			return nil //nolint:nilerr // unreadable workflow shouldn't fail the whole check.
		}

		if strings.Contains(string(body), "id-token: write") {
			found = true
		}

		return nil
	})

	return found, walkErr
}

func checkWorkflowVersionRefs(root string) Check {
	workflowsDir := filepath.Join(root, ".github", "workflows")

	if _, statErr := os.Stat(workflowsDir); os.IsNotExist(statErr) {
		// A repository without any GitHub Actions is a valid setup
		// (e.g., a project that uses GitLab CI exclusively). Don't
		// warn about not finding pinning evidence when there's
		// nothing to pin.
		return Check{
			Name:     checkWorkflowsPinReusable,
			Severity: SeverityOK,
			Message:  ".github/workflows/ absent; no GitHub Actions workflows to scan",
		}
	}

	floating, walkErr := findFloatingReusableCIRefs(workflowsDir)
	if walkErr != nil {
		return Check{
			Name:     checkWorkflowsPinReusable,
			Severity: SeverityWarn,
			Message:  fmt.Sprintf("could not scan %s: %v", workflowsDir, walkErr),
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

// findFloatingReusableCIRefs walks workflowsDir and returns the
// basenames of every YAML file that references diggsweden/reusable-ci
// via @main or @master rather than a tagged version.
func findFloatingReusableCIRefs(workflowsDir string) ([]string, error) {
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

		if hasFloatingReusableCIRef(body) {
			floating = append(floating, filepath.Base(path))
		}

		return nil
	})

	return floating, walkErr
}

// hasFloatingReusableCIRef reports whether body contains a
// `uses: ...diggsweden/reusable-ci...@main` or `@master` line.
func hasFloatingReusableCIRef(body []byte) bool {
	for _, line := range strings.Split(string(body), "\n") {
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "uses:") {
			continue
		}

		if strings.Contains(trimmed, "diggsweden/reusable-ci") && (strings.Contains(trimmed, "@main") || strings.Contains(trimmed, "@master")) {
			return true
		}
	}

	return false
}

func regularFileExists(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}

	return info.Mode().IsRegular()
}
