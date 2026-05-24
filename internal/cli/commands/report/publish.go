// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package report

import (
	"context"
	"fmt"
	"strconv"

	"github.com/urfave/cli/v3"

	appsummary "github.com/diggsweden/reusable-ci/internal/app/summary"
	"github.com/diggsweden/reusable-ci/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/internal/domain/errs"
)

// publishGroup wires `reusable-ci report publish <target>` — every
// subcommand here appends a step-summary block describing one
// publish-target upload (App Store, Google Play, Maven Central,
// GitHub Packages).
func publishGroup() *cli.Command {
	return &cli.Command{
		Name:  "publish",
		Usage: "append a per-target publish summary to the step summary",
		Commands: []*cli.Command{
			publishAppstoreCmd(),
			publishGooglePlayCmd(),
			publishMavenCentralCmd(),
			publishGitHubPackagesCmd(),
		},
	}
}

func publishAppstoreCmd() *cli.Command {
	return &cli.Command{
		Name:  "appstore",
		Usage: "append the App Store Connect upload summary block to the step summary",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "ipa-file", Sources: cli.EnvVars("IPA_FILE"), Usage: "uploaded .ipa file path (shown in the summary row)"},
			&cli.StringFlag{Name: "platform", Sources: cli.EnvVars("PLATFORM"), Usage: "App Store platform (ios/tvos/macos)"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			&cli.BoolFlag{Name: "skip-validation", Sources: cli.EnvVars("SKIP_VALIDATION"), Usage: "altool --skip-validation was used during upload"},
			&cli.BoolFlag{Name: "submit-review", Sources: cli.EnvVars("SUBMIT_REVIEW"), Usage: "the upload was submitted for review"},
			&cli.StringFlag{Name: "request-id", Sources: cli.EnvVars("REQUEST_ID"), Usage: "altool request-id returned for the upload"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			for _, f := range []string{"ipa-file", "platform"} {
				if cmd.String(f) == "" {
					return fmt.Errorf("--%s is required: %w", f, errs.ErrUsage)
				}
			}

			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				return appsummary.AppStoreUpload(ctx, d.SummarySink, appsummary.AppStoreUploadInput{
					IPAFile:        cmd.String("ipa-file"),
					Platform:       cmd.String("platform"),
					SkipValidation: cmd.Bool("skip-validation"),
					SubmitReview:   cmd.Bool("submit-review"),
					RequestID:      cmd.String("request-id"),
				})
			})
		},
	}
}

func publishGooglePlayCmd() *cli.Command {
	return &cli.Command{
		Name:  "google-play",
		Usage: "append the Google Play upload summary block to the step summary",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "aab-file", Sources: cli.EnvVars("AAB_FILE"), Usage: "uploaded .aab file path (shown in the summary row)"},
			&cli.StringFlag{Name: "package-name", Sources: cli.EnvVars("PACKAGE_NAME"), Usage: "Android application id (e.g. com.example.app)"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			&cli.StringFlag{Name: "track", Sources: cli.EnvVars("TRACK"), Usage: "Google Play track (internal/alpha/beta/production)"},
			&cli.StringFlag{Name: "status", Sources: cli.EnvVars("STATUS"), Usage: "Google Play release status (draft/inProgress/completed)"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			&cli.StringFlag{Name: "release-name", Sources: cli.EnvVars("RELEASE_NAME"), Usage: "release name shown in Google Play"},
			&cli.StringFlag{
				Name:    "user-fraction",
				Usage:   "staged-rollout fraction (e.g. 0.1). Empty → row omitted.",
				Sources: cli.EnvVars("USER_FRACTION"),
			},
			&cli.IntFlag{Name: "priority", Sources: cli.EnvVars("PRIORITY"), Usage: "Google Play in-app-update priority (0–5)"},
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

			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				return appsummary.GooglePlayUpload(ctx, d.SummarySink, appsummary.GooglePlayUploadInput{
					AABFile:         cmd.String("aab-file"),
					PackageName:     cmd.String("package-name"),
					Track:           cmd.String("track"),
					Status:          cmd.String("status"),
					ReleaseName:     cmd.String("release-name"),
					UserFraction:    fraction,
					UserFractionSet: fractionSet,
					Priority:        cmd.Int("priority"),
				})
			})
		},
	}
}

func publishMavenCentralCmd() *cli.Command {
	return &cli.Command{
		Name:  "maven-central",
		Usage: "append the Maven Central publish summary block to the step summary",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "version", Sources: cli.EnvVars("VERSION"), Usage: "published Maven version (used in the summary row)"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			&cli.BoolFlag{Name: "is-snapshot", Sources: cli.EnvVars("IS_SNAPSHOT"), Usage: "the published version is a -SNAPSHOT"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			if cmd.String("version") == "" {
				return fmt.Errorf("--version is required: %w", errs.ErrUsage)
			}

			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				return appsummary.MavenCentralPublish(ctx, d.SummarySink, appsummary.MavenCentralPublishInput{
					Version:    cmd.String("version"),
					IsSnapshot: cmd.Bool("is-snapshot"),
				})
			})
		},
	}
}

func publishGitHubPackagesCmd() *cli.Command {
	return &cli.Command{
		Name:  "github-packages",
		Usage: "append the GitHub Packages publish summary block to the step summary",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "repository", Sources: cli.EnvVars("REPOSITORY", "GITHUB_REPOSITORY"), Usage: "\"owner/repo\" the package was published from"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			&cli.StringFlag{Name: "package-type", Sources: cli.EnvVars("PACKAGE_TYPE"), Usage: "GitHub Packages package type (maven/npm/container/…)"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			for _, f := range []string{"repository", "package-type"} {
				if cmd.String(f) == "" {
					return fmt.Errorf("--%s is required: %w", f, errs.ErrUsage)
				}
			}

			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				return appsummary.GitHubPackagesPublish(ctx, d.SummarySink, appsummary.GitHubPackagesPublishInput{
					Repository:  cmd.String("repository"),
					PackageType: cmd.String("package-type"),
				})
			})
		},
	}
}
