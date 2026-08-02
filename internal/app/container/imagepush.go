// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/retry"
)

// ImagePushTool is the Buildah surface needed to push one local image.
type ImagePushTool interface {
	PushImageToRefWithDigest(ctx context.Context, authFile string, tlsVerify bool, image, destination string, out io.Writer) (string, error)
}

// PushImageInput drives `container image push`.
type PushImageInput struct {
	LocalImage    string
	Destination   string
	AuthFile      string
	TLSVerify     string
	RetryAttempts int
	RetryDelay    time.Duration
}

// PushImageOutput is the pushed image digest and digest-pinned ref.
type PushImageOutput struct {
	Digest string
	Ref    string
}

// PushImage pushes one local Buildah image to a registry ref, verifies the raw
// manifest digest now served by the registry, and emits digest/ref outputs.
func PushImage(ctx context.Context, tool ImagePushTool, registry RawManifestRegistry, sink ci.OutputSink, out io.Writer, in PushImageInput) (*PushImageOutput, error) {
	if err := validatePushImageInput(tool, registry, in); err != nil {
		return nil, err
	}

	tlsVerify := in.TLSVerify == tlsVerifyTrue

	buildahDigest, err := retry.Do(ctx, out, retry.Attempts(in.RetryAttempts, defaultBuildPushRetryAttempts), retry.Delay(in.RetryDelay, defaultBuildPushRetryDelay), func() (string, error) {
		return tool.PushImageToRefWithDigest(ctx, in.AuthFile, tlsVerify, in.LocalImage, in.Destination, out)
	})
	if err != nil {
		return nil, err
	}

	registryDigest, err := resolveVerifiedManifestDigest(ctx, registry, out, in.Destination, buildahDigest, true, in.RetryAttempts, in.RetryDelay)
	if err != nil {
		return nil, err
	}

	result := &PushImageOutput{Digest: registryDigest, Ref: in.Destination + "@" + registryDigest}
	if sink != nil {
		if err := sink.Set(ctx, outputKeyDigest, result.Digest); err != nil {
			return nil, err
		}

		if err := sink.Set(ctx, "ref", result.Ref); err != nil {
			return nil, err
		}
	}

	return result, nil
}

func validatePushImageInput(tool ImagePushTool, registry RawManifestRegistry, in PushImageInput) error {
	if tool == nil {
		return fmt.Errorf("container image push: buildah adapter is required: %w", errs.ErrUsage)
	}

	if registry == nil {
		return fmt.Errorf("container image push: registry adapter is required: %w", errs.ErrUsage)
	}

	if strings.TrimSpace(in.LocalImage) == "" {
		return fmt.Errorf("container image push: local image is required: %w", errs.ErrUsage)
	}

	if strings.TrimSpace(in.Destination) == "" || strings.ContainsAny(in.Destination, " \t\n\r") {
		return fmt.Errorf("container image push: destination ref is empty or unsafe: %w", errs.ErrUsage)
	}

	switch in.TLSVerify {
	case tlsVerifyTrue, tlsVerifyFalse:
		return nil
	default:
		return fmt.Errorf("container image push: tls-verify must be 'true' or 'false', got %q: %w", in.TLSVerify, errs.ErrUsage)
	}
}
