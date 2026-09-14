// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/cliio"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/pathsafe"
)

// XcodeArtifactNameInput drives XcodeArtifactName.
type XcodeArtifactNameInput struct {
	ArtifactName   string
	RepositoryName string
	IncludeTag     bool
	RefName        string
}

// XcodeArtifactName computes and emits the IPA upload-artifact name.
func XcodeArtifactName(ctx context.Context, sink ci.OutputSink, w io.Writer, in XcodeArtifactNameInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	baseName := firstNonEmpty(in.ArtifactName, in.RepositoryName)
	if baseName == "" {
		return fmt.Errorf("artifact name or repository name is required: %w", errs.ErrUsage)
	}

	name := baseName
	if in.IncludeTag && in.RefName != "" {
		name = baseName + "-" + in.RefName
	}

	if err := sink.Set(ctx, "ipa-name", name); err != nil {
		return fmt.Errorf("set ipa-name: %w", err)
	}

	_, _ = fmt.Fprintf(w, "IPA artifact: %s\n", name)

	return nil
}

// XcodeXCConfigInput drives XcodeXCConfig.
type XcodeXCConfigInput struct {
	Base64  string
	TempDir string
}

// XcodeXCConfig decodes an optional xcconfig secret and emits xcconfig-path
// only when a secret was provided.
func XcodeXCConfig(ctx context.Context, sink ci.OutputSink, annot output.Annotator, in XcodeXCConfigInput) error {
	body, err := decodeXcodeXCConfig(in.Base64)
	if err != nil {
		return err
	}

	path, err := writeXcodeXCConfig(body, in.TempDir)
	if err != nil {
		return err
	}

	if path == "" {
		annot.Noticef("XCCONFIG_BASE64 secret not set - no xcconfig will be applied")

		return nil
	}

	if err := sink.Set(ctx, "xcconfig-path", path); err != nil {
		return fmt.Errorf("set xcconfig-path: %w", err)
	}

	annot.Noticef("xcconfig decoded from XCCONFIG_BASE64 secret")

	return nil
}

func decodeXcodeXCConfig(encoded string) ([]byte, error) {
	if strings.TrimSpace(encoded) == "" {
		return nil, nil
	}

	body, err := decodeMobileSecret(encoded)
	if err != nil {
		return nil, fmt.Errorf("decode xcconfig base64: %w", err)
	}

	return body, nil
}

func writeXcodeXCConfig(body []byte, tempDir string) (string, error) {
	if len(body) == 0 {
		return "", nil
	}

	if tempDir == "" {
		tempDir = os.TempDir()
	}

	f, err := os.CreateTemp(tempDir, "ci-*.xcconfig") //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if err != nil {
		return "", fmt.Errorf("create xcconfig tempfile: %w", err)
	}

	if _, err := f.Write(body); err != nil {
		_ = f.Close()

		return "", fmt.Errorf("write xcconfig: %w", err)
	}

	if err := f.Close(); err != nil {
		return "", fmt.Errorf("close xcconfig: %w", err)
	}

	return f.Name(), nil
}

// XcodeVersionInfoInput drives XcodeVersionInfo.
type XcodeVersionInfoInput struct {
	// Project, when non-empty, points at the .xcodeproj directory the
	// version-info should be read from. Relative selectors are rooted at Root.
	Project string
	// Workspace selects a single project via group-relative FileRefs in
	// contents.xcworkspacedata. Ambiguous or unsupported workspaces are refused.
	Workspace string
	// Root bounds selected inputs; empty means cwd. Only with NO selector,
	// the historical first match in a depth-first .xcodeproj walk wins.
	Root string
}

// XcodeVersionInfo reads MARKETING_VERSION and CURRENT_PROJECT_VERSION
// from the project.pbxproj inside the resolved .xcodeproj and emits
// them as `version` / `build` outputs. These are source values, not evaluated
// settings for the selected scheme/configuration or archive build overrides.
//
// Missing keys remain "unknown". For compatibility, an explicitly supplied
// project with a missing PBX file also emits "unknown", never another project's
// values. Workspace selection must be inspectable; it has no unknown/read-error
// fallback. Selector-free discovery retains its historical read-error fallback.
func XcodeVersionInfo(ctx context.Context, sink ci.OutputSink, stderr io.Writer, annot output.Annotator, in XcodeVersionInfoInput) error {
	got, missing, err := resolveXcodeVersionInfo(in)
	if err != nil {
		return err
	}

	return reportXcodeVersionInfo(ctx, sink, stderr, annot, got, missing)
}

func resolveXcodeVersionInfo(in XcodeVersionInfoInput) (build.XcodeVersionInfo, bool, error) {
	root := in.Root
	if root == "" {
		var err error

		root, err = os.Getwd()
		if err != nil {
			return build.XcodeVersionInfo{}, false, fmt.Errorf("getwd: %w", err)
		}
	}

	project, workspace := strings.TrimSpace(in.Project), strings.TrimSpace(in.Workspace)

	body, err := readXcodeVersionProject(root, project, workspace)
	if err != nil {
		if workspace != "" || (project != "" && !errors.Is(err, fs.ErrNotExist)) {
			return build.XcodeVersionInfo{}, false, fmt.Errorf("resolve selected Xcode metadata: %w: %w", err, errs.ErrValidation)
		}

		return build.XcodeVersionInfo{Version: "unknown", Build: "unknown"}, true, nil
	}

	return build.ParseXcodeVersionFromPbxproj(string(body)), false, nil
}

func reportXcodeVersionInfo(ctx context.Context, sink ci.OutputSink, stderr io.Writer, annot output.Annotator, got build.XcodeVersionInfo, missing bool) error {
	if missing {
		annot.Warningf("Could not determine version from project file")
	}

	if err := sink.Set(ctx, "version", got.Version); err != nil {
		return err
	}

	if err := sink.Set(ctx, "build", got.Build); err != nil {
		return err
	}

	if !missing {
		_, _ = fmt.Fprintf(stderr, "Version: %s (%s)\n", got.Version, got.Build)
	}

	return nil
}

func readXcodeVersionProject(root, project, workspace string) ([]byte, error) {
	if project != "" && workspace != "" {
		return nil, fmt.Errorf("exactly one workspace or project is required: %w", errs.ErrUsage)
	}

	if project == "" && workspace == "" {
		project = findFirstXcodeproj(root)
		if project == "" {
			return nil, fs.ErrNotExist
		}

		return os.ReadFile(filepath.Join(project, "project.pbxproj")) //nolint:gosec // historical selector-free discovery only.
	}

	if workspace != "" {
		// Descriptor validation and FileRefs must share one normalized workspace identity.
		workspace = filepath.Clean(workspace)

		body, err := readXcodeMetadataFile(root, workspace, ".xcworkspace", "contents.xcworkspacedata")
		if err != nil {
			return nil, err
		}

		project, err = xcodeWorkspaceProject(body, filepath.Dir(workspace))
		if err != nil {
			return nil, err
		}
	}

	return readXcodeMetadataFile(root, project, ".xcodeproj", "project.pbxproj")
}

func readXcodeMetadataFile(root, identity, suffix, name string) ([]byte, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}

	if !filepath.IsAbs(identity) {
		identity = filepath.Join(absRoot, identity)
	}

	rel, err := filepath.Rel(absRoot, identity)
	if err != nil || !filepath.IsLocal(rel) || filepath.Ext(identity) != suffix {
		return nil, fmt.Errorf("selected Xcode identity must be a %s directory under Root: %w", suffix, errs.ErrValidation)
	}

	directory, err := pathsafe.OpenRoot(identity)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("open Xcode identity: %w: %w", err, errs.ErrMissingInput)
		}

		return nil, err
	}

	defer func() { _ = directory.Close() }()

	info, err := directory.Lstat(name)
	if err == nil && info.Mode()&fs.ModeSymlink != 0 {
		return nil, fmt.Errorf("selected Xcode metadata %s must not be a symlink: %w", name, errs.ErrValidation)
	}

	return cliio.ReadFileInRoot(directory, name)
}

// Only the common Workspace/Group/FileRef XML subset is understood. A group's
// location is relative to its parent; top-level refs are relative to the
// workspace's containing directory, NOT its basename or bundle directory.
// No scheme interpreter, basename guess, or global project search is safe here.
func xcodeWorkspaceProject(body []byte, base string) (string, error) { //nolint:cyclop,gocognit,gocyclo // explicit bounded XML state machine refuses unsupported or ambiguous selection.
	const fileRef = "FileRef"

	body = bytes.TrimPrefix(body, []byte("\xef\xbb\xbf"))
	decoder := xml.NewDecoder(bytes.NewReader(body))

	type group struct{ element, path string }

	var (
		stack   []group
		project string
	)

	seenWorkspace := false

	for {
		firstToken := decoder.InputOffset() == 0

		token, err := decoder.Token()
		if errors.Is(err, io.EOF) {
			break
		}

		if err != nil {
			return "", fmt.Errorf("read workspace XML: %w: %w", err, errs.ErrValidation)
		}

		switch node := token.(type) {
		case xml.StartElement:
			if node.Name.Space != "" || len(stack) >= 64 {
				return "", fmt.Errorf("unsupported workspace XML namespace or depth: %w", errs.ErrValidation)
			}

			if node.Name.Local == "Workspace" && !seenWorkspace {
				seenWorkspace = true

				stack = append(stack, group{"Workspace", base})

				continue
			}

			if len(stack) == 0 || stack[len(stack)-1].element == fileRef || (node.Name.Local != "Group" && node.Name.Local != fileRef) {
				return "", fmt.Errorf("unsupported workspace XML element %q: %w", node.Name.Local, errs.ErrValidation)
			}

			var location string

			locations := 0

			for _, attr := range node.Attr {
				if attr.Name.Local == "location" {
					locations++
					if locations > 1 || attr.Name.Space != "" {
						return "", fmt.Errorf("ambiguous workspace location: %w", errs.ErrValidation)
					}

					location = attr.Value
				}
			}

			rel, supported := strings.CutPrefix(location, "group:")
			if !supported || filepath.IsAbs(rel) {
				return "", fmt.Errorf("unsupported workspace location %q: %w", location, errs.ErrValidation)
			}

			path := filepath.Join(stack[len(stack)-1].path, rel)
			if node.Name.Local == fileRef {
				if filepath.Ext(path) != ".xcodeproj" || (project != "" && project != path) {
					return "", fmt.Errorf("workspace must select a single project: %w", errs.ErrValidation)
				}

				project = path
			}

			stack = append(stack, group{node.Name.Local, path})
		case xml.EndElement:
			stack = stack[:len(stack)-1]
		case xml.CharData:
			if strings.TrimSpace(string(node)) != "" {
				return "", fmt.Errorf("unexpected workspace XML text: %w", errs.ErrValidation)
			}
		case xml.ProcInst:
			// Token accepts declarations anywhere; XML permits one only at the start.
			if strings.EqualFold(node.Target, "xml") && (node.Target != "xml" || !firstToken) {
				return "", fmt.Errorf("misplaced or reserved-case workspace XML declaration: %w", errs.ErrValidation)
			}
		case xml.Directive:
			return "", fmt.Errorf("unsupported workspace XML directive: %w", errs.ErrValidation)
		}
	}

	if !seenWorkspace || len(stack) != 0 || project == "" {
		return "", fmt.Errorf("workspace must select a single project: %w", errs.ErrValidation)
	}

	return project, nil
}

// findFirstXcodeproj returns the first *.xcodeproj directory found
// under root, or empty if none. Matches `find ... | head -1`
// shape with deterministic ordering.
func findFirstXcodeproj(root string) string {
	var first string

	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		if err != nil {
			slog.Debug("findFirstXcodeproj: skipping unreadable entry", "path", path, "err", err)

			return nil
		}

		if d.IsDir() && strings.HasSuffix(path, ".xcodeproj") {
			first = path

			return filepath.SkipAll
		}

		return nil
	})

	return first
}
