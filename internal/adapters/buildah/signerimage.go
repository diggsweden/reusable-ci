// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package buildah

import (
	"context"
	"errors"
	"io"
	"os/exec"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/safeexec"
)

// BuildSignerImage builds the forgejo-ci signer image for one architecture into
// local buildah storage under the supplied local tag.
func (a *Adapter) BuildSignerImage(ctx context.Context, req container.SignerImageBuildToolRequest, out io.Writer) error {
	args := a.global("bud",
		"--authfile", req.AuthFile,
		"--isolation", "chroot",
		"--pull=missing",
		"--layers",
		"--platform", req.Platform,
		"--label", "org.opencontainers.image.source="+req.SourceURL,
		"--label", "org.opencontainers.image.revision="+req.Revision,
		"--label", "org.opencontainers.image.title=forgejo-ci signer",
		"--tag", req.LocalImage,
		"--file", req.Containerfile,
		req.Context,
	)

	return a.run(ctx, out, args...)
}

// PushImage pushes one local image to a registry tag.
func (a *Adapter) PushImage(ctx context.Context, authFile, localImage, dest string, out io.Writer) error {
	return a.run(ctx, out, a.global("push", "--quiet", "--authfile", authFile, localImage, "docker://"+dest)...)
}

// RawManifest returns the raw manifest document skopeo observes for image.
func (a *Adapter) RawManifest(ctx context.Context, authFile, image string, out io.Writer) ([]byte, error) {
	cmd := safeexec.Command(ctx, a.skopeoBin(), "inspect", "--raw", "--authfile", authFile, "docker://"+image)
	cmd.Stderr = out

	raw, err := cmd.Output()
	if err != nil {
		return nil, safeexec.WrapError(err, a.skopeoBin(), "inspect")
	}

	return raw, nil
}

// RemoveManifest removes a local buildah manifest list. Missing manifests are a
// successful no-op, matching `buildah manifest rm ... || true`.
func (a *Adapter) RemoveManifest(ctx context.Context, localManifest string, out io.Writer) error {
	if err := a.run(ctx, out, a.global("manifest", "rm", localManifest)...); err != nil {
		var exitErr *exec.ExitError
		if !errors.As(err, &exitErr) {
			return err
		}

		return nil
	}

	return nil
}

// CreateManifest creates a local buildah manifest list.
func (a *Adapter) CreateManifest(ctx context.Context, localManifest string, out io.Writer) error {
	return a.run(ctx, out, a.global("manifest", "create", localManifest)...)
}

// AddManifest adds one digest-pinned architecture image to a local manifest.
func (a *Adapter) AddManifest(ctx context.Context, req container.SignerImageManifestAddToolRequest, out io.Writer) error {
	return a.run(ctx, out, a.global("manifest", "add",
		"--authfile", req.AuthFile,
		"--arch", req.Arch,
		"--os", "linux",
		req.LocalManifest,
		"docker://"+req.Ref,
	)...)
}

// PushManifest pushes a local manifest list to a registry tag and removes the
// local manifest on success.
func (a *Adapter) PushManifest(ctx context.Context, authFile, localManifest, dest string, out io.Writer) error {
	return a.run(ctx, out, a.global("manifest", "push", "--all", "--quiet", "--rm", "--authfile", authFile, localManifest, "docker://"+dest)...)
}

func (a *Adapter) skopeoBin() string {
	if a.SkopeoBin != "" {
		return a.SkopeoBin
	}

	return "skopeo"
}
