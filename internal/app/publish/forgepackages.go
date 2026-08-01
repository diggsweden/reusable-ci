// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package publish

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// ForgePackagesDeployInput drives ForgePackagesDeploy.
type ForgePackagesDeployInput struct {
	// CLIOpts is $MAVEN_CLI_OPTS split into argv.
	CLIOpts []string
}

// ForgePackagesDeploy publishes a Maven package to the forge-native registry of
// the active forge — GitHub Packages, the GitLab Package Registry, or Forgejo
// packages — from one binary-owned sequence. The per-forge coordinates + auth
// come from the provider role (no platform switch here); this use case renders
// the credentialed settings.xml to a 0600 temp file (the token never reaches
// argv) and runs `mvn $CLI_OPTS deploy -DskipTests -s <settings>
// -DaltDeploymentRepository=…`.
//
// mvn runs in the process working directory (the job cd's into the project).
func ForgePackagesDeploy(ctx context.Context, ops MavenOps, resolver provider.ForgeMavenRegistryResolver, stdout, stderr io.Writer, in ForgePackagesDeployInput) error {
	reg, err := resolver.ResolveForgeMavenRegistry()
	if err != nil {
		return err
	}

	settingsPath, cleanup, err := writeTempSecretFile("reusable-ci-settings-*.xml", reg.RenderSettingsXML())
	if err != nil {
		return err
	}

	defer cleanup()

	_, _ = fmt.Fprintf(stdout, "Deploying to the forge package registry: %s\n", reg.URL)

	args := make([]string, 0, len(in.CLIOpts)+4)
	args = append(args, in.CLIOpts...)
	args = append(args, "deploy", "-DskipTests",
		"--settings", settingsPath,
		"-DaltDeploymentRepository="+reg.AltDeploymentRepository())

	return ops.RunInherit(ctx, stdout, stderr, args...)
}

// writeTempSecretFile writes body to a 0600 temp file (it carries a registry
// token — settings.xml or .npmrc) and returns its path plus a cleanup func that
// removes it. pattern is an os.CreateTemp name pattern (e.g. "x-*.xml").
func writeTempSecretFile(pattern, body string) (string, func(), error) {
	noop := func() {}

	file, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", noop, fmt.Errorf("create temp secret file: %w", err)
	}

	path := file.Name()
	cleanup := func() { _ = os.Remove(path) }

	if err := file.Chmod(0o600); err != nil {
		_ = file.Close()

		cleanup()

		return "", noop, fmt.Errorf("chmod temp secret file: %w", err)
	}

	if _, err := file.WriteString(body); err != nil {
		_ = file.Close()

		cleanup()

		return "", noop, fmt.Errorf("write temp secret file: %w", err)
	}

	if err := file.Close(); err != nil {
		cleanup()

		return "", noop, fmt.Errorf("close temp secret file: %w", err)
	}

	return path, cleanup, nil
}
