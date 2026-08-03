// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package config

import (
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
)

// buildSecretNamePattern is the env-var identifier shape:
// uppercase / digit / underscore, starting with letter or underscore.
// Each `build-secrets` entry has to fit this because the value flows
// through env-var lookup at materialize time.
//
//nolint:gochecknoglobals // immutable compiled regexp.
var buildSecretNamePattern = regexp.MustCompile(`^[A-Z_][A-Z0-9_]*$`)

// ValidBuildSecretName reports whether name has the env-var identifier
// shape build-secret entries must fit. Exported so the materialize verb
// re-checks the same single-sourced rule locally instead of trusting
// that its input already passed config validation — the shape doubles
// as the path-safety guarantee (no separators, no dots) when the name
// becomes a filename in the secrets directory.
func ValidBuildSecretName(name string) bool { return buildSecretNamePattern.MatchString(name) }

// reusableCIReservedSecretNames are names reusable-ci already uses for
// its own secret pass-through. Adopters can't shadow them as
// build-secret entries — that would create a name collision in the
// envelope decoding.
//
//nolint:gochecknoglobals // immutable lookup table.
var reusableCIReservedSecretNames = map[string]bool{
	"REUSABLE_CI_BUILD_SECRETS_JSON":           true,
	"RELEASE_GPG_PRIVATE_KEY":                  true,
	"RELEASE_GPG_PASSPHRASE":                   true,
	"RELEASE_GPG_PUBLIC_KEY":                   true,
	"RELEASE_TOKEN":                            true,
	"MAVEN_CENTRAL_USERNAME":                   true,
	"MAVEN_CENTRAL_PASSWORD":                   true,
	"NPM_TOKEN":                                true,
	"CODE_SCANNING_TOKEN":                      true,
	"ANDROID_KEYSTORE":                         true,
	"ANDROID_KEYSTORE_PASSWORD":                true,
	"ANDROID_KEY_ALIAS":                        true,
	"ANDROID_KEY_PASSWORD":                     true,
	"SECRETS_PROPERTIES_BASE64":                true,
	"IOS_SIGNING_CERTIFICATE_BASE64":           true,
	"IOS_SIGNING_CERTIFICATE_PASSPHRASE":       true,
	"PROVISIONING_PROFILE_BASE64":              true,
	"KEYCHAIN_PASSWORD":                        true,
	"XCCONFIG_BASE64":                          true,
	"APP_STORE_CONNECT_API_KEY_ID":             true,
	"APP_STORE_CONNECT_ISSUER_ID":              true,
	"APP_STORE_CONNECT_API_PRIVATE_KEY_BASE64": true,
	"GOOGLE_PLAY_SERVICE_ACCOUNT_JSON":         true,
}

// validateBuildSecrets enforces the env-var-name shape on each entry
// and rejects names that collide with reusable-ci's own secret slots.
// Empty list is fine (the feature is opt-in).
func validateBuildSecrets(ct Container) []string {
	var v []string //nolint:varnamelen // idiomatic short name (matches sibling validators).

	seen := make(map[string]bool, len(ct.BuildSecrets))

	for _, name := range ct.BuildSecrets {
		if !buildSecretNamePattern.MatchString(name) {
			v = append(v, fmt.Sprintf(
				"container %q build-secret %q is not a valid env-var name (need [A-Z_][A-Z0-9_]*); the name flows through env-var lookup at materialize time",
				ct.Name, name,
			))

			continue
		}

		if reusableCIReservedSecretNames[name] {
			v = append(v, fmt.Sprintf(
				"container %q build-secret %q collides with a reusable-ci-reserved secret name; pick a different name",
				ct.Name, name,
			))

			continue
		}

		if seen[name] {
			v = append(v, fmt.Sprintf(
				"container %q lists build-secret %q twice",
				ct.Name, name,
			))

			continue
		}

		seen[name] = true
	}

	return v
}

// ValidationError is returned when a Config fails one of the schema rules.
// Multiple violations are reported via Errors() so callers can show all of
// them rather than failing on the first. Callers test for it with errors.As.
type ValidationError struct {
	Violations []string
}

func (e *ValidationError) Error() string {
	if len(e.Violations) == 1 {
		return e.Violations[0]
	}

	return fmt.Sprintf("config: %d validation errors: %v", len(e.Violations), e.Violations)
}

// Unwrap ties a semantic config-validation failure to ErrInvalidConfig so
// main()'s exit-code ladder maps it to EX_CONFIG (78) — the same code the
// YAML-parse path uses — rather than the unclassified EX_SOFTWARE (70).
func (e *ValidationError) Unwrap() error { return errs.ErrInvalidConfig }

// unsupportedTargetReason explains why an artifact cannot publish to a
// target. For maven-central the real constraint is build-type — libraries
// publish to Central, applications don't — so the generic "does not
// support" wording would mislead (maven *does* support Central). Spell the
// rule out and show the artifact's actual build-type.
func unsupportedTargetReason(artifact Artifact, target PublishTarget) string {
	if target == PublishMavenCentral && artifact.ProjectType == projecttype.Maven {
		return fmt.Sprintf(
			"artifact %q: publish target %q requires build-type %q (got %q)",
			artifact.Name, target, BuildTypeLibrary, artifact.BuildType,
		)
	}

	return fmt.Sprintf(
		"artifact %q with project-type %q does not support publish target %q",
		artifact.Name, artifact.ProjectType, target,
	)
}

// Validate checks Config against the schema rules: artifacts list non-empty,
// every project-type recognised, publish targets are supported for the
// artifact type, and container `from` references existing artifacts.
// Maven-application-to-forge-packages combinations only emit a warning
// (returned via Warnings); they don't fail validation.
func Validate(cfg *Config) error {
	if cfg == nil {
		return &ValidationError{Violations: []string{"config: nil Config"}}
	}

	var violations []string

	if len(cfg.Artifacts) == 0 {
		violations = append(violations, "no artifacts found")
	}

	validTypes := validProjectTypeSet()
	validPublishTargets := validPublishTargetSet()

	artifactNames, artifactByName := walkArtifacts(cfg.Artifacts, validTypes, validPublishTargets, &violations)

	for _, container := range cfg.Containers {
		violations = append(violations, validateContainer(container, artifactNames, artifactByName)...)
	}

	if err := cfg.Sign.Validate(); err != nil {
		violations = append(violations, err.Error())
	}

	if err := cfg.GitSigning.Validate(); err != nil {
		violations = append(violations, err.Error())
	}

	if len(violations) > 0 {
		return &ValidationError{Violations: violations}
	}

	return nil
}

// validProjectTypeSet materialises the ordered ValidProjectTypes slice
// as a lookup set for per-artifact validation.
func validProjectTypeSet() map[projecttype.Type]bool {
	out := make(map[projecttype.Type]bool, len(ValidProjectTypes))
	for _, t := range ValidProjectTypes {
		out[t] = true
	}

	return out
}

// validPublishTargetSet mirrors validProjectTypeSet for PublishTarget.
func validPublishTargetSet() map[PublishTarget]bool {
	out := make(map[PublishTarget]bool, len(ValidPublishTargets))
	for _, t := range ValidPublishTargets {
		out[t] = true
	}

	return out
}

// walkArtifacts validates every artifact and builds the two lookup
// maps the container-level validator needs: artifactNames (set used
// for `from:` reference checks and duplicate detection) and
// artifactByName (full artifact, used by build-mode / ecosystem-pair
// rules). Appends per-artifact violations to *violations.
func walkArtifacts(artifacts []Artifact, validTypes map[projecttype.Type]bool, validPublishTargets map[PublishTarget]bool, violations *[]string) (map[string]bool, map[string]Artifact) {
	artifactNames := make(map[string]bool, len(artifacts))
	artifactByName := make(map[string]Artifact, len(artifacts))

	for i, art := range artifacts {
		*violations = append(*violations, validateArtifact(art, i, artifactNames, validTypes, validPublishTargets)...)

		if art.Name != "" {
			if _, seen := artifactByName[art.Name]; !seen {
				artifactByName[art.Name] = art
			}
		}
	}

	return artifactNames, artifactByName
}

// validateArtifact runs all per-artifact schema rules, mutating
// artifactNames as a side-effect (to detect duplicates across the loop).
// Returns the list of violations contributed by this artifact.
func validateArtifact(a Artifact, idx int, artifactNames map[string]bool, validTypes map[projecttype.Type]bool, validPublishTargets map[PublishTarget]bool) []string { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	var v []string //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	switch {
	case a.Name == "":
		v = append(v, fmt.Sprintf("artifact #%d has empty name", idx+1))
	case artifactNames[a.Name]:
		v = append(v, fmt.Sprintf("duplicate artifact name %q", a.Name))
	default:
		artifactNames[a.Name] = true
	}

	if !validTypes[a.ProjectType] {
		v = append(v, fmt.Sprintf(
			"invalid projectType %q for artifact %q (must be one of: %v)",
			a.ProjectType, a.Name, projectTypesAsStrings(),
		))
	}

	for _, target := range a.PublishTo {
		if !validPublishTargets[target] {
			v = append(v, fmt.Sprintf(
				"invalid publish target %q for artifact %q (must be one of: %v)",
				target, a.Name, publishTargetsAsStrings(),
			))

			continue
		}

		if !SupportedPublishTarget(a, target) {
			v = append(v, unsupportedTargetReason(a, target))
		}
	}

	if a.ProjectType == projecttype.Go {
		v = append(v, validateGoBuildMode(a)...)
	}

	if a.ProjectType == projecttype.Cargo {
		v = append(v, validateCargoBuildMode(a)...)
	}

	v = append(v, validateWorkingDirectory(a)...)
	v = append(v, validateArtifactFileNames(a)...)

	return v
}

// validateArtifactFileNames rejects scalar fields that flow into
// constructed filenames from carrying path separators or dot-segment
// escapes. This is input hygiene, not a security boundary: the runner
// is fresh per job and the workspace contains only the consumer's own
// files. A `binary-name: "../tmp/x"` doesn't cross trust boundaries —
// but it does land the compiled binary one directory up from where
// downstream upload-artifact / release-attach steps expect it, which
// surfaces as a confusing "file not found" deep in the publish stage.
// Catching the bad value at parse time produces a clear error pointing
// at the offending field instead.
//
// Fields validated:
//   - Artifact.Name              (SBOM filename prefix, upload-artifact tag)
//   - Artifact.Go.BinaryName     (dist/<goos>-<goarch>/<bin>-<goos>-<goarch>)
//   - Artifact.Cargo.BinaryName  (same)
func validateArtifactFileNames(a Artifact) []string { //nolint:varnamelen // idiomatic short name (matches sibling validators).
	var v []string //nolint:varnamelen // idiomatic short name.

	if err := checkNoPathSeparators(fmt.Sprintf("artifact %q name", a.Name), a.Name); err != "" {
		v = append(v, err)
	}

	if a.Go != nil {
		if err := checkNoPathSeparators(fmt.Sprintf("artifact %q config.binary-name", a.Name), a.Go.BinaryName); err != "" {
			v = append(v, err)
		}
	}

	if a.Cargo != nil {
		if err := checkNoPathSeparators(fmt.Sprintf("artifact %q config.binary-name", a.Name), a.Cargo.BinaryName); err != "" {
			v = append(v, err)
		}
	}

	return v
}

// checkNoPathSeparators returns a violation string when value contains
// characters that would change the location of a constructed filename:
// path separators, `..` segments, or control bytes. Empty returns "".
func checkNoPathSeparators(fieldRef, value string) string {
	if value == "" {
		return ""
	}

	if strings.ContainsAny(value, `/\`) {
		return fmt.Sprintf("%s %q contains a path separator — names are used as filename fragments, not paths", fieldRef, value)
	}

	if value == "." || value == ".." {
		return fmt.Sprintf("%s %q is a dot-segment — produces an unstable filename", fieldRef, value)
	}

	for _, ch := range value {
		if ch < 0x20 || ch == 0x7f {
			return fmt.Sprintf("%s %q contains a control character", fieldRef, value)
		}
	}

	return ""
}

// validateWorkingDirectory rejects working-directory values that would
// leak environment-specific state into the immutable artifact contract.
//
// reusable-ci treats artifacts.yml as application configuration (per
// 12-Factor App): it ships with the source and must not vary by
// deployment. A working-directory like `${WORKSPACE}/svc` or
// `/srv/build/svc` violates that: it ties the build to a particular
// runner layout (env config) instead of describing what to build
// (app config).
//
// The rules are deliberately strict:
//   - Absolute paths are rejected (env-coupled)
//   - `..` escaping is rejected (sandbox escape + env-coupled)
//   - `${…}` references are rejected (suggests runtime interpolation
//     that artifacts.yml does not perform — silent failure mode)
func validateWorkingDirectory(a Artifact) []string { //nolint:varnamelen // idiomatic short name (matches validateArtifact / validateGoBuildMode).
	dir := strings.TrimSpace(a.WorkingDirectory)
	if dir == "" {
		return nil
	}

	if filepath.IsAbs(dir) {
		return []string{fmt.Sprintf(
			"artifact %q working-directory %q must be relative — absolute paths couple the build to a specific runner layout",
			a.Name, dir,
		)}
	}

	if strings.Contains(dir, "${") {
		return []string{fmt.Sprintf(
			"artifact %q working-directory %q contains a shell-style reference (${…}); artifacts.yml is parsed literally, no env expansion is performed",
			a.Name, dir,
		)}
	}

	cleanSlash := filepath.ToSlash(filepath.Clean(dir))
	if cleanSlash == ".." || strings.HasPrefix(cleanSlash, "../") {
		return []string{fmt.Sprintf(
			"artifact %q working-directory %q escapes the workspace — use a path inside the repository",
			a.Name, dir,
		)}
	}

	return nil
}

// validateGoBuildMode rejects an invalid build-mode on a Go artifact.
// Omitted is fine — GoArtifactBuildMode defaults it to artifact-first —
// so only a typo/wrong value fails.
func validateGoBuildMode(a Artifact) []string { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	switch GoArtifactBuildMode(a) {
	case GoBuildModeArtifactFirst, GoBuildModeContainerFirst:
		return nil
	default:
		return []string{fmt.Sprintf(
			"artifact %q with project-type %q has invalid config.build-mode %q (must be artifact-first or container-first; omit for artifact-first)",
			a.Name, a.ProjectType, GoArtifactBuildMode(a),
		)}
	}
}

// validateCargoBuildMode is the Cargo counterpart of validateGoBuildMode.
// Omitted is fine — CargoArtifactBuildMode defaults it — so only a
// typo/wrong value fails.
func validateCargoBuildMode(a Artifact) []string { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	switch CargoArtifactBuildMode(a) {
	case CargoBuildModeArtifactFirst, CargoBuildModeContainerFirst:
		return nil
	default:
		return []string{fmt.Sprintf(
			"artifact %q with project-type %q has invalid config.build-mode %q (must be artifact-first or container-first; omit for artifact-first)",
			a.Name, a.ProjectType, CargoArtifactBuildMode(a),
		)}
	}
}

// validateContainer enforces container-level invariants: name present,
// every From entry resolves to a known artifact, and at most one
// artifact-first dep per compiled-native ecosystem (publish-container.yml
// carries exactly one *-artifact-name slot per language, so two
// artifact-first deps from the same ecosystem would collide).
func validateContainer(ct Container, artifactNames map[string]bool, artifactByName map[string]Artifact) []string {
	var v []string //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	if ct.Name == "" {
		v = append(v, "container with empty name")
	}

	artifactFirstGoDeps := []string{}
	artifactFirstCargoDeps := []string{}

	for _, dep := range ct.From {
		if !artifactNames[dep] {
			v = append(v, fmt.Sprintf(
				"container %q references unknown artifact %q",
				ct.Name, dep,
			))

			continue
		}

		a := artifactByName[dep]
		if a.ProjectType == projecttype.Go && GoArtifactBuildMode(a) == GoBuildModeArtifactFirst {
			artifactFirstGoDeps = append(artifactFirstGoDeps, dep)
		}

		if a.ProjectType == projecttype.Cargo && CargoArtifactBuildMode(a) == CargoBuildModeArtifactFirst {
			artifactFirstCargoDeps = append(artifactFirstCargoDeps, dep)
		}
	}

	if len(artifactFirstGoDeps) > 1 {
		v = append(v, fmt.Sprintf(
			"container %q references multiple artifact-first Go artifacts %v; only one Go build artifact can be downloaded per container",
			ct.Name, artifactFirstGoDeps,
		))
	}

	if len(artifactFirstCargoDeps) > 1 {
		v = append(v, fmt.Sprintf(
			"container %q references multiple artifact-first Cargo artifacts %v; only one Cargo build artifact can be downloaded per container",
			ct.Name, artifactFirstCargoDeps,
		))
	}

	v = append(v, validateBuildSecrets(ct)...)

	return v
}

// Warnings returns non-fatal observations about a Config: e.g. Maven
// applications publishing to forge-packages (libraries should, but
// applications shouldn't). Validation passes regardless.
func Warnings(c *Config) []string { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if c == nil {
		return nil
	}

	var w []string //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	for _, a := range c.Artifacts { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		if a.ProjectType == projecttype.Maven &&
			a.BuildType == BuildTypeApplication &&
			slices.Contains(a.PublishTo, PublishForgePackages) {
			w = append(w, fmt.Sprintf(
				"Maven application %q publishing to forge-packages — applications should not (libraries only)",
				a.Name,
			))
		}
	}

	return w
}

func projectTypesAsStrings() []string {
	out := make([]string, len(ValidProjectTypes))
	for i, t := range ValidProjectTypes {
		out[i] = string(t)
	}

	return out
}

func publishTargetsAsStrings() []string {
	out := make([]string, len(ValidPublishTargets))
	for i, t := range ValidPublishTargets {
		out[i] = string(t)
	}

	return out
}
