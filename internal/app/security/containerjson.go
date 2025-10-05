// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package security

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/ci"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/security"
	"github.com/diggsweden/reusable-ci/v3/internal/retry"
)

const (
	defaultRawContainerScanPlatform = "linux/amd64"
	defaultRawContainerScanTimeout  = "30m"
	defaultRawContainerScanAttempts = 3
	defaultRawContainerScanDelay    = 20 * time.Second
	defaultRawContainerScanScanners = "vuln"
)

// RawContainerScanInput drives RawContainerScan.
type RawContainerScanInput struct {
	ImageRef   string
	Platform   string
	Output     string
	Timeout    string
	Attempts   int
	RetryDelay time.Duration
	Scanners   string
}

// RawContainerScan runs Trivy against a remote image, validates the raw JSON shape, and writes result-path.
func RawContainerScan(ctx context.Context, trivy TrivyOps, sink ci.OutputSink, out, stderr io.Writer, in RawContainerScanInput) error {
	if err := validateRawContainerScanInput(in); err != nil {
		return err
	}

	platform := rawContainerScanDefault(in.Platform, defaultRawContainerScanPlatform)
	timeoutValue := rawContainerScanDefault(in.Timeout, defaultRawContainerScanTimeout)

	attempts := in.Attempts
	if attempts == 0 {
		attempts = defaultRawContainerScanAttempts
	}

	retryDelay := in.RetryDelay
	if retryDelay == 0 {
		retryDelay = defaultRawContainerScanDelay
	}

	scanners := rawContainerScanDefault(in.Scanners, defaultRawContainerScanScanners)

	scanErr := retry.Run(ctx, nil, attempts, retryDelay, func() error {
		// A report left by an earlier run must not be validated as this
		// run's: if it cannot be removed, nothing written afterwards can be
		// trusted to be fresh, and retrying does not change that.
		if err := os.Remove(in.Output); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return retry.Permanent(fmt.Errorf("remove previous trivy output %s: %w: %w", in.Output, err, errs.ErrValidation))
		}

		return runRawContainerTrivy(ctx, trivy, out, stderr, RawContainerScanInput{
			ImageRef: in.ImageRef,
			Platform: platform,
			Output:   in.Output,
			Timeout:  timeoutValue,
			Scanners: scanners,
		})
	}, retry.OnRetry(func(attempt, total int, _ time.Duration, _ error) {
		_, _ = fmt.Fprintf(stderr, "Trivy image scan failed (attempt %d/%d) for %s, retrying...\n", attempt, total, platform)
	}))
	if scanErr != nil {
		if errors.Is(scanErr, context.Canceled) || errors.Is(scanErr, context.DeadlineExceeded) {
			return fmt.Errorf("trivy image scan retry canceled: %w", scanErr)
		}

		return fmt.Errorf("trivy image scan failed for %s %s: %w", platform, in.ImageRef, scanErr)
	}

	return finishRawContainerScan(ctx, sink, out, platform, in)
}

// finishRawContainerScan validates the successful scan's JSON shape and emits result-path.
func finishRawContainerScan(ctx context.Context, sink ci.OutputSink, out io.Writer, platform string, in RawContainerScanInput) error {
	if err := validateRawContainerTrivyOutput(in.Output); err != nil {
		return err
	}

	_, _ = fmt.Fprintf(out, "Trivy image scan succeeded for %s %s\n", platform, in.ImageRef)

	if sink != nil {
		if err := sink.Set(ctx, "result-path", in.Output); err != nil {
			return err
		}
	}

	return nil
}

func validateRawContainerScanInput(in RawContainerScanInput) error {
	for name, value := range map[string]string{
		"image-ref": in.ImageRef,
		"output":    in.Output,
		"platform":  rawContainerScanDefault(in.Platform, defaultRawContainerScanPlatform),
		"timeout":   rawContainerScanDefault(in.Timeout, defaultRawContainerScanTimeout),
		"scanners":  rawContainerScanDefault(in.Scanners, defaultRawContainerScanScanners),
	} {
		if strings.TrimSpace(value) == "" {
			return fmt.Errorf("%s is required: %w", name, errs.ErrUsage)
		}

		if strings.ContainsAny(value, "\n\r") {
			return fmt.Errorf("%s must be a single line: %w", name, errs.ErrUsage)
		}
	}

	if in.Attempts < 0 {
		return fmt.Errorf("attempts must be a positive integer: %w", errs.ErrUsage)
	}

	if in.RetryDelay < 0 {
		return fmt.Errorf("retry delay must be positive: %w", errs.ErrUsage)
	}

	return nil
}

func runRawContainerTrivy(ctx context.Context, trivy TrivyOps, out, stderr io.Writer, in RawContainerScanInput) error {
	code, err := trivy.RunInherit(ctx, out, stderr,
		"image",
		"--timeout", in.Timeout,
		"--platform", in.Platform,
		"--scanners", in.Scanners,
		"--skip-version-check",
		"--format", "json",
		"--output", in.Output,
		in.ImageRef,
	)
	if err != nil {
		return fmt.Errorf("trivy image %s: %w", in.ImageRef, err)
	}

	if code != 0 {
		return fmt.Errorf("trivy image %s exited with status %d: %w", in.ImageRef, code, errs.ErrDependencyUnavailable)
	}

	return nil
}

func validateRawContainerTrivyOutput(path string) error {
	body, err := os.ReadFile(path) //nolint:gosec // caller-provided report path, same trust as CLI file flags.
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("trivy reported success but wrote no report to %s: %w", path, errs.ErrDependencyUnavailable)
	}

	if err != nil {
		return fmt.Errorf("read trivy output %s: %w", path, err)
	}

	if _, err := security.ParseTrivyReport(body); err != nil {
		return fmt.Errorf("parse trivy output %s: %w", path, err)
	}

	return nil
}

func rawContainerScanDefault(value, fallback string) string {
	if value != "" {
		return value
	}

	return fallback
}
