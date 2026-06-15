// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/internal/adapters/cosign"
	apprelease "github.com/diggsweden/reusable-ci/internal/app/release"
	"github.com/diggsweden/reusable-ci/internal/cli/cienv"
	"github.com/diggsweden/reusable-ci/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/internal/cliio"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/provenance"
)

// provenanceCmd generates an in-toto/SLSA-v1.0 provenance statement from
// a checksums file. It is the Go port of forgejo-ci's
// scripts/release/slsa-provenance.sh — emitting the JSON only; signing
// (cosign sign-blob / attest-blob) is a separate step, matching that
// script's separation of generate vs sign.
//
// The forge-specific predicate vocabulary comes from the active
// provider's ProvenanceProfile; gitlab/local don't implement it, so the
// command fails with a clear "unsupported on <forge>" rather than
// emitting a misleading predicate.
func provenanceCmd() *cli.Command {
	return &cli.Command{
		Name:  "provenance",
		Usage: "generate an in-toto/SLSA-v1.0 provenance statement from a checksums file (GitHub/Forgejo)",
		Description: `Reads GoReleaser-format checksums and emits a signed-ready in-toto
Statement (SLSA Provenance v1.0). Sign the output with cosign sign-blob.

Context (repository, ref, commit) is read from the active provider; the
workflow, run id, and build timestamp come from flags/env. The build
timestamp defaults to $SOURCE_DATE_EPOCH for reproducibility.`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "checksum-file", Value: cliio.StdSentinel, Sources: cli.EnvVars("CHECKSUM_FILE"), Usage: "GoReleaser checksums file (\"-\" reads stdin)"},
			&cli.StringFlag{Name: "go-sum", Value: "go.sum", Usage: "go.sum for resolved module deps (empty string to skip)"},
			&cli.StringFlag{Name: "workflow", Sources: cienv.Workflow(), Usage: "calling workflow filename, e.g. release.yml"},
			&cli.StringFlag{Name: "run-id", Sources: cienv.RunID(), Usage: "Actions run id"},
			&cli.StringFlag{Name: "started-on", Usage: "RFC3339 build timestamp (default: $SOURCE_DATE_EPOCH)"},
			&cli.StringFlag{Name: "output", Aliases: []string{"o"}, Value: cliio.StdSentinel, Usage: "output file (\"-\" for stdout)"},
			&cli.StringFlag{Name: "sign-key", Usage: "sign the statement with cosign sign-blob using this key ref (e.g. env://COSIGN_KEY); empty = generate only"},
			&cli.StringFlag{Name: "bundle", Usage: "signature bundle output path (default: <output>.bundle)"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
				profiler, err := d.RequireProvenanceProfiler()
				if err != nil {
					return err
				}

				evt, err := d.Provider.ResolveContext(ctx)
				if err != nil {
					return err
				}

				startedOn, err := resolveStartedOn(cmd.String("started-on"))
				if err != nil {
					return err
				}

				checksums, err := cliio.ReadFile(cmd.String("checksum-file"))
				if err != nil {
					return err
				}

				prof := profiler.ProvenanceProfile()

				out, err := apprelease.GenerateProvenance(apprelease.ProvenanceInput{
					Checksums:     bytes.NewReader(checksums),
					GoSum:         openGoSum(cmd.String("go-sum")),
					RepositoryURL: evt.RepoURL,
					Ref:           evt.RefName,
					SHA:           evt.SHA,
					WorkflowFile:  cmd.String("workflow"),
					RunID:         cmd.String("run-id"),
					StartedOn:     startedOn,
					Profile: provenance.Profile{
						BuildType:         prof.BuildType,
						WorkflowDirPrefix: prof.WorkflowDirPrefix,
						RunnerLabel:       prof.RunnerLabel,
					},
				})
				if err != nil {
					return err
				}

				output := cmd.String("output")
				if err := cliio.WriteFile(output, out, 0o644); err != nil { //nolint:gosec // provenance JSON is public attestation material, not a secret.
					return err
				}

				return signProvenance(ctx, output, cmd.String("sign-key"), cmd.String("bundle"))
			})
		},
	}
}

// signProvenance signs the written provenance file with cosign sign-blob
// when keyRef is set, emitting a Sigstore bundle. For an env://VAR key
// the cosign subprocess is isolated to that key var (+ COSIGN_PASSWORD)
// so no forge token, GPG key, or registry credential is visible to it —
// the forgejo-ci secret-family-isolation guarantee. KMS/file/keyless
// keys are not isolated (those backends need their own cloud/OIDC env).
func signProvenance(ctx context.Context, output, keyRef, bundle string) error {
	if keyRef == "" {
		return nil // generate-only
	}

	if output == cliio.StdSentinel {
		return fmt.Errorf("provenance: --output must be a file when --sign-key is set (cosign signs a path): %w", errs.ErrUsage)
	}

	if bundle == "" {
		bundle = output + ".bundle"
	}

	adapter := cosign.New()
	if allow := provenanceSignAllow(keyRef); allow != nil {
		adapter = cosign.NewIsolated(allow...)
	}

	return adapter.SignBlob(ctx, cosign.SignBlobInput{
		Artefact:   output,
		BundlePath: bundle,
		KeyRef:     keyRef,
	}, os.Stderr)
}

// provenanceSignAllow returns the secret env vars cosign may read when
// signing with keyRef, or nil when no isolation applies. For an
// env://VAR key, only that var and COSIGN_PASSWORD are allowed; KMS,
// file, and keyless refs return nil (they need their own credential env,
// so isolating them would break signing).
func provenanceSignAllow(keyRef string) []string {
	v, ok := strings.CutPrefix(keyRef, "env://")
	if !ok || v == "" {
		return nil
	}

	return []string{v, cosignPasswordEnv}
}

// cosignPasswordEnv is the env var cosign reads for the signing-key
// passphrase; always allowed alongside an env:// signing key.
const cosignPasswordEnv = "COSIGN_PASSWORD"

// resolveStartedOn returns the reproducible build timestamp as RFC3339
// UTC. Precedence: an explicit --started-on (validated as RFC3339), then
// $SOURCE_DATE_EPOCH (unix seconds, the repo-wide reproducibility
// convention). One of the two is required — a provenance without an
// honest timestamp is refused.
func resolveStartedOn(flagVal string) (string, error) {
	if flagVal != "" {
		if _, err := time.Parse(time.RFC3339, flagVal); err != nil {
			return "", fmt.Errorf("provenance: --started-on %q is not RFC3339: %w", flagVal, errs.ErrUsage)
		}

		return flagVal, nil
	}

	epoch := os.Getenv("SOURCE_DATE_EPOCH")
	if epoch == "" {
		return "", fmt.Errorf("provenance: no build timestamp — pass --started-on <RFC3339> or set $SOURCE_DATE_EPOCH: %w", errs.ErrUsage)
	}

	secs, err := strconv.ParseInt(epoch, 10, 64)
	if err != nil {
		return "", fmt.Errorf("provenance: SOURCE_DATE_EPOCH %q is not an integer: %w", epoch, errs.ErrUsage)
	}

	return time.Unix(secs, 0).UTC().Format(time.RFC3339), nil
}

// openGoSum returns a reader over the go.sum at path, or nil when the
// path is empty or the file is absent (module deps are best-effort —
// their absence is not fatal).
func openGoSum(path string) io.Reader {
	if path == "" {
		return nil
	}

	data, err := os.ReadFile(path) //nolint:gosec // go.sum path is an operator-supplied build input, not attacker-controlled.
	if err != nil {
		return nil
	}

	return bytes.NewReader(data)
}
