// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package publish

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/internal/adapters/gpg"
	"github.com/diggsweden/reusable-ci/internal/adapters/gradle"
	apppublish "github.com/diggsweden/reusable-ci/internal/app/publish"
	domainpublish "github.com/diggsweden/reusable-ci/internal/domain/publish"
)

// gradleCmd groups the Gradle-toolchain publish path. The group is the
// toolchain and the destination is a flag, matching `build gradle` /
// `build maven` / `build npm`: the destination is the thin dimension —
// it only selects a credential binding set and a task.
func gradleCmd() *cli.Command {
	return &cli.Command{
		Name:  "gradle",
		Usage: "publish gradle artifacts from source",
		Commands: []*cli.Command{
			gradleDeployCmd(),
		},
	}
}

func gradleDeployCmd() *cli.Command {
	return &cli.Command{
		Name:  "deploy",
		Usage: "run the target's gradle publish task with credentials bound as ORG_GRADLE_PROJECT_* properties",
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "target",
				Required: true,
				Sources:  cli.EnvVars("PUBLISH_TARGET"),
				Usage:    "publish destination: github-packages or maven-central",
			},
			&cli.StringFlag{
				Name:    "tasks",
				Sources: cli.EnvVars("GRADLE_PUBLISH_TASKS"),
				Usage:   "override the target-derived publish task (e.g. publishToMavenCentral for the vanniktech plugin)",
			},
			&cli.StringFlag{
				Name:    "working-directory",
				Sources: cli.EnvVars("WORKING_DIRECTORY"),
				Usage:   "directory containing ./gradlew",
			},
			&cli.StringFlag{
				Name:    "fingerprint",
				Sources: cli.EnvVars("GPG_FINGERPRINT"),
				Usage:   "signing key fingerprint emitted by 'release gpg import' (required for maven-central)",
			},
			&cli.StringFlag{
				Name:    "key-id",
				Sources: cli.EnvVars("GPG_KEY_ID"),
				Usage:   "signing key id emitted by 'release gpg import' (required for maven-central)",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			target, err := domainpublish.ParseGradleTarget(cmd.String("target"))
			if err != nil {
				return err
			}

			return apppublish.GradleDeploy(ctx, gradle.New(), gpg.New(), os.Stderr, os.Stderr,
				apppublish.GradleDeployInput{
					Target:           target,
					Tasks:            cmd.String("tasks"),
					WorkingDirectory: cmd.String("working-directory"),
					Fingerprint:      cmd.String("fingerprint"),
					KeyID:            cmd.String("key-id"),
				})
		},
	}
}
