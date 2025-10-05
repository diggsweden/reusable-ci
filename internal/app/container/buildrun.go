// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"io"

	appsecurity "github.com/diggsweden/reusable-ci/v3/internal/app/security"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
)

// BuildAndScanInput drives BuildAndScan: the build-image parameters plus the
// in-build vulnerability scan toggle/threshold.
type BuildAndScanInput struct {
	Build BuildImageInput

	// EnableScan runs a trivy scan of the just-built image. Scans only when a
	// digest was produced (push-by-digest mode), matching the workflow's
	// `enable-scan && push-image` gate.
	EnableScan bool
	// ScanSeverity is the trivy --severity filter AND fail-on threshold.
	ScanSeverity string
	// TrivyVersion is embedded in the GitLab container-scanning report.
	TrivyVersion string
}

// BuildAndScan builds one platform image and, when it was pushed by digest,
// scans it — threading the digest in-process (build → scan name@digest) instead
// of round-tripping it through the output sink between two workflow steps. The
// digest is still emitted on the sink for the downstream cross-platform
// `container manifest merge`, and the scan writes its SARIF + GitLab
// container-scanning report at the default paths for the forge job to upload.
//
// This is the container sibling of the language `build <eco> run` consolidation:
// the build-logic sequence is owned in Go; the forge job keeps only the
// platform-transport steps (login, artifact download/upload) around it.
func BuildAndScan(
	ctx context.Context,
	builder imageBuilder,
	pusher layoutPusher,
	trivy appsecurity.TrivyOps,
	sink ci.OutputSink,
	out, stderr io.Writer, //nolint:varnamelen // idiomatic short names (io conventions).
	annot output.Annotator,
	in BuildAndScanInput,
) (string, error) {
	digest, err := BuildImage(ctx, builder, pusher, sink, out, in.Build)
	if err != nil {
		return "", err
	}

	// No scan when scanning is off or nothing was pushed (load/local mode has
	// no digest to anchor a registry scan to).
	if !in.EnableScan || digest == "" {
		return digest, nil
	}

	imageRef := in.Build.ImageRef + "@" + digest

	if err := appsecurity.ScanContainer(ctx, trivy, out, stderr, annot, appsecurity.ScanContainerInput{
		ImageRef:     imageRef,
		Severity:     in.ScanSeverity,
		TrivyVersion: in.TrivyVersion,
	}); err != nil {
		return digest, err
	}

	return digest, nil
}
