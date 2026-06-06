// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/internal/domain/pipeline"
	domainrelease "github.com/diggsweden/reusable-ci/internal/domain/release"
)

// ArtifactDownloader downloads one CI artifact by exact name.
type ArtifactDownloader interface {
	DownloadArtifact(ctx context.Context, in domainrelease.ArtifactDownloadInput) error
}

// DownloadArtifactsInput drives DownloadArtifacts.
type DownloadArtifactsInput struct {
	ArtifactTransferPlanJSON string
	RunID                    string
	Repository               string
}

// DownloadArtifacts downloads release artifacts from an explicit transfer plan.
//nolint:cyclop // download flow: list → per-artifact filter + unzip + place.
func DownloadArtifacts(ctx context.Context, dl ArtifactDownloader, stderr io.Writer, in DownloadArtifactsInput) error {
	if strings.TrimSpace(in.ArtifactTransferPlanJSON) == "" {
		return fmt.Errorf("artifact-transfer-plan-json is required: %w", errs.ErrUsage)
	}

	var plan pipeline.ArtifactTransferPlan
	if err := json.Unmarshal([]byte(in.ArtifactTransferPlanJSON), &plan); err != nil {
		return fmt.Errorf("parse artifact-transfer-plan-json: %w: %w", err, errs.ErrInvalidConfig)
	}

	if plan.Version != pipeline.ArtifactTransferPlanVersion {
		return fmt.Errorf("artifact transfer plan has unsupported version %d: %w", plan.Version, errs.ErrInvalidConfig)
	}

	for _, item := range plan.Items {
		if err := pipeline.ValidateArtifactTransferItem(item); err != nil {
			return err
		}

		name, err := transferArtifactName(item, in.RunID)
		if err != nil {
			return err
		}

		err = dl.DownloadArtifact(ctx, domainrelease.ArtifactDownloadInput{
			RunID:      in.RunID,
			Repository: in.Repository,
			Name:       name,
			Dir:        item.Path,
		})
		if err == nil {
			if stderr != nil {
				_, _ = fmt.Fprintf(stderr, "✓ Downloaded %s → %s\n", name, item.Path)
			}

			continue
		}

		if item.Required {
			return err
		}

		if stderr != nil {
			_, _ = fmt.Fprintf(stderr, "warning: optional artifact %q was not downloaded: %v\n", name, err)
		}
	}

	return nil
}

func transferArtifactName(item pipeline.ArtifactTransfer, runID string) (string, error) {
	if item.Name != "" {
		return item.Name, nil
	}

	if strings.Contains(item.NameTemplate, "{run_id}") && runID == "" {
		return "", fmt.Errorf("run-id is required for artifact transfer %q: %w", item.NameTemplate, errs.ErrUsage)
	}

	name := strings.ReplaceAll(item.NameTemplate, "{run_id}", runID)
	if strings.ContainsAny(name, "{}") {
		return "", fmt.Errorf("artifact transfer name template %q contains unresolved placeholders: %w", item.NameTemplate, errs.ErrInvalidConfig)
	}

	return name, nil
}
