// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/pathsafe"
)

type pendingXcodeOutput struct {
	entries []func(context.Context, ci.OutputSink) error
}

func (p *pendingXcodeOutput) Set(_ context.Context, key, value string) error {
	p.entries = append(p.entries, func(ctx context.Context, sink ci.OutputSink) error {
		return sink.Set(ctx, key, value)
	})

	return nil
}

func (p *pendingXcodeOutput) SetBool(_ context.Context, key string, value bool) error {
	p.entries = append(p.entries, func(ctx context.Context, sink ci.OutputSink) error {
		return sink.SetBool(ctx, key, value)
	})

	return nil
}

func (p *pendingXcodeOutput) SetMultiline(_ context.Context, key string, lines []string) error {
	p.entries = append(p.entries, func(ctx context.Context, sink ci.OutputSink) error {
		return sink.SetMultiline(ctx, key, lines)
	})

	return nil
}

func (p *pendingXcodeOutput) Close(context.Context) error { return nil }

func (p *pendingXcodeOutput) flush(ctx context.Context, sink ci.OutputSink) error {
	for _, set := range p.entries {
		if err := set(ctx, sink); err != nil {
			return err
		}
	}

	return nil
}

func publishXcodeOutputs(ctx context.Context, pending *pendingXcodeOutput, sink ci.OutputSink) error {
	if err := pending.flush(ctx, sink); err != nil {
		return fmt.Errorf("publish xcode build outputs: %w", err)
	}

	return nil
}

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
// step: preflight the inputs and release paths, set up code signing (when
// enabled), report captured metadata, write the xcconfig, archive, export the
// IPA (when signing), and list the release artifacts. The xcconfig is threaded
// to the archive in-process; artifact name + version are emitted as job outputs.
// Release output paths are create-only. Success requires a new archive directory
// and, when signed, exactly one nonempty regular direct lowercase .ipa under
// build/export, matching the workflow's upload glob. Archive
// internals are not inspected or certified as a native Apple bundle.
//
// macOS-only. The macOS environment setup (xcode-select, brew, xcodegen) stays
// in the forge YAML — it is not reusable-ci's to own — so this consolidates the
// `reusable-ci build xcode-ios *` steps, not the whole job.
//
//nolint:cyclop,varnamelen // linear preflight/effect/publication sequence; writer convention.
func XcodeReleaseBuild(ctx context.Context, sink ci.OutputSink, sec XcodeSecurityOps, buildOps XcodeBuildOps, annot output.Annotator, w, stderr io.Writer, in XcodeReleaseBuildInput) (err error) {
	pending := &pendingXcodeOutput{}

	var banner bytes.Buffer
	if nameErr := XcodeArtifactName(ctx, pending, &banner, XcodeArtifactNameInput{
		ArtifactName:   in.ArtifactName,
		RepositoryName: in.RepositoryName,
		IncludeTag:     in.IncludeTag,
		RefName:        in.RefName,
	}); nameErr != nil {
		return fmt.Errorf("compose artifact name: %w", nameErr)
	}

	archive := XcodeArchiveInput{
		Workspace: in.Workspace, Project: in.Project, Scheme: in.Scheme,
		Configuration: in.Configuration, Destination: in.Destination, BuildNumber: in.BuildNumber,
	}
	if archiveErr := validateXcodeArchive(archive); archiveErr != nil {
		return fmt.Errorf("archive: %w", archiveErr)
	}

	// Bind source metadata to the archive selector before publishing anything.
	// This does not verify the selected scheme/configuration's effective settings;
	// BuildNumber remains an archive override, not a source metadata replacement.
	metadata, missing, err := resolveXcodeVersionInfo(XcodeVersionInfoInput{Project: in.Project, Workspace: in.Workspace})
	if err != nil {
		return fmt.Errorf("resolve version: %w", err)
	}

	xcconfig, err := decodeXcodeXCConfig(in.XCConfigBase64)
	if err != nil {
		return fmt.Errorf("set up xcconfig: %w", err)
	}

	signing := XcodeSetupCodeSigningInput{
		CertBase64: in.CertBase64, CertPassphrase: in.CertPassphrase,
		PPBase64: in.PPBase64, KeychainPassword: in.KeychainPassword,
	}

	var cert, profile, exportOptions []byte

	if in.EnableCodeSigning {
		if strings.ContainsRune(in.CertPassphrase, 0) || strings.ContainsRune(in.KeychainPassword, 0) {
			return fmt.Errorf("signing passwords must not contain NUL: %w", errs.ErrUsage)
		}

		cert, profile, err = decodeXcodeSigningSecrets(signing)
		if err != nil {
			return fmt.Errorf("set up code signing: %w", err)
		}

		exportOptions, err = decodeXcodeExportOptions(XcodeExportIPAInput{ExportOptionsBase64: in.ExportOptionsBase64, ExportOptionsVar: in.ExportOptionsVar})
		if err != nil {
			return fmt.Errorf("export IPA: %w", err)
		}
	}

	if err = preflightXcodeReleaseOutputs(in.EnableCodeSigning); err != nil {
		return fmt.Errorf("release outputs: %w", err)
	}

	_, _ = io.Copy(w, &banner)
	if in.EnableCodeSigning {
		state, setupErr := setupXcodeSigning(ctx, sec, w, signing, cert, profile)
		if setupErr != nil {
			return fmt.Errorf("set up code signing: %w", setupErr)
		}
		// This build owns the signing state for its whole lifetime: a successful
		// build and every later failure both leave the host as they found it.
		defer func() { err = errors.Join(err, teardownXcodeSigning(ctx, sec, state)) }()
	}

	if err = reportXcodeVersionInfo(ctx, pending, stderr, annot, metadata, missing); err != nil {
		return fmt.Errorf("report version: %w", err)
	}

	archive.XcconfigPath, err = writeXcodeXCConfig(xcconfig, "")
	if err != nil {
		return fmt.Errorf("set up xcconfig: %w", err)
	}

	if archive.XcconfigPath != "" {
		// Release scratch, unlike the standalone setup-xcconfig verb whose
		// emitted path a later job step still has to read.
		defer func() { err = errors.Join(err, removeRunOwnedXcconfig(archive.XcconfigPath)) }()
	}

	if err = runXcodeArchive(ctx, buildOps, w, stderr, archive); err != nil {
		return fmt.Errorf("archive: %w", err)
	}

	archiveDir, err := pathsafe.OpenRoot("build/app.xcarchive")
	if err != nil {
		return fmt.Errorf("expected fresh archive directory build/app.xcarchive: %w: %w", err, errs.ErrValidation)
	}

	containErr := containedArchiveBundle(archiveDir)
	identityErr := verifyArchiveScheme(archiveDir, archive.Scheme, w)
	_ = archiveDir.Close()

	if containErr != nil {
		return containErr
	}

	if identityErr != nil {
		return identityErr
	}

	artifacts := []string{"build/app.xcarchive"}

	if in.EnableCodeSigning {
		if err := exportXcodeIPA(ctx, buildOps, w, stderr, exportOptions); err != nil {
			return fmt.Errorf("export IPA: %w", err)
		}

		ipa, err := xcodeReleaseIPA()
		if err != nil {
			return fmt.Errorf("export IPA artifacts: %w", err)
		}

		artifacts = append(artifacts, ipa)
	}

	_, _ = fmt.Fprintln(w, "Built artifacts:")
	for _, path := range artifacts {
		_, _ = fmt.Fprintln(w, path)
	}

	return publishXcodeOutputs(ctx, pending, sink)
}

func removeRunOwnedXcconfig(path string) error {
	if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove run-owned xcconfig: %w", err)
	}

	return nil
}

// containedArchiveBundle requires every link inside the produced archive to
// resolve within the archive itself.
//
// The archive directory is published as a release artifact and consumed by
// whatever unpacks it later. A link inside it that points outside the bundle
// either breaks on the consumer's machine or, worse, resolves there to
// something else entirely; either way the archive does not carry what it
// appears to. Apple bundles legitimately use relative links, notably a
// framework's Versions/Current, so those are supported: what is refused is a
// link leaving the bundle, absolute or relative.
//
// This is containment of the bundle's own structure. It does not certify the
// archive as a valid Apple bundle, and it does not inspect file contents.
func containedArchiveBundle(root *os.Root) error {
	return fs.WalkDir(root.FS(), ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return fmt.Errorf("inspect archive bundle: %w", walkErr)
		}

		if entry.Type()&fs.ModeSymlink == 0 {
			return nil
		}

		target, err := root.Readlink(name)
		if err != nil {
			return fmt.Errorf("read archive bundle link %s: %w", name, err)
		}

		if filepath.IsAbs(target) {
			return fmt.Errorf("archive bundle link %s points outside the bundle (%s): %w", name, target, errs.ErrValidation)
		}

		resolved := path.Join(path.Dir(name), filepath.ToSlash(target))
		if resolved == ".." || strings.HasPrefix(resolved, "../") {
			return fmt.Errorf("archive bundle link %s escapes the bundle (%s): %w", name, target, errs.ErrValidation)
		}

		return nil
	})
}

// Release outputs are create-only; do not erase build/ or adopt stale products.
// These checks assume stable paths and one builder, not hostile concurrent I/O.
func preflightXcodeReleaseOutputs(signed bool) error {
	root, err := pathsafe.OpenRoot("build")
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}

	if err != nil {
		return err
	}

	defer func() { _ = root.Close() }()

	if _, err = root.Lstat("app.xcarchive"); err == nil {
		return fmt.Errorf("build/app.xcarchive must not already exist: %w", errs.ErrValidation)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}

	if !signed {
		return nil
	}

	export, err := pathsafe.OpenRoot("build/export")
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}

	if err != nil {
		return err
	}

	defer func() { _ = export.Close() }()

	entries, err := fs.ReadDir(export.FS(), ".")
	if err != nil {
		return err
	}

	if len(entries) != 0 {
		return fmt.Errorf("build/export must be absent or empty: %w", errs.ErrValidation)
	}

	return nil
}

func xcodeReleaseIPA() (string, error) { //nolint:cyclop // rooted discovery checks links, type, size and cardinality before returning a path.
	root, err := pathsafe.OpenRoot("build/export")
	if err != nil {
		return "", fmt.Errorf("expected build/export directory: %w: %w", err, errs.ErrValidation)
	}
	defer func() { _ = root.Close() }()

	var ipa string

	err = fs.WalkDir(root.FS(), ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		if entry.Type()&fs.ModeSymlink != 0 {
			return fmt.Errorf("export tree must not contain symlinks: %w", errs.ErrValidation)
		}

		if !strings.EqualFold(filepath.Ext(path), ".ipa") {
			return nil
		}

		if filepath.Dir(path) != "." || filepath.Ext(path) != ".ipa" {
			return fmt.Errorf("export IPA must be a direct lowercase .ipa under build/export: %w", errs.ErrValidation)
		}

		info, infoErr := entry.Info()
		if infoErr != nil {
			return infoErr
		}

		if !info.Mode().IsRegular() || info.Size() == 0 || ipa != "" {
			return fmt.Errorf("export must contain exactly one nonempty regular IPA: %w", errs.ErrValidation)
		}

		ipa = filepath.Join("build/export", filepath.FromSlash(path))

		return nil
	})
	if err != nil {
		return "", err
	}

	if ipa == "" {
		return "", fmt.Errorf("export must contain exactly one nonempty regular IPA: %w", errs.ErrValidation)
	}

	return ipa, nil
}

// verifyArchiveScheme compares the scheme the produced bundle records against
// the one this run selected.
//
// Everything up to here proves the bundle came from this run's xcodebuild
// invocation: the archive path is refused if it already exists, and the argv is
// pinned exactly. What none of that shows is what xcodebuild actually built. A
// project whose scheme list changed, or a resolution that picked something
// else, yields an archive for a different target — signed, exported and
// published as this release without a word.
//
// Only a MISMATCH refuses, and that line is held consistently: an Info.plist
// that is absent, unreadable, or carries no SchemeName says nothing about which
// scheme was used, so each is reported and the build continues. Turning any of
// them into a refusal would fail real releases over a plist encoding rather
// than over a wrong archive — a guess dressed as a check. Whether the bundle is
// well-formed at all is a different question, answered by
// containedArchiveBundle and the artifact listing.
func verifyArchiveScheme(root *os.Root, selected string, w io.Writer) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	body, err := root.ReadFile("Info.plist")
	if err != nil {
		_, _ = fmt.Fprintf(w, "Note: the archive has no readable Info.plist (%v); "+
			"the built scheme was not verified against %q\n", err, selected)

		return nil
	}

	recorded, err := build.ArchiveSchemeName(body)
	if err != nil {
		_, _ = fmt.Fprintf(w, "Note: the archive's recorded scheme could not be read (%v); "+
			"the built scheme was not verified against %q\n", err, selected)

		return nil
	}

	if recorded == "" {
		_, _ = fmt.Fprintf(w, "Note: the archive records no SchemeName; "+
			"the built scheme was not verified against %q\n", selected)

		return nil
	}

	if recorded != selected {
		return fmt.Errorf("archive was built for scheme %q, not the selected %q; refusing to publish it: %w",
			recorded, selected, errs.ErrValidation)
	}

	_, _ = fmt.Fprintf(w, "Archive scheme verified: %s\n", recorded)

	return nil
}
