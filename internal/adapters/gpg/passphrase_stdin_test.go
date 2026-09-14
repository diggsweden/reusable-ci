// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package gpg_test

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	adaptergpg "github.com/diggsweden/reusable-ci/v3/internal/adapters/gpg"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/mockbinary"
	"github.com/stretchr/testify/require"
)

// ImportKey's "stdin, never argv" claim has a test. DetachedSign and
// PresetPassphrase make the same claim about the passphrase -- "provided
// on stdin, never argv or environment", "fed through stdin so it never
// appears in ps" -- and had none. These are those tests.
//
// No t.Parallel() anywhere here: mockbinary prepends to PATH via
// t.Setenv.

const testPassphrase = "correct horse battery staple"

// envDumpingStub is a stub body that records its own environment next to
// the mockbinary recording, so a test can assert what the subprocess
// could see. $1-style expansion is avoided: the path is baked in.
func envDumpingStub(dumpPath string) string {
	return "cat > /dev/null\nenv > " + dumpPath + "\n"
}

func TestDetachedSign_PassphraseArrivesOnStdinNotArgv(t *testing.T) {
	bins := mockbinary.New(t)
	bins.Add("gpg", `cat > /dev/null`)

	adapter := &adaptergpg.Adapter{GPGBin: bins.Path("gpg")}

	dir := t.TempDir()
	input := filepath.Join(dir, "artifact.tar.gz")

	if err := os.WriteFile(input, []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}

	err := adapter.DetachedSign(context.Background(), "ABCDEF0123456789ABCDEF0123456789ABCDEF01",
		testPassphrase, input, filepath.Join(dir, "artifact.tar.gz.sig"))
	if err != nil {
		t.Fatalf("DetachedSign: %v", err)
	}

	invs := bins.Invocations("gpg")
	if len(invs) != 1 {
		t.Fatalf("gpg invocations = %d, want 1", len(invs))
	}

	inv := invs[0]

	for _, arg := range inv.Args {
		if strings.Contains(arg, testPassphrase) {
			t.Fatalf("the passphrase is in argv, where ps can read it: %v", inv.Args)
		}
	}

	if strings.TrimRight(inv.Stdin, "\n") != testPassphrase {
		t.Errorf("stdin = %q, want the passphrase", inv.Stdin)
	}

	// These three are what make stdin the passphrase channel. Without
	// loopback pinentry gpg ignores --passphrase-fd and prompts; without
	// --passphrase-fd 0 it does not read the pipe at all. A test that
	// only checked stdin would pass while gpg hung or prompted.
	if !slices.Contains(inv.Args, "--pinentry-mode") || !slices.Contains(inv.Args, "loopback") {
		t.Errorf("missing loopback pinentry, so gpg would not read the pipe: %v", inv.Args)
	}

	if i := slices.Index(inv.Args, "--passphrase-fd"); i < 0 || i+1 >= len(inv.Args) || inv.Args[i+1] != "0" {
		t.Errorf("--passphrase-fd is not 0, so the passphrase on stdin is not read: %v", inv.Args)
	}

	// The signing key is selected explicitly rather than left to gpg's
	// default secret key, which on a runner with more than one imported
	// key would sign as whichever came first.
	if i := slices.Index(inv.Args, "--local-user"); i < 0 || i+1 >= len(inv.Args) ||
		inv.Args[i+1] != "ABCDEF0123456789ABCDEF0123456789ABCDEF01" {
		t.Errorf("--local-user does not pin the signing fingerprint: %v", inv.Args)
	}
}

// TestDetachedSign_PassphraseIsNotInTheEnvironment covers the other half
// of the claim. argv is visible through ps; the environment is visible
// through /proc/<pid>/environ and is inherited by anything gpg spawns.
func TestDetachedSign_PassphraseIsNotInTheEnvironment(t *testing.T) {
	dump := filepath.Join(t.TempDir(), "env.txt")

	bins := mockbinary.New(t)
	bins.Add("gpg", envDumpingStub(dump))

	adapter := &adaptergpg.Adapter{GPGBin: bins.Path("gpg")}

	dir := t.TempDir()
	input := filepath.Join(dir, "artifact.tar.gz")

	if err := os.WriteFile(input, []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := adapter.DetachedSign(context.Background(), "ABCDEF01", testPassphrase, input,
		filepath.Join(dir, "sig")); err != nil {
		t.Fatalf("DetachedSign: %v", err)
	}

	body, err := os.ReadFile(dump)
	if err != nil {
		t.Fatalf("the stub recorded no environment: %v", err)
	}

	if strings.Contains(string(body), testPassphrase) {
		t.Error("the passphrase is in the subprocess environment")
	}
}

// Hex is reversible secret material, not redaction. Both it and the keygrip
// belong on stdin, with exactly one command terminator and only /bye in argv.
func TestPresetPassphrase_StdinAndEnvironmentBoundary(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("the owned test-executable symlink recorder is scoped to Linux and Darwin")
	}

	executable, err := os.Executable()
	require.NoError(t, err)

	const (
		keygrip   = "1111222233334444555566667777888899990000"
		secretHex = "636F727265637420686F727365206261747465727920737461706C65"
		wantStdin = "PRESET_PASSPHRASE 1111222233334444555566667777888899990000 -1 636F727265637420686F727365206261747465727920737461706C65\n"
	)

	for _, tc := range []struct {
		name     string
		new      func() *adaptergpg.Adapter
		isolated bool
	}{
		{name: "New", new: adaptergpg.New},
		{name: "NewIsolated", new: adaptergpg.NewIsolated, isolated: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			for _, name := range []string{"empty-path", "home", "tmp", "gnupg"} {
				require.NoError(t, os.Mkdir(filepath.Join(dir, name), 0o700))
			}

			require.NoError(t, os.WriteFile(filepath.Join(dir, "tty"), nil, 0o600))

			// No ambient credentials may reach the recorder, even through New.
			// Setenv registers restoration, including originally unset values.
			for _, entry := range os.Environ() {
				key, _, _ := strings.Cut(entry, "=")
				t.Setenv(key, "")
				require.NoError(t, os.Unsetenv(key))
			}

			runtimeEnv := []string{
				"PATH=" + filepath.Join(dir, "empty-path"),
				"HOME=" + filepath.Join(dir, "home"),
				"TMPDIR=" + filepath.Join(dir, "tmp"),
				"GNUPGHOME=" + filepath.Join(dir, "gnupg"),
				"GPG_TTY=" + filepath.Join(dir, "tty"),
				"LANG=C.UTF-8", "LC_ALL=C", "LC_CTYPE=C.UTF-8", "LC_MESSAGES=C",
			}
			// New intentionally inherits preexisting secrets. Its promise here is
			// not to newly export the supplied argument, not to scrub the parent.
			parentEnv := slices.Concat(runtimeEnv, []string{
				"GPG_PRIVATE_KEY=owned-canonical-key",
				"GPG_SIGNING_KEY=owned-legacy-key",
				"GPG_PASSPHRASE=owned-preexisting-passphrase",
				"GPG_SIGNING_PASSWORD=owned-preexisting-password",
				"P018_UNRELATED=owned-unrelated-value",
			})
			if tc.isolated {
				parentEnv = append(parentEnv, "P018_ARBITRARY_HEX="+secretHex)
			}

			for _, entry := range parentEnv {
				key, value, _ := strings.Cut(entry, "=")
				t.Setenv(key, value)
			}

			wantEnv := slices.Clone(parentEnv)

			if tc.isolated {
				t.Setenv("GPG_PASSPHRASE", testPassphrase)
				t.Setenv("GPG_SIGNING_PASSWORD", testPassphrase)

				wantEnv = slices.Clone(runtimeEnv)
			}

			adapter := tc.new()
			if tc.isolated {
				// NewIsolated captures runtime values at construction, not at Run.
				t.Setenv("LANG", "after-construction")
			}

			adapter.AgentBin = filepath.Join(dir, presetRecorderName)
			require.NoError(t, os.Symlink(executable, adapter.AgentBin))

			ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
			defer cancel()

			require.NoError(t, adapter.PresetPassphrase(ctx, keygrip, testPassphrase), "PresetPassphrase")

			body, err := os.ReadFile(filepath.Join(dir, presetRecordName))
			require.NoError(t, err, "agent did not record one invocation")

			var inv presetInvocation
			require.NoError(t, json.Unmarshal(body, &inv), "decode agent invocation")
			require.Equal(t, wantStdin, string(inv.Stdin), "agent stdin")

			for _, secret := range []string{testPassphrase, secretHex} {
				require.NotContains(t, strings.Join(inv.Args, "\x00"), secret,
					"supplied passphrase (plain or hex) reached argv")
				require.NotContains(t, strings.Join(inv.Env, "\x00"), secret,
					"supplied passphrase (plain or hex) reached environment")
			}

			require.Equal(t, []string{"/bye"}, inv.Args, "agent argv")

			slices.Sort(inv.Env)
			slices.Sort(wantEnv)
			require.Equal(t, wantEnv, inv.Env, "agent environment")
		})
	}
}

// TestNewIsolated_SubprocessDoesNotSeeSigningSecrets covers what
// NewIsolated is for. The reusable-ci process may have read a private
// key and a passphrase out of its own environment before it ever calls
// gpg; those variables must not be inherited by the subprocess.
//
// The keep-set is an allowlist, so this also asserts the two variables
// gpg genuinely needs survive it -- an isolation that dropped GNUPGHOME
// would send gpg to the wrong keyring, and one that dropped PATH would
// break the subprocess outright.
func TestNewIsolated_SubprocessDoesNotSeeSigningSecrets(t *testing.T) {
	gnupgHome := t.TempDir()
	t.Setenv("GNUPGHOME", gnupgHome)
	t.Setenv("GPG_SIGNING_KEY", "-----BEGIN PGP PRIVATE KEY BLOCK-----\nsecret\n")
	t.Setenv("GPG_SIGNING_PASSWORD", testPassphrase)
	t.Setenv("COSIGN_PASSWORD", "another-secret")
	t.Setenv("REGISTRY_TOKEN", "a-registry-token")

	dump := filepath.Join(t.TempDir(), "env.txt")

	bins := mockbinary.New(t)
	bins.Add("gpg", envDumpingStub(dump))

	adapter := adaptergpg.NewIsolated()
	adapter.GPGBin = bins.Path("gpg")

	if _, err := adapter.ListSecretKeys(context.Background()); err != nil {
		t.Fatalf("ListSecretKeys: %v", err)
	}

	body, err := os.ReadFile(dump)
	if err != nil {
		t.Fatalf("the stub recorded no environment: %v", err)
	}

	env := string(body)

	for _, secret := range []string{
		"GPG_SIGNING_KEY", "GPG_SIGNING_PASSWORD", "COSIGN_PASSWORD", "REGISTRY_TOKEN",
	} {
		if strings.Contains(env, secret+"=") {
			t.Errorf("%s reached the gpg subprocess", secret)
		}
	}

	// Values as well as names: a variable renamed but still exported
	// would pass the check above.
	if strings.Contains(env, testPassphrase) || strings.Contains(env, "a-registry-token") {
		t.Error("a secret value reached the gpg subprocess under some other name")
	}

	if !strings.Contains(env, "GNUPGHOME="+gnupgHome) {
		t.Errorf("GNUPGHOME did not survive isolation, so gpg would use the wrong keyring:\n%s", env)
	}

	if !strings.Contains(env, "PATH=") {
		t.Error("PATH did not survive isolation")
	}
}
