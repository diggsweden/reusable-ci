// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/gradle"
	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/secret"
)

func gradleAndroidCmd() *cli.Command {
	return &cli.Command{
		Name:  "gradle-android",
		Usage: "android (gradle) build pipeline",
		Commands: []*cli.Command{
			gradleAndroidRunCmd(),
			gradleAndroidMetadataCmd(),
			gradleAndroidDecodeKeystoreCmd(),
			gradleAndroidWriteSecretsPropertiesCmd(),
			gradleAndroidListArtifactsCmd(),
		},
	}
}

// gradleAndroidRunCmd runs the whole Android release build in one step (the
// Android sibling of `build gradle run`); the granular subcommands remain the
// composable units. The job keeps only checkout + artifact upload around it.
func gradleAndroidRunCmd() *cli.Command {
	return &cli.Command{
		Name:  subCmdRun,
		Usage: "run the full Android release build (artifact-names, keystore, metadata, tasks, build, SBOM)",
		Description: `Runs the whole Android build sequence in one step: compose artifact names,
   make ./gradlew executable, decode the signing keystore (when --enable-signing),
   resolve metadata + tasks, write secrets.properties, run the gradle build, and
   generate the Build SBOM. Signing passwords stay env vars the gradle build
   reads; the keystore is decoded outside the project dir. Emits the artifact
   names + version as outputs for downstream upload/publish jobs.

   ./gradlew executes in the current directory, not --working-dir: run this from
   the project root, or cd in first (the forge job does). --working-dir only
   locates gradle.properties for metadata.

EXAMPLE:
   cd app && reusable-ci build gradle-android run --build-types release --include-aab --enable-signing`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagWorkingDir, Value: ".", Sources: cli.EnvVars("WORKING_DIRECTORY"), Usage: "directory of gradle.properties to read for metadata; ./gradlew itself runs in the current directory (cd in first)"},
			&cli.BoolFlag{Name: "include-date", Value: true, Sources: cli.EnvVars("INCLUDE_DATE_STAMP"), Usage: "append a YYYYMMDD-HHMMSS stamp to the artifact name"},
			&cli.StringFlag{Name: "prefix", Sources: cli.EnvVars("ARTIFACT_NAME_PREFIX"), Usage: "optional prefix prepended to every artifact name"},
			&cli.StringFlag{Name: flagRepositoryName, Sources: cli.EnvVars("REPOSITORY_NAME"), Usage: "repository basename used in the default name"},
			&cli.StringFlag{Name: flagFlavor, Sources: cli.EnvVars("PRODUCT_FLAVOR"), Usage: "Android product flavor"},
			&cli.StringFlag{Name: "name-override", Sources: cli.EnvVars("ARTIFACT_NAME"), Usage: "explicit artifact-name override"},
			&cli.BoolFlag{Name: "enable-signing", Sources: cli.EnvVars("ENABLE_SIGNING"), Usage: "decode the keystore and sign the release build"},
			&cli.StringFlag{Name: "keystore-base64-file", Usage: "file with the base64-encoded release keystore (\"-\" stdin; defaults to $ANDROID_KEYSTORE_BASE64; required when --enable-signing)"},
			&cli.StringFlag{Name: "secrets-base64-file", Usage: "file with the base64-encoded secrets.properties (\"-\" stdin; defaults to $SECRETS_PROPERTIES_BASE64; optional)"},
			&cli.StringFlag{Name: "tasks-override", Sources: cli.EnvVars("GRADLE_TASKS_OVERRIDE"), Usage: "explicit task list override; bypasses the build-type heuristics"},
			&cli.StringFlag{Name: "build-types", Value: "debug,release", Sources: cli.EnvVars("BUILD_TYPES"), Usage: "comma-separated Android build types to assemble"},
			&cli.BoolFlag{Name: "include-aab", Value: true, Sources: cli.EnvVars("INCLUDE_AAB"), Usage: "also emit bundleRelease (produces an AAB)"},
			&cli.StringFlag{Name: flagBuildModule, Value: defaultBuildModule, Sources: cli.EnvVars("BUILD_MODULE"), Usage: "gradle module name (e.g. \"app\")"},
			&cli.BoolFlag{Name: flagSkipTests, Sources: cli.EnvVars("SKIP_TESTS"), Usage: usageSkipTestTask},
			&cli.BoolFlag{Name: flagBuildSBOM, Value: true, Sources: cli.EnvVars("ENABLE_BUILD_SBOM"), Usage: "generate the cyclonedx-gradle-plugin Build SBOM (default true)"},
			&cli.StringFlag{Name: flagSBOMToolVersion, Sources: cli.EnvVars("CYCLONEDX_GRADLE_VERSION"), Usage: "pinned cyclonedx-gradle-plugin version (required when --build-sbom)"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			keystoreBase64, err := secret.Resolve(cmd.String("keystore-base64-file"), "ANDROID_KEYSTORE_BASE64")
			if err != nil {
				return err
			}

			secretsBase64, err := secret.Resolve(cmd.String("secrets-base64-file"), "SECRETS_PROPERTIES_BASE64")
			if err != nil {
				return err
			}

			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				return appbuild.AndroidReleaseBuild(ctx, d.OutputSink, d.SummarySink, gradle.New(), deps.Annotator(cmd), os.Stderr, os.Stderr, appbuild.AndroidReleaseBuildInput{
					ReleaseBuildOptions: appbuild.ReleaseBuildOptions{
						Dir:             cmd.String(flagWorkingDir),
						ArtifactName:    cmd.String("name-override"),
						SkipTests:       cmd.Bool(flagSkipTests),
						EnableBuildSBOM: cmd.Bool(flagBuildSBOM),
					},
					IncludeDateStamp:        cmd.Bool("include-date"),
					ArtifactNamePrefix:      cmd.String("prefix"),
					RepoName:                cmd.String(flagRepositoryName),
					ProductFlavor:           cmd.String(flagFlavor),
					EnableSigning:           cmd.Bool("enable-signing"),
					KeystoreBase64:          keystoreBase64,
					SecretsPropertiesBase64: secretsBase64,
					GradleTasksOverride:     cmd.String("tasks-override"),
					BuildTypes:              cmd.String("build-types"),
					IncludeAAB:              cmd.Bool("include-aab"),
					BuildModule:             cmd.String(flagBuildModule),
					SBOMToolVersion:         cmd.String(flagSBOMToolVersion),
				})
			})
		},
	}
}

func gradleAndroidWriteSecretsPropertiesCmd() *cli.Command {
	return &cli.Command{
		Name:  "write-secrets-properties",
		Usage: "base64-decode $SECRETS_PROPERTIES_BASE64 into secrets.properties (mode 0600); empty secret is a no-op",
		Description: `EXAMPLE:
   SECRETS_PROPERTIES_BASE64="..." reusable-ci build gradle-android write-secrets-properties`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "base64-file",
				Usage: "file with the base64-encoded secrets.properties body (\"-\" stdin; defaults to $SECRETS_PROPERTIES_BASE64)",
			},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			base64, err := secret.Resolve(cmd.String("base64-file"), "SECRETS_PROPERTIES_BASE64")
			if err != nil {
				return err
			}

			return appbuild.AndroidWriteSecretsProperties(os.Stderr, appbuild.AndroidWriteSecretsPropertiesInput{
				Base64: base64,
			})
		},
	}
}

func gradleAndroidMetadataCmd() *cli.Command {
	return &cli.Command{
		Name:  subCmdMetadata,
		Usage: "read project metadata (versionName / versionCode from gradle.properties) and emit CI outputs",
		Description: `EXAMPLE:
   # Run in the project root (reads gradle.properties; takes no flags)
   reusable-ci build gradle-android metadata`,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				annot := deps.Annotator(cmd)

				return appbuild.AndroidVersionInfo(ctx, d.OutputSink, os.Stderr, annot, appbuild.AndroidVersionInfoInput{})
			})
		},
	}
}

func gradleAndroidDecodeKeystoreCmd() *cli.Command {
	return &cli.Command{
		Name:  "decode-keystore",
		Usage: "base64-decode the release keystore to release.keystore; prints ANDROID_KEYSTORE_PATH=<path> to stdout (workflow redirects to the runner's env file)",
		Description: `EXAMPLE:
   ANDROID_KEYSTORE_BASE64="..." reusable-ci build gradle-android decode-keystore`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:  "base64-file",
				Usage: "file with the base64-encoded keystore body (\"-\" stdin; defaults to $ANDROID_KEYSTORE_BASE64)",
			},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			base64, err := secret.Resolve(cmd.String("base64-file"), "ANDROID_KEYSTORE_BASE64")
			if err != nil {
				return err
			}

			// decode-keystore's primary output is `ANDROID_KEYSTORE_PATH=<path>`,
			// captured by `>> $GITHUB_ENV` / the runner's env file. The value goes
			// to stdout per clig.dev; progress/errors go to stderr.
			return appbuild.AndroidDecodeKeystore(os.Stdout, os.Stderr, appbuild.AndroidDecodeKeystoreInput{
				Base64: base64,
			})
		},
	}
}

func gradleAndroidListArtifactsCmd() *cli.Command {
	return &cli.Command{
		Name:  "list-artifacts",
		Usage: "list APK/AAB files under <build-module>/build/outputs",
		Description: `EXAMPLE:
   reusable-ci build gradle-android list-artifacts --build-module app`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagBuildModule, Value: defaultBuildModule, Sources: cli.EnvVars("BUILD_MODULE"), Usage: "gradle module under which the APK/AAB outputs live"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			return appbuild.AndroidListArtifacts(os.Stderr, appbuild.AndroidListArtifactsInput{
				BuildModule: cmd.String(flagBuildModule),
			})
		},
	}
}
