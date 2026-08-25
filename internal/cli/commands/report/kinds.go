// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package report

import (
	"context"
	"fmt"
	"strconv"

	"github.com/urfave/cli/v3"

	appsummary "github.com/diggsweden/reusable-ci/v3/internal/app/summary"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

// summaryKind is one row of the report kind registry: a step-summary
// writer whose whole contract is "flat flags in → one markdown block
// appended to the step summary". The per-kind differences are wording
// and rows only, so the CLI scaffolding (required-flag check,
// deps wiring, EXAMPLE formatting) lives once in command().
//
// Verbs with a different contract stay hand-written elsewhere:
// filesystem probes (extracted-binaries, status build-sbom/sbom-count),
// git queries (status prerequisites), gate semantics (swift-lint),
// positional args (status quality-check), typed-manifest composition
// (stage-result, job-result), and stage-JSON aggregation
// (pr, release, snapshot-release).
type summaryKind struct {
	name     string
	usage    string
	example  string
	flags    []cli.Flag
	required []string // string flags checked non-empty before deps are built
	write    func(ctx context.Context, cmd *cli.Command, d *deps.Deps) error
}

// command materialises the shared CLI scaffolding for one kind.
func (k summaryKind) command() *cli.Command {
	return &cli.Command{
		Name:        k.name,
		Usage:       k.usage,
		Description: "EXAMPLE:\n   " + k.example,
		Flags:       k.flags,
		Action: func(ctx context.Context, cmd *cli.Command) error {
			for _, f := range k.required {
				if cmd.String(f) == "" {
					return fmt.Errorf("--%s is required: %w", f, errs.ErrUsage)
				}
			}

			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				return k.write(ctx, cmd, d)
			})
		},
	}
}

func kindGroup(name, usage string, kinds []summaryKind) *cli.Command {
	cmds := make([]*cli.Command, 0, len(kinds))
	for _, k := range kinds {
		cmds = append(cmds, k.command())
	}

	return &cli.Command{Name: name, Usage: usage, Commands: cmds}
}

// buildGroup wires `reusable-ci report build <ecosystem>` — every kind
// here appends a step-summary block describing one ecosystem-specific
// build (maven, npm, gradle, android, go, xcode).
func buildGroup() *cli.Command {
	return kindGroup("build", "append a per-ecosystem build summary to the step summary", buildKinds())
}

// publishGroup wires `reusable-ci report publish <target>` — every kind
// here appends a step-summary block describing one publish-target
// upload (App Store, Google Play, Maven Central, forge packages).
func publishGroup() *cli.Command {
	return kindGroup("publish", "append a per-target publish summary to the step summary", publishKinds())
}

func buildKinds() []summaryKind {
	return []summaryKind{
		mavenBuildKind(),
		npmBuildKind(),
		gradleBuildKind(),
		androidBuildKind(),
		goBuildKind(),
		xcodeBuildKind(),
	}
}

func publishKinds() []summaryKind {
	return []summaryKind{
		appstorePublishKind(),
		googlePlayPublishKind(),
		mavenCentralPublishKind(),
		forgePackagesPublishKind(),
	}
}

func mavenBuildKind() summaryKind {
	return summaryKind{
		name:    "maven",
		usage:   "append the Maven build summary block to the step summary",
		example: `reusable-ci report build maven --group-id com.example --artifact-id app --version 1.2.3 --java-version 21`,
		flags: []cli.Flag{
			&cli.StringFlag{Name: "build-type", Sources: cli.EnvVars("BUILD_TYPE"), Usage: "Maven build type (library/application)"},
			&cli.StringFlag{Name: "group-id", Sources: cli.EnvVars("GROUP_ID"), Usage: "Maven groupId of the built artifact"},
			&cli.StringFlag{Name: "artifact-id", Sources: cli.EnvVars("ARTIFACT_ID"), Usage: "Maven artifactId of the built artifact"},
			&cli.StringFlag{Name: "version", Sources: cli.EnvVars("VERSION"), Usage: "Maven version of the built artifact"},                          //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			&cli.StringFlag{Name: "java-version", Sources: cli.EnvVars("JAVA_VERSION"), Usage: "JDK major version used for the build"},               //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			&cli.BoolFlag{Name: "skip-tests", Sources: cli.EnvVars("SKIP_TESTS"), Usage: "tests were skipped (toggles the test-status summary row)"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			&cli.BoolFlag{Name: "is-snapshot", Sources: cli.EnvVars("IS_SNAPSHOT"), Usage: "the built version is a -SNAPSHOT"},
		},
		required: []string{"build-type", "group-id", "artifact-id", "version", "java-version"},
		write: func(ctx context.Context, cmd *cli.Command, d *deps.Deps) error {
			return appsummary.MavenBuild(ctx, d.SummarySink, appsummary.MavenBuildInput{
				BuildType:   cmd.String("build-type"),
				GroupID:     cmd.String("group-id"),
				ArtifactID:  cmd.String("artifact-id"),
				Version:     cmd.String("version"),
				JavaVersion: cmd.String("java-version"),
				SkipTests:   cmd.Bool("skip-tests"),
				IsSnapshot:  cmd.Bool("is-snapshot"),
			})
		},
	}
}

func npmBuildKind() summaryKind {
	return summaryKind{
		name:    "npm",
		usage:   "append the NPM build summary block to the step summary",
		example: `reusable-ci report build npm --package-name @org/app --version 1.2.3 --node-version 22`,
		flags: []cli.Flag{
			&cli.StringFlag{Name: "package-name", Sources: cli.EnvVars("PACKAGE_NAME"), Usage: "npm package name from package.json"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			&cli.StringFlag{Name: "version", Sources: cli.EnvVars("VERSION"), Usage: "npm package version from package.json"},
			&cli.StringFlag{Name: "node-version", Sources: cli.EnvVars("NODE_VERSION"), Usage: "Node.js version used for the build"},
			&cli.BoolFlag{Name: "skip-tests", Sources: cli.EnvVars("SKIP_TESTS"), Usage: "tests were skipped (toggles the test-status summary row)"},
		},
		required: []string{"package-name", "version", "node-version"},
		write: func(ctx context.Context, cmd *cli.Command, d *deps.Deps) error {
			return appsummary.NPMBuild(ctx, d.SummarySink, appsummary.NPMBuildInput{
				PackageName: cmd.String("package-name"),
				Version:     cmd.String("version"),
				NodeVersion: cmd.String("node-version"),
				SkipTests:   cmd.Bool("skip-tests"),
			})
		},
	}
}

func gradleBuildKind() summaryKind {
	return summaryKind{
		name:    "gradle",
		usage:   "append the Gradle (JVM) build summary block to the step summary",
		example: `reusable-ci report build gradle --version 1.2.3 --java-version 21 --tasks "build"`,
		flags: []cli.Flag{
			&cli.StringFlag{Name: "java-version", Sources: cli.EnvVars("JAVA_VERSION"), Usage: "JDK major version used for the build"},
			&cli.StringFlag{Name: "tasks", Sources: cli.EnvVars("GRADLE_TASKS"), Usage: "gradle tasks that ran (shown verbatim in the summary)"},
			&cli.BoolFlag{Name: "skip-tests", Sources: cli.EnvVars("SKIP_TESTS"), Usage: "tests were skipped (toggles the test-status summary row)"},
			&cli.StringFlag{Name: "version", Sources: cli.EnvVars("VERSION"), Usage: "gradle project version (from gradle.properties)"},
		},
		required: []string{"java-version", "tasks"},
		write: func(ctx context.Context, cmd *cli.Command, d *deps.Deps) error {
			return appsummary.GradleBuild(ctx, d.SummarySink, appsummary.GradleBuildInput{
				JavaVersion: cmd.String("java-version"),
				GradleTasks: cmd.String("tasks"),
				SkipTests:   cmd.Bool("skip-tests"),
				Version:     cmd.String("version"),
			})
		},
	}
}

func androidBuildKind() summaryKind {
	return summaryKind{
		name:    "android",
		usage:   "append the Android variants build summary block to the step summary",
		example: `reusable-ci report build android --version 1.2.3 --version-code 42 --java-version 21`,
		flags: []cli.Flag{
			&cli.StringFlag{Name: "java-version", Sources: cli.EnvVars("JAVA_VERSION"), Usage: "JDK major version used for the build"},
			&cli.StringFlag{Name: "jdk-dist", Sources: cli.EnvVars("JDK_DIST"), Usage: "JDK distribution (e.g. temurin, zulu, corretto)"},
			&cli.StringFlag{Name: "build-module", Sources: cli.EnvVars("BUILD_MODULE"), Usage: "gradle module name (e.g. \"app\")"},
			&cli.StringFlag{Name: "flavor", Sources: cli.EnvVars("FLAVOR"), Usage: "Android product flavor"},
			&cli.StringFlag{Name: "build-types", Value: "debug,release", Sources: cli.EnvVars("BUILD_TYPES"), Usage: "comma-separated Android build types"},
			&cli.BoolFlag{Name: "include-aab", Value: true, Sources: cli.EnvVars("INCLUDE_AAB"), Usage: "an AAB was bundled (toggles its summary row)"},
			&cli.BoolFlag{Name: "signing", Sources: cli.EnvVars("SIGNING"), Usage: "release signing keys were applied (toggles the signing row)"},
			&cli.BoolFlag{Name: "skip-tests", Sources: cli.EnvVars("SKIP_TESTS"), Usage: "tests were skipped (toggles the test-status summary row)"},
			&cli.StringFlag{Name: "version", Sources: cli.EnvVars("VERSION"), Usage: "Android versionName from build.gradle"},
			&cli.StringFlag{Name: "version-code", Sources: cli.EnvVars("VERSION_CODE"), Usage: "Android versionCode from build.gradle"},
			&cli.StringFlag{Name: "debug-name", Sources: cli.EnvVars("DEBUG_NAME"), Usage: "filename of the debug APK"},
			&cli.StringFlag{Name: "release-name", Sources: cli.EnvVars("RELEASE_NAME"), Usage: "filename of the release APK"},
			&cli.StringFlag{Name: "aab-name", Sources: cli.EnvVars("AAB_NAME"), Usage: "filename of the bundled AAB"},
			&cli.BoolFlag{Name: "library", Sources: cli.EnvVars("ANDROID_LIBRARY"), Usage: "library mode: report the AAR instead of the APK/AAB variants"},
			&cli.StringFlag{Name: "aar-name", Sources: cli.EnvVars("AAR_NAME"), Usage: "filename of the library AAR"},
		},
		required: []string{"java-version", "jdk-dist", "build-module"},
		write: func(ctx context.Context, cmd *cli.Command, d *deps.Deps) error {
			return appsummary.AndroidBuild(ctx, d.SummarySink, appsummary.AndroidBuildInput{
				JavaVersion: cmd.String("java-version"),
				JDKDist:     cmd.String("jdk-dist"),
				BuildModule: cmd.String("build-module"),
				Flavor:      cmd.String("flavor"),
				BuildTypes:  cmd.String("build-types"),
				IncludeAAB:  cmd.Bool("include-aab"),
				Signing:     cmd.Bool("signing"),
				SkipTests:   cmd.Bool("skip-tests"),
				Version:     cmd.String("version"),
				VersionCode: cmd.String("version-code"),
				DebugName:   cmd.String("debug-name"),
				ReleaseName: cmd.String("release-name"),
				AABName:     cmd.String("aab-name"),
				Library:     cmd.Bool("library"),
				AARName:     cmd.String("aar-name"),
			})
		},
	}
}

func goBuildKind() summaryKind {
	return summaryKind{
		name:    "go",
		usage:   "append the Go build summary block to the step summary",
		example: `reusable-ci report build go --binary-name app --version 1.2.3 --platforms linux/amd64,linux/arm64`,
		flags: []cli.Flag{
			&cli.StringFlag{Name: "binary-name", Sources: cli.EnvVars("BINARY_NAME"), Usage: "name of the produced Go binary"},
			&cli.StringFlag{Name: "module", Sources: cli.EnvVars("MODULE"), Usage: "Go module path (from go.mod)"},
			&cli.StringFlag{Name: "platforms", Sources: cli.EnvVars("PLATFORMS"), Usage: "comma-separated GOOS/GOARCH pairs the binary was built for"},
			&cli.StringFlag{Name: "version", Sources: cli.EnvVars("VERSION"), Usage: "release version embedded via -ldflags"},
			&cli.BoolFlag{Name: "skip-tests", Sources: cli.EnvVars("SKIP_TESTS"), Usage: "tests were skipped (toggles the test-status summary row)"},
		},
		required: []string{"binary-name", "module", "platforms", "version"},
		write: func(ctx context.Context, cmd *cli.Command, d *deps.Deps) error {
			return appsummary.GoBuild(ctx, d.SummarySink, appsummary.GoBuildInput{
				BinaryName: cmd.String("binary-name"),
				Module:     cmd.String("module"),
				Platforms:  cmd.String("platforms"),
				Version:    cmd.String("version"),
				SkipTests:  cmd.Bool("skip-tests"),
			})
		},
	}
}

func xcodeBuildKind() summaryKind {
	return summaryKind{
		name:    "xcode",
		usage:   "append the Xcode build summary block to the step summary",
		example: `reusable-ci report build xcode --scheme App --version 1.2.3 --configuration Release`,
		flags: []cli.Flag{
			&cli.StringFlag{Name: "xcode-version", Sources: cli.EnvVars("XCODE_VERSION"), Usage: "Xcode version used for the build"},
			&cli.StringFlag{Name: "scheme", Sources: cli.EnvVars("SCHEME"), Usage: "Xcode scheme that was archived"},
			&cli.StringFlag{Name: "configuration", Value: "Release", Sources: cli.EnvVars("CONFIGURATION"), Usage: "Xcode build configuration"},
			&cli.StringFlag{Name: "destination", Value: "generic/platform=iOS", Sources: cli.EnvVars("DESTINATION"), Usage: "Xcode destination spec used for the archive"},
			&cli.BoolFlag{Name: "signing", Value: true, Sources: cli.EnvVars("SIGNING"), Usage: "code signing was performed (toggles the signing row)"},
			&cli.StringFlag{Name: "version", Value: "unknown", Sources: cli.EnvVars("VERSION"), Usage: "MARKETING_VERSION baked into the IPA"},
			&cli.StringFlag{Name: "build-number", Value: "unknown", Sources: cli.EnvVars("BUILD_NUMBER"), Usage: "CURRENT_PROJECT_VERSION baked into the IPA"},
			&cli.StringFlag{Name: "ipa-name", Sources: cli.EnvVars("IPA_NAME"), Usage: "filename of the produced IPA"},
		},
		required: []string{"xcode-version", "scheme"},
		write: func(ctx context.Context, cmd *cli.Command, d *deps.Deps) error {
			return appsummary.XcodeBuild(ctx, d.SummarySink, appsummary.XcodeBuildInput{
				XcodeVersion:  cmd.String("xcode-version"),
				Scheme:        cmd.String("scheme"),
				Configuration: cmd.String("configuration"),
				Destination:   cmd.String("destination"),
				Signing:       cmd.Bool("signing"),
				Version:       cmd.String("version"),
				BuildNumber:   cmd.String("build-number"),
				IPAName:       cmd.String("ipa-name"),
			})
		},
	}
}

func appstorePublishKind() summaryKind {
	return summaryKind{
		name:    "appstore",
		usage:   "append the App Store Connect upload summary block to the step summary",
		example: `reusable-ci report publish appstore --ipa-file App.ipa --platform ios`,
		flags: []cli.Flag{
			&cli.StringFlag{Name: "ipa-file", Sources: cli.EnvVars("IPA_FILE"), Usage: "uploaded .ipa file path (shown in the summary row)"},
			&cli.StringFlag{Name: "platform", Sources: cli.EnvVars("PLATFORM"), Usage: "App Store platform (ios/tvos/macos)"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			&cli.BoolFlag{Name: "skip-validation", Sources: cli.EnvVars("SKIP_VALIDATION"), Usage: "altool --skip-validation was used during upload"},
			&cli.BoolFlag{Name: "submit-review", Sources: cli.EnvVars("SUBMIT_REVIEW"), Usage: "the upload was submitted for review"},
			&cli.StringFlag{Name: "request-id", Sources: cli.EnvVars("REQUEST_ID"), Usage: "altool request-id returned for the upload"},
		},
		required: []string{"ipa-file", "platform"},
		write: func(ctx context.Context, cmd *cli.Command, d *deps.Deps) error {
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

func googlePlayPublishKind() summaryKind {
	return summaryKind{
		name:    "google-play",
		usage:   "append the Google Play upload summary block to the step summary",
		example: `reusable-ci report publish google-play --aab-file app.aab --track internal`,
		flags: []cli.Flag{
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
		required: []string{"aab-file", "package-name", "track", "status"},
		write: func(ctx context.Context, cmd *cli.Command, d *deps.Deps) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
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
		},
	}
}

func mavenCentralPublishKind() summaryKind {
	return summaryKind{
		name:    "maven-central",
		usage:   "append the Maven Central publish summary block to the step summary",
		example: `reusable-ci report publish maven-central --version 1.2.3`,
		flags: []cli.Flag{
			&cli.StringFlag{Name: "version", Sources: cli.EnvVars("VERSION"), Usage: "published Maven version (used in the summary row)"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			&cli.BoolFlag{Name: "is-snapshot", Sources: cli.EnvVars("IS_SNAPSHOT"), Usage: "the published version is a -SNAPSHOT"},
		},
		required: []string{"version"},
		write: func(ctx context.Context, cmd *cli.Command, d *deps.Deps) error {
			return appsummary.MavenCentralPublish(ctx, d.SummarySink, appsummary.MavenCentralPublishInput{
				Version:    cmd.String("version"),
				IsSnapshot: cmd.Bool("is-snapshot"),
			})
		},
	}
}

func forgePackagesPublishKind() summaryKind {
	return summaryKind{
		name:    "forge-packages",
		usage:   "append the forge-native package-registry publish summary block to the step summary",
		example: `reusable-ci report publish forge-packages --repository org/app --package-type maven`,
		flags: []cli.Flag{
			&cli.StringFlag{Name: "repository", Sources: cienv.Repository(), Usage: "\"owner/repo\" the package was published from"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			&cli.StringFlag{Name: "package-type", Sources: cli.EnvVars("PACKAGE_TYPE"), Usage: "package type (maven/npm/container/…)"},
			&cli.StringFlag{Name: "registry-name", Sources: cli.EnvVars("REGISTRY_NAME"), Usage: "registry display name for the summary; defaults to the detected forge's (e.g. \"GitHub Packages\")"},
		},
		required: []string{"repository", "package-type"},
		write: func(ctx context.Context, cmd *cli.Command, d *deps.Deps) error { //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
			registryName := cmd.String("registry-name")
			if registryName == "" {
				registryName = deps.DescriberForDetected().Describe().DisplayName + " Packages"
			}

			return appsummary.ForgePackagesPublish(ctx, d.SummarySink, appsummary.ForgePackagesPublishInput{
				Repository:   cmd.String("repository"),
				PackageType:  cmd.String("package-type"),
				RegistryName: registryName,
			})
		},
	}
}
