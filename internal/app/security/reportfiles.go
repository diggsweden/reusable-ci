// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package security

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/diggsweden/reusable-ci/v3/internal/cliio"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/pathsafe"
)

// Configured reports belong to the current invocation, not a last-success cache.
// Refused path/config preflight preserves them. Once a scan starts, old reports
// are retired and only fresh validated files may be published. This is not a
// cross-file transaction or rollback of arbitrary filesystem failures.
func prepareScanReports(destinations ...string) (string, error) {
	if err := validateReportPaths(destinations...); err != nil {
		return "", err
	}

	tempRoot, err := filepath.EvalSymlinks(os.TempDir())
	if err != nil {
		return "", err
	}

	workDir, err := os.MkdirTemp(tempRoot, "reusable-ci-scan-")
	if err != nil {
		return "", err
	}

	for _, destination := range destinations {
		if err := retireScanReport(destination); err != nil {
			_ = os.RemoveAll(workDir)

			return "", err
		}
	}

	return workDir, nil
}

// Input files retain the scanner's native read/link policy, but an output must
// never retire or replace the file being scanned, including through an alias.
func validateScanInputReports(input string, destinations ...string) error {
	source, err := os.Stat(input)
	if err != nil || !source.Mode().IsRegular() {
		return nil //nolint:nilerr // missing/non-file scan targets are validated by the native scanner.
	}

	for _, destination := range destinations {
		absolute, err := filepath.Abs(destination)
		if err != nil {
			return err
		}

		info, err := os.Stat(absolute)
		if err == nil && os.SameFile(source, info) {
			return fmt.Errorf("scan input aliases a report destination: %w", errs.ErrUsage)
		}
	}

	return nil
}

func retireScanReport(destination string) error {
	root, err := pathsafe.OpenRoot(filepath.Dir(destination))
	if err != nil {
		return err
	}
	defer func() { _ = root.Close() }()

	name := filepath.Base(destination)

	info, err := root.Lstat(name)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}

	if err != nil {
		return err
	}

	if !info.Mode().IsRegular() {
		return fmt.Errorf("scan report changed to a nonregular destination: %w", errs.ErrValidation)
	}

	if err := root.Remove(name); err != nil {
		return fmt.Errorf("retire prior scan report %q: %w", destination, err)
	}

	return nil
}

func readScanReport(path string) ([]byte, error) {
	root, err := pathsafe.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, err
	}
	defer func() { _ = root.Close() }()

	name := filepath.Base(path)

	info, err := root.Lstat(name)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("read fresh scan report %q: %w: %w", path, err, errs.ErrMissingInput)
		}

		if errors.Is(err, os.ErrPermission) {
			return nil, fmt.Errorf("read fresh scan report %q: %w: %w", path, err, errs.ErrPermissionDenied)
		}

		return nil, err
	}

	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("fresh scan report must be a nonlinked regular file: %w", errs.ErrMalformedInput)
	}

	return cliio.ReadFileInRoot(root, name)
}

// JSON reports must at least be non-null objects. Detailed native envelope
// policy remains at the domain parser, not a duplicate scanner schema here.
func readJSONScanReport(path string) ([]byte, error) {
	body, err := readScanReport(path)
	if err != nil {
		return nil, err
	}

	var object map[string]json.RawMessage
	if decodeErr := json.Unmarshal(body, &object); decodeErr != nil {
		return nil, fmt.Errorf("parse fresh JSON report %q: %w: %w", path, decodeErr, errs.ErrMalformedInput)
	}

	if object == nil {
		return nil, fmt.Errorf("fresh JSON report %q must be an object: %w", path, errs.ErrMalformedInput)
	}

	return body, nil
}

func publishScanReport(source, destination string, jsonReport bool) error {
	read := readScanReport
	if jsonReport {
		read = readJSONScanReport
	}

	body, err := read(source)
	if err != nil {
		return err
	}

	stage, err := pathsafe.NewArtifactStaging(filepath.Dir(destination))
	if err != nil {
		return err
	}

	defer func() { _ = stage.Close() }()

	if err := stage.Root().WriteFile(filepath.Base(destination), body, 0o644); err != nil {
		return err
	}

	return stage.Install()
}
