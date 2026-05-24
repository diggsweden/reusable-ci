// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package build

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
)

// NPMOps is the npm adapter surface needed by capture-style NPM build helpers
// (those that parse machine-readable npm output, e.g. `npm pack --json`).
type NPMOps interface {
	Run(ctx context.Context, dir string, args ...string) (w, stderr string, err error)
}

// NPMRunner is the npm adapter surface needed by streaming NPM build helpers
// (those that run a build script and forward all output live).
type NPMRunner interface {
	RunInherit(ctx context.Context, dir string, w, stderr io.Writer, args ...string) error
}

// NPMMetadataInput drives NPMMetadata.
type NPMMetadataInput struct {
	Dir          string
	PackageScope string
}

type npmPackageJSON struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

// NPMMetadata reads package.json, validates an optional scope, and emits the
// package name/version outputs for workflow callers.
func NPMMetadata(ctx context.Context, sink ci.OutputSink, w io.Writer, annot output.Annotator, in NPMMetadataInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	dir := defaultDir(in.Dir)

	meta, err := readNPMPackageJSON(dir)
	if err != nil {
		return err
	}

	if meta.Name == "" {
		return fmt.Errorf("package.json name is required: %w", errs.ErrInvalidConfig)
	}

	if meta.Version == "" {
		return fmt.Errorf("package.json version is required: %w", errs.ErrInvalidConfig)
	}

	if in.PackageScope != "" && !strings.HasPrefix(meta.Name, strings.TrimRight(in.PackageScope, "/")+"/") {
		annot.Errorf("package.json name must be scoped as %s/<pkg>", in.PackageScope)

		_, _ = fmt.Fprintf(w, "Found: %s\n", meta.Name)

		return fmt.Errorf("package name %q does not match scope %q: %w", meta.Name, in.PackageScope, errs.ErrInvalidConfig)
	}

	// Deterministic emission order — map iteration would randomise it.
	outputs := []struct{ key, value string }{
		{"name", meta.Name},
		{"version", meta.Version}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
	}
	for _, out := range outputs {
		if err := sink.Set(ctx, out.key, out.value); err != nil {
			return fmt.Errorf("set %s: %w", out.key, err)
		}
	}

	_, _ = fmt.Fprintf(w, "Package: %s@%s\n", meta.Name, meta.Version)

	return nil
}

// NPMApplicationInput drives NPMApplication.
type NPMApplicationInput struct {
	// Dir is the project root containing package.json. Defaults to ".".
	Dir string
	// ScriptName is the npm script to run. Defaults to "build".
	ScriptName string
}

// NPMApplication runs `npm run <script>` iff package.json declares the
// named script. Empty/absent script is logged and returns nil — npm projects
// without a build step (lots of CLI/lib packages) are valid.
func NPMApplication(ctx context.Context, ops NPMRunner, w, stderr io.Writer, in NPMApplicationInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	dir := defaultDir(in.Dir)

	script := in.ScriptName
	if script == "" {
		script = subCmdBuild
	}

	has, err := npmHasScript(dir, script)
	if err != nil {
		return err
	}

	if !has {
		_, _ = fmt.Fprintf(w, "No %q script in package.json; skipping build step\n", script)

		return nil
	}

	_, _ = fmt.Fprintf(w, "Running %q npm script...\n", script)

	if err := ops.RunInherit(ctx, dir, w, stderr, "run", script); err != nil {
		return err
	}

	return nil
}

// npmHasScript reports whether package.json defines a script named s with a
// non-empty body. Reading the JSON is preferable to `grep -q '"build"'` —
// it avoids false positives on string literals elsewhere in the file.
func npmHasScript(dir, name string) (bool, error) {
	body, err := os.ReadFile(filepath.Join(dir, "package.json")) //nolint:gosec // dir is CLI-flag-derived; filename component is hardcoded.
	if err != nil {
		return false, fmt.Errorf("read package.json: %w", err)
	}

	var pkg struct {
		Scripts map[string]string `json:"scripts"`
	}
	if err := json.Unmarshal(body, &pkg); err != nil {
		return false, fmt.Errorf("parse package.json: %w: %w", err, errs.ErrInvalidConfig)
	}

	return strings.TrimSpace(pkg.Scripts[name]) != "", nil
}

// NPMPack runs `npm pack --json`, validates the machine-readable result, and
// emits the created tarball filename.
func NPMPack(ctx context.Context, ops NPMOps, sink ci.OutputSink, w, stderr io.Writer, in NPMMetadataInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	dir := defaultDir(in.Dir)

	_, _ = fmt.Fprintln(w, "Running npm pack...")

	out, errOut, err := ops.Run(ctx, dir, "pack", "--json")
	if errOut != "" {
		_, _ = fmt.Fprint(stderr, errOut)
	}

	if err != nil {
		return err
	}

	filename, err := parseNPMPackFilename(out)
	if err != nil {
		return err
	}

	if _, err := os.Stat(filepath.Join(dir, filename)); err != nil {
		return fmt.Errorf("stat npm pack output %q: %w", filename, err)
	}

	if err := sink.Set(ctx, "tarball", filename); err != nil {
		return fmt.Errorf("set tarball: %w", err)
	}

	_, _ = fmt.Fprintf(w, "Created tarball: %s\n", filename)

	return nil
}

func readNPMPackageJSON(dir string) (npmPackageJSON, error) {
	path := filepath.Join(dir, "package.json")

	body, err := os.ReadFile(path) //nolint:gosec // dir is CLI-flag-derived; filename component is hardcoded.
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return npmPackageJSON{}, fmt.Errorf("package.json not found at %q: %w", path, errs.ErrMissingInput)
		}

		return npmPackageJSON{}, fmt.Errorf("read package.json at %q: %w", path, err)
	}

	var meta npmPackageJSON
	if err := json.Unmarshal(body, &meta); err != nil {
		return npmPackageJSON{}, fmt.Errorf("parse package.json: %w: %w", err, errs.ErrInvalidConfig)
	}

	return meta, nil
}

func parseNPMPackFilename(value string) (string, error) {
	var items []struct {
		Filename string `json:"filename"`
	}
	if err := json.Unmarshal([]byte(value), &items); err != nil {
		return "", fmt.Errorf("parse npm pack --json output: %w: %w", err, errs.ErrInvalidConfig)
	}

	if len(items) != 1 {
		return "", fmt.Errorf("npm pack returned %d packages, want exactly 1: %w", len(items), errs.ErrInvalidConfig)
	}

	if items[0].Filename == "" {
		return "", fmt.Errorf("npm pack output missing filename: %w", errs.ErrInvalidConfig)
	}

	return items[0].Filename, nil
}

func defaultDir(dir string) string {
	if dir == "" {
		return "."
	}

	return dir
}
