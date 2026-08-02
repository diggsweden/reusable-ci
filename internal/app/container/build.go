// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/diggsweden/reusable-ci/v3/internal/clicolor"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/container"
)

// imageBuilder is the slice of adapters/buildah.Adapter that BuildImage drives.
// Build handles the load/local modes; BuildToLayout stages a push-by-digest
// build into an OCI layout for the pusher. Narrow port so app-layer tests fake
// the build without invoking buildah.
type imageBuilder interface {
	Build(ctx context.Context, req container.BuildRequest, out io.Writer) error
	BuildToLayout(ctx context.Context, req container.BuildRequest, layoutDir string, out io.Writer) error
}

// layoutPusher is the slice of adapters/ociregistry.Adapter that BuildImage
// uses to publish a staged build. It writes the image TAGLESS by digest (the
// daemonless push-by-digest), returning that digest.
type layoutPusher interface {
	PushLayoutByDigest(ctx context.Context, layoutDir, imageRef string) (string, error)
}

// BuildImageInput drives `reusable-ci container build`. Mirrors the workflow
// env contract and maps 1:1 onto domain/container.BuildRequest.
type BuildImageInput struct {
	Context       string
	Containerfile string
	Target        string
	Platform      string
	BuildArgs     []string
	Secrets       []string
	Labels        []string

	SourceDateEpoch string
	CacheRepo       string
	CacheScope      string
	CachePush       bool

	Mode      container.BuildOutputMode
	ImageRef  string
	OutputDir string

	// DigestKey is the OutputSink key for the pushed digest (push-by-digest
	// mode). Empty defaults to "digest".
	DigestKey string
}

// BuildImage builds one image. For push-by-digest it stages the build into an
// OCI layout with buildah and pushes it TAGLESS by digest with ggcr (no daemon,
// no GitHub action, no ephemeral tag to clean up), recording the digest on the
// OutputSink so the downstream `container manifest merge` can reference it.
// Load and local modes build in place and push nothing.
func BuildImage(
	ctx context.Context,
	builder imageBuilder,
	pusher layoutPusher,
	sink ci.OutputSink,
	out io.Writer, //nolint:varnamelen // idiomatic short name (io conventions).
	in BuildImageInput,
) (string, error) {
	req := buildRequest(in)
	if err := req.Validate(); err != nil {
		return "", err
	}

	if req.Mode != container.BuildModePushByDigest {
		if err := builder.Build(ctx, req, out); err != nil {
			return "", fmt.Errorf("build image: %w", err)
		}

		reportBuild(out, req, "")

		return "", nil
	}

	digest, err := pushByDigest(ctx, builder, pusher, req, out)
	if err != nil {
		return "", err
	}

	if sink != nil {
		key := in.DigestKey
		if key == "" {
			key = outputKeyDigest
		}

		if err := sink.Set(ctx, key, digest); err != nil {
			return "", fmt.Errorf("emit digest: %w", err)
		}
	}

	reportBuild(out, req, digest)

	return digest, nil
}

// pushByDigest stages the build into a temp OCI layout and pushes it tagless.
func pushByDigest(
	ctx context.Context,
	builder imageBuilder,
	pusher layoutPusher,
	req container.BuildRequest,
	out io.Writer, //nolint:varnamelen // idiomatic short name (io conventions).
) (string, error) {
	layoutDir, err := os.MkdirTemp("", "reusable-ci-oci-*")
	if err != nil {
		return "", fmt.Errorf("create oci layout dir: %w", err)
	}

	defer func() { _ = os.RemoveAll(layoutDir) }()

	if err = builder.BuildToLayout(ctx, req, layoutDir, out); err != nil {
		return "", fmt.Errorf("build image: %w", err)
	}

	digest, err := pusher.PushLayoutByDigest(ctx, layoutDir, req.ImageRef)
	if err != nil {
		return "", fmt.Errorf("push by digest: %w", err)
	}

	return digest, nil
}

func buildRequest(in BuildImageInput) container.BuildRequest {
	return container.BuildRequest{
		Context:         in.Context,
		Containerfile:   in.Containerfile,
		Target:          in.Target,
		Platform:        in.Platform,
		BuildArgs:       in.BuildArgs,
		Secrets:         in.Secrets,
		Labels:          in.Labels,
		SourceDateEpoch: in.SourceDateEpoch,
		CacheRepo:       in.CacheRepo,
		CacheScope:      in.CacheScope,
		CachePush:       in.CachePush,
		Mode:            in.Mode,
		ImageRef:        in.ImageRef,
		OutputDir:       in.OutputDir,
	}
}

func reportBuild(out io.Writer, req container.BuildRequest, digest string) {
	check := clicolor.Check(out)

	switch req.Mode {
	case container.BuildModePushByDigest:
		_, _ = fmt.Fprintf(out, "%s Built and pushed by digest %s → %s\n", check, req.ImageRef, digest)
	case container.BuildModeLoad:
		_, _ = fmt.Fprintf(out, "%s Built %s into local storage\n", check, req.ImageRef)
	case container.BuildModeLocal:
		_, _ = fmt.Fprintf(out, "%s Built and exported to %s\n", check, req.OutputDir)
	}
}
