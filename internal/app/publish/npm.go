// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package publish

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/archive"
	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
)

// NPMRCInput drives WriteNPMRC.
type NPMRCInput struct {
	// Registry is the npm registry URL. http:// and https:// are accepted;
	// other schemes are rejected. Required.
	Registry string
	// Scope is the optional npm package scope (must start with @).
	Scope string
	// Output is where the generated .npmrc body is written. The caller
	// chooses the file path; passing os.Stdout is supported for dry-runs.
	Output io.Writer
}

// WriteNPMRC composes an `.npmrc` body matching the historical bash heredoc
// in publish-dev-npm.yml. The literal `${NODE_AUTH_TOKEN}` placeholder is
// emitted as-is so npm expands it at publish time from the env var.
//
// Format:
//
//	//<host>/:_authToken=${NODE_AUTH_TOKEN}
//	@scope:registry=<registry>     (when Scope is set)
//	registry=<registry>            (when Scope is empty)
//	always-auth=true
func WriteNPMRC(in NPMRCInput) error {
	if in.Output == nil {
		return fmt.Errorf("output writer is required: %w", errs.ErrUsage)
	}

	settings, err := validateNPMRCInput(in)
	if err != nil {
		return err
	}

	return emitNPMRCLines(in.Output, settings)
}

// npmrcSettings is the parsed/validated form of the .npmrc inputs.
type npmrcSettings struct {
	host               string
	normalisedRegistry string
	scope              string
}

// validateNPMRCInput parses the registry URL, validates the optional
// scope, and returns the settings WriteNPMRC will emit.
func validateNPMRCInput(in NPMRCInput) (npmrcSettings, error) {
	registry := strings.TrimSpace(in.Registry)
	if registry == "" {
		return npmrcSettings{}, fmt.Errorf("registry is required: %w", errs.ErrUsage)
	}

	parsed, err := url.Parse(registry)
	if err != nil {
		return npmrcSettings{}, fmt.Errorf("parse registry %q: %w: %w", registry, err, errs.ErrUsage)
	}

	if parsed.Scheme != "https" && parsed.Scheme != "http" {
		return npmrcSettings{}, fmt.Errorf("registry scheme %q must be http or https: %w", parsed.Scheme, errs.ErrUsage)
	}

	if parsed.Host == "" {
		return npmrcSettings{}, fmt.Errorf("registry %q has no host: %w", registry, errs.ErrUsage)
	}

	scope := strings.TrimSpace(in.Scope)
	if scope != "" && !isNPMScope(scope) {
		return npmrcSettings{}, fmt.Errorf("scope %q is not a valid npm scope: %w", scope, errs.ErrUsage)
	}

	host := parsed.Host
	if parsed.Path != "" && parsed.Path != "/" {
		host += strings.TrimSuffix(parsed.Path, "/")
	}

	return npmrcSettings{
		host:               host,
		normalisedRegistry: parsed.Scheme + "://" + parsed.Host + strings.TrimSuffix(parsed.Path, "/"),
		scope:              scope,
	}, nil
}

// emitNPMRCLines writes the three-line .npmrc body. Scoped vs unscoped
// projects differ only in the registry-key prefix.
func emitNPMRCLines(w io.Writer, s npmrcSettings) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if _, err := fmt.Fprintf(w, "//%s/:_authToken=${NODE_AUTH_TOKEN}\n", s.host); err != nil {
		return fmt.Errorf("write .npmrc: %w", err)
	}

	registryLine := "registry=" + s.normalisedRegistry + "\n"
	if s.scope != "" {
		registryLine = s.scope + ":registry=" + s.normalisedRegistry + "\n"
	}

	if _, err := fmt.Fprint(w, registryLine); err != nil {
		return fmt.Errorf("write .npmrc: %w", err)
	}

	if _, err := fmt.Fprintln(w, "always-auth=true"); err != nil {
		return fmt.Errorf("write .npmrc: %w", err)
	}

	return nil
}

// isNPMScope reports whether s is a valid npm scope: leading '@', followed by
// 1+ characters drawn from [a-z0-9-_.~]. Mirrors npm's own scope-name rules
// without pulling in a regex dependency.
//nolint:cyclop // npm scope validation: one branch per allowed/disallowed rune class.
func isNPMScope(s string) bool {
	if len(s) < 2 || s[0] != '@' {
		return false
	}

	for _, r := range s[1:] {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= '0' && r <= '9':
		case r == '-' || r == '_' || r == '.' || r == '~':
		default:
			return false
		}
	}

	return true
}

// NPMOps is the npm adapter surface needed by publish helpers.
type NPMOps interface {
	Run(ctx context.Context, dir string, args ...string) (w, stderr string, err error)
}

// NPMValidateTarballInput drives NPMValidateTarball.
type NPMValidateTarballInput struct {
	// Dir is the directory containing the tarball. Empty → cwd.
	Dir string
}

// NPMCheckVersionInput drives NPMCheckVersion.
type NPMCheckVersionInput struct {
	Dir      string
	Name     string
	Version  string
	Registry string
}

// NPMCheckVersion checks whether package@version already exists in a registry
// and emits already-published=true|false. E404-style npm failures are treated
// as not found; other npm failures surface as errors.
func NPMCheckVersion(ctx context.Context, npm NPMOps, sink ci.OutputSink, w io.Writer, annot output.Annotator, in NPMCheckVersionInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	name := in.Name
	if name == "" {
		var err error

		name, err = npmPackageName(defaultDir(in.Dir))
		if err != nil {
			return err
		}
	}

	if in.Version == "" {
		return fmt.Errorf("version is required: %w", errs.ErrUsage)
	}

	args := []string{"view", name + "@" + in.Version, "version"}
	if in.Registry != "" {
		args = append(args, "--registry", in.Registry)
	}

	_, stderr, err := npm.Run(ctx, defaultDir(in.Dir), args...)
	if err == nil {
		annot.Warningf("Package %s@%s already exists in registry - skipping publish", name, in.Version)

		if setErr := setAlreadyPublished(ctx, sink, "true"); setErr != nil {
			return setErr
		}

		return nil
	}

	if isNPMNotFound(stderr) {
		_, _ = fmt.Fprintf(w, "Version %s not found in registry - will publish\n", in.Version)

		if setErr := setAlreadyPublished(ctx, sink, "false"); setErr != nil {
			return setErr
		}

		return nil
	}

	return err
}

func setAlreadyPublished(ctx context.Context, sink ci.OutputSink, value string) error {
	if err := sink.Set(ctx, "already-published", value); err != nil {
		return fmt.Errorf("set already-published: %w", err)
	}

	return nil
}

// NPMValidateTarball finds a single *.tgz / *.tar.gz at the top of Dir,
// extracts it with `tar --strip-components=1` semantics, removes the
// tarball, lists the extracted contents from dist/ or build/, and
// checks that dist/cli.js exists. Mirrors.
func NPMValidateTarball(_ context.Context, w, stderr io.Writer, annot output.Annotator, in NPMValidateTarballInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	dir := in.Dir
	if dir == "" {
		var err error

		dir, err = os.Getwd()
		if err != nil {
			return fmt.Errorf("getwd: %w", err)
		}
	}

	tarball, err := findFirstTarball(dir)
	if err != nil {
		return err
	}

	if tarball == "" {
		annot.Errorf("No tarball found in artifacts")

		return fmt.Errorf("no tarball found in %s: %w", dir, errs.ErrMissingInput)
	}

	_, _ = fmt.Fprintf(w, "Extracting %s...\n", tarball)

	if err := archive.UntarStripOne(tarball, dir); err != nil {
		return fmt.Errorf("extract %s: %w", tarball, err)
	}

	if err := os.Remove(tarball); err != nil {
		return fmt.Errorf("remove tarball: %w", err)
	}

	_, _ = fmt.Fprintln(w, "Extracted contents:")

	listed := listFiles(w, filepath.Join(dir, "dist"))
	if !listed {
		listFiles(w, filepath.Join(dir, "build"))
	}

	cliJS := filepath.Join(dir, "dist", "cli.js")

	_, _ = fmt.Fprintln(w, "")
	_, _ = fmt.Fprintln(w, "Verifying dist/cli.js exists:")

	if _, err := os.Stat(cliJS); err == nil {
		_, _ = fmt.Fprintln(w, "✓ dist/cli.js found")
	} else {
		_, _ = fmt.Fprintln(w, "✗ dist/cli.js NOT found")

		return fmt.Errorf("dist/cli.js not found in extracted tarball: %w", errs.ErrValidation)
	}

	return nil
}

func npmPackageName(dir string) (string, error) {
	body, err := os.ReadFile(filepath.Join(dir, "package.json")) //nolint:gosec // dir is CLI-flag-derived; filename component is hardcoded.
	if err != nil {
		return "", fmt.Errorf("read package.json: %w", err)
	}

	var meta struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &meta); err != nil {
		return "", fmt.Errorf("parse package.json: %w: %w", err, errs.ErrInvalidConfig)
	}

	if meta.Name == "" {
		return "", fmt.Errorf("package.json name is required: %w", errs.ErrInvalidConfig)
	}

	return meta.Name, nil
}

func isNPMNotFound(stderr string) bool {
	lower := strings.ToLower(stderr)

	return strings.Contains(lower, "npm err! code e404") ||
		strings.Contains(lower, "npm error code e404") ||
		strings.Contains(lower, "npm err! 404") ||
		strings.Contains(lower, "npm error 404")
}

func defaultDir(dir string) string {
	if dir == "" {
		return "."
	}

	return dir
}

// findFirstTarball returns the path to the first *.tgz / *.tar.gz file
// found in dir (non-recursive). Matches `find . -maxdepth 1`.
func findFirstTarball(dir string) (string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return "", fmt.Errorf("read %s: %w", dir, err)
	}

	for _, e := range entries {
		if e.IsDir() {
			continue
		}

		name := e.Name()
		if strings.HasSuffix(name, ".tgz") || strings.HasSuffix(name, ".tar.gz") {
			return filepath.Join(dir, name), nil
		}
	}

	return "", nil
}

// listFiles prints "<path>" for every regular file under dir. Returns
// true iff dir existed and was walked.
func listFiles(out io.Writer, dir string) bool {
	info, err := os.Stat(dir)
	if err != nil || !info.IsDir() {
		return false
	}

	_ = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		if err != nil {
			slog.Debug("listFiles: skipping unreadable entry", "path", path, "err", err)

			return nil
		}

		if !d.IsDir() {
			_, _ = fmt.Fprintln(out, path)
		}

		return nil
	})

	return true
}
