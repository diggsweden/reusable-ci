// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package container

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/diggsweden/reusable-ci/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

// PlatformPlanInput drives `container platform-plan`.
type PlatformPlanInput struct {
	Platforms string
	Platform  string
}

// PlatformPlanOutput is the normalized platform matrix and optional arch suffix.
type PlatformPlanOutput struct {
	Platforms []string `json:"platforms"`
	Suffix    string   `json:"suffix,omitempty"`
}

// PlatformPlan writes platform matrix/suffix values to the output sink.
//nolint:cyclop // selects platforms per (CI provider, refType, override) combination.
func PlatformPlan(ctx context.Context, sink ci.OutputSink, w io.Writer, in PlatformPlanInput) (*PlatformPlanOutput, error) { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	platforms, err := splitPlatforms(in.Platforms)
	if err != nil {
		return nil, err
	}

	platform := strings.TrimSpace(in.Platform)
	if platform != "" {
		if err := validateNativePlatform(platform); err != nil {
			return nil, err
		}
	}

	out := &PlatformPlanOutput{
		Platforms: platforms,
		Suffix:    PlatformSuffix(platform),
	}
	if len(out.Platforms) > 0 {
		body, err := json.Marshal(out.Platforms)
		if err != nil {
			return nil, err
		}

		if err := sink.Set(ctx, "platforms-json", string(body)); err != nil {
			return nil, err
		}
	}

	if out.Suffix != "" {
		if err := sink.Set(ctx, "suffix", out.Suffix); err != nil {
			return nil, err
		}
	}

	if w != nil {
		if len(out.Platforms) > 0 {
			_, _ = fmt.Fprintf(w, "platforms: %s\n", strings.Join(out.Platforms, ","))
		}

		if out.Suffix != "" {
			_, _ = fmt.Fprintf(w, "suffix: %s\n", out.Suffix)
		}
	}

	return out, nil
}

func splitPlatforms(value string) ([]string, error) {
	var out []string

	for _, raw := range strings.Split(value, ",") {
		platform := strings.TrimSpace(raw)
		if platform == "" {
			continue
		}

		if err := validateNativePlatform(platform); err != nil {
			return nil, err
		}

		out = append(out, platform)
	}

	if len(out) == 0 && strings.TrimSpace(value) == "" {
		out = append(out, "linux/amd64")
	}

	if len(out) == 0 {
		return nil, fmt.Errorf("platforms is empty: %w", errs.ErrUsage)
	}

	return out, nil
}

func validateNativePlatform(platform string) error {
	switch platform {
	case "linux/amd64", "linux/arm64":
		return nil
	default:
		return fmt.Errorf("unsupported container platform %q: native builds currently support linux/amd64 and linux/arm64: %w", platform, errs.ErrValidation)
	}
}

// PlatformSuffix returns the per-platform artifact/cache suffix.
func PlatformSuffix(platform string) string {
	platform = strings.TrimSpace(platform)
	platform = strings.TrimPrefix(platform, "linux/")
	platform = strings.ReplaceAll(platform, "/", "-")

	return platform
}
