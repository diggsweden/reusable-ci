// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package publish

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	apppublish "github.com/diggsweden/reusable-ci/internal/app/publish"
	"github.com/diggsweden/reusable-ci/internal/cli/deps"
)

func mavenCentralCmd() *cli.Command {
	return &cli.Command{
		Name:  "maven-central",
		Usage: "maven central pre-flight checks",
		Commands: []*cli.Command{
			mavenCentralValidateArtifactsCmd(),
		},
	}
}

func mavenCentralValidateArtifactsCmd() *cli.Command {
	return &cli.Command{
		Name:  "validate-artifacts",
		Usage: "verify sources + javadoc JARs are present under */target/ before deploying to Maven Central",
		Action: func(ctx context.Context, cmd *cli.Command) error {
			annot := deps.Annotator(cmd)
			_, err := apppublish.MavenValidateArtifacts(ctx, os.Stdout, os.Stderr, annot, apppublish.MavenValidateArtifactsInput{})
			return err
		},
	}
}
