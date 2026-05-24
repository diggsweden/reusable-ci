// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package build_test

import (
	"context"
	"encoding/base64"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appbuild "github.com/diggsweden/reusable-ci/internal/app/build"
	"github.com/diggsweden/reusable-ci/internal/testutil/testfs"
)

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
	tmpFS := testfs.NewReal(t)
	profilesFS := testfs.NewReal(t)
	tmp := tmpFS.Root
	profiles := profilesFS.Root
	cert := base64.StdEncoding.EncodeToString([]byte("fake-cert"))
	pp := base64.StdEncoding.EncodeToString([]byte("fake-pp"))

	sec := &fakeSecurity{}

	var out strings.Builder
	if err := appbuild.XcodeSetupCodeSigning(context.Background(), sec, &out, appbuild.XcodeSetupCodeSigningInput{
		CertBase64:              cert,
		CertPassphrase:          "secret",
		PPBase64:                pp,
		KeychainPassword:        "passw0rd",
		TempDir:                 tmp,
		ProvisioningProfilesDir: profiles,
	}); err != nil {
		t.Fatal(err)
	}

	// 6 security invocations: create / set-settings / unlock / import /
	// partition-list / list-keychain.
	if len(sec.calls) != 6 {
		t.Errorf("expected 6 security calls, got %d", len(sec.calls))
	}
	// Cert decoded to disk at mode 0600.
	if _, err := os.Stat(filepath.Join(tmp, "certificate.p12")); err != nil {
		t.Errorf("certificate not written: %v", err)
	}
	// Provisioning profile copied into profilesDir.
	if _, err := os.Stat(filepath.Join(profiles, "pp.mobileprovision")); err != nil {
		t.Errorf("provisioning profile not installed: %v", err)
	}
	// Sanity: first security call is create-keychain with the path.
	if sec.calls[0][0] != "create-keychain" {
		t.Errorf("first call = %v", sec.calls[0])
	}

	if got := strings.TrimSpace(out.String()); got != "✓ Code signing configured successfully" {
		t.Errorf("out = %q", got)
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
		{"no keychain password", appbuild.XcodeSetupCodeSigningInput{CertBase64: "x", PPBase64: "y"}, "KEYCHAIN_PASSWORD"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := appbuild.XcodeSetupCodeSigning(context.Background(), &fakeSecurity{}, io.Discard, c.in)
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Errorf("got %v, want substring %q", err, c.want)
			}
		})
	}
}

func TestXcodeSetupCodeSigning_SecurityFailureBubbles(t *testing.T) {
	tmpFS := testfs.NewReal(t)
	tmp := tmpFS.Root
	cert := base64.StdEncoding.EncodeToString([]byte("c"))
	pp := base64.StdEncoding.EncodeToString([]byte("p"))
	sec := &fakeSecurity{err: errors.New("security: keychain in use")} //nolint:err113 // test mock error

	err := appbuild.XcodeSetupCodeSigning(context.Background(), sec, io.Discard, appbuild.XcodeSetupCodeSigningInput{
		CertBase64: cert, PPBase64: pp, KeychainPassword: "p", TempDir: tmp,
		ProvisioningProfilesDir: testfs.NewReal(t).Root,
	})
	if err == nil {
		t.Fatal("expected error")
	}
}
