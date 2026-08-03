// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package toolchain wires bootstrap/toolchain management commands.
package toolchain

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/apt"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/mise"
	"github.com/urfave/cli/v3"

	apptoolchain "github.com/diggsweden/reusable-ci/v3/internal/app/toolchain"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/planfile"
)

// Plan-file scopes of the flag-heavy toolchain verbs in $REUSABLE_CI_PLAN
// (flag > plan > env > default). One const per command; the scope string is
// the command path.
const (
	planScopeInstallMise              = "toolchain install-mise"
	planScopeInstallMiseTools         = "toolchain install-mise-tools"
	planScopeInstallChangelogRenderer = "toolchain install-changelog-renderer"
	planScopeSetupMiseEnv             = "toolchain setup-mise-env"

	// flagTempDir is the scratch-directory flag. flagTempDirLegacy is its
	// former name, kept as an alias: --runner-temp echoed GitHub's
	// $RUNNER_TEMP vocabulary in a tool whose point is forge neutrality,
	// and it read oddly beside the $CI_TEMP_DIR it also honours.
	flagTempDir       = "temp-dir"
	flagTempDirLegacy = "runner-temp"
)

// tempDirPlan sources the scratch directory for scope: the plan file under
// the current flag name, then under the former one, then the env chain.
//
// A plan file "maps flag names to values" (see planfile's doc), so renaming
// the flag renames its plan key. Honouring the old key too makes the rename
// a true alias rather than a silent break: a plan still saying "runner-temp"
// would otherwise fall through to $RUNNER_TEMP and usually resolve to the
// same path anyway — hiding the breakage from exactly the person who set the
// key deliberately to override it.
func tempDirPlan(scope string) cli.ValueSourceChain {
	return planfile.Chain(scope, flagTempDir,
		planfile.Chain(scope, flagTempDirLegacy, cienv.TempDir()),
	)
}

// Shared flag names, defaults and usage strings across the toolchain
// subcommands, declared once so spellings cannot drift and the package stays
// under goconst's literal budget. Values are part of the CLI contract —
// docs/cli-reference.md is generated from them and a sync test gates any
// change.
const (
	flagMiseRoot = "root"
	flagLocked   = "locked"
	flagBinHome  = "bin-home"
	flagPathFile = "path-file"
	// lockedDefault is the shared default for the --locked flags.
	lockedDefault = "false"
	// usageMiseRoot is the shared usage string for the --root flags.
	usageMiseRoot = "repository root containing mise config"
)

// New returns the `toolchain` subgroup command tree.
func New() *cli.Command {
	return &cli.Command{
		Name:  "toolchain",
		Usage: "bootstrap and expose CI toolchains",
		Commands: []*cli.Command{
			cacheDiscriminatorCmd(),
			exposeMiseToolsCmd(),
			installChangelogRendererCmd(),
			installMiseCmd(),
			installMiseToolsCmd(),
			installSystemDependenciesCmd(),
			setupMiseEnvCmd(),
			trustMiseConfigCmd(),
			validateMiseInstallCmd(),
		},
	}
}

func cacheDiscriminatorCmd() *cli.Command {
	return &cli.Command{
		Name:  "cache-discriminator",
		Usage: "emit setup-toolchain's stable tool-cache discriminator",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "tools", Sources: cli.EnvVars("TOOLSET_TOOLS"), Usage: "whitespace-separated mise tool selectors requested by the caller"},
			&cli.StringFlag{Name: "install-dev-tools", Sources: cli.EnvVars("TOOLSET_DEV"), Usage: "whether consumer dev-tool installers are enabled"},
			&cli.StringFlag{Name: "extra-cache-paths", Sources: cli.EnvVars("TOOLSET_EXTRA_PATHS"), Usage: "additional newline-separated cache paths"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			hash := apptoolchain.CacheDiscriminator(apptoolchain.CacheDiscriminatorInput{
				Tools:           cmd.String("tools"),
				InstallDevTools: cmd.String("install-dev-tools"),
				ExtraCachePaths: cmd.String("extra-cache-paths"),
			})
			_, _ = fmt.Fprintln(os.Stdout, hash)

			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				return d.OutputSink.Set(ctx, "hash", hash)
			})
		},
	}
}

func exposeMiseToolsCmd() *cli.Command {
	return &cli.Command{
		Name:  "expose-mise-tools",
		Usage: "expose installed mise tool binaries to later CI steps",
		Description: `Lists installed mise tools, appends each existing mise bin-path to the
runner path file, symlinks executable files into ~/.local/bin, and exposes
rustup-managed cargo/rustc bins when the repository declares rustup and
rust-toolchain.toml. Intended to run unconditionally after cache restore/install.`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagMiseRoot, Value: ".", Sources: cli.EnvVars("MISE_PROJECT_ROOT"), Usage: usageMiseRoot},
			&cli.StringFlag{Name: flagBinHome, Sources: cli.EnvVars("TOOLCHAIN_BIN_HOME"), Usage: "directory where executable symlinks are written (default: $HOME/.local/bin)"},
			&cli.StringFlag{Name: flagPathFile, Required: true, Sources: cli.EnvVars("FORGEJO_PATH", "GITHUB_PATH"), Usage: "runner path file to append exposed bin directories to"},
			&cli.StringFlag{Name: flagLocked, Value: lockedDefault, Sources: cli.EnvVars("MISE_LOCKED_INSTALL"), Usage: "whether rustup cargo exposure should use locked mise mode: true|false"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return apptoolchain.ExposeMiseTools(ctx, mise.New(), os.Stdout, apptoolchain.ExposeMiseToolsInput{
				Root:     cmd.String(flagMiseRoot),
				BinHome:  cmd.String(flagBinHome),
				PathFile: cmd.String(flagPathFile),
				Locked:   cmd.String(flagLocked),
			})
		},
	}
}

func installMiseToolsCmd() *cli.Command {
	return &cli.Command{
		Name:  "install-mise-tools",
		Usage: "install mise-managed CI tools with runtime bootstrap and retry",
		Description: `Runs the setup-toolchain mise install sequence: sanitize child env,
install declared backend runtimes (uv/go/rust/rustup) first, optionally install a
caller-selected tool subset, and retry the final mise install. Tokens are inherited
only as mise-specific env vars and are never passed on argv. Every flag may also be
fed from the $REUSABLE_CI_PLAN plan file under the "toolchain install-mise-tools"
scope (flag > plan > env > default).`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagMiseRoot, Value: ".", Sources: planfile.Vars(planScopeInstallMiseTools, flagMiseRoot, "MISE_PROJECT_ROOT"), Usage: usageMiseRoot},
			&cli.StringFlag{Name: flagLocked, Value: lockedDefault, Sources: planfile.Vars(planScopeInstallMiseTools, flagLocked, "MISE_LOCKED_INSTALL"), Usage: "whether mise install should use locked mode: true|false"},
			&cli.StringFlag{Name: "tools", Sources: planfile.Vars(planScopeInstallMiseTools, "tools", "TOOLS_SUBSET", "MISE_TOOLS"), Usage: "whitespace-separated mise tool selectors to install instead of the full config"},
			&cli.StringFlag{Name: "mise-bin", Sources: planfile.Vars(planScopeInstallMiseTools, "mise-bin", "MISE_BIN"), Usage: "mise binary path (default: $HOME/.local/bin/mise when present, else mise from PATH)"},
			&cli.IntFlag{Name: "retry-attempts", Value: 3, Sources: planfile.Vars(planScopeInstallMiseTools, "retry-attempts", "MISE_INSTALL_RETRY_ATTEMPTS"), Usage: "attempts for the final mise install"},
			&cli.IntFlag{Name: "retry-delay-seconds", Value: 10, Sources: planfile.Vars(planScopeInstallMiseTools, "retry-delay-seconds", "MISE_INSTALL_RETRY_DELAY_SECONDS"), Usage: "seconds between final mise install retry attempts"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			runner := mise.New()
			runner.Bin = resolveMiseBin(cmd.String("mise-bin"))

			return apptoolchain.InstallMiseTools(ctx, runner, os.Stdout, apptoolchain.InstallMiseToolsInput{
				Root:          cmd.String(flagMiseRoot),
				Locked:        cmd.String(flagLocked),
				Tools:         cmd.String("tools"),
				RetryAttempts: cmd.Int("retry-attempts"),
				RetryDelay:    time.Duration(cmd.Int("retry-delay-seconds")) * time.Second,
			})
		},
	}
}

func installChangelogRendererCmd() *cli.Command {
	return &cli.Command{
		Name:  "install-changelog-renderer",
		Usage: "install the pinned changelog renderer in an isolated mise tree",
		Description: `Installs pinned mise, creates an isolated mise data/cache/state tree,
installs either git-chglog or git-cliff with mise --no-config, symlinks the real
renderer binary into ~/.local/bin, and appends that bin directory to the runner
PATH file for subsequent steps. Every flag may also be fed from the
$REUSABLE_CI_PLAN plan file under the "toolchain install-changelog-renderer"
scope (flag > plan > env > default).`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "backend", Required: true, Sources: planfile.Vars(planScopeInstallChangelogRenderer, "backend", "CHANGELOG_BACKEND"), Usage: "changelog renderer backend: git-chglog or git-cliff"},
			&cli.StringFlag{Name: "git-chglog-version", Sources: planfile.Vars(planScopeInstallChangelogRenderer, "git-chglog-version", "GIT_CHGLOG_VERSION"), Usage: "pinned git-chglog version"},
			&cli.StringFlag{Name: "git-cliff-version", Sources: planfile.Vars(planScopeInstallChangelogRenderer, "git-cliff-version", "GIT_CLIFF_VERSION"), Usage: "pinned git-cliff version"},
			&cli.StringFlag{Name: "mise-version", Required: true, Sources: planfile.Vars(planScopeInstallChangelogRenderer, "mise-version", "MISE_VERSION"), Usage: "mise version without leading v"},
			&cli.StringFlag{Name: "mise-linux-x64-sha256", Required: true, Sources: planfile.Vars(planScopeInstallChangelogRenderer, "mise-linux-x64-sha256", "MISE_LINUX_X64_MUSL_TAR_GZ_SHA256"), Usage: "SHA-256 of mise-v<version>-linux-x64-musl.tar.gz"},
			&cli.StringFlag{Name: "mise-linux-arm64-sha256", Required: true, Sources: planfile.Vars(planScopeInstallChangelogRenderer, "mise-linux-arm64-sha256", "MISE_LINUX_ARM64_MUSL_TAR_GZ_SHA256"), Usage: "SHA-256 of mise-v<version>-linux-arm64-musl.tar.gz"},
			&cli.StringFlag{Name: "mise-base-url", Sources: planfile.Vars(planScopeInstallChangelogRenderer, "mise-base-url", "MISE_RELEASE_BASE_URL"), Usage: "override mise release base URL for tests/mirrors"},
			&cli.StringFlag{Name: flagBinHome, Sources: planfile.Vars(planScopeInstallChangelogRenderer, flagBinHome, "TOOLCHAIN_BIN_HOME"), Usage: "directory where the renderer symlink is written (default: $HOME/.local/bin)"},
			&cli.StringFlag{Name: flagPathFile, Required: true, Sources: planfile.Vars(planScopeInstallChangelogRenderer, flagPathFile, "FORGEJO_PATH", "GITHUB_PATH"), Usage: "runner path file to append the bin directory to"},
			&cli.StringFlag{Name: flagTempDir, Aliases: []string{flagTempDirLegacy}, Sources: tempDirPlan(planScopeInstallChangelogRenderer), Usage: "scratch directory for the isolated mise tree"},
			&cli.StringFlag{Name: "run-id", Sources: planfile.Chain(planScopeInstallChangelogRenderer, "run-id", cienv.RunID()), Usage: "run identifier used to name the isolated mise tree"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			runner := mise.New()

			return apptoolchain.InstallChangelogRenderer(ctx, runner, os.Stdout, apptoolchain.InstallChangelogRendererInput{
				Backend:          cmd.String("backend"),
				GitChglogVersion: cmd.String("git-chglog-version"),
				GitCliffVersion:  cmd.String("git-cliff-version"),
				Mise: apptoolchain.InstallMiseInput{
					Version:          cmd.String("mise-version"),
					LinuxX64SHA256:   cmd.String("mise-linux-x64-sha256"),
					LinuxARM64SHA256: cmd.String("mise-linux-arm64-sha256"),
					BaseURL:          cmd.String("mise-base-url"),
				},
				BinHome:    cmd.String(flagBinHome),
				PathFile:   cmd.String(flagPathFile),
				RunnerTemp: cmd.String(flagTempDir),
				RunID:      cmd.String("run-id"),
			})
		},
	}
}

func setupMiseEnvCmd() *cli.Command {
	return &cli.Command{
		Name:  "setup-mise-env",
		Usage: "write setup-toolchain mise PATH/env file entries",
		Description: `Every flag may also be fed from the $REUSABLE_CI_PLAN plan file under
the "toolchain setup-mise-env" scope (flag > plan > env > default).`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "cache", Value: "true", Sources: planfile.Vars(planScopeSetupMiseEnv, "cache", "SETUP_CACHE"), Usage: "setup-toolchain cache mode: true|false"},
			&cli.StringFlag{Name: flagBinHome, Sources: planfile.Vars(planScopeSetupMiseEnv, flagBinHome, "TOOLCHAIN_BIN_HOME"), Usage: "directory containing exposed tool symlinks (default: $HOME/.local/bin)"},
			&cli.StringFlag{Name: flagPathFile, Required: true, Sources: planfile.Vars(planScopeSetupMiseEnv, flagPathFile, "FORGEJO_PATH", "GITHUB_PATH"), Usage: "runner path file to append PATH entries to"},
			&cli.StringFlag{Name: "env-file", Required: true, Sources: planfile.Vars(planScopeSetupMiseEnv, "env-file", "FORGEJO_ENV", "GITHUB_ENV"), Usage: "runner env file receiving MISE_* directory exports"},
			&cli.StringFlag{Name: flagTempDir, Aliases: []string{flagTempDirLegacy}, Sources: tempDirPlan(planScopeSetupMiseEnv), Usage: "scratch directory for the isolated mise dirs"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			return apptoolchain.SetupMiseEnv(apptoolchain.SetupMiseEnvInput{
				Cache:      cmd.String("cache"),
				BinHome:    cmd.String(flagBinHome),
				PathFile:   cmd.String(flagPathFile),
				EnvFile:    cmd.String("env-file"),
				RunnerTemp: cmd.String(flagTempDir),
			})
		},
	}
}

func resolveMiseBin(value string) string {
	if value != "" {
		return value
	}

	home, err := os.UserHomeDir()
	if err == nil {
		candidate := filepath.Join(home, ".local", "bin", "mise")
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			return candidate
		}
	}

	return "mise"
}

func installSystemDependenciesCmd() *cli.Command {
	return &cli.Command{
		Name:  "install-system-dependencies",
		Usage: "install apt bootstrap packages when apt-get is available",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "packages", Required: true, Sources: cli.EnvVars("SYSTEM_DEPENDENCY_PACKAGES"), Usage: "whitespace-separated apt package names to install"},
			&cli.BoolFlag{Name: "skip-if-missing-apt", Value: true, Sources: cli.EnvVars("SKIP_IF_MISSING_APT"), Usage: "skip successfully when apt-get is unavailable"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return apptoolchain.InstallSystemDependencies(ctx, apt.New(), os.Stdout, apptoolchain.InstallSystemDependenciesInput{
				Packages:         cmd.String("packages"),
				SkipIfMissingAPT: cmd.Bool("skip-if-missing-apt"),
			})
		},
	}
}

func trustMiseConfigCmd() *cli.Command {
	return &cli.Command{
		Name:  "trust-mise-config",
		Usage: "trust the consumer repository mise config in the current directory",
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "mise-bin", Sources: cli.EnvVars("MISE_BIN"), Usage: "mise binary path (default: $HOME/.local/bin/mise when present, else mise from PATH)"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			runner := mise.New()
			runner.Bin = resolveMiseBin(cmd.String("mise-bin"))

			return apptoolchain.TrustMiseConfig(ctx, runner, os.Stdout, apptoolchain.TrustMiseConfigInput{})
		},
	}
}

func installMiseCmd() *cli.Command {
	return &cli.Command{
		Name:  "install-mise",
		Usage: "download, checksum-verify, and install the pinned mise binary",
		Description: `Installs mise from the official linux-<arch>-musl release
archive after verifying a caller-owned SHA-256 pin. The version and checksums are
passed as flags so the consuming CI template owns its pin/update policy while
reusable-ci owns the download, checksum, archive-shape, and install mechanics.
Every flag may also be fed from the $REUSABLE_CI_PLAN plan file under the
"toolchain install-mise" scope (flag > plan > env > default).`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: "version", Required: true, Sources: planfile.Vars(planScopeInstallMise, "version", "MISE_VERSION"), Usage: "mise version without leading v"},
			&cli.StringFlag{Name: "linux-x64-sha256", Required: true, Sources: planfile.Vars(planScopeInstallMise, "linux-x64-sha256", "MISE_LINUX_X64_MUSL_TAR_GZ_SHA256"), Usage: "SHA-256 of mise-v<version>-linux-x64-musl.tar.gz"},
			&cli.StringFlag{Name: "linux-arm64-sha256", Required: true, Sources: planfile.Vars(planScopeInstallMise, "linux-arm64-sha256", "MISE_LINUX_ARM64_MUSL_TAR_GZ_SHA256"), Usage: "SHA-256 of mise-v<version>-linux-arm64-musl.tar.gz"},
			&cli.StringFlag{Name: "dest-dir", Sources: planfile.Vars(planScopeInstallMise, "dest-dir", "MISE_INSTALL_DEST_DIR"), Usage: "installation directory (default: $HOME/.local/bin)"},
			&cli.StringFlag{Name: "base-url", Sources: planfile.Vars(planScopeInstallMise, "base-url", "MISE_RELEASE_BASE_URL"), Usage: "override release base URL for tests/mirrors (default: GitHub mise releases)"},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			_, err := apptoolchain.InstallMise(ctx, nil, os.Stderr, apptoolchain.InstallMiseInput{
				Version:          cmd.String("version"),
				LinuxX64SHA256:   cmd.String("linux-x64-sha256"),
				LinuxARM64SHA256: cmd.String("linux-arm64-sha256"),
				DestDir:          cmd.String("dest-dir"),
				BaseURL:          cmd.String("base-url"),
			})

			return err
		},
	}
}

func validateMiseInstallCmd() *cli.Command {
	return &cli.Command{
		Name:  "validate-mise-install",
		Usage: "validate setup-toolchain's mise install mode before token-bearing work",
		Description: `Checks the mise install policy used by CI setup wrappers: locked mode
requires a committed mise.lock, unlocked repositories with mise config require the
caller to confirm a GitHub rate-limit token is present, and locked backend tools
must pin their runtime (uv for pipx, go for go, rust or rustup+rust-toolchain for cargo).`,
		Flags: []cli.Flag{
			&cli.StringFlag{Name: flagMiseRoot, Value: ".", Sources: cli.EnvVars("MISE_PROJECT_ROOT"), Usage: usageMiseRoot},
			&cli.StringFlag{Name: flagLocked, Value: lockedDefault, Sources: cli.EnvVars("MISE_LOCKED_INSTALL"), Usage: "whether mise install will run in locked mode: true|false"},
			&cli.BoolFlag{Name: "github-token-present", Sources: cli.EnvVars("MISE_GITHUB_TOKEN_PRESENT"), Usage: "whether the caller has a GitHub rate-limit token; pass presence only, never the token"},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			return apptoolchain.ValidateMiseInstall(apptoolchain.ValidateMiseInstallInput{
				Root:               cmd.String(flagMiseRoot),
				Locked:             cmd.String(flagLocked),
				GithubTokenPresent: cmd.Bool("github-token-present"),
			})
		},
	}
}
