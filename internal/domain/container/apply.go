// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/provider"
)

// MetadataContext is the slice of provider.EventContext that tag-rule
// evaluation needs. Carrying our own narrower type keeps the domain
// signature explicit about what's read.
type MetadataContext struct {
	RefName    string
	RefType    provider.RefType
	BranchName string // current branch — used by sha,prefix={{branch}}-
	ShortSHA   string
	PRNumber   string
}

// FromEventContext projects a provider.EventContext onto MetadataContext.
// BranchName falls back to RefName for tag pushes so {{branch}}-<sha>
// templates produce a usable value off-branch (matching the bash).
func FromEventContext(evt *provider.EventContext) MetadataContext {
	branch := evt.Branch
	if branch == "" {
		branch = evt.RefName
	}

	return MetadataContext{
		RefName:    evt.RefName,
		RefType:    evt.RefType,
		BranchName: branch,
		ShortSHA:   evt.ShortSHA,
		PRNumber:   evt.PRNumber,
	}
}

// AppliedTag is the result of evaluating one Rule against a context: the
// concrete tag (post-template-expansion) and its priority for primary-
// version selection.
type AppliedTag struct {
	Tag      string
	Priority int
}

// Apply evaluates a Rule against ctx. Returns (applied, true, nil) when
// the rule fires, (zero, false, nil) when it's silently skipped (disabled
// rule, wrong context for ref/semver, missing short-sha), and an error on
// configuration that the parser couldn't catch (e.g. semver pattern
// outside the supported set).
//
// Skip semantics mirror the bash: callers that produce a final tag list
// just drop empty results.
func Apply(r Rule, ctx MetadataContext) (AppliedTag, bool, error) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if !r.Enable {
		return AppliedTag{}, false, nil
	}

	tag, ok, err := tagFor(r, ctx)
	if err != nil {
		return AppliedTag{}, false, err
	}

	if !ok || tag == "" {
		return AppliedTag{}, false, nil
	}

	// Final guarantee: no rule may emit a tag the registry will reject.
	// Ref-derived tags are pre-sanitized, so this only trips on an invalid
	// operator-supplied raw value or template — surfaced as a loud config
	// error instead of an "invalid reference format" deep in `docker buildx`.
	if !dockerTagValid.MatchString(tag) {
		return AppliedTag{}, false, fmt.Errorf(
			"rule produced invalid docker tag %q (must match %s): %w",
			tag, dockerTagValid.String(), errs.ErrValidation,
		)
	}

	return AppliedTag{Tag: tag, Priority: r.Priority()}, true, nil
}

func tagFor(r Rule, ctx MetadataContext) (string, bool, error) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	switch r.Type {
	case RuleTypeRaw:
		return r.Value, r.Value != "", nil
	case RuleTypeRef:
		return refTag(r.Event, ctx)
	case RuleTypeSemver:
		return semverTag(r.Pattern, ctx)
	case RuleTypeSHA:
		if ctx.ShortSHA == "" {
			return "", false, nil
		}

		// Sanitize the branch the same way as ref tags: a {{branch}}- prefix
		// with a slashed branch (feat/refactor-go) would otherwise yield an
		// invalid tag.
		resolved := strings.ReplaceAll(r.Prefix, "{{branch}}", sanitizeRefTag(ctx.BranchName))

		return resolved + ctx.ShortSHA, true, nil
	}

	return "", false, fmt.Errorf("unhandled rule type %q: %w", r.Type, errs.ErrValidation)
}

// maxDockerTagLen is the OCI/distribution limit on a reference tag.
const maxDockerTagLen = 128

// dockerTagInvalid matches any run of characters outside the Docker image-tag
// alphabet ([A-Za-z0-9_.-]). Git ref names routinely contain others — most
// often the '/' in `feat/refactor-go`, which would otherwise abort the push
// with "invalid reference format".
var dockerTagInvalid = regexp.MustCompile(`[^a-zA-Z0-9_.-]+`)

// dockerTagValid is the OCI/distribution reference tag grammar. Every emitted
// tag is checked against it as a final safety net (see Apply).
var dockerTagValid = regexp.MustCompile(`^[a-zA-Z0-9_][a-zA-Z0-9_.-]{0,127}$`)

// sanitizeRefTag turns an arbitrary git ref name into a tag that satisfies the
// distribution grammar `[A-Za-z0-9_][A-Za-z0-9_.-]{0,127}`, mirroring
// docker/metadata-action:
//   - runs of tag-invalid characters collapse to a single '-'
//     (feat/refactor-go -> feat-refactor-go),
//   - a leading '.' or '-' (illegal as the first character) is trimmed, and
//   - the result is capped at 128 characters.
//
// Returns "" when nothing valid remains, so callers skip the tag rather than
// emit an empty/invalid reference.
func sanitizeRefTag(ref string) string {
	tag := dockerTagInvalid.ReplaceAllString(ref, "-")
	tag = strings.TrimLeft(tag, ".-")

	if len(tag) > maxDockerTagLen {
		tag = tag[:maxDockerTagLen]
	}

	return tag
}

func refTag(event RefEvent, ctx MetadataContext) (string, bool, error) {
	switch event {
	case RefEventBranch:
		if ctx.RefType != provider.RefTypeBranch {
			return "", false, nil
		}

		tag := sanitizeRefTag(ctx.RefName)

		return tag, tag != "", nil
	case RefEventTag:
		if ctx.RefType != provider.RefTypeTag {
			return "", false, nil
		}

		tag := sanitizeRefTag(ctx.RefName)

		return tag, tag != "", nil
	case RefEventPR:
		if ctx.PRNumber == "" {
			return "", false, nil
		}

		return "pr-" + ctx.PRNumber, true, nil
	}

	return "", false, fmt.Errorf("unsupported ref event %q: %w", event, errs.ErrValidation)
}

// semverRE is the official SemVer 2.0.0 grammar, verbatim from the named-group
// variant published at
// https://semver.org/#is-there-a-suggested-regular-expression-regex-to-check-a-semver-string
// It is RE2-compatible (no look-around/back-references), so it compiles under
// Go's regexp. Capture groups: major, minor, patch, prerelease, buildmetadata.
var semverRE = regexp.MustCompile(`^(?P<major>0|[1-9]\d*)\.(?P<minor>0|[1-9]\d*)\.(?P<patch>0|[1-9]\d*)(?:-(?P<prerelease>(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?(?:\+(?P<buildmetadata>[0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?$`)

func semverTag(pattern string, ctx MetadataContext) (string, bool, error) {
	if ctx.RefType != provider.RefTypeTag {
		return "", false, nil
	}

	stripped := strings.TrimPrefix(ctx.RefName, "v")

	// Not a strict semver tag (e.g. a date or codename) — skip the semver
	// rules rather than emit a malformed version.
	m := semverRE.FindStringSubmatch(stripped)
	if m == nil {
		return "", false, nil
	}

	// Substitute the supported placeholders into the pattern template. Literal
	// text around them is preserved, so `v{{major}}` yields `v3` — letting the
	// image tags match how consumers pin the workflow (`uses: …@v3`).
	out := strings.NewReplacer(
		"{{version}}", stripped,
		"{{major}}.{{minor}}.{{patch}}", stripped,
		"{{major}}.{{minor}}", m[semverRE.SubexpIndex("major")]+"."+m[semverRE.SubexpIndex("minor")],
		"{{major}}", m[semverRE.SubexpIndex("major")],
		"{{minor}}", m[semverRE.SubexpIndex("minor")],
		"{{patch}}", m[semverRE.SubexpIndex("patch")],
	).Replace(pattern)

	// A leftover placeholder means the pattern used an unknown token.
	if strings.Contains(out, "{{") {
		return "", false, fmt.Errorf("unsupported semver pattern %q: %w", pattern, errs.ErrValidation)
	}

	return out, true, nil
}
