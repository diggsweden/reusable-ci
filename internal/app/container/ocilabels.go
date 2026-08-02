// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// OCIReleaseLabelsInput is the canonical org.opencontainers.image.* release
// label contract shared by reusable-ci image builds and bespoke Buildah flows.
type OCIReleaseLabelsInput struct {
	Title         string
	Version       string
	Created       string
	Revision      string
	RefName       string
	Source        string
	Documentation string
	Description   string
	Licenses      string
	Vendor        string
	Authors       string
}

// OCIReleaseIdentityInput is the read-side identity predicate for release
// labels. Source is compared case-insensitively to tolerate forge owner-case
// drift while revision, version, and ref.name remain exact pins.
type OCIReleaseIdentityInput struct {
	Revision string
	Version  string
	RefName  string
	Source   string
}

// OCIImageLabelsRegistry is the registry surface needed to read image config
// labels without shelling out to skopeo/jq.
type OCIImageLabelsRegistry interface {
	Labels(ctx context.Context, ref string) (map[string]string, error)
}

// OCIReleaseLabels returns canonical OCI release labels as key=value strings.
func OCIReleaseLabels(in OCIReleaseLabelsInput) ([]string, error) {
	if in.Documentation == "" && in.Source != "" {
		in.Documentation = in.Source + "#readme"
	}

	required := []struct {
		key   string
		value string
	}{
		{key: "org.opencontainers.image.title", value: in.Title},
		{key: "org.opencontainers.image.version", value: in.Version},
		{key: "org.opencontainers.image.created", value: in.Created},
		{key: "org.opencontainers.image.revision", value: in.Revision},
		{key: "org.opencontainers.image.ref.name", value: in.RefName},
		{key: "org.opencontainers.image.source", value: in.Source},
		{key: "org.opencontainers.image.url", value: in.Source},
		{key: "org.opencontainers.image.documentation", value: in.Documentation},
	}

	for _, label := range required {
		if label.value == "" {
			return nil, fmt.Errorf("required OCI label %s is empty: %w", label.key, errs.ErrUsage)
		}
	}

	labels := make([]string, 0, len(required)+4)
	for _, label := range required {
		labels = append(labels, label.key+"="+label.value)
	}

	optional := []struct {
		key   string
		value string
	}{
		{key: "org.opencontainers.image.description", value: in.Description},
		{key: "org.opencontainers.image.licenses", value: in.Licenses},
		{key: "org.opencontainers.image.vendor", value: in.Vendor},
		{key: "org.opencontainers.image.authors", value: in.Authors},
	}

	for _, label := range optional {
		if label.value != "" {
			labels = append(labels, label.key+"="+label.value)
		}
	}

	return labels, nil
}

// OCIReleaseLabelFlags returns OCIReleaseLabels as Buildah-compatible
// alternating --label / key=value argv tokens.
func OCIReleaseLabelFlags(in OCIReleaseLabelsInput) ([]string, error) {
	labels, err := OCIReleaseLabels(in)
	if err != nil {
		return nil, err
	}

	flags := make([]string, 0, len(labels)*2)
	for _, label := range labels {
		flags = append(flags, "--label", label)
	}

	return flags, nil
}

// OCIReleaseIdentityMatches returns true when labelsJSON identifies the
// expected release identity.
func OCIReleaseIdentityMatches(labelsJSON string, in OCIReleaseIdentityInput) (bool, error) {
	var labels map[string]string
	if err := json.Unmarshal([]byte(labelsJSON), &labels); err != nil {
		return false, fmt.Errorf("parse OCI labels JSON: %w: %w", err, errs.ErrMalformedInput)
	}

	return labels["org.opencontainers.image.revision"] == in.Revision &&
		labels["org.opencontainers.image.version"] == in.Version &&
		labels["org.opencontainers.image.ref.name"] == in.RefName &&
		strings.EqualFold(labels["org.opencontainers.image.source"], in.Source), nil
}

// OCIImageLabelsJSON fetches an image's config labels and returns a compact JSON
// object for shell consumers that need the old oci_image_labels_json contract.
func OCIImageLabelsJSON(ctx context.Context, registry OCIImageLabelsRegistry, ref string) (string, error) {
	if registry == nil {
		return "", fmt.Errorf("registry is required: %w", errs.ErrUsage)
	}

	if ref == "" {
		return "", fmt.Errorf("image ref is required: %w", errs.ErrUsage)
	}

	labels, err := registry.Labels(ctx, ref)
	if err != nil {
		return "", err
	}

	if labels == nil {
		labels = map[string]string{}
	}

	raw, err := json.Marshal(labels)
	if err != nil {
		return "", fmt.Errorf("encode OCI labels JSON: %w", err)
	}

	return string(raw), nil
}
