// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package summary

import (
	"context"
	"fmt"
	"strconv"

	"github.com/urfave/cli/v3"

	appsummary "github.com/diggsweden/reusable-ci/internal/app/summary"
	"github.com/diggsweden/reusable-ci/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

func googlePlayUploadCmd() *cli.Command {
	return &cli.Command{
		Name:  "google-play-upload",
		Usage: "append the Google Play upload summary block to the step summary",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "aab-file", Sources: cli.EnvVars("AAB_FILE")},
			&cli.StringFlag{Name: "package-name", Sources: cli.EnvVars("PACKAGE_NAME")},
			&cli.StringFlag{Name: "track", Sources: cli.EnvVars("TRACK")},
			&cli.StringFlag{Name: "status", Sources: cli.EnvVars("STATUS")},
			&cli.StringFlag{Name: "release-name", Sources: cli.EnvVars("RELEASE_NAME")},
			&cli.StringFlag{
				Name:    "user-fraction",
				Usage:   "staged-rollout fraction (e.g. 0.1). Empty → row omitted.",
				Sources: cli.EnvVars("USER_FRACTION"),
			},
			&cli.IntFlag{Name: "priority", Sources: cli.EnvVars("PRIORITY")},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			for _, f := range []string{"aab-file", "package-name", "track", "status"} {
				if cmd.String(f) == "" {
					return fmt.Errorf("--%s is required: %w", f, errs.ErrUsage)
				}
			}

			fractionStr := cmd.String("user-fraction")
			fraction := 0.0
			fractionSet := false
			if fractionStr != "" {
				v, err := strconv.ParseFloat(fractionStr, 64)
				if err != nil {
					return fmt.Errorf("--user-fraction: %w: %w", err, errs.ErrUsage)
				}
				fraction = v
				fractionSet = true
			}

			d, err := deps.Build(ctx)
			if err != nil {
				return err
			}
			return appsummary.GooglePlayUpload(ctx, d.SummarySink, appsummary.GooglePlayUploadInput{
				AABFile:         cmd.String("aab-file"),
				PackageName:     cmd.String("package-name"),
				Track:           cmd.String("track"),
				Status:          cmd.String("status"),
				ReleaseName:     cmd.String("release-name"),
				UserFraction:    fraction,
				UserFractionSet: fractionSet,
				Priority:        int(cmd.Int("priority")),
			})
		},
	}
}

func appStoreUploadCmd() *cli.Command {
	return &cli.Command{
		Name:  "appstore-upload",
		Usage: "append the App Store Connect upload summary block to the step summary",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "ipa-file", Sources: cli.EnvVars("IPA_FILE")},
			&cli.StringFlag{Name: "platform", Sources: cli.EnvVars("PLATFORM")},
			&cli.BoolFlag{Name: "skip-validation", Sources: cli.EnvVars("SKIP_VALIDATION")},
			&cli.BoolFlag{Name: "submit-review", Sources: cli.EnvVars("SUBMIT_REVIEW")},
			&cli.StringFlag{Name: "request-id", Sources: cli.EnvVars("REQUEST_ID")},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			for _, f := range []string{"ipa-file", "platform"} {
				if cmd.String(f) == "" {
					return fmt.Errorf("--%s is required: %w", f, errs.ErrUsage)
				}
			}
			d, err := deps.Build(ctx)
			if err != nil {
				return err
			}
			return appsummary.AppStoreUpload(ctx, d.SummarySink, appsummary.AppStoreUploadInput{
				IPAFile:        cmd.String("ipa-file"),
				Platform:       cmd.String("platform"),
				SkipValidation: cmd.Bool("skip-validation"),
				SubmitReview:   cmd.Bool("submit-review"),
				RequestID:      cmd.String("request-id"),
			})
		},
	}
}
