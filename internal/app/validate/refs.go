// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package validate is the app-layer entry point for the
// `reusable-ci validate ...` subcommands. It groups three kinds of
// validator: pure-domain (ref shape, tag format, changelog presence),
// local-tool (git, gpg state, lockfile presence), and provider
// (token scopes, allowlisted signers). Each subcommand wires one
// validator and translates the structured result into a user-facing
// message + appropriate errs.Err* wrap.
package validate

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/clicolor"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/validate"
)

// RefTypeInput drives `reusable-ci validate ref-type`.
type RefTypeInput struct {
	RefType provider.RefType
	RefName string // "v1.0.0", "main", …
	Ref     string // "refs/tags/v1.0.0", "refs/heads/main", …
}

// RefType requires that the trigger ref type is `tag`. Writes a
// human-readable success line to out on success; on mismatch returns
// an error whose Error() carries the structured guidance the bash
// script printed to stderr.
func RefType(out io.Writer, in RefTypeInput) error {
	if err := validate.RequireTagRefType(in.RefType, in.Ref); err != nil {
		var rte *validate.RefTypeError
		if errors.As(err, &rte) {
			return fmt.Errorf(
				"release workflow must be triggered by pushing a tag\n"+
					"Current trigger: %s (%s)\n"+
					"To create a release, push a signed tag:\n"+
					"  git tag -s v1.0.0 -m 'Release v1.0.0'\n"+
					"  git push origin v1.0.0: %w",
				rte.Got, rte.Ref, errs.ErrValidation)
		}

		return err
	}

	_, _ = fmt.Fprintf(out, "%s Triggered by tag: %s\n", clicolor.Check(out), in.RefName)

	return nil
}

// TagFormatInput drives `reusable-ci validate tag-format`.
type TagFormatInput struct {
	Tag string
}

// TagFormat validates a release tag against the project's permissive
// semver pattern. Prints the parsed parts on success; returns an error
// listing the help text on failure.
func TagFormat(out io.Writer, in TagFormatInput) error {
	tf, err := validate.ParseTagFormat(in.Tag)
	if err != nil {
		return fmt.Errorf("%w\n\n"+
			"Tags must follow semantic versioning: vMAJOR.MINOR.PATCH[-PRERELEASE]\n"+
			"Valid: v1.0.0, v2.3.4-beta.1, v1.0.0-rc.2, v3.0.0-alpha, v1.0.0-dev\n"+
			"Learn more: https://semver.org",
			err)
	}

	_, _ = fmt.Fprintf(out, "## Validating Tag Format\n")
	_, _ = fmt.Fprintf(out, "%s Valid semantic version tag\n", clicolor.Check(out))
	_, _ = fmt.Fprintf(out, "   Version: %s.%s.%s\n", tf.Major, tf.Minor, tf.Patch)

	if tf.IsStable() {
		_, _ = fmt.Fprintf(out, "   Type: Stable release\n")
	} else {
		_, _ = fmt.Fprintf(out, "   Pre-release: %s\n", tf.Prerelease)

		if tf.PrereleaseStandard {
			_, _ = fmt.Fprintf(out, "   %s Pre-release identifier follows convention\n", clicolor.Check(out))
		} else {
			_, _ = fmt.Fprintf(out, "   ℹ️ Non-standard pre-release identifier: %s\n", tf.Prerelease)
			_, _ = fmt.Fprintf(out, "      Standard identifiers: alpha, beta, rc, snapshot, SNAPSHOT, dev\n")
			_, _ = fmt.Fprintf(out, "      (Release will proceed - this is informational only)\n")
		}
	}

	_, _ = fmt.Fprintf(out, "\n### Tag Format Summary:\n")
	_, _ = fmt.Fprintf(out, "%s Tag follows semantic versioning (vX.Y.Z)\n", clicolor.Check(out))
	_, _ = fmt.Fprintf(out, "%s Tag format validation passed\n", clicolor.Check(out))

	return nil
}

// ReleaseTagGuardInput drives ReleaseTagGuard.
type ReleaseTagGuardInput struct {
	Tag     string
	Pattern string
}

// ReleaseTagGuard validates a stable final release tag or release-request tag and emits normalized outputs.
func ReleaseTagGuard(ctx context.Context, sink ci.OutputSink, out io.Writer, in ReleaseTagGuardInput) error {
	pattern := in.Pattern
	if pattern == "" {
		pattern = `^v[0-9]+[.][0-9]+[.][0-9]+$`
	}

	if !strings.HasPrefix(pattern, "^") || !strings.HasSuffix(pattern, "$") {
		return fmt.Errorf("release-tag-guard: pattern must be anchored (^...$), got %q: %w", pattern, errs.ErrUsage)
	}

	re, err := regexp.Compile(pattern)
	if err != nil {
		return fmt.Errorf("compile release tag pattern %q: %w: %w", pattern, err, errs.ErrUsage)
	}

	releaseTag, releaseRequest, err := normalizeReleaseTag(re, pattern, in.Tag)
	if err != nil {
		return err
	}

	if releaseRequest != "" && releaseTag == "" {
		return fmt.Errorf("release request is missing its final release tag: %w", errs.ErrValidation)
	}

	if err := emitReleaseTagOutputs(ctx, sink, releaseTag, releaseRequest); err != nil {
		return err
	}

	if releaseRequest != "" {
		_, _ = fmt.Fprintf(out, "Release request accepted: %s -> %s\n", releaseRequest, releaseTag)
	} else {
		_, _ = fmt.Fprintf(out, "Release tag accepted: %s\n", releaseTag)
	}

	return nil
}

// normalizeReleaseTag resolves tag against the anchored pattern, accepting a
// final release tag directly or via a release-request/<tag> ref. It returns
// the normalized release tag and, when present, the release-request ref name.
func normalizeReleaseTag(re *regexp.Regexp, pattern, tag string) (string, string, error) {
	if re.MatchString(tag) {
		return tag, "", nil
	}

	releaseRequest := ""

	candidate := strings.TrimPrefix(tag, "refs/tags/")
	if strings.HasPrefix(candidate, "release-request/") {
		releaseRequest = candidate
		candidate = strings.TrimPrefix(candidate, "release-request/")
	}

	if !re.MatchString(candidate) {
		return "", "", fmt.Errorf("release tag must match %s (got %q): %w", pattern, tag, errs.ErrValidation)
	}

	return candidate, releaseRequest, nil
}

// emitReleaseTagOutputs writes the normalized tag outputs when a sink is configured.
func emitReleaseTagOutputs(ctx context.Context, sink ci.OutputSink, releaseTag, releaseRequest string) error {
	if sink == nil {
		return nil
	}

	if err := sink.Set(ctx, "release-tag", releaseTag); err != nil {
		return err
	}

	return sink.Set(ctx, "release-request", releaseRequest)
}

// ChangelogInput drives `reusable-ci validate changelog`. Path is the
// changelog file (e.g. CHANGELOG.md). Required=true makes a missing
// file an error; Required=false makes a missing file return a
// "no changes for this release" sentinel via sink.
type ChangelogInput struct {
	Path     string
	Required bool
}

// Changelog reads a changelog file and either:
//   - Required=true: errors when the file is missing.
//   - Required=false: emits sink["content"] with the file's contents
//     when present, or `"No changes for this release"` when absent.
//
// Returns ("", nil) and writes to sink only in the !Required path.
// Required=true returns the content as a courtesy (callers can ignore it).
func Changelog(ctx context.Context, sink ci.OutputSink, out io.Writer, in ChangelogInput) error {
	data, statErr := os.ReadFile(in.Path)
	if statErr != nil && !os.IsNotExist(statErr) {
		return fmt.Errorf("read %s: %w", in.Path, statErr)
	}

	exists := statErr == nil

	if in.Required {
		if !exists {
			return fmt.Errorf("full changelog (%s) not found\nthis file is required for the version bump commit: %w", in.Path, errs.ErrMissingInput)
		}

		_, _ = fmt.Fprintf(out, "%s Full changelog found (%d lines)\n", clicolor.Check(out), validate.CountLines(data))

		return nil
	}

	if !exists {
		return sink.Set(ctx, "content", "No changes for this release")
	}
	// Multiline write preserves embedded newlines; the heredoc-based GHA
	// sink handles them; GitLab dotenv falls back to scalar Set.
	lines := splitLinesPreservingTrailing(data)

	return sink.SetMultiline(ctx, "content", lines)
}

// splitLinesPreservingTrailing splits raw on '\n'. A trailing newline
// produces a final empty element that we drop — matches `printf "%s\n"
// "$content"` semantics: a single newline at the end, not two.
func splitLinesPreservingTrailing(raw []byte) []string {
	if len(raw) == 0 {
		return nil
	}

	s := string(raw) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if s[len(s)-1] == '\n' {
		s = s[:len(s)-1]
	}

	out := []string{}
	start := 0

	for i := range len(s) {
		if s[i] == '\n' {
			out = append(out, s[start:i])
			start = i + 1
		}
	}

	out = append(out, s[start:])

	return out
}
