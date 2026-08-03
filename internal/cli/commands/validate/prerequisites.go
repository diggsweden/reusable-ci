// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/cargo"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/git"
	appvalidate "github.com/diggsweden/reusable-ci/v3/internal/app/validate"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/v3/internal/runcontext"
)

// prerequisitesCmd is the collapsed entry that replaces ~10 sequential
// `validate ...` steps in the workflow with a single Go invocation that
// runs the independent checks concurrently via errgroup.
func prerequisitesCmd() *cli.Command {
	return &cli.Command{
		Name:  "prerequisites",
		Usage: "run all release-prerequisite validators concurrently and append a summary table",
		Description: `EXAMPLE:
   reusable-ci validate prerequisites --tag v1.2.3 --ref-type tag --repository org/app`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "tag", Sources: cienv.Tag(), Usage: "tag being released (e.g. v1.2.3)"},                          //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			&cli.StringFlag{Name: "ref-type", Sources: cienv.RefType(), Usage: "trigger ref type (\"tag\" required for releases)"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			&cli.StringFlag{Name: "ref", Sources: cienv.Ref(), Usage: "fully-qualified ref (refs/tags/X) for prefix verification"},
			&cli.StringFlag{Name: "target-branch", Sources: cli.EnvVars("TARGET_BRANCH", "BRANCH"), Usage: "branch the tag commit must be reachable from"},
			&cli.StringFlag{Name: "repository", Sources: cienv.Repository(), Usage: "\"owner/repo\" on GitHub; \"group/project[/sub]\" on GitLab"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			&cli.BoolFlag{Name: "require-allowlisted-signer", Sources: cli.EnvVars("REQUIRE_ALLOWLISTED_SIGNER"), Usage: "require the tag signer to be allowlisted in .reusable-ci/allowed_signers (SSH) or .reusable-ci/allowed_gpg_keys.asc (GPG)"},
			&cli.BoolFlag{Name: "sign-artifacts", Sources: cli.EnvVars("SIGN_ARTIFACTS"), Usage: "require a GPG public key (release-artifact signing is enabled)"},
			&cli.BoolFlag{Name: "has-maven-central", Sources: cli.EnvVars("HAS_MAVEN_CENTRAL_TARGET"), Usage: "the plan targets Maven Central (enables credential check)"},
			&cli.BoolFlag{Name: "has-cargo", Sources: cli.EnvVars("HAS_CARGO_TARGET"), Usage: "the plan targets crates.io (enables Cargo prerequisites check)"},
			&cli.BoolFlag{Name: "has-jvm", Sources: cli.EnvVars("HAS_JVM_TARGET"), Usage: "the plan includes a Maven/Gradle/Gradle-Android artifact (enables JVM reproducibility check)"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
				annot := deps.Annotator(cmd)

				tv, err := d.RequireTokenValidator()
				if err != nil {
					return err
				}

				result, runErr := appvalidate.Prerequisites(ctx, appvalidate.PrerequisitesDeps{
					GitRepo:  git.New(),
					Provider: tv,
					Cargo:    cargo.New(),
				}, os.Stderr, annot, appvalidate.PrerequisitesInput{
					Tag:                      cmd.String("tag"),
					RefType:                  cmd.String("ref-type"),
					Ref:                      cmd.String("ref"),
					Branch:                   cmd.String("target-branch"),
					Repository:               cmd.String("repository"),
					RequireAllowlistedSigner: cmd.Bool("require-allowlisted-signer"),
					SignArtifacts:            cmd.Bool("sign-artifacts"),
					HasMavenCentralTarget:    cmd.Bool("has-maven-central"),
					HasCargoTarget:           cmd.Bool("has-cargo"),
					HasJVMTarget:             cmd.Bool("has-jvm"),
					// Secrets stay in env — never passed via argv.
					ReleaseGPGPublicKey: os.Getenv("RELEASE_GPG_PUBLIC_KEY"),
					// Named destination: this token is about to be sent to the forge
					// this run targets, so it is only handed over if it was issued
					// there. A token bound elsewhere yields "", and prerequisites
					// then reports a missing release token -- fail closed, and
					// truthful, since it could not have authenticated there anyway.
					ReleaseToken: runcontext.ReleaseToken().Resolve(os.Getenv).
						For(runcontext.ServerURL().Resolve(os.Getenv)),
					MavenCentralUsername: os.Getenv("MAVEN_CENTRAL_USERNAME"),
					MavenCentralPassword: os.Getenv("MAVEN_CENTRAL_PASSWORD"),
					PublishStagePlanJSON: os.Getenv("PUBLISH_STAGE_PLAN_JSON"),
					ConfigPlanJSON:       os.Getenv("CONFIG_PLAN_JSON"),
				})
				if writeErr := appvalidate.WritePrerequisitesSummary(ctx, d.SummarySink, result); writeErr != nil && runErr == nil {
					return writeErr
				}

				return runErr
			})
		},
	}
}
