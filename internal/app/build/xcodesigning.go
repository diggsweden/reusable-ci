// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/clicolor"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

// XcodeSecurityOps abstracts the macOS `security` CLI for tests.
//
//nolint:iface // see SwiftFilesLister — distinct consumer-defined port.
type XcodeSecurityOps interface {
	Run(ctx context.Context, args ...string) (string, error)
}

// XcodeSetupCodeSigningInput drives XcodeSetupCodeSigning.
type XcodeSetupCodeSigningInput struct {
	CertBase64       string
	CertPassphrase   string
	PPBase64         string // provisioning profile, base64
	KeychainPassword string
	// TempDir is the location for transient files. Empty → $CI_TEMP_DIR
	// then $RUNNER_TEMP then os.TempDir().
	TempDir string
	// ProvisioningProfilesDir is where the provisioning profile gets
	// installed. Empty → ~/Library/MobileDevice/Provisioning Profiles.
	ProvisioningProfilesDir string
}

// XcodeSetupCodeSigning decodes a base64-encoded code-signing
// certificate + provisioning profile, creates a transient macOS
// keychain, imports the cert, and installs the profile.
//
// Pure darwin operation — the linux unit tests use a fake
// XcodeSecurityOps that records the security-CLI invocations.
func XcodeSetupCodeSigning(ctx context.Context, sec XcodeSecurityOps, w io.Writer, in XcodeSetupCodeSigningInput) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if err := validateXcodeSigningSecrets(in); err != nil {
		return err
	}

	tmp := in.TempDir
	if tmp == "" {
		// CLI binding reads $CI_TEMP_DIR / $RUNNER_TEMP via flag sources;
		// fall back to the OS-level tmp dir for direct app-layer callers.
		tmp = os.TempDir()
	}

	certPath := filepath.Join(tmp, "certificate.p12")
	ppPath := filepath.Join(tmp, "pp.mobileprovision")
	keychainPath := filepath.Join(tmp, "app-signing.keychain-db")

	if err := stageXcodeSigningFiles(in, certPath, ppPath); err != nil {
		return err
	}

	if err := createXcodeKeychain(ctx, sec, certPath, keychainPath, in); err != nil {
		return err
	}

	if err := installProvisioningProfile(ppPath, in.ProvisioningProfilesDir); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(w, "%s Code signing configured successfully\n", clicolor.Check(w))

	return nil
}

// validateXcodeSigningSecrets checks that the three secret inputs are
// non-empty. CLI binding pre-validates required flags; this is the
// belt-and-braces for direct app-layer callers.
func validateXcodeSigningSecrets(in XcodeSetupCodeSigningInput) error {
	if in.CertBase64 == "" {
		return fmt.Errorf("IOS_SIGNING_CERTIFICATE_BASE64 secret not found but enable-code-signing is true: %w", errs.ErrPermissionDenied)
	}

	if in.PPBase64 == "" {
		return fmt.Errorf("PROVISIONING_PROFILE_BASE64 secret not found but enable-code-signing is true: %w", errs.ErrPermissionDenied)
	}

	if in.KeychainPassword == "" {
		return fmt.Errorf("KEYCHAIN_PASSWORD secret not found but enable-code-signing is true: %w", errs.ErrPermissionDenied)
	}

	return nil
}

// stageXcodeSigningFiles base64-decodes the cert and the provisioning
// profile to their on-disk locations at 0o600.
func stageXcodeSigningFiles(in XcodeSetupCodeSigningInput, certPath, ppPath string) error {
	if err := decodeBase64ToFile(in.CertBase64, certPath, 0o600); err != nil {
		return fmt.Errorf("decode certificate: %w: %w", err, errs.ErrMalformedInput)
	}

	if err := decodeBase64ToFile(in.PPBase64, ppPath, 0o600); err != nil {
		return fmt.Errorf("decode provisioning profile: %w: %w", err, errs.ErrMalformedInput)
	}

	return nil
}

// createXcodeKeychain runs the six security(1) commands that create a
// transient keychain, unlock it, import the .p12, and make the
// resulting identity accessible to xcodebuild without further prompts.
func createXcodeKeychain(ctx context.Context, sec XcodeSecurityOps, certPath, keychainPath string, in XcodeSetupCodeSigningInput) error {
	if _, err := sec.Run(ctx, "create-keychain", "-p", in.KeychainPassword, keychainPath); err != nil {
		return fmt.Errorf("create-keychain: %w", err)
	}

	if _, err := sec.Run(ctx, "set-keychain-settings", "-lut", "21600", keychainPath); err != nil {
		return fmt.Errorf("set-keychain-settings: %w", err)
	}

	if _, err := sec.Run(ctx, "unlock-keychain", "-p", in.KeychainPassword, keychainPath); err != nil {
		return fmt.Errorf("unlock-keychain: %w", err)
	}

	if _, err := sec.Run(ctx,
		"import", certPath,
		"-P", in.CertPassphrase,
		"-A", "-t", "cert", "-f", "pkcs12", "-k", keychainPath); err != nil {
		return fmt.Errorf("security import: %w", err)
	}

	if _, err := sec.Run(ctx,
		"set-key-partition-list",
		"-S", "apple-tool:,apple:",
		"-k", in.KeychainPassword, keychainPath); err != nil {
		return fmt.Errorf("set-key-partition-list: %w", err)
	}

	if _, err := sec.Run(ctx, "list-keychain", "-d", "user", "-s", keychainPath); err != nil {
		return fmt.Errorf("list-keychain: %w", err)
	}

	return nil
}

// installProvisioningProfile copies the staged .mobileprovision file
// into the user's profiles directory at 0o600.
func installProvisioningProfile(ppPath, profilesDir string) error {
	if profilesDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return fmt.Errorf("user home dir: %w", err)
		}

		profilesDir = filepath.Join(home, "Library", "MobileDevice", "Provisioning Profiles")
	}

	if err := os.MkdirAll(profilesDir, 0o755); err != nil { //nolint:gosec // macOS xcodebuild expects this exact dir mode.
		return fmt.Errorf("mkdir provisioning profiles dir: %w", err)
	}

	if err := copyFileMode(ppPath, filepath.Join(profilesDir, filepath.Base(ppPath)), 0o600); err != nil {
		return fmt.Errorf("install provisioning profile: %w", err)
	}

	return nil
}

// decodeBase64ToFile decodes std-encoded base64 (whitespace tolerated)
// and writes the result to path at the given mode.
func decodeBase64ToFile(b64, path string, mode os.FileMode) error {
	// Strip whitespace defensively — macOS `base64 --decode` does this too.
	clean := strings.Map(func(r rune) rune { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
		switch r {
		case ' ', '\t', '\r', '\n':
			return -1
		}

		return r
	}, b64)

	body, err := base64.StdEncoding.DecodeString(clean)
	if err != nil {
		return err
	}

	return os.WriteFile(path, body, mode)
}

func copyFileMode(src, dst string, mode os.FileMode) error {
	body, err := os.ReadFile(src) //nolint:gosec // src is a CLI-flag path under operator control.
	if err != nil {
		return err
	}

	return os.WriteFile(dst, body, mode) //nolint:gosec // dst is a CLI-flag path under operator control.
}
