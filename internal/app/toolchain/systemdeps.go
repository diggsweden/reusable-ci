// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package toolchain

import (
	"context"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

var aptPackageNameRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9.+_-]*$`)

// SystemPackageInstaller installs system packages for toolchain bootstrap.
type SystemPackageInstaller interface {
	Install(ctx context.Context, packages []string, out io.Writer) error
}

// InstallSystemDependenciesInput drives `toolchain install-system-dependencies`.
type InstallSystemDependenciesInput struct {
	Packages         string
	SkipIfMissingAPT bool
}

// InstallSystemDependencies installs validated Debian packages when apt-get is
// available. It intentionally mirrors setup-toolchain's historical no-op on
// non-apt runners when SkipIfMissingAPT is true.
func InstallSystemDependencies(ctx context.Context, installer SystemPackageInstaller, out io.Writer, in InstallSystemDependenciesInput) error {
	packages, err := systemDependencyPackages(in.Packages)
	if err != nil {
		return err
	}

	if len(packages) == 0 {
		return nil
	}

	if _, err := exec.LookPath("apt-get"); err != nil {
		if in.SkipIfMissingAPT {
			if out != nil {
				_, _ = fmt.Fprintln(out, "apt-get not found; skipping bootstrap system dependencies")
			}

			return nil
		}

		return fmt.Errorf("apt-get is required to install bootstrap system dependencies: %w", errs.ErrDependencyUnavailable)
	}

	if installer == nil {
		return fmt.Errorf("system package installer is required: %w", errs.ErrUsage)
	}

	return installer.Install(ctx, packages, out)
}

func systemDependencyPackages(raw string) ([]string, error) {
	seen := map[string]bool{}

	var packages []string

	for _, pkg := range strings.Fields(strings.ReplaceAll(raw, "\n", " ")) {
		if !aptPackageNameRE.MatchString(pkg) {
			return nil, fmt.Errorf("unsafe apt package name: %s: %w", pkg, errs.ErrUsage)
		}

		if !seen[pkg] {
			packages = append(packages, pkg)
			seen[pkg] = true
		}
	}

	return packages, nil
}

// TrustMiseConfigInput drives `toolchain trust-mise-config`.
type TrustMiseConfigInput struct{}

// TrustMiseConfig trusts the consumer repository's mise configuration in the
// current working directory.
func TrustMiseConfig(ctx context.Context, runner MiseRunner, out io.Writer, _ TrustMiseConfigInput) error {
	if runner == nil {
		return fmt.Errorf("mise runner is required: %w", errs.ErrUsage)
	}

	return runMise(ctx, runner, nil, out, "trust")
}
