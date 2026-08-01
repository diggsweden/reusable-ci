// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package config

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/diggsweden/reusable-ci/v3/internal/cliio"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/pipeline"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
)

// EmitConfigPlanInput drives `config parse-artifacts`.
type EmitConfigPlanInput struct {
	Path string // path to artifacts.yml
	// FS overrides read access for tests. When nil, Path is read from the real OS filesystem.
	FS fs.FS
}

// EmitConfigPlan reads + validates artifacts.yml, derives the typed
// config-plan contract, and writes it through OutputSink + SummarySink.
//
//nolint:cyclop // emits one sink output per typed-output field of the ConfigPlan.
func EmitConfigPlan(
	ctx context.Context,
	sink ci.OutputSink,
	summary ci.SummarySink,
	stderr io.Writer,
	annot output.Annotator,
	in EmitConfigPlanInput,
) error {
	if in.Path == "" {
		return fmt.Errorf("artifacts.yml path is required: pass --file <path> or set $ARTIFACTS_CONFIG: %w", errs.ErrUsage)
	}

	cfg, err := loadConfig(in, annot)
	if err != nil {
		return err
	}

	if len(cfg.Artifacts) == 0 {
		return fmt.Errorf("no artifacts found in %s: %w", in.Path, errs.ErrInvalidConfig)
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

	if err := emitValueOutput(ctx, sink, "config-plan-json", pipeline.NewConfigPlan(cfg)); err != nil {
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

// loadConfig reads + parses the artifacts.yml at in.Path. When the
// file is absent, it falls back to AutoDeriveConfig — the operator
// gets a working plan from a single root manifest without writing any
// configuration. annot.Noticef announces which path was taken so the
// behaviour is visible in CI logs.
func loadConfig(in EmitConfigPlanInput, annot output.Annotator) (*config.Config, error) {
	data, err := readArtifactsFile(in)
	if err == nil {
		cfg, parseErr := config.Parse(data)
		if parseErr != nil {
			return nil, fmt.Errorf("parse %s: %w: %w", in.Path, parseErr, errs.ErrInvalidConfig)
		}

		return cfg, nil
	}

	// File missing — try auto-derive from a single root manifest.
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("read %s: %w", in.Path, err)
	}

	cfg, deriveErr := AutoDeriveConfig(in.FS, ".")
	if deriveErr != nil {
		return nil, deriveErr
	}

	annot.Noticef("No %s found; auto-derived a single-artifact plan from the root manifest.", in.Path)

	return cfg, nil
}

func readArtifactsFile(in EmitConfigPlanInput) ([]byte, error) {
	if in.FS == nil {
		return cliio.ReadFile(in.Path)
	}
	// An in-memory fs.FS doesn't carry stdin semantics; only honour
	// the "-" sentinel when reading from the real filesystem above.
	path := filepath.ToSlash(filepath.Clean(in.Path))
	path = strings.TrimPrefix(path, "./")
	path = strings.TrimPrefix(path, "/")

	return fs.ReadFile(in.FS, path)
}

func emitValueOutput[T any](ctx context.Context, sink ci.OutputSink, key string, value T) error {
	b, err := json.Marshal(value)
	if err != nil {
		return err
	}

	return sink.Set(ctx, key, string(b))
}

func renderConfigSummary(cfg *config.Config) string {
	var b strings.Builder //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	b.WriteString("## Configuration\n")

	for _, a := range cfg.Artifacts {
		_, _ = fmt.Fprintf(&b, "### %s\n", a.Name)
		_, _ = fmt.Fprintf(&b, "- **Type:** %s\n", a.ProjectType)
		_, _ = fmt.Fprintf(&b, "- **Publish To:** %s\n", joinPublishTo(a.PublishTo))
		_, _ = fmt.Fprintf(&b, "- **Directory:** %s\n\n", a.WorkingDirectory)
	}

	if len(cfg.Containers) > 0 {
		b.WriteString("\n## Containers\n")

		for _, c := range cfg.Containers {
			_, _ = fmt.Fprintf(&b, "### %s\n", c.Name)
			_, _ = fmt.Fprintf(&b, "- **From:** %s\n", strings.Join(c.From, ", "))
			_, _ = fmt.Fprintf(&b, "- **Artifact Types:** %s\n", joinProjectTypes(c.ArtifactTypes))
			_, _ = fmt.Fprintf(&b, "- **Containerfile:** %s\n\n", c.ContainerFile)
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
