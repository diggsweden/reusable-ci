// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package publish

import (
	"context"
	"os"
	"strings"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/maven"
	apppublish "github.com/diggsweden/reusable-ci/v3/internal/app/publish"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
)

func mavenCentralCmd() *cli.Command {
	return &cli.Command{
		Name:  "maven-central",
		Usage: "maven central pre-flight checks",
		Commands: []*cli.Command{
			mavenCentralValidateArtifactsCmd(),
			mavenCentralDeployCmd(),
		},
	}
}

func mavenCentralDeployCmd() *cli.Command {
	return &cli.Command{
		Name:  "deploy",
		Usage: "validate settings.xml (if set) and run `mvn deploy -P<profile> -DskipTests`",
		Description: `EXAMPLE:
   reusable-ci publish maven-central deploy --profile release --settings-path settings.xml`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "cli-opts", Sources: cli.EnvVars("MAVEN_CLI_OPTS"), Usage: "extra args forwarded to mvn (whitespace-separated, e.g. \"-B -ntp\")"},
			&cli.StringFlag{Name: "settings-path", Sources: cli.EnvVars("SETTINGS_PATH"), Usage: "path to the settings.xml passed via -s"},
			&cli.StringFlag{Name: "profile", Required: true, Sources: cli.EnvVars("MAVEN_PROFILE"), Usage: "Maven profile activated for the deploy (e.g. release)"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return apppublish.MavenCentralDeploy(ctx, maven.New(), os.Stderr, os.Stderr, apppublish.MavenCentralDeployInput{
				CLIOpts:      strings.Fields(cmd.String("cli-opts")),
				SettingsPath: cmd.String("settings-path"),
				Profile:      cmd.String("profile"),
			})
		},
	}
}

func mavenCentralValidateArtifactsCmd() *cli.Command {
	return &cli.Command{
		Name:  "validate-artifacts",
		Usage: "verify sources + javadoc JARs are present under */target/ before deploying to Maven Central",
		Description: `EXAMPLE:
   # Run from the build root after a Maven build (takes no flags)
   reusable-ci publish maven-central validate-artifacts`,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			annot := deps.Annotator(cmd)
			_, err := apppublish.MavenValidateArtifacts(ctx, os.Stderr, os.Stderr, annot, apppublish.MavenValidateArtifactsInput{})

			return err
		},
	}
}
