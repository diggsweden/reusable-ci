// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
//
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package livetest

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"time"
)

// Signing scenarios drive the real cosign binary, and cosign publishes every
// signature to the PUBLIC Rekor transparency log by default. A Rekor entry is
// permanent, public and append-only: a test run cannot take one back, and what
// it would publish is a lab image digest against a throwaway key, in a record
// that outlives both.
//
// So containment is not a scenario's responsibility here. $REUSABLE_CI_COSIGN_
// TRANSPARENCY=none is set for EVERY invocation in cliEnv, the same way the
// black-box suite sets it suite-wide rather than per test — the property being
// bought is that no test can publish, including tests nobody has written yet.
// A scenario that forgot would otherwise publish once, permanently, and pass.
//
// The key is generated here rather than supplied by the lab. It is not a forge
// resource: it lives in the test's own temp directory and dies with it, which is
// a better lifecycle than anything revocable — there is no window in which a
// crashed run leaves key material behind to be cleaned up later.

// CosignKey generates a throwaway signing key pair in dir and returns the
// private and public key paths.
//
// The password is empty on purpose. An encrypted key would exercise one extra
// cosign code path while adding a secret to marshal through the closed
// environment, and the key is worth nothing: it exists for the length of one
// test and signs images in a disposable lab.
func CosignKey(tb TB, dir string) (string, string) {
	tb.Helper()

	if _, err := exec.LookPath("cosign"); err != nil {
		tb.Fatalf("livetest: cosign is required for signing scenarios and is not on PATH: %v", err)
	}

	// Bounded: key generation is local work, so anything slower than this is a
	// hang rather than a slow machine.
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()

	cmd := exec.CommandContext(ctx, "cosign", "generate-key-pair", "--output-key-prefix", "lab-cosign")
	cmd.Dir = dir
	// Named lab-cosign rather than cosign so the files cannot be mistaken for a
	// production key by a person, a glob, or a tool assuming the conventional
	// name carries conventional trust.
	cmd.Env = append(os.Environ(), "COSIGN_PASSWORD=")

	if out, err := cmd.CombinedOutput(); err != nil {
		tb.Fatalf("livetest: cosign generate-key-pair: %v\n%s", err, out)
	}

	privateKey := filepath.Join(dir, "lab-cosign.key")
	publicKey := filepath.Join(dir, "lab-cosign.pub")

	for _, path := range []string{privateKey, publicKey} {
		info, err := os.Stat(path)
		if err != nil || info.Size() == 0 {
			tb.Fatalf("livetest: cosign generate-key-pair produced no %s", path)
		}
	}

	return privateKey, publicKey
}

// CosignEnv is what a signing invocation needs beyond the closed environment:
// the (empty) key password, and registry credentials for the signature push.
//
// cosign attaches an image signature by pushing it to the same repository, so a
// signing run needs registry auth as much as the promote path does. It reads a
// Docker config directory from $DOCKER_CONFIG, so authDir is the directory
// holding the config.json written by RegistryAuthConfigDir.
//
// Transparency is deliberately absent from this map: it is set for every
// invocation in cliEnv, so it cannot be dropped by a caller assembling this one.
func CosignEnv(authDir string) map[string]string {
	return map[string]string{
		"COSIGN_PASSWORD": "",
		"DOCKER_CONFIG":   authDir,
	}
}
