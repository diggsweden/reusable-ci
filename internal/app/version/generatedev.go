// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package version

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
	"github.com/diggsweden/reusable-ci/internal/domain/version"
)

// GenerateDevOps is the slice of git-adapter methods GenerateDevVersion
// needs. Tests inject a fake; production passes adapter/git.New().
type GenerateDevOps interface {
	Run(ctx context.Context, args ...string) (string, error)
	ListTags(ctx context.Context, pattern string) ([]string, error)
	ShortSHA(ctx context.Context, ref string, n int) (string, error)
}

// GenerateDevVersionInput drives GenerateDevVersion.
type GenerateDevVersionInput struct {
	// RefName is the source branch / ref to sanitise into the dev-version
	// suffix. Mirrors $CI_REF_NAME in scripts/version/generate-dev-version.sh.
	RefName string

	// Format is the output encoding. Zero-value (FormatText / empty)
	// preserves the historical "one bare version line on stdout" shape;
	// FormatJSON emits {"version":"<dev>"}; CI-platform formats reuse
	// FormatText since the dev version is consumed via stdout capture.
	Format output.Format
}

// GenerateDevVersion prints `<base>-dev-<sanitised-branch>-<short-sha>`
// to stdout. Mirrors scripts/version/generate-dev-version.sh.
//
// Best-effort `git fetch --tags` is attempted; failures are ignored
// (matches the bash `|| true`) so the function still works on a shallow
// checkout that already has the tags it needs.
func GenerateDevVersion(ctx context.Context, ops GenerateDevOps, stdout io.Writer, in GenerateDevVersionInput) error {
	if in.RefName == "" {
		return fmt.Errorf("ref-name is required: %w", errs.ErrUsage)
	}

	// Best-effort tag fetch. The bash also uses `|| true` here.
	_, _ = ops.Run(ctx, "fetch", "--tags")

	tags, err := ops.ListTags(ctx, "v[0-9]*.[0-9]*.[0-9]*")
	if err != nil {
		return fmt.Errorf("list tags: %w", err)
	}
	latest := version.LatestSemverTag(tags)
	base := version.DevVersionDefaultBase
	if latest != "" {
		base = version.StripVPrefix(latest)
	}

	shortSHA, err := ops.ShortSHA(ctx, "HEAD", version.DevShortSHALen)
	if err != nil {
		return fmt.Errorf("rev-parse --short HEAD: %w", err)
	}

	dev := version.ComposeDevVersion(base, in.RefName, shortSHA)
	return writeDevVersion(stdout, dev, in.Format)
}

// writeDevVersion renders dev according to format. The historical
// shape is a bare line on stdout; FormatJSON wraps in a one-key
// object. Unknown formats fall back to the bare line.
func writeDevVersion(w io.Writer, dev string, format output.Format) error {
	if format == output.FormatJSON {
		body, err := json.Marshal(struct {
			Version string `json:"version"`
		}{Version: dev})
		if err != nil {
			return fmt.Errorf("encode dev version json: %w", err)
		}
		fmt.Fprintln(w, string(body))
		return nil
	}
	fmt.Fprintln(w, dev)
	return nil
}
