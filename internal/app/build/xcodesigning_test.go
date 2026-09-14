// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/stretchr/testify/require"
)

var errKeychainInUse = errors.New("security: keychain in use")

type fakeSecurity struct {
	calls [][]string
	err   error
}

func (f *fakeSecurity) Run(_ context.Context, args ...string) (string, error) {
	f.calls = append(f.calls, args)
	if f.err != nil {
		return "", f.err
	}

	return "", nil
}

func TestXcodeSetupCodeSigning_HappyPath(t *testing.T) {
	for _, name := range []string{"explicit_paths", "owned_defaults", "empty_cert_passphrase"} {
		t.Run(name, func(t *testing.T) {
			in := xcodeSecurityFixture(t)

			tmp, profiles := in.TempDir, in.ProvisioningProfilesDir
			if name == "owned_defaults" {
				root := filepath.Dir(in.TempDir)
				tmp = filepath.Join(root, "TMPDIR")
				profiles = filepath.Join(root, "HOME", "Library", "MobileDevice", "Provisioning Profiles")
				in.TempDir, in.ProvisioningProfilesDir = "", ""
			}

			if name == "empty_cert_passphrase" {
				in.CertPassphrase = ""
			}

			// The run mints the keychain password, so the expected argv exists
			// only once the first call has been recorded.
			var want [][]string

			expect := func(calls [][]string) [][]string {
				if want == nil {
					want = xcodeSecurityVectors(in, tmp, observedKeychainPassword(t, calls))
				}

				return want
			}

			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()

			sec := &recordingXcodeSecurity{}
			out := &xcodeSecurityWriter{}
			sec.run = func(index int) (string, error) {
				want := expect(sec.calls)
				require.Less(t, index, len(want), "unexpected security command")
				require.Equal(t, want[:index+1], sec.calls, "complete ordered security argv")
				require.Same(t, ctx, sec.contexts[index], "security context")
				require.Empty(t, out.calls, "no success write before security finishes")
				assertXcodeSecret(t, filepath.Join(tmp, "certificate.p12"), xcodeContractCertificate)
				assertXcodeSecret(t, filepath.Join(tmp, "pp.mobileprovision"), xcodeContractProfile)
				requireEmptyXcodeDir(t, profiles, "installation follows every security command")

				return xcodeSecurityCanaries(in) + "\nport output at " + want[index][0], nil
			}
			out.observe = func() {
				require.Equal(t, expect(sec.calls), sec.calls, "success write follows every security command")
				assertXcodeSecret(t, filepath.Join(profiles, "pp.mobileprovision"), xcodeContractProfile)
			}
			beforeEnv := os.Environ()
			checkProcess := captureXcodeProcessDiagnostics(t, in)
			err := appbuild.XcodeSetupCodeSigning(ctx, sec, out, in)

			checkProcess()
			require.NoError(t, err)
			require.Equal(t, beforeEnv, os.Environ())
			require.Equal(t, expect(sec.calls), sec.calls, "complete ordered security argv")
			requireGeneratedKeychainPassword(t, in.KeychainPassword, observedKeychainPassword(t, sec.calls))
			require.Equal(t, []string{xcodeSigningSuccess}, out.calls)
			require.Equal(t, xcodeSigningSuccess, out.accepted.String())
			assertXcodeSecurityNonleakage(t, in, out.accepted.String())
			assertXcodeSecret(t, filepath.Join(tmp, "certificate.p12"), xcodeContractCertificate)
			assertXcodeSecret(t, filepath.Join(tmp, "pp.mobileprovision"), xcodeContractProfile)
			assertXcodeSecret(t, filepath.Join(profiles, "pp.mobileprovision"), xcodeContractProfile)
			// The standalone verb installs signing state for a later job step to
			// use, so it deliberately keeps it. Only the owning release build,
			// which knows when the state stops being needed, tears it down.
		})
	}
}

func TestXcodeSetupCodeSigning_MissingSecretsError(t *testing.T) {
	cases := []struct {
		name string
		in   appbuild.XcodeSetupCodeSigningInput
		want string
	}{
		{"no cert", appbuild.XcodeSetupCodeSigningInput{}, "CERTIFICATE_BASE64"},
		{"no pp", appbuild.XcodeSetupCodeSigningInput{CertBase64: "x"}, "PROVISIONING_PROFILE"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			owned := xcodeSecurityFixture(t)
			c.in.TempDir, c.in.ProvisioningProfilesDir = owned.TempDir, owned.ProvisioningProfilesDir
			before := ownedTree(t, filepath.Dir(owned.TempDir))
			sec, out := &fakeSecurity{}, &xcodeSecurityWriter{}
			checkProcess := captureXcodeProcessDiagnostics(t, c.in)
			err := appbuild.XcodeSetupCodeSigning(t.Context(), sec, out, c.in)

			checkProcess()
			require.Empty(t, sec.calls)
			require.Empty(t, out.calls)
			require.Equal(t, before, ownedTree(t, filepath.Dir(owned.TempDir)))

			// ErrPermissionDenied (EX_NOPERM), not a generic failure: an
			// absent signing secret is something the operator fixes in the
			// forge, not a bad command line.
			if !errors.Is(err, errs.ErrPermissionDenied) {
				t.Fatalf("err = %v, want ErrPermissionDenied", err)
			}

			if !strings.Contains(err.Error(), c.want) {
				t.Errorf("err = %v, want it to name %q", err, c.want)
			}
		})
	}
}

func TestXcodeSetupCodeSigning_SecurityFailureBubbles(t *testing.T) {
	in := xcodeSecurityFixture(t)
	sec := &fakeSecurity{err: errKeychainInUse}

	checkProcess := captureXcodeProcessDiagnostics(t, in)
	err := appbuild.XcodeSetupCodeSigning(t.Context(), sec, io.Discard, in)

	checkProcess()

	if !errors.Is(err, errKeychainInUse) {
		t.Fatalf("err = %v, want the security tool's own error to survive wrapping", err)
	}
}
