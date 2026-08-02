// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"fmt"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
	domaincontainer "github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// UsableImageDigestRegistry is the registry read surface needed to decide
// whether an existing image tag is safe to reuse.
type UsableImageDigestRegistry interface {
	InspectImage(ctx context.Context, ref, arch string) (digest string, architecture string, labels map[string]string, err error)
}

// UsableImageDigestInput drives `container image usable-digest`.
type UsableImageDigestInput struct {
	Ref            string
	Arch           string
	RequiredLabels []string
}

// UsableImageDigestOutput is the reusable manifest digest and digest-pinned ref.
type UsableImageDigestOutput struct {
	Digest string
	Ref    string
}

type requiredImageLabel struct {
	key   string
	value string
}

// UsableImageDigest resolves an existing image digest only when the image's
// architecture and caller-provided labels match exactly.
func UsableImageDigest(ctx context.Context, registry UsableImageDigestRegistry, sink ci.OutputSink, in UsableImageDigestInput) (*UsableImageDigestOutput, error) {
	labels, err := validateUsableImageDigestInput(registry, in)
	if err != nil {
		return nil, err
	}

	ref := strings.TrimSpace(in.Ref)
	arch := strings.TrimSpace(in.Arch)

	digest, gotArch, gotLabels, err := registry.InspectImage(ctx, ref, arch)
	if err != nil {
		return nil, err
	}

	digest = strings.TrimSpace(digest)
	if !domaincontainer.ValidDigest(digest) {
		return nil, fmt.Errorf("existing image %s returned invalid digest %q: %w", ref, digest, errs.ErrInvalidConfig)
	}

	if gotArch != arch {
		return nil, fmt.Errorf("existing image is not reusable: %s: architecture %q does not match required %q: %w", ref, gotArch, arch, errs.ErrValidation)
	}

	for _, want := range labels {
		if gotLabels[want.key] != want.value {
			return nil, fmt.Errorf("existing image is not reusable: %s: label %s does not match required value: %w", ref, want.key, errs.ErrValidation)
		}
	}

	result := &UsableImageDigestOutput{Digest: digest, Ref: domaincontainer.StripDigest(ref) + "@" + digest}
	if sink != nil {
		if err := sink.Set(ctx, "digest", result.Digest); err != nil {
			return nil, err
		}

		if err := sink.Set(ctx, "ref", result.Ref); err != nil {
			return nil, err
		}
	}

	return result, nil
}

func validateUsableImageDigestInput(registry UsableImageDigestRegistry, in UsableImageDigestInput) ([]requiredImageLabel, error) {
	if registry == nil {
		return nil, fmt.Errorf("container image usable-digest: registry adapter is required: %w", errs.ErrUsage)
	}

	ref := strings.TrimSpace(in.Ref)
	if ref == "" || strings.ContainsAny(ref, " \t\n\r") {
		return nil, fmt.Errorf("container image usable-digest: ref is empty or unsafe: %w", errs.ErrUsage)
	}

	arch := strings.TrimSpace(in.Arch)
	if arch == "" || strings.ContainsAny(arch, " \t\n\r") {
		return nil, fmt.Errorf("container image usable-digest: arch is empty or unsafe: %w", errs.ErrUsage)
	}

	return parseRequiredImageLabels(in.RequiredLabels)
}

func parseRequiredImageLabels(values []string) ([]requiredImageLabel, error) {
	labels := make([]requiredImageLabel, 0, len(values))
	for _, raw := range values {
		entry := strings.TrimSpace(raw)

		key, value, ok := strings.Cut(entry, "=")
		if !ok || key == "" || strings.ContainsAny(key, " \t\n\r") {
			return nil, fmt.Errorf("container image usable-digest: require-label must be key=value, got %q: %w", raw, errs.ErrUsage)
		}

		labels = append(labels, requiredImageLabel{key: key, value: value})
	}

	return labels, nil
}
