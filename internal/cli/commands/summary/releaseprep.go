// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package summary

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/internal/adapters/git"
	appsummary "github.com/diggsweden/reusable-ci/internal/app/summary"
	"github.com/diggsweden/reusable-ci/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/internal/domain/projecttype"
	"github.com/diggsweden/reusable-ci/internal/domain/provider"
	domainsummary "github.com/diggsweden/reusable-ci/internal/domain/summary"
)

func prerequisitesCmd() *cli.Command {
	return &cli.Command{
		Name:  "prerequisites",
		Usage: "append the release prerequisites validation report (tag/commit info, secrets, validations)",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "tag-name", Sources: cli.EnvVars("TAG_NAME")},
			&cli.StringFlag{Name: "commit-sha", Sources: cli.EnvVars("COMMIT_SHA")},
			&cli.StringFlag{Name: "ref-type", Sources: cli.EnvVars("REF_TYPE")},
			&cli.StringFlag{Name: "artifacts", Sources: cli.EnvVars("ARTIFACTS")},
			&cli.StringFlag{Name: "project-types", Sources: cli.EnvVars("PROJECT_TYPES")},
			&cli.StringFlag{Name: "build-types", Sources: cli.EnvVars("BUILD_TYPES")},
			&cli.StringFlag{Name: "container-registry", Sources: cli.EnvVars("CONTAINER_REGISTRY")},
			&cli.BoolFlag{Name: "sign-artifacts", Sources: cli.EnvVars("SIGN_ARTIFACTS")},
			&cli.BoolFlag{Name: "check-authorization", Sources: cli.EnvVars("CHECK_AUTHORIZATION")},
			&cli.StringFlag{Name: "actor", Sources: cli.EnvVars("ACTOR")},
			&cli.StringFlag{Name: "job-status", Sources: cli.EnvVars("JOB_STATUS")},
			&cli.StringFlag{Name: "publish-to", Sources: cli.EnvVars("PUBLISH_TO")},
			&cli.BoolFlag{Name: "has-release-gpg-private-key", Sources: cli.EnvVars("HAS_RELEASE_GPG_PRIVATE_KEY")},
			&cli.BoolFlag{Name: "has-release-gpg-passphrase", Sources: cli.EnvVars("HAS_RELEASE_GPG_PASSPHRASE")},
			&cli.BoolFlag{Name: "has-release-token", Sources: cli.EnvVars("HAS_RELEASE_TOKEN")},
			&cli.BoolFlag{Name: "has-release-gpg-public-key", Sources: cli.EnvVars("HAS_RELEASE_GPG_PUBLIC_KEY")},
			&cli.BoolFlag{Name: "has-maven-central-username", Sources: cli.EnvVars("HAS_MAVEN_CENTRAL_USERNAME")},
			&cli.BoolFlag{Name: "has-maven-central-password", Sources: cli.EnvVars("HAS_MAVEN_CENTRAL_PASSWORD")},
			&cli.BoolFlag{Name: "has-npm-token", Sources: cli.EnvVars("HAS_NPM_TOKEN")},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			d, err := deps.Build(ctx)
			if err != nil {
				return err
			}
			return appsummary.Prerequisites(ctx, d.SummarySink, git.New(), appsummary.PrerequisitesInput{
				TagName:                 cmd.String("tag-name"),
				CommitSHA:               cmd.String("commit-sha"),
				RefType:                 provider.RefType(cmd.String("ref-type")),
				Artifacts:               cmd.String("artifacts"),
				ProjectTypes:            cmd.String("project-types"),
				BuildTypes:              cmd.String("build-types"),
				ContainerRegistry:       cmd.String("container-registry"),
				SignArtifacts:           cmd.Bool("sign-artifacts"),
				CheckAuthorization:      cmd.Bool("check-authorization"),
				Actor:                   cmd.String("actor"),
				JobStatus:               domainsummary.NormalizeResult(cmd.String("job-status")),
				PublishTo:               cmd.String("publish-to"),
				HasReleaseGPGPrivateKey: cmd.Bool("has-release-gpg-private-key"),
				HasReleaseGPGPassphrase: cmd.Bool("has-release-gpg-passphrase"),
				HasReleaseToken:         cmd.Bool("has-release-token"),
				HasReleaseGPGPublicKey:  cmd.Bool("has-release-gpg-public-key"),
				HasMavenCentralUsername: cmd.Bool("has-maven-central-username"),
				HasMavenCentralPassword: cmd.Bool("has-maven-central-password"),
				HasNPMToken:             cmd.Bool("has-npm-token"),
			})
		},
	}
}

func devReleaseCmd() *cli.Command {
	return &cli.Command{
		Name:  "dev-release",
		Usage: "append the dev-release step-summary (npm install snippet + container/npm job rows)",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "project-type", Sources: cli.EnvVars("PROJECT_TYPE")},
			&cli.StringFlag{Name: "release-ref", Sources: cli.EnvVars("RELEASE_REF")},
			&cli.StringFlag{Name: "release-sha", Sources: cli.EnvVars("RELEASE_SHA")},
			&cli.StringFlag{Name: "release-actor", Sources: cli.EnvVars("RELEASE_ACTOR")},
			&cli.StringFlag{Name: "release-repository", Sources: cli.EnvVars("RELEASE_REPOSITORY")},
			&cli.StringFlag{Name: "run-url", Sources: cli.EnvVars("CI_RUN_URL")},
			&cli.StringFlag{Name: "publish-stage-result-json", Sources: cli.EnvVars("PUBLISH_STAGE_RESULT_JSON")},
			&cli.StringFlag{Name: "dev-artifacts-json", Sources: cli.EnvVars("DEV_ARTIFACTS_JSON")},
			&cli.StringFlag{Name: "server-url", Sources: cli.EnvVars("CI_SERVER_URL")},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			d, err := deps.Build(ctx)
			if err != nil {
				return err
			}
			return appsummary.DevReleaseSummary(ctx, d.SummarySink, os.Stdout, appsummary.DevReleaseSummaryInput{
				ProjectType:       projecttype.Type(cmd.String("project-type")),
				ReleaseRef:        cmd.String("release-ref"),
				ReleaseSHA:        cmd.String("release-sha"),
				ReleaseActor:      cmd.String("release-actor"),
				ReleaseRepository: cmd.String("release-repository"),
				RunURL:            cmd.String("run-url"),
				PublishStageJSON:  cmd.String("publish-stage-result-json"),
				DevArtifactsJSON:  cmd.String("dev-artifacts-json"),
				Platform:          d.Platform,
				ServerURL:         cmd.String("server-url"),
			})
		},
	}
}
