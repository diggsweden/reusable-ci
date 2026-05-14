// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package container

import (
	"fmt"
	"regexp"
	"strings"

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
func Apply(r Rule, ctx MetadataContext) (AppliedTag, bool, error) {
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
	return AppliedTag{Tag: tag, Priority: r.Priority()}, true, nil
}

func tagFor(r Rule, ctx MetadataContext) (string, bool, error) {
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
		resolved := strings.ReplaceAll(r.Prefix, "{{branch}}", ctx.BranchName)
		return resolved + ctx.ShortSHA, true, nil
	}
	return "", false, fmt.Errorf("unhandled rule type %q", r.Type)
}

func refTag(event RefEvent, ctx MetadataContext) (string, bool, error) {
	switch event {
	case RefEventBranch:
		if ctx.RefType != provider.RefTypeBranch {
			return "", false, nil
		}
		return ctx.RefName, ctx.RefName != "", nil
	case RefEventTag:
		if ctx.RefType != provider.RefTypeTag {
			return "", false, nil
		}
		return ctx.RefName, ctx.RefName != "", nil
	case RefEventPR:
		if ctx.PRNumber == "" {
			return "", false, nil
		}
		return "pr-" + ctx.PRNumber, true, nil
	}
	return "", false, fmt.Errorf("unsupported ref event %q", event)
}

var semverMajorMinor = regexp.MustCompile(`^([0-9]+)\.([0-9]+)`)
var semverMajor = regexp.MustCompile(`^([0-9]+)`)

func semverTag(pattern string, ctx MetadataContext) (string, bool, error) {
	if ctx.RefType != provider.RefTypeTag {
		return "", false, nil
	}
	stripped := strings.TrimPrefix(ctx.RefName, "v")
	switch pattern {
	case "{{version}}":
		return stripped, stripped != "", nil
	case "{{major}}.{{minor}}":
		m := semverMajorMinor.FindStringSubmatch(stripped)
		if m == nil {
			return "", false, nil
		}
		return m[1] + "." + m[2], true, nil
	case "{{major}}":
		m := semverMajor.FindStringSubmatch(stripped)
		if m == nil {
			return "", false, nil
		}
		return m[1], true, nil
	}
	return "", false, fmt.Errorf("unsupported semver pattern %q", pattern)
}
