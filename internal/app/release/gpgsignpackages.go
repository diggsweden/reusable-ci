// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	domaingpg "github.com/diggsweden/reusable-ci/v3/internal/domain/gpg"
)

type gpgPackageSigner interface {
	ImportKey(ctx context.Context, keyData []byte) error
	ListSecretKeys(ctx context.Context) (string, error)
	DetachedSign(ctx context.Context, fingerprint, passphrase, inputPath, outputPath string) error
}

// GPGSignPackagesInput drives release gpg sign-packages.
type GPGSignPackagesInput struct {
	PrivateKey  string
	Fingerprint string
	Passphrase  string
	Dir         string
}

// GPGSignPackages imports the configured GPG key into the current GNUPGHOME,
// verifies that the imported keyring contains the expected fingerprint, and
// creates binary detached .sig files for distro packages under Dir.
func GPGSignPackages(ctx context.Context, signer gpgPackageSigner, out io.Writer, in GPGSignPackagesInput) (int, error) {
	if err := validateGPGSignPackagesInput(signer, in); err != nil {
		return 0, err
	}

	dir := in.Dir
	if dir == "" {
		dir = "dist"
	}

	keyData, err := domaingpg.DecodeKey(in.PrivateKey)
	if err != nil {
		return 0, fmt.Errorf("gpg sign-packages: %w", err)
	}

	if importErr := signer.ImportKey(ctx, keyData); importErr != nil {
		return 0, fmt.Errorf("gpg sign-packages: import key: %w", importErr)
	}

	listed, err := signer.ListSecretKeys(ctx)
	if err != nil {
		return 0, fmt.Errorf("gpg sign-packages: list imported keys: %w", err)
	}

	if got := firstGPGFingerprint(listed); got != in.Fingerprint {
		return 0, fmt.Errorf("gpg sign-packages: imported GPG key fingerprint does not match expected (got %q, want %q): %w", got, in.Fingerprint, errs.ErrValidation)
	}

	packages, err := packageFiles(dir)
	if err != nil {
		return 0, err
	}

	for _, pkg := range packages {
		if err := signer.DetachedSign(ctx, in.Fingerprint, in.Passphrase, pkg, pkg+".sig"); err != nil {
			return 0, err
		}
	}

	_, _ = fmt.Fprintf(out, "GPG-signed %d package(s)\n", len(packages))

	return len(packages), nil
}

func validateGPGSignPackagesInput(signer gpgPackageSigner, in GPGSignPackagesInput) error {
	if signer == nil {
		return fmt.Errorf("gpg sign-packages: gpg signer is required: %w", errs.ErrUsage)
	}

	if in.PrivateKey == "" {
		return fmt.Errorf("gpg sign-packages: private key is required: %w", errs.ErrUsage)
	}

	if in.Fingerprint == "" {
		return fmt.Errorf("gpg sign-packages: fingerprint is required: %w", errs.ErrUsage)
	}

	if in.Passphrase == "" {
		return fmt.Errorf("gpg sign-packages: passphrase is required: %w", errs.ErrUsage)
	}

	return nil
}

func firstGPGFingerprint(colons string) string {
	for _, line := range strings.Split(colons, "\n") {
		fields := strings.Split(line, ":")
		if len(fields) > 9 && fields[0] == "fpr" {
			return fields[9]
		}
	}

	return ""
}

func packageFiles(dir string) ([]string, error) {
	var files []string

	for _, pattern := range []string{"*.deb", "*.rpm", "*.apk"} {
		matches, err := filepath.Glob(filepath.Join(dir, pattern))
		if err != nil {
			return nil, fmt.Errorf("glob %s: %w", pattern, err)
		}

		sort.Strings(matches)

		for _, match := range matches {
			info, err := os.Stat(match)
			if err != nil || info.IsDir() {
				continue
			}

			files = append(files, match)
		}
	}

	return files, nil
}
