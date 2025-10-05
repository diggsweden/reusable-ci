// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

// Both entry points here take a secret and then talk about it: they print what
// they did, name the file they wrote, and wrap failures from the filesystem
// underneath. Every one of those lines reaches a CI log.
//
// The existing tests use short fixtures — "s3cret", "alice" — and assert what
// was written where. None asserts what was NOT written, and a short fixture
// could not carry that assertion anyway: "s3cret" is too small to distinguish a
// leak from a coincidence, and the registry and username are supposed to appear.
//
// The canaries below are long and unmistakable, and the assertions exclude the
// files these functions exist to write: the auth file legitimately holds the
// credential, and a materialised secret file legitimately holds the secret.
// What must never carry them is stdout, the returned error, or the CI outputs.
const (
	canaryPassword = "CANARY-REGISTRY-PASSWORD-4f2a9c7e1b83"
	//nolint:gosec // G101: a deliberately recognisable canary, not a credential.
	canarySecret = "CANARY-BUILD-SECRET-VALUE-6d1e08b4af95"
	canarySecond = "CANARY-BUILD-SECRET-OTHER-3a7c62fe10db"
)

func assertNoCanary(t *testing.T, surfaces map[string]string, canaries ...string) {
	t.Helper()

	for name, body := range surfaces {
		for _, canary := range canaries {
			if strings.Contains(body, canary) {
				t.Errorf("%s leaked a credential (%s):\n%s", name, canary, body)
			}
		}
	}
}

func TestRegistryLogin_NeverEchoesThePassword(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		authDir func(t *testing.T) string
		wantErr bool
	}{
		{
			name: "a successful login",
			authDir: func(t *testing.T) string {
				t.Helper()

				return filepath.Join(t.TempDir(), "config.json")
			},
		},
		{
			// The failure path is the one most likely to quote its input:
			// the auth path is a directory, so the write fails underneath.
			name: "a failing write",
			authDir: func(t *testing.T) string {
				t.Helper()

				dir := filepath.Join(t.TempDir(), "config.json")
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}

				return dir
			},
			wantErr: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var out strings.Builder

			path := tc.authDir(t)

			err := appcontainer.RegistryLogin(&out, appcontainer.RegistryLoginInput{
				Registry: "ghcr.io",
				Username: "alice",
				Password: canaryPassword,
				AuthFile: path,
			})
			if gotErr := err != nil; gotErr != tc.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}

			surfaces := map[string]string{"stdout": out.String()}
			if err != nil {
				surfaces["error"] = err.Error()
			}

			assertNoCanary(t, surfaces, canaryPassword)

			// The registry and username are operator configuration and are
			// meant to be visible; a redaction that hid them would be
			// reverted the first time someone debugged a failed login.
			if !tc.wantErr && !strings.Contains(out.String(), "ghcr.io") {
				t.Errorf("stdout does not name the registry: %q", out.String())
			}
		})
	}
}

func TestMaterializeBuildSecrets_NeverEchoesTheSecretValues(t *testing.T) {
	t.Parallel()

	envelope := `{"TOKEN_A":"` + canarySecret + `","TOKEN_B":"` + canarySecond + `"}`

	for _, tc := range []struct {
		name     string
		names    string
		envelope string
		wantErr  bool
	}{
		{name: "both secrets materialised", names: "TOKEN_A,TOKEN_B", envelope: envelope},
		{
			// A declared name missing from the envelope: the diagnostic
			// names the key, and must not dump the envelope that holds the
			// other secrets.
			name:  "a declared name is absent from the envelope",
			names: "TOKEN_A,MISSING", envelope: envelope, wantErr: true,
		},
		{
			// A malformed envelope is the worst case: the parse error comes
			// from encoding/json, which has the raw bytes in hand.
			name:  "the envelope is malformed",
			names: "TOKEN_A", envelope: `{"TOKEN_A":"` + canarySecret + `"`, wantErr: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var out strings.Builder

			sink := fakeoutputsink.New(t)
			outputDir := t.TempDir()

			err := appcontainer.MaterializeBuildSecrets(context.Background(), sink, &out,
				appcontainer.MaterializeBuildSecretsInput{
					Names:        tc.names,
					EnvelopeJSON: tc.envelope,
					OutputDir:    outputDir,
				})
			if gotErr := err != nil; gotErr != tc.wantErr {
				t.Fatalf("err = %v, wantErr = %v", err, tc.wantErr)
			}

			surfaces := map[string]string{"stdout": out.String()}
			if err != nil {
				surfaces["error"] = err.Error()
			}

			// Every CI output, including the mount specification, which
			// names paths rather than values.
			for key, value := range sink.AllScalar() {
				surfaces["output "+key] = value
			}

			assertNoCanary(t, surfaces, canarySecret, canarySecond)

			// The names are operator configuration and stay visible, so a
			// blanket redaction cannot satisfy the assertions above.
			// The mount ids are the declared names, lowercased.
			if !tc.wantErr && !strings.Contains(sink.Single("secret-mounts"), "id=token_a") {
				t.Errorf("the mounts do not name the declared secret: %q", sink.Single("secret-mounts"))
			}
		})
	}
}
