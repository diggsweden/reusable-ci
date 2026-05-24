// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

// Package syft shells out to the system `syft` binary. The runtime
// image bakes syft in; local runs need it on PATH.
package syft

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/safeexec"
)

// Adapter wraps the syft binary. Bin is overridable for tests.
type Adapter struct {
	Bin string // empty → "syft"
}

// New returns an Adapter using the system syft.
func New() *Adapter { return &Adapter{} }

// Generate scans target once and writes one SBOM file per (format,
// outputFile) entry in outputs (e.g. "spdx-json" → "/tmp/x.spdx.json").
// syft's native `-o format=file` flag supports multiple outputs in a
// single invocation, so the scan happens once regardless of how many
// formats are requested. Stderr is forwarded to errOut.
//
// Mirrors invocations, batched:
//
//	syft "$scan_target" -o spdx-json="$spdx_file" -o cyclonedx-json="$cdx_file"
//
// Map iteration order is non-deterministic; argv is built by walking
// sorted keys so test assertions and logs stay stable.
func (a *Adapter) Generate(ctx context.Context, target string, outputs map[string]string, errOut io.Writer) error {
	if len(outputs) == 0 {
		return fmt.Errorf("syft: no outputs requested: %w", errs.ErrUsage)
	}

	formats := make([]string, 0, len(outputs))
	for f := range outputs {
		formats = append(formats, f)
	}

	sort.Strings(formats)

	args := []string{target}
	for _, f := range formats {
		args = append(args, "-o", f+"="+outputs[f])
	}

	cmd := safeexec.Command(ctx, a.bin(), args...)

	cmd.Stderr = errOut
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("syft %s: %w", strings.Join(args, " "), err)
	}

	return nil
}

// RunInherit invokes `syft` with args, streaming stdout/stderr to the
// provided writers. Used by callers that need the raw stdout (e.g.
// `syft --version`).
func (a *Adapter) RunInherit(ctx context.Context, stdout, stderr io.Writer, args ...string) error {
	cmd := safeexec.Command(ctx, a.bin(), args...)
	cmd.Stdout = stdout

	cmd.Stderr = stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("syft %s: %w", strings.Join(args, " "), err)
	}

	return nil
}

func (a *Adapter) bin() string {
	if a.Bin != "" {
		return a.Bin
	}

	return "syft"
}
