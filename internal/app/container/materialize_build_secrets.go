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
	// OutputDir is where to write the per-secret tmpfiles. Each file
	// gets mode 0600. Empty → <TempDir>/build-secrets.
	OutputDir string
	// TempDir is the run context's scratch directory, used to site the
	// default OutputDir. The CLI binding reads $CI_TEMP_DIR /
	// $RUNNER_TEMP via flag sources. Empty → os.TempDir().
	TempDir string
}

// MaterializeBuildSecrets unpacks the JSON envelope into per-secret
// tmpfiles on disk and emits the `secret-mounts` output that
// `container build --secrets` consumes as buildah `--secret` mount specs
// (id=NAME,src=PATH lines).
//
// The contract is deliberately narrow:
//   - Every name declared in `Names` MUST have a value in the envelope.
//     A mismatch fails fast with a clear error pointing at the missing
//     key — the alternative (silently producing an empty mount) would
//     surface as an inscrutable build failure deep inside BuildKit.
//   - The envelope MAY contain additional keys (the caller could pack
//     more than the artifact declares). Extra keys are ignored.
//   - Each value is written to <OutputDir>/<lowercased-name> with mode
//     0600. buildah mounts via `src=` so the file's contents become
//     /run/secrets/<id> inside the RUN step's mount namespace.
//
// Files written here live only for the lifetime of the runner job.
// Cleanup is implicit (GitHub-hosted runners are ephemeral); a
// self-hosted-runner adopter who treats the workspace as persistent
// would need a trap-on-exit to remove the files. Documented in
// docs/artifacts-reference.md.
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

	// The scratch root is threaded in from --temp-dir ($CI_TEMP_DIR,
	// $RUNNER_TEMP); reading the environment here would see only the latter
	// and so site the secrets somewhere other than the run's scratch dir.
	dir := strings.TrimSpace(in.OutputDir)
	if dir == "" {
		root := strings.TrimSpace(in.TempDir)
		if root == "" {
			root = os.TempDir()
		}

		dir = filepath.Join(root, "build-secrets")
	}

	if err := os.MkdirAll(dir, 0o700); err != nil { //nolint:gosec // private secret directory.
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}
	// MkdirAll is a no-op on an existing directory, so it won't tighten a
	// pre-existing loose mode. The path ($RUNNER_TEMP/build-secrets) is
	// fixed and predictable — a prior step or a self-hosted-runner leftover
	// could have created it world-listable, exposing the secret filenames.
	// Enforce 0700 unconditionally, mirroring the gpg adapter's GNUPGHOME
	// handling.
	if err := os.Chmod(dir, 0o700); err != nil { //nolint:gosec // private secret directory.
		return fmt.Errorf("chmod %s: %w", dir, err)
	}

	// Build the buildah `--secret` payload as we materialize.
	// Each line: `id=<lowercased-name>,src=<path>`.
	var formatted strings.Builder

	for _, name := range names {
		// Re-check the single-sourced name shape locally: the same rule
		// config validation enforces, but asserted here where the name
		// becomes a filename — so this verb is safe even when invoked
		// directly with a hand-crafted --names, not only downstream of a
		// validated artifacts.yml (no separators or dots can reach the
		// filepath.Join below).
		if !config.ValidBuildSecretName(name) {
			return fmt.Errorf(
				"build-secret name %q is not a valid env-var name (need [A-Z_][A-Z0-9_]*): %w",
				name, errs.ErrInvalidConfig,
			)
		}

		value, ok := envelope[name]
		if !ok {
			return fmt.Errorf(
				"REUSABLE_CI_BUILD_SECRETS_JSON does not contain key %q "+
					"(declared in containers[].build-secrets); the caller "+
					"workflow's envelope must include every name from the "+
					"artifacts.yml list: %w",
				name, errs.ErrInvalidConfig,
			)
		}

		// Lowercase the secret id so the Containerfile's
		// `--mount=type=secret,id=db_password` matches consistently.
		// Adopters who prefer the UPPER form on the mount line can
		// continue to use it — id is just a string lookup.
		secretID := strings.ToLower(name)
		path := filepath.Join(dir, secretID)

		if err := os.WriteFile(path, []byte(value), 0o600); err != nil { //nolint:gosec // intentional secret file at mode 0600.
			return fmt.Errorf("write %s: %w", path, err)
		}

		_, _ = fmt.Fprintf(&formatted, "id=%s,src=%s\n", secretID, path)
	}

	if err := sink.Set(ctx, "secret-mounts", strings.TrimRight(formatted.String(), "\n")); err != nil {
		return fmt.Errorf("emit secret-mounts: %w", err)
	}

	_, _ = fmt.Fprintf(w, "Materialized %d build-secret(s) to %s\n", len(names), dir)

	return nil
}

// splitNames accepts the comma/space/newline-separated workflow input and
// returns the cleaned list. Empty / whitespace-only entries are dropped.
func splitNames(raw string) []string {
	return listval.Tokens(raw)
}
