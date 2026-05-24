// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package summary

import (
	"context"
	"fmt"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/ci"
)

// GoBuildInput drives GoBuild.
type GoBuildInput struct {
	BinaryName string
	Module     string
	Platforms  string
	Version    string
	SkipTests  bool
}

// GoBuild appends the Go build summary block.
func GoBuild(ctx context.Context, sink ci.SummarySink, in GoBuildInput) error {
	testStatus := "go test ./..."
	if in.SkipTests {
		testStatus = "skipped"
	}

	var b strings.Builder //nolint:varnamelen // idiomatic short name (testing/http/io conventions).

	_, _ = fmt.Fprintf(&b, "## Go Build Summary\n")
	_, _ = fmt.Fprintf(&b, "- **Module:** %s\n", in.Module)
	_, _ = fmt.Fprintf(&b, "- **Binary:** %s\n", in.BinaryName)
	_, _ = fmt.Fprintf(&b, "- **Version:** %s\n", in.Version)
	_, _ = fmt.Fprintf(&b, "- **Platforms:** %s\n", in.Platforms)
	_, _ = fmt.Fprintf(&b, "- **Tests:** %s\n", testStatus)

	return sink.Append(ctx, b.String())
}
