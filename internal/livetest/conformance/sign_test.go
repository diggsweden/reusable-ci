// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
//
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

//go:build live

package conformance_test

// PAR-SIGN-1: `container ledger sign` against a real registry.
//
// Signing is the last Tier A verb family with no coverage, and the one whose
// unit tests prove the least: what `ledger sign` does is resolve each ledger
// entry to a digest, shell out to cosign, and leave a signature in the registry.
// A fake can confirm the argv; only a registry can confirm a verifier can then
// find and check the signature.
//
// Containment is not set up here. $REUSABLE_CI_COSIGN_TRANSPARENCY=none is in
// the closed environment every invocation gets (see livetest/cli.go), because
// the failure mode — publishing a lab signature to the permanent, append-only
// public Rekor log — is one a scenario must not be able to cause by forgetting.
// This scenario asserts the containment held, rather than assuming it.

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/livetest"
)

func TestSign_LedgerImages_ProducesAVerifiableSignature(t *testing.T) {
	const (
		releaseTag   = "v0.0.1"
		candidateTag = "staging-" + releaseTag
	)

	for _, kind := range forgesClaiming(t, alwaysValidatesTokens, "an OCI registry") {
		t.Run(string(kind), func(t *testing.T) {
			target := livetest.Accept(t, kind)

			registry, err := livetest.RegistryHost(target)
			if err != nil {
				t.Fatal(err)
			}

			repo := livetest.NewScratchRepo(t, target, "ledger-sign")
			imagePath := registry + "/" + target.Owner + "/" + repo

			work := t.TempDir()
			authFile := livetest.RegistryAuthFile(t, target, work)
			authDir := livetest.RegistryAuthConfigDir(t, target, work)
			privateKey, publicKey := livetest.CosignKey(t, work)

			opts := livetest.RunOptions{Dir: work, Env: livetest.CosignEnv(authDir)}

			pushed := livetest.PushImageTags(t, target, repo, candidateTag, releaseTag)

			ledger := "release-images.json"

			add := livetest.CLIIn(t, target, repo, opts,
				"container", "ledger", "add",
				"--ledger", ledger, "--auth-file", authFile, "--tag", releaseTag,
				"--kind", "distroless",
				"--candidate-tag", imagePath+":"+candidateTag,
				"--final-tag", imagePath+":"+releaseTag,
				"--sbom", writeSBOM(t, work, "sign"),
				"--capture-digest",
			)
			if add.ExitCode != 0 {
				t.Fatalf("%s ledger add exited %d\nstderr: %s", kind, add.ExitCode, add.Stderr)
			}

			sign := livetest.CLIIn(t, target, repo, opts,
				"container", "ledger", "sign",
				"--ledger", ledger, "--auth-file", authFile, "--tag", releaseTag,
				"--method", "kms", "--key", privateKey,
				"--provenance-predicate", writePredicate(t, work),
			)
			if sign.ExitCode != 0 {
				t.Fatalf("%s ledger sign exited %d\nstderr: %s", kind, sign.ExitCode, sign.Stderr)
			}

			// The claim, checked with cosign rather than with the tool's own
			// report: a verifier holding only the public key accepts the image
			// the ledger recorded.
			verifyCosignSignature(t, string(kind), imagePath+"@"+pushed.Digest, publicKey, authDir)

			// And that the containment held. Publishing to Rekor is
			// irreversible, so this is asserted every run rather than trusted
			// to the environment variable that implements it.
			if livetest.SignaturePublishedToTransparencyLog(t, target, repo, pushed.Digest) {
				t.Errorf("%s: the signature carries a transparency-log entry — it reached the public Rekor log, "+
					"which is append-only and cannot be withdrawn", kind)
			}
		})
	}
}

// writePredicate supplies the SLSA predicate `ledger sign` enriches per image.
// Its contents are not what this scenario is about — the signature is — so it is
// the smallest in-toto statement the enrichment accepts.
func writePredicate(t *testing.T, dir string) string {
	t.Helper()

	name := "slsa-provenance.predicate.json"
	body := `{"buildDefinition":{"buildType":"https://example.invalid/livetest","externalParameters":{}},` +
		`"runDetails":{"builder":{"id":"https://example.invalid/livetest"}}}`

	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	return name
}

// verifyCosignSignature runs the real cosign against the registry, which is the
// only check that means anything here: it proves the signature is discoverable
// and valid to a third party, not merely that the product reported success.
//
// --insecure-ignore-tlog is required BECAUSE the suite signs with the
// transparency log off. Its presence is the assertion's companion, not a
// weakening of it: a signature that needed no such flag would be one that had
// been published to the public log, which is the outcome the containment exists
// to prevent.
func verifyCosignSignature(t *testing.T, kind, digestRef, publicKey, authDir string) {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "cosign", "verify",
		"--key", publicKey,
		"--insecure-ignore-tlog=true",
		digestRef,
	)
	cmd.Env = append(os.Environ(), "DOCKER_CONFIG="+authDir, "COSIGN_PASSWORD=")

	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s: cosign could not verify the signature reusable-ci wrote for %s: %v\n%s",
			kind, digestRef, err, out)
	}

}
