// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/internal/domain/container"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/imageledger"
	"github.com/diggsweden/reusable-ci/internal/domain/provider"
)

// ComputeMetadataInput drives `reusable-ci container metadata`. Mirrors
// the workflow env-var contract.
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
// internal/domain/container; this layer drives those steps, talks to
// the provider for the build context and OCI-label repo metadata, and
// writes the canonical outputs (tags, labels, version, json, ref-clean) to
// sink.
//
// All three platform adapters satisfy both interfaces — local returns
// empty RepoMetadata, which the OCI label assembly degrades to blank
// fields gracefully.
//
//nolint:cyclop // emits one output per metadata field (image, tag, labels).
func ComputeMetadata(
	ctx context.Context,
	prov provider.Provider,
	meta provider.RepoMetadataFetcher,
	sink ci.OutputSink,
	in ComputeMetadataInput,
) (*ComputeMetadataOutput, error) {
	if in.ImageName == "" {
		return nil, fmt.Errorf("image name is required: pass --image-name <ref> or set $IMAGE_NAME: %w", errs.ErrUsage)
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
		a, ok, err := container.Apply(r, mctx) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
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
		desc, license := resolveOCIFields(ctx, meta, evt, in.Description, in.License)

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

	// ref-clean tells the publish workflow whether this tag build's ref is
	// usable verbatim as an OCI tag. The promotion staging tag and ledger are
	// assembled from the raw ref (not the sanitizer), so they push only when
	// the ref is clean — an unusual tag degrades to "no promotion", never a
	// failed manifest push.
	refClean := mctx.RefType == provider.RefTypeTag && container.IsCleanRefTag(mctx.RefName)
	if err := sink.Set(ctx, "ref-clean", strconv.FormatBool(refClean)); err != nil {
		return nil, fmt.Errorf("set ref-clean: %w", err)
	}

	// staging-tag is the promotion candidate tag this build pushes, derived in
	// Go from the one source of the convention (imageledger.DeriveTags) so the
	// workflow doesn't re-encode "staging-<tag>" in YAML. Empty unless the ref
	// is a clean tag, so the merge step pushes it only when promotable.
	var stagingTag string
	if refClean {
		_, stagingTag = imageledger.DeriveTags(in.ImageName, mctx.RefName)
	}

	if err := sink.Set(ctx, "staging-tag", stagingTag); err != nil {
		return nil, fmt.Errorf("set staging-tag: %w", err)
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
	prov provider.RepoMetadataFetcher,
	evt *provider.EventContext,
	descOverride, licOverride string,
) (string, string) {
	description := descOverride
	license := licOverride

	if description != "" && license != "" {
		return description, license
	}

	if evt.Repo == "" {
		return description, license
	}

	md, err := prov.FetchRepoMetadata(ctx, evt.Repo)
	if err != nil {
		// Network / auth errors are not fatal — labels are best-effort —
		// but we surface them via slog so operators can correlate empty
		// labels with a real failure. The build proceeds.
		slog.Warn("container metadata: FetchRepoMetadata failed; labels left blank",
			"repo", evt.Repo, "err", err)

		return description, license
	}

	if description == "" {
		description = md.Description
	}

	if license == "" {
		license = md.LicenseSPDX
	}

	return description, license
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
