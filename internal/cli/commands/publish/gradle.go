// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package publish

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/gpg"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/gradle"
	apppublish "github.com/diggsweden/reusable-ci/v3/internal/app/publish"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/commonflags"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	domainpublish "github.com/diggsweden/reusable-ci/v3/internal/domain/publish"
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
		Description: `Publishes by running the project's OWN maven-publish configuration, so unlike
   the maven path it does not consume a downloaded artifact — Gradle needs the
   project to produce its publications, and the job rebuilds from source.

   The task is derived per target as publishAllPublicationsTo<Name>Repository,
   deliberately not the bare "publish" task (which would push every publication
   to every configured repository). Override it with --tasks when the project
   uses a plugin that names its task differently.

   Credentials are resolved and checked BEFORE gradle starts, and reach only the
   child process environment. --target forge-packages draws its username/token
   from the detected forge's own package registry; --target maven-central reads
   $MAVEN_CENTRAL_USERNAME / $MAVEN_CENTRAL_PASSWORD and re-exports the key
   imported by "release gpg import" for signing.

EXAMPLE:
   reusable-ci publish gradle deploy --target maven-central --fingerprint "$GPG_FINGERPRINT" --key-id "$GPG_KEY_ID"`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "target",
				Required: true,
				Sources:  cli.EnvVars("PUBLISH_TARGET"),
				Usage:    "publish destination: forge-packages or maven-central",
			},
			&cli.StringFlag{
				Name:    "tasks",
				Sources: cli.EnvVars("GRADLE_PUBLISH_TASKS"),
				Usage:   "override the target-derived publish task (e.g. publishToMavenCentral for the vanniktech plugin)",
			},
			commonflags.WorkingDir("directory containing ./gradlew"),
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

			in := apppublish.GradleDeployInput{
				Target:           target,
				Tasks:            cmd.String("tasks"),
				WorkingDirectory: cmd.String("working-dir"),
				Fingerprint:      cmd.String("fingerprint"),
				KeyID:            cmd.String("key-id"),
			}

			// The provider role is required only by targets that draw a
			// credential from the forge. Asking for it unconditionally
			// would make a maven-central publish fail on a forge that has
			// no package registry, for a credential it never reads.
			if !domainpublish.NeedsForgeRegistry(target) {
				return apppublish.GradleDeploy(ctx, gradle.New(), gpg.New(), nil, os.Stderr, os.Stderr, in)
			}

			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error { //nolint:varnamelen // idiomatic short name for the deps handle.
				resolver, err := d.RequireForgeMavenRegistryResolver()
				if err != nil {
					return err
				}

				return apppublish.GradleDeploy(ctx, gradle.New(), gpg.New(), resolver, os.Stderr, os.Stderr, in)
			})
		},
	}
}
