// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package buildah

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/container"
)

// BuildManifest builds one platform into a shared buildah manifest list.
func (a *Adapter) BuildManifest(ctx context.Context, req container.BuildPushManifestBuildRequest, out io.Writer) error {
	args := a.global("bud",
		"--pull=always",
		"--isolation", "chroot",
		"--platform", req.Platform,
		fmt.Sprintf("--tls-verify=%t", req.TLSVerify),
		"--timestamp", req.SourceDateEpoch,
		"--manifest", req.Manifest,
	)

	for _, label := range req.Labels {
		args = append(args, "--label", label)
	}

	args = append(args, "-f", req.Containerfile)
	for _, buildArg := range req.BuildArgs {
		args = append(args, "--build-arg", buildArg)
	}

	args = append(args, req.Context)

	return a.runWithEnv(ctx, out, registryAuthEnv(req.AuthFile), args...)
}

// PushManifestWithDigest pushes a shared buildah manifest list and returns the digest written by buildah.
func (a *Adapter) PushManifestWithDigest(ctx context.Context, authFile string, tlsVerify bool, manifest string, out io.Writer) (string, error) {
	digestFile, err := os.CreateTemp("", "manifest-digest.*")
	if err != nil {
		return "", fmt.Errorf("create manifest digest file: %w", err)
	}

	digestPath := digestFile.Name()
	_ = digestFile.Close()

	defer func() { _ = os.Remove(digestPath) }()

	args := a.global("manifest", "push",
		"--all",
		fmt.Sprintf("--tls-verify=%t", tlsVerify),
		"--digestfile", digestPath,
		manifest,
		"docker://"+manifest,
	)
	if runErr := a.runWithEnv(ctx, out, registryAuthEnv(authFile), args...); runErr != nil {
		return "", runErr
	}

	body, err := os.ReadFile(digestPath) //nolint:gosec // path is our own mktemp.
	if err != nil {
		return "", fmt.Errorf("read manifest digest file: %w", err)
	}

	return string(body), nil
}

// PushManifestToRefWithDigest pushes a local buildah manifest list to a registry
// reference and returns the digest written by buildah.
func (a *Adapter) PushManifestToRefWithDigest(ctx context.Context, authFile string, tlsVerify bool, manifest, destination string, remove bool, out io.Writer) (string, error) {
	digestFile, err := os.CreateTemp("", "manifest-digest.*")
	if err != nil {
		return "", fmt.Errorf("create manifest digest file: %w", err)
	}

	digestPath := digestFile.Name()
	_ = digestFile.Close()

	defer func() { _ = os.Remove(digestPath) }()

	args := a.global("manifest", "push",
		"--all",
		"--quiet",
		fmt.Sprintf("--tls-verify=%t", tlsVerify),
		"--digestfile", digestPath,
	)
	if remove {
		args = append(args, "--rm")
	}

	if authFile != "" {
		args = append(args, "--authfile", authFile)
	}

	args = append(args, manifest, "docker://"+destination)

	if runErr := a.runWithEnv(ctx, out, registryAuthEnv(authFile), args...); runErr != nil {
		return "", runErr
	}

	body, err := os.ReadFile(digestPath) //nolint:gosec // path is our own mktemp.
	if err != nil {
		return "", fmt.Errorf("read manifest digest file: %w", err)
	}

	return string(body), nil
}

// PushImageToRefWithDigest pushes one local buildah image to a registry
// reference and returns the digest written by buildah.
func (a *Adapter) PushImageToRefWithDigest(ctx context.Context, authFile string, tlsVerify bool, image, destination string, out io.Writer) (string, error) {
	digestFile, err := os.CreateTemp("", "image-digest.*")
	if err != nil {
		return "", fmt.Errorf("create image digest file: %w", err)
	}

	digestPath := digestFile.Name()
	_ = digestFile.Close()

	defer func() { _ = os.Remove(digestPath) }()

	args := a.global("push",
		"--quiet",
		fmt.Sprintf("--tls-verify=%t", tlsVerify),
		"--digestfile", digestPath,
	)
	if authFile != "" {
		args = append(args, "--authfile", authFile)
	}

	args = append(args, image, "docker://"+destination)

	if runErr := a.runWithEnv(ctx, out, registryAuthEnv(authFile), args...); runErr != nil {
		return "", runErr
	}

	body, err := os.ReadFile(digestPath) //nolint:gosec // path is our own mktemp.
	if err != nil {
		return "", fmt.Errorf("read image digest file: %w", err)
	}

	return string(body), nil
}

func registryAuthEnv(authFile string) []string {
	if authFile == "" {
		return nil
	}

	return []string{"REGISTRY_AUTH_FILE=" + authFile}
}
