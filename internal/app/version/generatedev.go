// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package version

import (
	"context"
	"encoding/json"
	"fmt"
	"io"

	"github.com/diggsweden/reusable-ci/internal/domain/ci"
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
	// suffix.
	RefName string

	// Format is the output encoding. Zero-value (FormatText / empty)
	// preserves the historical "one bare version line on w" shape;
	// FormatJSON emits {"version":"<dev>"}; CI-platform formats reuse
	// the bare w shape and additionally emit a named CI output when a
	// sink is supplied.
	Format output.Format
	Sink   ci.OutputSink
}

// GenerateDevVersion prints `<base>-dev-<sanitised-branch>-<short-sha>`
// to w. Best-effort `git fetch --tags` is attempted; failures are
// ignored so the function still works on a shallow checkout that
// already has the tags it needs.
func GenerateDevVersion(ctx context.Context, ops GenerateDevOps, w io.Writer, in GenerateDevVersionInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if in.RefName == "" {
		return fmt.Errorf("ref-name is required: %w", errs.ErrUsage)
	}

	// Best-effort tag fetch — ignored on failure (shallow checkout).
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
	if in.Sink != nil && (in.Format == output.FormatGitHub || in.Format == output.FormatGitLab) {
		if err := in.Sink.Set(ctx, "dev-version", dev); err != nil {
			return err
		}
	}

	return writeDevVersion(w, dev, in.Format)
}

// writeDevVersion renders dev according to format. The historical
// shape is a bare line on w; FormatJSON wraps in a one-key
// object. Unknown formats fall back to the bare line.
func writeDevVersion(w io.Writer, dev string, format output.Format) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if format == output.FormatJSON {
		body, err := json.Marshal(struct {
			Version string `json:"version"`
		}{Version: dev})
		if err != nil {
			return fmt.Errorf("encode dev version json: %w", err)
		}

		_, _ = fmt.Fprintln(w, string(body))

		return nil
	}

	_, _ = fmt.Fprintln(w, dev)

	return nil
}
