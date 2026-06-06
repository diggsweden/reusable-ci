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

	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
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
	// gets mode 0600. Defaults to $RUNNER_TEMP/buildkit-secrets when
	// empty.
	OutputDir string
}

// MaterializeBuildSecrets unpacks the JSON envelope into per-secret
// tmpfiles on disk and emits the `buildx-secrets` output that
// docker/build-push-action consumes as its `secrets:` input.
//
// The contract is deliberately narrow:
//   - Every name declared in `Names` MUST have a value in the envelope.
//     A mismatch fails fast with a clear error pointing at the missing
//     key — the alternative (silently producing an empty mount) would
//     surface as an inscrutable build failure deep inside BuildKit.
//   - The envelope MAY contain additional keys (the caller could pack
//     more than the artefact declares). Extra keys are ignored.
//   - Each value is written to <OutputDir>/<lowercased-name> with mode
//     0600. BuildKit mounts via `src=` so the file's contents become
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
		// No build-secrets declared — emit empty output so the workflow
		// `secrets:` input is unset, and skip envelope handling entirely.
		// docker/build-push-action accepts an empty `secrets:` value as
		// "no mounts," which is the desired no-op.
		if err := sink.Set(ctx, "buildx-secrets", ""); err != nil {
			return fmt.Errorf("emit buildx-secrets: %w", err)
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

	dir := strings.TrimSpace(in.OutputDir)
	if dir == "" {
		if rt := strings.TrimSpace(os.Getenv("RUNNER_TEMP")); rt != "" {
			dir = filepath.Join(rt, "buildkit-secrets")
		} else {
			dir = filepath.Join(os.TempDir(), "buildkit-secrets")
		}
	}

	if err := os.MkdirAll(dir, 0o700); err != nil { //nolint:gosec // private secret directory.
		return fmt.Errorf("mkdir %s: %w", dir, err)
	}

	// Build the docker/build-push-action `secrets:` payload as we
	// materialize. Each line: `id=<lowercased-name>,src=<path>`.
	var formatted strings.Builder

	for _, name := range names {
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

		// Lowercase the BuildKit id so the Containerfile's
		// `--mount=type=secret,id=db_password` matches consistently.
		// Adopters who prefer the UPPER form on the mount line can
		// continue to use it — id is just a string lookup.
		bkID := strings.ToLower(name)
		path := filepath.Join(dir, bkID)

		if err := os.WriteFile(path, []byte(value), 0o600); err != nil { //nolint:gosec // intentional secret file at mode 0600.
			return fmt.Errorf("write %s: %w", path, err)
		}

		_, _ = fmt.Fprintf(&formatted, "id=%s,src=%s\n", bkID, path)
	}

	if err := sink.Set(ctx, "buildx-secrets", strings.TrimRight(formatted.String(), "\n")); err != nil {
		return fmt.Errorf("emit buildx-secrets: %w", err)
	}

	_, _ = fmt.Fprintf(w, "Materialized %d build-secret(s) to %s\n", len(names), dir)

	return nil
}

// splitNames accepts the newline-or-comma-separated workflow input and
// returns the cleaned list. Empty / whitespace-only entries are dropped.
func splitNames(raw string) []string {
	var out []string

	for _, line := range strings.FieldsFunc(raw, func(r rune) bool {
		return r == '\n' || r == ',' || r == ' ' || r == '\t' || r == '\r'
	}) {
		name := strings.TrimSpace(line)
		if name != "" {
			out = append(out, name)
		}
	}

	return out
}
