// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package container

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/internal/domain/container"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/provider"
)

// ComputeMetadataInput drives `reusable-ci container metadata`. Mirrors
// the env-var contract of scripts/container/compute-image-metadata.sh.
//
// Description and License act as overrides for the OCI labels: when
// non-empty, the use case skips the Provider.FetchRepoMetadata call.
// EmitLabels gates whether labels are produced at all.
type ComputeMetadataInput struct {
	ImageName   string
	TagRules    string
	Flavor      string
	EmitLabels  bool
	Description string
	License     string
	// Now is the timestamp baked into org.opencontainers.image.created.
	// Tests pass a fixed time; the CLI passes time.Now.
	Now time.Time
}

// ComputeMetadataOutput is the structured result of computing tags +
// labels. Returned to callers that want to inspect the values; the
// CLI consumer additionally writes them to the OutputSink.
type ComputeMetadataOutput struct {
	Tags    []string
	Labels  []container.Label
	Primary string
	JSON    container.JSONOutput
}

// ComputeMetadata is the full use case orchestrator. Pure-domain logic
// (parsing, applying rules, label assembly, JSON shape) lives in
// internal/domain/container; this layer drives those steps, talks to the
// Provider for repo metadata when needed, and writes the four canonical
// outputs (tags, labels, version, json) to sink.
func ComputeMetadata(
	ctx context.Context,
	prov provider.Provider,
	sink ci.OutputSink,
	in ComputeMetadataInput,
) (*ComputeMetadataOutput, error) {
	if in.ImageName == "" {
		return nil, fmt.Errorf("IMAGE_NAME is required: %w", errs.ErrUsage)
	}
	if err := container.ValidateFlavor(in.Flavor); err != nil {
		return nil, err
	}

	rules, err := container.ParseRules(in.TagRules)
	if err != nil {
		return nil, err
	}

	evt, err := prov.ResolveContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("resolve event context: %w", err)
	}
	mctx := container.FromEventContext(evt)

	var applied []container.AppliedTag
	for _, r := range rules {
		a, ok, err := container.Apply(r, mctx)
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		applied = append(applied, a)
	}

	tags := container.FormatTags(in.ImageName, applied)
	primary := container.PrimaryVersion(applied)

	var labels []container.Label
	if in.EmitLabels {
		desc, license, err := resolveOCIFields(ctx, prov, evt, in.Description, in.License)
		if err != nil {
			return nil, err
		}
		now := in.Now
		if now.IsZero() {
			now = time.Now()
		}
		labels = container.BuildLabels(container.LabelInputs{
			ImageName:   in.ImageName,
			RepoURL:     evt.RepoURL,
			SHA:         evt.SHA,
			Primary:     primary,
			Description: desc,
			License:     license,
			CreatedAt:   now,
		})
	}

	jsonOut := container.BuildJSONOutput(tags, labels)

	if err := writeOutputs(ctx, sink, tags, labels, primary, jsonOut, in.EmitLabels); err != nil {
		return nil, err
	}

	return &ComputeMetadataOutput{
		Tags: tags, Labels: labels, Primary: primary, JSON: jsonOut,
	}, nil
}

// resolveOCIFields fills the description / license OCI fields, taking
// the operator-provided override first, falling back to a single
// FetchRepoMetadata call. The provider returning empty strings is
// non-fatal: labels just stay empty (best-effort).
func resolveOCIFields(
	ctx context.Context,
	prov provider.Provider,
	evt *provider.EventContext,
	descOverride, licOverride string,
) (description, license string, err error) {
	description = descOverride
	license = licOverride
	if description != "" && license != "" {
		return description, license, nil
	}
	if evt.Repo == "" {
		return description, license, nil
	}
	md, err := prov.FetchRepoMetadata(ctx, evt.Repo)
	if err != nil {
		// Network / auth errors are not fatal — labels are best-effort —
		// but we surface them via slog so operators can correlate empty
		// labels with a real failure. The build proceeds.
		slog.Warn("container metadata: FetchRepoMetadata failed; labels left blank",
			"repo", evt.Repo, "err", err)
		return description, license, nil
	}
	if description == "" {
		description = md.Description
	}
	if license == "" {
		license = md.LicenseSPDX
	}
	return description, license, nil
}

func writeOutputs(
	ctx context.Context,
	sink ci.OutputSink,
	tags []string,
	labels []container.Label,
	primary string,
	jsonOut container.JSONOutput,
	emitLabels bool,
) error {
	if err := sink.Set(ctx, "version", primary); err != nil {
		return err
	}
	if len(tags) > 0 {
		if err := sink.SetMultiline(ctx, "tags", tags); err != nil {
			return err
		}
	} else {
		if err := sink.Set(ctx, "tags", ""); err != nil {
			return err
		}
	}
	if emitLabels {
		lines := make([]string, 0, len(labels))
		for _, l := range labels {
			lines = append(lines, l.String())
		}
		if err := sink.SetMultiline(ctx, "labels", lines); err != nil {
			return err
		}
	}
	jsonStr, err := marshalJSONCompact(jsonOut)
	if err != nil {
		return err
	}
	return sink.Set(ctx, "json", jsonStr)
}

func marshalJSONCompact(o container.JSONOutput) (string, error) {
	b, err := o.MarshalJSON()
	if err != nil {
		return "", err
	}
	// MarshalJSON already produces compact output for our struct.
	return strings.TrimSpace(string(b)), nil
}
