// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package validate

import (
	"context"
	"os"

	"github.com/urfave/cli/v3"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/git"
	appvalidate "github.com/diggsweden/reusable-ci/v3/internal/app/validate"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/cienv"
	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/version"
)

// tagGroup wires `reusable-ci validate tag <verb>` — every subcommand
// here validates one aspect of an annotated git tag (format,
// uniqueness, commit reachability, cryptographic signature).
func tagGroup() *cli.Command {
	return &cli.Command{
		Name:  "tag", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		Usage: "validate one aspect of a release tag",
		Commands: []*cli.Command{
			tagReleaseGuardCmd(),
			tagFormatCmd(),
			tagUniquenessCmd(),
			tagCommitCmd(),
			tagSignatureCmd(),
		},
	}
}

func tagReleaseGuardCmd() *cli.Command {
	return &cli.Command{
		Name:  "release-guard",
		Usage: "validate a stable release tag or release-request tag and emit normalized outputs",
		Description: `EXAMPLE:
   reusable-ci validate tag release-guard --tag release-request/v1.2.3`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "tag",
				Required: true,
				Sources:  cienv.Tag(),
				Usage:    "final tag or release-request tag to validate",
			},
			&cli.StringFlag{
				Name:    "pattern",
				Value:   version.StableSemverTagRE.String(),
				Sources: cli.EnvVars("RELEASE_TAG_PATTERN"),
				Usage:   "anchored regex the final tag must fully match",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return deps.FromCmd(ctx, cmd, func(d *deps.Deps) error {
				return appvalidate.ReleaseTagGuard(ctx, d.OutputSink, os.Stderr, appvalidate.ReleaseTagGuardInput{
					Tag:     cmd.String("tag"),
					Pattern: cmd.String("pattern"),
				})
			})
		},
	}
}

func refTypeCmd() *cli.Command {
	return &cli.Command{
		Name:  "ref-type", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		Usage: "require that the trigger is a tag push",
		Description: `EXAMPLE:
   reusable-ci validate ref-type --ref-type tag --ref-name v1.2.3`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "ref-type",
				Required: true,
				Sources:  cienv.RefType(),
				Usage:    "trigger ref type (\"tag\" required for releases)",
			},
			&cli.StringFlag{
				Name:     "ref-name",
				Required: true,
				Sources:  cienv.RefName(),
				Usage:    "trigger ref name (the tag or branch)",
			},
			&cli.StringFlag{
				Name:    "ref",
				Sources: cienv.Ref(),
				Usage:   "fully-qualified ref (refs/tags/X); when set the prefix is also checked",
			},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			return appvalidate.RefType(os.Stderr, appvalidate.RefTypeInput{
				RefType: provider.RefType(cmd.String("ref-type")),
				RefName: cmd.String("ref-name"),
				Ref:     cmd.String("ref"),
			})
		},
	}
}

func tagFormatCmd() *cli.Command {
	return &cli.Command{
		Name:  "format",
		Usage: "validate a tag against the project's permissive semver pattern",
		Description: `EXAMPLE:
   reusable-ci validate tag format --tag v1.2.3`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "tag",
				Required: true,
				Sources:  cienv.Tag(),
				Usage:    "tag name (e.g. v1.2.3)", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			},
		},
		Action: func(_ context.Context, cmd *cli.Command) error {
			return appvalidate.TagFormat(os.Stderr, appvalidate.TagFormatInput{Tag: cmd.String("tag")})
		},
	}
}

func tagUniquenessCmd() *cli.Command {
	return &cli.Command{
		Name:  "uniqueness",
		Usage: "fail when other tags point to the same commit (git-cliff guard)",
		Description: `EXAMPLE:
   reusable-ci validate tag uniqueness --tag v1.2.3`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "tag",
				Required: true,
				Sources:  cienv.Tag(),
				Usage:    "tag name (e.g. v1.2.3)",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return appvalidate.TagUniqueness(ctx, git.New(), os.Stderr,
				appvalidate.TagUniquenessInput{Tag: cmd.String("tag")})
		},
	}
}

func tagCommitCmd() *cli.Command {
	return &cli.Command{
		Name:  "commit",
		Usage: "verify the tag commit is reachable from origin/<branch>",
		Description: `EXAMPLE:
   reusable-ci validate tag commit --tag v1.2.3 --branch main`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "tag",
				Required: true,
				Sources:  cienv.Tag(),
				Usage:    "tag name (e.g. v1.2.3)",
			},
			&cli.StringFlag{
				Name:    "branch",
				Sources: cli.EnvVars("BRANCH"),
				Usage:   "branch the tag commit must be reachable from (default: repo default branch)",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			return appvalidate.TagCommit(ctx, git.New(), os.Stderr, appvalidate.TagCommitInput{
				Tag:    cmd.String("tag"),
				Branch: cmd.String("branch"),
			})
		},
	}
}

func tagSignatureCmd() *cli.Command {
	return &cli.Command{
		Name:  "signature",
		Usage: "verify a tag is annotated and cryptographically signed (GPG or SSH)",
		Description: `EXAMPLE:
   reusable-ci validate tag signature --tag v1.2.3 --repository org/app --require-allowlisted-signer`,
		Flags: []cli.Flag{
			&cli.StringFlag{
				Name:     "tag",
				Required: true,
				Sources:  cienv.Tag(),
				Usage:    "tag name (e.g. v1.2.3)",
			},
			&cli.StringFlag{
				Name:    "repository", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
				Sources: cienv.Repository(),
				Usage:   "repository slug used in error messages and SSH allowed-signers lookup",
			},
			&cli.StringFlag{
				Name:    "release-gpg-public-key",
				Sources: cli.EnvVars("RELEASE_GPG_PUBLIC_KEY"),
				Usage:   "armored GPG public key to import before verification",
			},
			&cli.BoolFlag{
				Name:    "require-allowlisted-signer",
				Sources: cli.EnvVars("REQUIRE_ALLOWLISTED_SIGNER"),
				Usage:   "require the signer to appear in .reusable-ci/allowed_signers (SSH) or .reusable-ci/allowed_gpg_keys.asc (GPG); missing/empty allowlist or unverifiable signature fails closed",
			},
			&cli.StringFlag{
				Name:    "allowed-signers-file",
				Sources: cli.EnvVars("ALLOWED_SIGNERS_FILE"),
				Usage:   "override the default SSH allowed_signers path (default: .reusable-ci/allowed_signers)",
			},
			&cli.StringFlag{
				Name:    "allowed-gpg-keys-file",
				Sources: cli.EnvVars("ALLOWED_GPG_KEYS_FILE"),
				Usage:   "override the default armored allowed-GPG-keys bundle path (default: .reusable-ci/allowed_gpg_keys.asc); its keys are both verification material and the authorised set",
			},
		},
		Action: func(ctx context.Context, cmd *cli.Command) error {
			in := appvalidate.TagSignatureInput{
				Tag:                      cmd.String("tag"),
				Repository:               cmd.String("repository"),
				RequireAllowlistedSigner: cmd.Bool("require-allowlisted-signer"),
				AllowedSignersPath:       cmd.String("allowed-signers-file"),
				AllowedGPGKeysPath:       cmd.String("allowed-gpg-keys-file"),
			}
			if k := cmd.String("release-gpg-public-key"); k != "" {
				in.ReleaseGPGPublicKey = []byte(k)
			}

			return appvalidate.TagSignature(ctx, git.New(), os.Stderr, deps.Annotator(cmd), in)
		},
	}
}
