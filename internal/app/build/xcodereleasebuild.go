// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build

import (
	"context"
	"fmt"
	"io"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
)

// XcodeReleaseBuildInput drives XcodeReleaseBuild. Unlike the JVM/Go builds,
// xcode has no SBOM/skip-tests/working-dir, so it does not embed
// ReleaseBuildOptions — every knob is explicit here.
type XcodeReleaseBuildInput struct {
	// Artifact naming.
	ArtifactName   string
	RepositoryName string
	IncludeTag     bool
	RefName        string

	// Archive (xcodebuild).
	Workspace     string
	Project       string
	Scheme        string
	Configuration string
	Destination   string
	BuildNumber   string

	// Code signing (certificate + provisioning profile).
	EnableCodeSigning bool
	CertBase64        string
	CertPassphrase    string
	PPBase64          string
	KeychainPassword  string

	// xcconfig secret + IPA export options.
	XCConfigBase64      string
	ExportOptionsBase64 string
	ExportOptionsVar    string
}

// XcodeReleaseBuild runs the build-logic core of the iOS release build as one
// step: compose the artifact name, set up code signing (when enabled), resolve
// metadata, decode the xcconfig, archive, export the IPA (when signing), and
// list the built artifacts. The xcconfig path is threaded to the archive
// in-process; the artifact name + version are emitted as job outputs.
//
// macOS-only. The macOS environment setup (xcode-select, brew, xcodegen) stays
// in the forge YAML — it is not reusable-ci's to own — so this consolidates the
// `reusable-ci build xcode-ios *` steps, not the whole job.
//
//nolint:varnamelen // idiomatic short names (w/in) — testing/http/io conventions, matching the sibling build funcs.
func XcodeReleaseBuild(ctx context.Context, sink ci.OutputSink, sec XcodeSecurityOps, buildOps XcodeBuildOps, annot output.Annotator, w, stderr io.Writer, in XcodeReleaseBuildInput) error {
	if err := XcodeArtifactName(ctx, sink, w, XcodeArtifactNameInput{
		ArtifactName:   in.ArtifactName,
		RepositoryName: in.RepositoryName,
		IncludeTag:     in.IncludeTag,
		RefName:        in.RefName,
	}); err != nil {
		return fmt.Errorf("compose artifact name: %w", err)
	}

	if in.EnableCodeSigning {
		if err := XcodeSetupCodeSigning(ctx, sec, w, XcodeSetupCodeSigningInput{
			CertBase64:       in.CertBase64,
			CertPassphrase:   in.CertPassphrase,
			PPBase64:         in.PPBase64,
			KeychainPassword: in.KeychainPassword,
		}); err != nil {
			return fmt.Errorf("set up code signing: %w", err)
		}
	}

	if err := XcodeVersionInfo(ctx, sink, stderr, annot, XcodeVersionInfoInput{Project: in.Project, Workspace: in.Workspace}); err != nil {
		return fmt.Errorf("resolve version: %w", err)
	}

	xcconfigPath, err := resolveXcodeXCConfig(XcodeXCConfigInput{Base64: in.XCConfigBase64})
	if err != nil {
		return fmt.Errorf("set up xcconfig: %w", err)
	}

	if err := XcodeArchive(ctx, buildOps, w, stderr, XcodeArchiveInput{
		Workspace:     in.Workspace,
		Project:       in.Project,
		Scheme:        in.Scheme,
		Configuration: in.Configuration,
		Destination:   in.Destination,
		XcconfigPath:  xcconfigPath,
		BuildNumber:   in.BuildNumber,
	}); err != nil {
		return fmt.Errorf("archive: %w", err)
	}

	if in.EnableCodeSigning {
		if err := XcodeExportIPA(ctx, buildOps, w, stderr, XcodeExportIPAInput{
			ExportOptionsBase64: in.ExportOptionsBase64,
			ExportOptionsVar:    in.ExportOptionsVar,
		}); err != nil {
			return fmt.Errorf("export IPA: %w", err)
		}
	}

	if err := XcodeListBuiltArtifacts(w); err != nil {
		return fmt.Errorf("list artifacts: %w", err)
	}

	return nil
}
