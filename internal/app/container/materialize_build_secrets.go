// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/listval"
	"github.com/diggsweden/reusable-ci/v3/internal/pathsafe"
)

// MaterializeBuildSecretsInput drives MaterializeBuildSecrets.
type MaterializeBuildSecretsInput struct {
	// Names is the newline/comma-separated list of build-secret names
	// declared in artifacts.yml. Each must appear as a key in the
	// envelope JSON.
	Names string
	// EnvelopeJSON is the value of $REUSABLE_CI_BUILD_SECRETS_JSON: a
	// JSON object mapping each declared name to its secret value. The
	// envelope is constructed by the caller workflow at YAML parse time
	// (one ${{ secrets.X }} interpolation per entry).
	EnvelopeJSON string
	// OutputDir is the existing parent under which a unique private secret
	// directory is created. Empty uses TempDir.
	OutputDir string
	// TempDir is the run context's scratch directory. The CLI binding reads $CI_TEMP_DIR /
	// $RUNNER_TEMP via flag sources. Empty → os.TempDir().
	TempDir string
}

// MaterializeBuildSecrets unpacks the JSON envelope into per-secret
// tmpfiles on disk and emits the `secret-mounts` output that `container build
// --secrets` consumes as buildah `--secret` mount specs. The output is a compact
// JSON array so it remains a single-line scalar on GitHub, Forgejo, and GitLab.
//
// The contract is deliberately narrow:
//   - Every name declared in `Names` MUST have a value in the envelope.
//     A mismatch fails fast with a clear error pointing at the missing
//     key — the alternative (silently producing an empty mount) would
//     surface as an inscrutable build failure deep inside BuildKit.
//   - The envelope MAY contain additional keys (the caller could pack
//     more than the artifact declares). Extra keys are ignored.
//   - Each value is written to a fresh mode-0700 directory beneath the
//     selected temp directory, with mode 0600. buildah mounts via `src=` so the file's contents become
//     /run/secrets/<id> inside the RUN step's mount namespace.
//
// A failed invocation removes every file it wrote. The workflow removes the
// dedicated directory under an always-run step after the final build consumer,
// including on persistent self-hosted runners.
//
//nolint:cyclop // sequential fail-fast guards (empty names, empty envelope, malformed JSON, mkdir, per-name presence + write); each branch is a distinct contract violation.
func MaterializeBuildSecrets(
	ctx context.Context,
	sink ci.OutputSink,
	w io.Writer, //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	in MaterializeBuildSecretsInput,
) error {
	names := splitNames(in.Names)
	if len(names) == 0 {
		// No build-secrets declared — emit empty output so the build's
		// `--secrets` input is unset, and skip envelope handling entirely.
		// `container build` accepts an empty `--secrets` value as
		// "no mounts," which is the desired no-op.
		if err := sink.Set(ctx, "secret-dir", ""); err != nil {
			return fmt.Errorf("emit secret-dir: %w", err)
		}

		if err := sink.Set(ctx, "secret-mounts", ""); err != nil {
			return fmt.Errorf("emit secret-mounts: %w", err)
		}

		return nil
	}

	if strings.TrimSpace(in.EnvelopeJSON) == "" {
		return fmt.Errorf("REUSABLE_CI_BUILD_SECRETS_JSON is empty but build-secrets declared %v: %w", names, errs.ErrMissingInput)
	}

	envelope := map[string]string{}
	if err := json.Unmarshal([]byte(in.EnvelopeJSON), &envelope); err != nil {
		return fmt.Errorf("parse REUSABLE_CI_BUILD_SECRETS_JSON (expected object of {name: value}): %w: %w", err, errs.ErrInvalidConfig)
	}

	values, err := declaredSecretValues(names, envelope)
	if err != nil {
		return err
	}

	// The scratch root is threaded in from --temp-dir ($CI_TEMP_DIR,
	// $RUNNER_TEMP); reading the environment here would see only the latter
	// and so site the secrets somewhere other than the run's scratch dir.
	parent := strings.TrimSpace(in.OutputDir)
	if parent == "" {
		parent = strings.TrimSpace(in.TempDir)
	}

	if parent == "" {
		parent = os.TempDir()
	}

	parentRoot, err := pathsafe.OpenRoot(parent)
	if err != nil {
		return fmt.Errorf("open build-secret temp directory %s: %w", parent, err)
	}

	_ = parentRoot.Close()

	dir, err := os.MkdirTemp(parent, "reusable-ci-build-secrets-")
	if err != nil {
		return fmt.Errorf("create private build-secret directory in %s: %w", parent, err)
	}

	root, err := pathsafe.OpenRoot(dir)
	if err != nil {
		_ = os.RemoveAll(dir)

		return fmt.Errorf("open private build-secret directory %s: %w", dir, err)
	}

	keep := false

	defer func() {
		_ = root.Close()

		if !keep {
			_ = os.RemoveAll(dir)
		}
	}()

	// Emit the unpredictable path before writing files. If any later operation
	// fails, the app removes it and the workflow's always-run cleanup repeats
	// that exact-path removal rather than guessing a shared directory name.
	if setErr := sink.Set(ctx, "secret-dir", dir); setErr != nil {
		return fmt.Errorf("emit secret-dir: %w", setErr)
	}

	mounts := make([]string, 0, len(names))
	for index, name := range names {
		// Lowercase the secret id so the Containerfile's
		// `--mount=type=secret,id=db_password` matches consistently.
		// Adopters who prefer the UPPER form on the mount line can
		// continue to use it — id is just a string lookup.
		secretID := strings.ToLower(name)
		path := filepath.Join(dir, secretID)

		file, openErr := root.OpenFile(secretID, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if openErr != nil {
			return fmt.Errorf("write %s: %w", path, openErr)
		}

		if _, writeErr := file.WriteString(values[index]); writeErr != nil {
			_ = file.Close()

			return fmt.Errorf("write %s: %w", path, writeErr)
		}

		if closeErr := file.Close(); closeErr != nil {
			return fmt.Errorf("close %s: %w", path, closeErr)
		}

		mounts = append(mounts, fmt.Sprintf("id=%s,src=%s", secretID, path))
	}

	encoded, err := json.Marshal(mounts)
	if err != nil {
		return fmt.Errorf("encode secret-mounts: %w", err)
	}

	if err := sink.Set(ctx, "secret-mounts", string(encoded)); err != nil {
		return fmt.Errorf("emit secret-mounts: %w", err)
	}

	keep = true
	_, _ = fmt.Fprintf(w, "Materialized %d build-secret(s) to %s\n", len(names), dir)

	return nil
}

// declaredSecretValues returns each declared name's value from the envelope,
// in declaration order, refusing a repeated, malformed or missing name.
func declaredSecretValues(names []string, envelope map[string]string) ([]string, error) {
	values := make([]string, 0, len(names))
	seen := make(map[string]bool, len(names))

	for _, name := range names {
		// A repeated name would map two declarations to one mount file; the
		// second create fails only after the first secret is on disk.
		if seen[name] {
			return nil, fmt.Errorf("build-secret name %q is declared more than once: %w", name, errs.ErrInvalidConfig)
		}

		seen[name] = true

		// Re-check the single-sourced name shape locally: the same rule
		// config validation enforces, but asserted here where the name
		// becomes a filename — so this verb is safe even when invoked
		// directly with a hand-crafted --names, not only downstream of a
		// validated artifacts.yml (no separators or dots can reach the
		// filepath.Join below).
		if !config.ValidBuildSecretName(name) {
			return nil, fmt.Errorf(
				"build-secret name %q is not a valid env-var name (need [A-Z_][A-Z0-9_]*): %w",
				name, errs.ErrInvalidConfig,
			)
		}

		value, ok := envelope[name]
		if !ok || value == "" {
			return nil, fmt.Errorf(
				"REUSABLE_CI_BUILD_SECRETS_JSON does not contain key %q with a nonempty value "+
					"(declared in containers[].build-secrets); the caller "+
					"workflow's envelope must include every name from the "+
					"artifacts.yml list: %w",
				name, errs.ErrInvalidConfig,
			)
		}

		values = append(values, value)
	}

	return values, nil
}

// splitNames accepts the comma/space/newline-separated workflow input and
// returns the cleaned list. Empty / whitespace-only entries are dropped.
func splitNames(raw string) []string {
	return listval.Tokens(raw)
}
