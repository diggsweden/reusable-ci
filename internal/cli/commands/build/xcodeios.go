// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/xcode"
	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/secret"
)

// xcodeIOSRunCmd runs the build-logic core of the iOS release build in one step
// (the xcode sibling of `build go run`); the granular subcommands remain the
// composable units. macOS-only; the host setup (xcode-select/brew/xcodegen)
// stays in the forge YAML.
// xcodeIOSRunCmd takes its two external ports rather than constructing them,
// matching gradleAndroidRunCmd.
//
// Without the seam the only way a test could reach this command was to make it
// refuse before either tool ran, so every case it covered was a refusal and the
// successful path — which is what the command exists to do — had no root-level
// coverage at all. The wiring stays in xcodeIOSCmd, so production behaviour is
// unchanged.
func xcodeIOSRunCmd(sec appbuild.XcodeSecurityOps, builder appbuild.XcodeBuildOps) *cli.Command {
	return &cli.Command{
		Name:  subCmdRun,
		Usage: "run the iOS build-logic core (artifact-name, signing, metadata, xcconfig, archive, export, list)",
		Description: `Runs the reusable-ci iOS build sequence in one step: compose the artifact
   name, set up code signing (when --enable-code-signing), resolve metadata,
   decode the xcconfig, archive, export the IPA, and list artifacts. The macOS
   host setup (xcode-select, brew, xcodegen) stays in the forge YAML.

   This verb owns the signing state it creates. On every exit, successful or
   not, it restores the keychain search list, deletes the keychain it made and
   removes the staged certificate, profile and xcconfig. Use setup-code-signing
   instead when a later job step still needs the signing state.

EXAMPLE:
   reusable-ci build xcode-ios run --scheme MyApp --configuration Release --enable-code-signing`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagArtifactName, Sources: cli.EnvVars("ARTIFACT_NAME"), Usage: "exact artifact name (overrides the derived name)"},
			&cli.StringFlag{Name: flagRepositoryName, Sources: cli.EnvVars("REPOSITORY_NAME"), Usage: "repository basename used to derive the default name"},
			&cli.BoolFlag{Name: "include-tag", Sources: cli.EnvVars("INCLUDE_TAG"), Usage: "append the git tag/ref to the artifact name"},
			&cli.StringFlag{Name: "ref-name", Sources: cienv.RefName(), Usage: "ref/tag appended when --include-tag is set"},
			&cli.StringFlag{Name: flagWorkspace, Sources: cli.EnvVars("WORKSPACE"), Usage: "path to the .xcworkspace"},
			&cli.StringFlag{Name: flagProject, Sources: cli.EnvVars("PROJECT"), Usage: "path to the .xcodeproj (fallback when no workspace)"},
			&cli.StringFlag{Name: "scheme", Sources: cli.EnvVars("SCHEME"), Usage: "xcodebuild scheme"},
			&cli.StringFlag{Name: "configuration", Sources: cli.EnvVars("CONFIGURATION"), Usage: "xcodebuild configuration (e.g. Release)"},
			&cli.StringFlag{Name: "destination", Sources: cli.EnvVars("DESTINATION"), Usage: "xcodebuild destination"},
			&cli.StringFlag{Name: "build-number", Sources: cli.EnvVars("BUILD_NUMBER"), Usage: "CURRENT_PROJECT_VERSION to set"},
			&cli.BoolFlag{Name: "enable-code-signing", Value: true, Sources: cli.EnvVars("ENABLE_CODE_SIGNING"), Usage: "set up signing and export a signed IPA"},
			&cli.StringFlag{Name: "cert-base64", Sources: cli.EnvVars("IOS_SIGNING_CERTIFICATE_BASE64"), Usage: "base64-encoded P12 signing certificate"},
			&cli.StringFlag{Name: "cert-passphrase-file", Usage: "file with the certificate passphrase (\"-\" stdin; defaults to $IOS_SIGNING_CERTIFICATE_PASSPHRASE)"},
			&cli.StringFlag{Name: "pp-base64", Sources: cli.EnvVars("PROVISIONING_PROFILE_BASE64"), Usage: "base64-encoded provisioning profile"},
			&cli.StringFlag{Name: "keychain-password-file", Usage: "no longer used: the run mints its own transient keychain password, which no operator secret has to supply"},
			&cli.StringFlag{Name: "xcconfig-base64", Sources: cli.EnvVars("XCCONFIG_BASE64"), Usage: "base64-encoded .xcconfig body (optional)"},
			&cli.StringFlag{Name: "export-options-base64", Sources: cli.EnvVars("EXPORT_OPTIONS_BASE64"), Usage: "base64-encoded export-options.plist (required for signed export)"},
			&cli.StringFlag{Name: "export-options-var", Value: "EXPORT_OPTIONS_BASE64", Sources: cli.EnvVars("EXPORT_OPTIONS_VAR"), Usage: "env var name shown in error messages when the export options are empty"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			var certPassphrase string

			if cmd.Bool("enable-code-signing") {
				var err error

				// The keychain password is not read: this run mints its own, so
				// no operator secret reaches security(1) argv. The flag stays
				// declared so callers that still pass one keep working.
				certPassphrase, err = secret.Resolve(cmd.String("cert-passphrase-file"), "IOS_SIGNING_CERTIFICATE_PASSPHRASE")
				if err != nil {
					return err
				}
			}

			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				return appbuild.XcodeReleaseBuild(ctx, d.OutputSink, sec, builder, deps.Annotator(cmd), os.Stderr, os.Stderr, appbuild.XcodeReleaseBuildInput{
					ArtifactName:        cmd.String(flagArtifactName),
					RepositoryName:      cmd.String(flagRepositoryName),
					IncludeTag:          cmd.Bool("include-tag"),
					RefName:             cmd.String("ref-name"),
					Workspace:           cmd.String(flagWorkspace),
					Project:             cmd.String(flagProject),
					Scheme:              cmd.String("scheme"),
					Configuration:       cmd.String("configuration"),
					Destination:         cmd.String("destination"),
					BuildNumber:         cmd.String("build-number"),
					EnableCodeSigning:   cmd.Bool("enable-code-signing"),
					CertBase64:          cmd.String("cert-base64"),
					CertPassphrase:      certPassphrase,
					PPBase64:            cmd.String("pp-base64"),
					XCConfigBase64:      cmd.String("xcconfig-base64"),
					ExportOptionsBase64: cmd.String("export-options-base64"),
					ExportOptionsVar:    cmd.String("export-options-var"),
				})
			})
		},
	}
}

func xcodeIOSCmd() *cli.Command {
	return &cli.Command{
		Name:  "xcode-ios",
		Usage: "xcode-ios build pipeline (macOS-only; workflows install reusable-ci on the macOS host)",
		Commands: []*cli.Command{
			xcodeIOSRunCmd(xcode.NewSecurity(), xcode.NewBuild()),
			xcodeIOSMetadataCmd(),
			xcodeIOSSetupXCConfigCmd(),
			xcodeIOSSetupCodeSigningCmd(),
		},
	}
}

func xcodeIOSMetadataCmd() *cli.Command {
	return &cli.Command{
		Name:  subCmdMetadata,
		Usage: "read project metadata (MARKETING_VERSION / CURRENT_PROJECT_VERSION from project.pbxproj) and emit CI outputs",
		Description: `EXAMPLE:
   reusable-ci build xcode-ios metadata --project App.xcodeproj`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:    "project",
				Sources: cli.EnvVars("PROJECT_PATH"),
				Usage:   "path to the .xcodeproj (defaults to a discovered one in working-dir)",
			},
			&cli.StringFlag{
				Name:    "workspace",
				Sources: cli.EnvVars("WORKSPACE_PATH"),
				Usage:   "path to the .xcworkspace (when the project is part of one)",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				annot := deps.Annotator(cmd)

				return appbuild.XcodeVersionInfo(ctx, d.OutputSink, os.Stderr, annot, appbuild.XcodeVersionInfoInput{
					Project:   cmd.String(flagProject),
					Workspace: cmd.String(flagWorkspace),
				})
			})
		},
	}
}

func xcodeIOSSetupXCConfigCmd() *cli.Command {
	return &cli.Command{
		Name:  "setup-xcconfig",
		Usage: "decode optional XCCONFIG_BASE64 and emit xcconfig-path",
		Description: `EXAMPLE:
   XCCONFIG_BASE64="..." reusable-ci build xcode-ios setup-xcconfig`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "base64", Sources: cli.EnvVars("XCCONFIG_BASE64"), Usage: "base64-encoded .xcconfig body; empty value is a no-op"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			&cli.StringFlag{Name: flagTempDir, Sources: cienv.TempDir(), Usage: "directory the decoded .xcconfig is written to"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				annot := deps.Annotator(cmd)

				return appbuild.XcodeXCConfig(ctx, d.OutputSink, annot, appbuild.XcodeXCConfigInput{
					Base64:  cmd.String("base64"),
					TempDir: cmd.String(flagTempDir),
				})
			})
		},
	}
}
