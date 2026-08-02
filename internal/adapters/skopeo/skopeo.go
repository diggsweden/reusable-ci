// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package skopeo shells out to skopeo for the few places where reusable-ci must
// materialize an OCI image locally in the same format forgejo-ci already uses.
package skopeo

import (
	"context"
	"errors"
	"io"
	"os"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/safeexec"
)

// Static precondition errors for the copy variants; each guards one required
// argument so err113 callers can branch with errors.Is.
var (
	errSourceLayoutEmpty = errors.New("skopeo copy: source OCI layout is empty")
	errDestLayoutEmpty   = errors.New("skopeo copy: destination OCI layout is empty")
	errSourceRefEmpty    = errors.New("skopeo copy: source ref is empty")
	errSourceDigestEmpty = errors.New("skopeo copy: source digest is empty")
	errDestArchiveEmpty  = errors.New("skopeo copy: destination archive is empty")
	errPlatformOSEmpty   = errors.New("skopeo copy: platform OS is empty")
	errPlatformArchEmpty = errors.New("skopeo copy: platform architecture is empty")
)

// Adapter wraps the skopeo binary. Bin is overridable for tests; AuthFile is a
// Docker-compatible auth config used with --authfile.
type Adapter struct {
	Bin      string
	AuthFile string
	UnsetEnv []string
}

// New returns an Adapter with default binary lookup and no auth file.
func New() *Adapter { return &Adapter{} }

// WithAuthFile returns an Adapter that passes path via --authfile on the
// copy variants that talk to a registry.
func WithAuthFile(path string) *Adapter { return &Adapter{AuthFile: path} }

// CopyOCILayoutToOCILayout copies one platform from a local OCI layout to a
// single-platform OCI layout tagged "scan".
func (a *Adapter) CopyOCILayoutToOCILayout(ctx context.Context, sourceLayout, destLayout, osName, arch string, errOut io.Writer) error {
	if sourceLayout == "" {
		return errSourceLayoutEmpty
	}

	if destLayout == "" {
		return errDestLayoutEmpty
	}

	return a.copyToOCILayout(ctx, "oci:"+sourceLayout+":scan", destLayout, osName, arch, false, false, errOut)
}

// CopyDockerDigestToOCILayout copies one platform from a registry digest ref to
// a single-platform OCI layout tagged "scan".
func (a *Adapter) CopyDockerDigestToOCILayout(ctx context.Context, ref, digest, destLayout, osName, arch string, errOut io.Writer) error {
	if ref == "" {
		return errSourceRefEmpty
	}

	if digest == "" {
		return errSourceDigestEmpty
	}

	if destLayout == "" {
		return errDestLayoutEmpty
	}

	return a.copyToOCILayout(ctx, "docker://"+ref+"@"+digest, destLayout, osName, arch, true, true, errOut)
}

// CopyDockerToOCIArchive copies docker://ref to an OCI archive tarball. The
// retry count intentionally matches the forgejo-ci signer workflow this replaces.
func (a *Adapter) CopyDockerToOCIArchive(ctx context.Context, ref, archive string, errOut io.Writer) error {
	if ref == "" {
		return errSourceRefEmpty
	}

	if archive == "" {
		return errDestArchiveEmpty
	}

	args := []string{"copy", "--retry-times", "5"}
	if a.AuthFile != "" {
		args = append(args, "--authfile", a.AuthFile)
	}

	args = append(args, "docker://"+ref, "oci-archive:"+archive)

	cmd := safeexec.Command(ctx, a.bin(), args...)
	if len(a.UnsetEnv) > 0 {
		cmd.Env = envWithout(os.Environ(), a.UnsetEnv)
	}

	cmd.Stderr = errOut
	if err := cmd.Run(); err != nil {
		return safeexec.WrapError(err, a.bin(), "copy")
	}

	return nil
}

func (a *Adapter) copyToOCILayout(ctx context.Context, source, destLayout, osName, arch string, retry, useAuth bool, errOut io.Writer) error {
	if osName == "" {
		return errPlatformOSEmpty
	}

	if arch == "" {
		return errPlatformArchEmpty
	}

	args := []string{"copy"}
	if retry {
		args = append(args, "--retry-times", "5")
	}

	args = append(args, "--override-arch", arch, "--override-os", osName)
	if useAuth && a.AuthFile != "" {
		args = append(args, "--authfile", a.AuthFile)
	}

	args = append(args, source, "oci:"+destLayout+":scan")

	cmd := safeexec.Command(ctx, a.bin(), args...)
	if len(a.UnsetEnv) > 0 {
		cmd.Env = envWithout(os.Environ(), a.UnsetEnv)
	}

	cmd.Stderr = errOut
	if err := cmd.Run(); err != nil {
		return safeexec.WrapError(err, a.bin(), "copy")
	}

	return nil
}

func envWithout(env, names []string) []string {
	drop := make(map[string]bool, len(names))
	for _, name := range names {
		drop[name] = true
	}

	out := env[:0]
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if !drop[name] {
			out = append(out, kv)
		}
	}

	return out
}

func (a *Adapter) bin() string {
	if a.Bin != "" {
		return a.Bin
	}

	return "skopeo"
}
