// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
//
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

//go:build live

package conformance_test

// PAR-REG-2: the image ledger against a real registry.
//
// The ledger's whole promise is that a digest recorded at build time is the
// digest served after promotion — that what a release publishes is what was
// signed and scanned, not something that drifted in between. Capture, verify
// and promote each read or write a real registry, so a fake one can only prove
// the code calls itself correctly.
//
// The digest is the thread: captured from the registry at add, re-checked at
// validate-digests, and required to survive the tag copy at promote. A
// promotion that lands a different manifest is the failure this exists to
// catch, and it is most plausible for an index, which is why both shapes run.
//
// What promotion actually does is worth stating, because it is easy to assume
// otherwise: it copies the candidate to the stage's *moving pointer*
// (<base>:release), on the same digest. The immutable :<version> tag is applied
// once at build and is never rewritten here.

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/livetest"
)

func TestLedger_RecordVerifyPromote_PreservesTheDigest(t *testing.T) {
	const (
		releaseTag   = "v0.0.1"
		candidateTag = "staging-" + releaseTag
		promotedTag  = "release"
	)

	for _, kind := range forgesClaiming(t, alwaysValidatesTokens, "an OCI registry") {
		t.Run(string(kind), func(t *testing.T) {
			target := livetest.Accept(t, kind)

			registry, err := livetest.RegistryHost(target)
			if err != nil {
				t.Fatal(err)
			}

			for _, shape := range []struct {
				name string
				push func(livetest.TB, livetest.Target, string, string) livetest.Image
			}{
				{name: "image", push: livetest.PushImage},
				{name: "index", push: livetest.PushIndex},
			} {
				t.Run(shape.name, func(t *testing.T) {
					// A scratch repository per shape: promotion targets one
					// moving pointer per repository, so sharing a repository
					// would have the second shape overwrite the first's
					// :release and make the assertion depend on ordering.
					repo := livetest.NewScratchRepo(t, target, "ledger-"+shape.name)
					imagePath := registry + "/" + target.Owner + "/" + repo

					work := t.TempDir()
					authFile := livetest.RegistryAuthFile(t, target, work)

					// The ledger records paths a consumer resolves later, so it
					// requires them relative — which only means something
					// against a working directory. Running from the release
					// tree is what a release job does too.
					//
					// Credentials go in as an explicit --auth-file rather than
					// an ambient keychain: the closed environment the CLI runs
					// in has no keychain by design, and a test that leaned on
					// the operator's ~/.docker/config.json would pass or fail
					// based on who ran it.
					opts := livetest.RunOptions{Dir: work}
					pushed := shape.push(t, target, repo, candidateTag)

					ledger := "release-images.json"
					sbom := writeSBOM(t, work, shape.name)

					// add --capture-digest reads the digest from the registry
					// rather than trusting a value passed in, which is the
					// property that removes the transcription gap between push
					// and record.
					add := livetest.CLIIn(t, target, repo, opts,
						"container", "ledger", "add",
						"--ledger", ledger,
						"--auth-file", authFile,
						"--tag", releaseTag,
						"--kind", "distroless",
						"--candidate-tag", imagePath+":"+candidateTag,
						"--final-tag", imagePath+":"+releaseTag,
						"--sbom", sbom,
						"--capture-digest",
					)
					if add.ExitCode != 0 {
						t.Fatalf("%s ledger add exited %d\nstderr: %s", kind, add.ExitCode, add.Stderr)
					}

					recorded := ledgerDigest(t, filepath.Join(work, ledger))
					if recorded != pushed.Digest {
						t.Fatalf("%s recorded %s but the registry serves %s — the capture is not reading the registry",
							kind, recorded, pushed.Digest)
					}

					// validate is the offline trust-boundary check: every tag
					// scoped to the release tag.
					validate := livetest.CLIIn(t, target, repo, opts,
						"container", "ledger", "validate",
						"--ledger", ledger, "--tag", releaseTag, "--non-empty")
					if validate.ExitCode != 0 {
						t.Fatalf("%s ledger validate exited %d\nstderr: %s", kind, validate.ExitCode, validate.Stderr)
					}

					// validate-digests is the registry-facing half: what the
					// forge serves must still be the recorded digest.
					verify := livetest.CLIIn(t, target, repo, opts,
						"container", "ledger", "validate-digests",
						"--ledger", ledger, "--auth-file", authFile, "--tag", releaseTag)
					if verify.ExitCode != 0 {
						t.Fatalf("%s ledger validate-digests exited %d against the registry it just recorded\nstderr: %s",
							kind, verify.ExitCode, verify.Stderr)
					}

					promote := livetest.CLIIn(t, target, repo, opts,
						"container", "ledger", "promote",
						"--ledger", ledger, "--auth-file", authFile,
						"--tag", releaseTag, "--stage", promotedTag)
					if promote.ExitCode != 0 {
						t.Fatalf("%s ledger promote exited %d\nstderr: %s", kind, promote.ExitCode, promote.Stderr)
					}

					// The claim, checked against the registry rather than the
					// tool's own report: the moving pointer now serves exactly
					// the manifest the candidate did.
					served, found := livetest.ImageDigest(t, target, repo, promotedTag)
					if !found {
						t.Fatalf("%s: promotion reported success but %s:%s serves nothing", kind, imagePath, promotedTag)
					}

					if served != pushed.Digest {
						t.Errorf("%s: %s:%s serves %s, want the promoted %s — the copy did not preserve the manifest",
							kind, imagePath, promotedTag, served, pushed.Digest)
					}
				})
			}
		})
	}
}

// writeSBOM produces a minimal CycloneDX document at a path the ledger's schema
// accepts (it requires a *.cyclonedx.json name). Content is not inspected by the
// ledger — the entry records the path so a later step can find it.
func writeSBOM(t *testing.T, dir, flavour string) string {
	t.Helper()

	name := "image-sbom-" + flavour + ".cyclonedx.json"
	body := `{"bomFormat":"CycloneDX","specVersion":"1.5","version":1,"components":[]}`

	if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	// Relative on purpose: the ledger refuses an absolute path, because the
	// entry is read later from wherever the release artifacts are.
	return name
}

// ledgerDigest reads back the digest the ledger recorded, so the assertion is
// against the file the product wrote rather than against its stdout.
func ledgerDigest(t *testing.T, path string) string {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read ledger: %v", err)
	}

	var entries []struct {
		Digest string `json:"digest"`
	}

	if err := json.Unmarshal(content, &entries); err != nil {
		// The ledger may be an object with an entries array depending on
		// schema version; report the shape rather than guessing wrong.
		t.Fatalf("decode ledger %s: %v\ncontent: %s", path, err, strings.TrimSpace(string(content)))
	}

	if len(entries) != 1 {
		t.Fatalf("ledger holds %d entries, want exactly 1", len(entries))
	}

	return entries[0].Digest
}
