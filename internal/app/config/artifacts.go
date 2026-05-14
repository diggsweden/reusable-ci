// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package config

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/internal/domain/config"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/output"
	"github.com/diggsweden/reusable-ci/internal/domain/projecttype"
)

// ParseArtifactsInput drives `config parse-artifacts`.
type ParseArtifactsInput struct {
	Path string // path to artifacts.yml
	// FS overrides read access for tests. When nil, Path is read from the real OS filesystem.
	FS fs.FS
}

// ParseArtifacts reads + validates artifacts.yml, computes
// effective-sboms / pipeline-sboms / per-type / per-publish-target
// outputs and writes them through OutputSink + SummarySink. Mirrors
// scripts/config/parse-artifacts-config.sh end-to-end.
func ParseArtifacts(
	ctx context.Context,
	sink ci.OutputSink,
	summary ci.SummarySink,
	stderr io.Writer,
	annot output.Annotator,
	in ParseArtifactsInput,
) error {
	if in.Path == "" {
		return fmt.Errorf("File not found: (empty path): %w", errs.ErrUsage)
	}
	data, err := readArtifactsFile(in)
	if err != nil {
		return fmt.Errorf("File not found: %s: %w", in.Path, errs.ErrMissingInput)
	}
	cfg, err := config.Parse(data)
	if err != nil {
		return fmt.Errorf("parse %s: %w: %w", in.Path, err, errs.ErrInvalidConfig)
	}
	if len(cfg.Artifacts) == 0 {
		return fmt.Errorf("No artifacts found in %s: %w", in.Path, errs.ErrInvalidConfig)
	}
	if err := config.Validate(cfg); err != nil {
		return fmt.Errorf("%w: %w", err, errs.ErrInvalidConfig)
	}
	for _, w := range config.Warnings(cfg) {
		annot.Warningf("%s", w)
	}

	if err := config.Derive(cfg); err != nil {
		return err
	}

	// Emit outputs.
	if err := emitArrayOutput(ctx, sink, "artifacts", cfg.Artifacts); err != nil {
		return err
	}
	if err := emitArrayOutput(ctx, sink, "containers", cfg.Containers); err != nil {
		return err
	}
	if err := emitPerTypeOutputs(ctx, sink, cfg.Artifacts); err != nil {
		return err
	}
	if err := emitPerPublishTargetOutputs(ctx, sink, cfg.Artifacts); err != nil {
		return err
	}
	if err := sink.Set(ctx, "any-require-authorization", strconv.FormatBool(config.AnyRequireAuthorization(cfg.Artifacts))); err != nil {
		return err
	}
	if err := sink.Set(ctx, "pipeline-sboms", config.PipelineSBOMs(cfg.Artifacts)); err != nil {
		return err
	}

	// Markdown summary block.
	if summary != nil {
		if err := summary.Append(ctx, renderConfigSummary(cfg)); err != nil {
			return err
		}
	}
	return nil
}

func readArtifactsFile(in ParseArtifactsInput) ([]byte, error) {
	if in.FS == nil {
		return os.ReadFile(in.Path)
	}
	path := filepath.ToSlash(filepath.Clean(in.Path))
	path = strings.TrimPrefix(path, "./")
	path = strings.TrimPrefix(path, "/")
	return fs.ReadFile(in.FS, path)
}

func emitArrayOutput[T any](ctx context.Context, sink ci.OutputSink, key string, value []T) error {
	if value == nil {
		value = make([]T, 0)
	}
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}
	return sink.Set(ctx, key, string(b))
}

// emitPerTypeOutputs writes one `<type>-artifacts` output per
// ValidProjectType, with normalised key names ("gradle-android" →
// "gradleandroid"). Mirrors output_artifacts_by_type in bash.
func emitPerTypeOutputs(ctx context.Context, sink ci.OutputSink, artifacts []config.Artifact) error {
	for _, pt := range config.ValidProjectTypes {
		filtered := config.ArtifactsByProjectType(artifacts, pt)
		key := normaliseName(string(pt)) + "-artifacts"
		if err := emitArrayOutput(ctx, sink, key, filtered); err != nil {
			return err
		}
	}
	return nil
}

// emitPerPublishTargetOutputs writes one `<target>-artifacts` output per
// ValidPublishTarget. github-packages excludes maven applications
// (Maven apps shouldn't publish to GHP — warning is in Warnings()).
func emitPerPublishTargetOutputs(ctx context.Context, sink ci.OutputSink, artifacts []config.Artifact) error {
	for _, pt := range config.ValidPublishTargets {
		filtered := config.ArtifactsByPublishTarget(artifacts, pt)
		key := normaliseName(string(pt)) + "-artifacts"
		if err := emitArrayOutput(ctx, sink, key, filtered); err != nil {
			return err
		}
	}
	return nil
}

// normaliseName drops dashes — "gradle-android" → "gradleandroid",
// "github-packages" → "githubpackages". Mirrors normalize_name in bash.
func normaliseName(s string) string {
	return strings.ReplaceAll(s, "-", "")
}

func renderConfigSummary(cfg *config.Config) string {
	var b strings.Builder
	b.WriteString("## Configuration\n")
	for _, a := range cfg.Artifacts {
		fmt.Fprintf(&b, "### %s\n", a.Name)
		fmt.Fprintf(&b, "- **Type:** %s\n", a.ProjectType)
		fmt.Fprintf(&b, "- **Publish To:** %s\n", joinPublishTo(a.PublishTo))
		fmt.Fprintf(&b, "- **Directory:** %s\n\n", a.WorkingDirectory)
	}
	if len(cfg.Containers) > 0 {
		b.WriteString("\n## Containers\n")
		for _, c := range cfg.Containers {
			fmt.Fprintf(&b, "### %s\n", c.Name)
			fmt.Fprintf(&b, "- **From:** %s\n", strings.Join(c.From, ", "))
			fmt.Fprintf(&b, "- **Artifact Types:** %s\n", joinProjectTypes(c.ArtifactTypes))
			fmt.Fprintf(&b, "- **Containerfile:** %s\n\n", c.ContainerFile)
		}
	}
	return b.String()
}

func joinPublishTo(targets []config.PublishTarget) string {
	parts := make([]string, len(targets))
	for i, t := range targets {
		parts[i] = string(t)
	}
	return strings.Join(parts, ", ")
}

func joinProjectTypes(types []projecttype.Type) string {
	parts := make([]string, len(types))
	for i, t := range types {
		parts[i] = string(t)
	}
	return strings.Join(parts, ", ")
}
