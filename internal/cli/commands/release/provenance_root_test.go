// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"bytes"
	"fmt"
	"log"
	"log/slog"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	rootcli "github.com/diggsweden/reusable-ci/v3/internal/cli"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testenv"
)

func TestProvenanceRoot_GenerateOnly(t *testing.T) {
	for _, tc := range []struct {
		name, runner, repo, ref, sha, builder, invocation string
		missing                                           string
		legacy                                            bool
	}{
		{
			name: "github", runner: "github", repo: "https://github.invalid/gh/source", ref: "v1.2.3",
			sha:        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			builder:    "https://github.invalid/gh/source/.github/workflows/build.yml@refs/tags/v1.2.3",
			invocation: "https://github.invalid/gh/source/actions/runs/11",
		},
		{
			name: "forgejo", runner: "forgejo", repo: "https://forgejo.invalid/fj/source", ref: "v4.5.6",
			sha:        "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			builder:    "https://forgejo.invalid/fj/source/.forgejo/workflows/build.yml@refs/tags/v4.5.6",
			invocation: "https://forgejo.invalid/fj/source/actions/runs/22",
		},
		{
			name: "gitlab", runner: "gitlab", repo: "https://gitlab.invalid/gl/source", ref: "v7.8.9",
			sha:        "cccccccccccccccccccccccccccccccccccccccc",
			builder:    "https://gitlab.invalid/gl/source/-/jobs/33",
			invocation: "https://gitlab.invalid/gl/source/-/jobs/33",
		},
		{
			name: "compatibility aliases", runner: "aliases", repo: "https://github.invalid/gh/source", ref: "v1.2.3",
			sha:        "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
			builder:    "https://github.invalid/gh/source/.github/workflows/build.yml@refs/tags/v1.2.3",
			invocation: "https://github.invalid/gh/source/actions/runs/11",
		},
		{name: "local refuses unknown source", runner: "local"},
		{name: "missing runner repository", runner: "github", missing: "GITHUB_REPOSITORY"},
		{
			name: "legacy native preserved", runner: "forgejo", legacy: true,
			repo: "https://forgejo.invalid/fj/source", ref: "v4.5.6", sha: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			builder:    "https://forgejo.invalid/fj/source/.forgejo/workflows/legacy.yml@v4.5.6",
			invocation: "https://forgejo.invalid/fj/source/actions/runs/22",
		},
		{
			// Preservation of the shipped shape is not a cross-forge trust policy.
			name: "legacy cross forge preserved", runner: "github", legacy: true,
			repo: "https://forgejo.invalid/fj/source", ref: "v4.5.6", sha: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
			builder:    "https://forgejo.invalid/fj/source/.forgejo/workflows/legacy.yml@v4.5.6",
			invocation: "https://github.invalid/gh/source/actions/runs/11",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			targets := []string{"github", "forgejo", "gitlab", "local"}

			var profileArgs []string

			if tc.legacy {
				targets = []string{"forgejo"}
				profileArgs = []string{"--profile", "forgejo-actions", "--workflow", "legacy.yml"}
			}

			for _, target := range targets {
				t.Run(target, func(t *testing.T) {
					env := provenanceRootEnv(t, tc.runner)
					if tc.missing != "" {
						t.Setenv(tc.missing, "")
					}

					const checksums = "dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd  app.tgz\n"
					require.NoError(t, os.WriteFile(env.Path("checksums"), []byte(checksums), 0o600))
					require.NoError(t, os.WriteFile(env.Path("statement.json"), []byte("output canary\n"), 0o600))
					args := append([]string{"reusable-ci", "--provider", target, "--format", "text", "release", "provenance",
						"--checksum-file", env.Path("checksums"), "--output", env.Path("statement.json"),
						"--started-on", "2026-01-02T03:04:05Z", "--go-sum", ""}, profileArgs...)
					command := rootcli.New(rootcli.BuildInfo{Version: "dev"})
					command.Writer, command.ErrWriter = os.Stdout, os.Stderr
					runErr := command.Run(t.Context(), args)
					body, err := os.ReadFile(env.Path("statement.json"))
					require.NoError(t, err)

					if tc.repo == "" {
						require.ErrorIs(t, runErr, errs.ErrMissingInput)
						require.ErrorContains(t, runErr, "runner repository URL is unknown")
						require.Equal(t, "output canary\n", string(body))
					} else {
						require.NoError(t, runErr)

						ext := fmt.Sprintf(`{"source":%q,"ref":%q}`, "git+"+tc.repo, tc.ref)
						buildType, internal := "https://diggsweden.github.io/reusable-ci/release-build/v1", "{}"

						if tc.legacy {
							ext = fmt.Sprintf(`{"workflow":{"repository":%q,"ref":%q,"path":".forgejo/workflows/legacy.yml"}}`, tc.repo, tc.ref)
							buildType, internal = "https://forgejo.org/actions/buildtypes/workflow/v1", `{"runner":"forgejo-actions"}`
						}

						want := fmt.Sprintf(`{
							"_type":"https://in-toto.io/Statement/v1",
							"subject":[{"name":"app.tgz","digest":{"sha256":"dddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddddd"}}],
							"predicateType":"https://slsa.dev/provenance/v1",
							"predicate":{
								"buildDefinition":{"buildType":%q,"externalParameters":%s,"internalParameters":%s,
									"resolvedDependencies":[{"uri":%q,"digest":{"gitCommit":%q}}]},
								"runDetails":{"builder":{"id":%q},"metadata":{"invocationId":%q,
									"startedOn":"2026-01-02T03:04:05Z","finishedOn":"2026-01-02T03:04:05Z"}}
							}}`, buildType, ext, internal, "git+"+tc.repo+"@"+tc.ref, tc.sha, tc.builder, tc.invocation)
						require.JSONEq(t, want, string(body))
					}

					for name, want := range map[string]string{"checksums": checksums, "command-log": "", "GITHUB_OUTPUT": "sink canary\n", "GITHUB_STEP_SUMMARY": "sink canary\n", "CI_OUTPUT": "sink canary\n", "CI_SUMMARY_FILE": "sink canary\n"} {
						got, readErr := os.ReadFile(env.Path(name))
						require.NoError(t, readErr)
						require.Equal(t, want, string(got), name)
					}

					require.NoFileExists(t, env.Path("statement.json.bundle"))
					require.NoDirExists(t, env.Path("results"))
				})
			}
		})
	}
}

func TestProvenanceRootEnv_RestoresLogging(t *testing.T) {
	for _, mode := range []string{"existing default", "custom default"} {
		t.Run(mode, func(t *testing.T) {
			originalLogger := slog.Default()
			originalWriter, originalFlags := log.Writer(), log.Flags()

			t.Cleanup(func() {
				slog.SetDefault(originalLogger)
				log.SetOutput(originalWriter)
				log.SetFlags(originalFlags)
			})

			var structured, standard bytes.Buffer
			if mode == "custom default" {
				slog.SetDefault(slog.New(slog.NewTextHandler(&structured, nil)))
			}

			logger := slog.Default()

			log.SetOutput(&standard)
			log.SetFlags(log.Lmsgprefix)

			stdout, stderr := os.Stdout, os.Stderr

			var commandLog *os.File

			t.Run("fixture", func(t *testing.T) {
				env := provenanceRootEnv(t, "github")
				commandLog = os.Stderr
				command := rootcli.New(rootcli.BuildInfo{Version: "dev"})
				command.Writer, command.ErrWriter = os.Stdout, os.Stderr
				// Root logger setup runs before this owned missing-input refusal.
				err := command.Run(t.Context(), []string{"reusable-ci", "--provider", "forgejo", "--format", "text", "release", "provenance",
					"--checksum-file", env.Path("missing-checksums"), "--output", env.Path("statement.json"),
					"--started-on", "2026-01-02T03:04:05Z", "--go-sum", ""})
				require.ErrorIs(t, err, os.ErrNotExist)
			})

			require.NotNil(t, commandLog)
			_, err := commandLog.Stat()
			require.ErrorIs(t, err, os.ErrClosed)
			require.Same(t, stdout, os.Stdout)
			require.Same(t, stderr, os.Stderr)
			require.Same(t, logger, slog.Default())
			require.Same(t, &standard, log.Writer())
			require.Equal(t, log.Lmsgprefix, log.Flags())
			log.Print("after fixture cleanup")
			require.Equal(t, log.Prefix()+"after fixture cleanup\n", standard.String())
			require.Empty(t, structured.String())
		})
	}
}

func provenanceRootEnv(t *testing.T, runner string) *testenv.Env {
	t.Helper()
	env := testenv.New(t)
	t.Chdir(env.Temp)
	t.Setenv("PATH", env.MkdirAll("empty-path"))

	for key, value := range map[string]string{
		"REUSABLE_CI_PLAN": "", "REUSABLE_CI_PROVIDER": "", "REUSABLE_CI_RUNNER": "",
		"SIGN_METHOD": "", "SIGN_KEY": "", "SLSA_EXTERNAL_PARAMETERS_JSON": "",
		"SOURCE_DATE_EPOCH": "invalid-unused-epoch",
		"GITHUB_SERVER_URL": "https://github.invalid///", "GITHUB_REPOSITORY": "gh/source", "GITHUB_REF_NAME": "v1.2.3", "GITHUB_SHA": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"GITHUB_WORKFLOW_REF": "gh/source/.github/workflows/build.yml@refs/tags/v1.2.3", "GITHUB_WORKFLOW": "Wrong display name", "GITHUB_RUN_ID": "11",
		"FORGEJO_SERVER_URL": "https://forgejo.invalid///", "FORGEJO_REPOSITORY": "fj/source", "FORGEJO_REF_NAME": "v4.5.6", "FORGEJO_SHA": "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb",
		"FORGEJO_WORKFLOW_REF": "fj/source/.forgejo/workflows/build.yml@refs/tags/v4.5.6", "FORGEJO_RUN_ID": "22",
		"CI_SERVER_URL": "https://gitlab.invalid///", "CI_PROJECT_PATH": "gl/source", "CI_PROJECT_URL": "https://gitlab.invalid/gl/source", "CI_COMMIT_REF_NAME": "v7.8.9", "CI_COMMIT_SHA": "cccccccccccccccccccccccccccccccccccccccc",
		"CI_JOB_URL": "https://gitlab.invalid/gl/source/-/jobs/33", "CI_PIPELINE_URL": "https://gitlab.invalid/gl/source/-/pipelines/44",
		"REPOSITORY": "neutral/wrong", "CI_REPO": "neutral/wrong", "REF_NAME": "wrong-neutral-ref", "CI_REF_NAME": "wrong-ci-ref",
		"CI_COMMIT": "eeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeeee", "COMMIT_SHA": "ffffffffffffffffffffffffffffffffffffffff",
		"FORGEJO_SERVER": "https://alias.invalid", "FORGEJO_REPO": "alias/wrong", "CI_RUN_ID": "wrong-neutral-run",
	} {
		t.Setenv(key, value)
	}

	switch runner {
	case "github":
		t.Setenv("GITHUB_ACTIONS", "true")
	case "forgejo":
		t.Setenv("GITHUB_ACTIONS", "true")
		t.Setenv("FORGEJO_ACTIONS", "true")
	case "aliases":
		t.Setenv("GITHUB_ACTIONS", "true")
		t.Setenv("GITEA_ACTIONS", "true")

		for _, key := range []string{"FORGEJO_SERVER_URL", "FORGEJO_REPOSITORY", "FORGEJO_REF_NAME", "FORGEJO_SHA", "FORGEJO_WORKFLOW_REF", "FORGEJO_RUN_ID"} {
			t.Setenv(key, "")
		}
	case "gitlab":
		t.Setenv("GITLAB_CI", "true")
	}

	for _, key := range []string{"GITHUB_OUTPUT", "GITHUB_STEP_SUMMARY", "CI_OUTPUT", "CI_SUMMARY_FILE"} {
		t.Setenv(key, env.Path(key))
		require.NoError(t, os.WriteFile(env.Path(key), []byte("sink canary\n"), 0o600))
	}

	t.Setenv("CI_RESULTS_DIR", env.Path("results"))

	logFile, err := os.Create(env.Path("command-log"))
	require.NoError(t, err)

	stdout, stderr, logger := os.Stdout, os.Stderr, slog.Default()
	standardWriter, standardFlags := log.Writer(), log.Flags()
	os.Stdout, os.Stderr = logFile, logFile //nolint:reassign // capture the real root command's process streams in this serial test.

	t.Cleanup(func() {
		os.Stdout, os.Stderr = stdout, stderr //nolint:reassign // restore process streams before closing the owned file.

		slog.SetDefault(logger)
		// SetDefault can rewire the standard logger, so restore it last.
		log.SetOutput(standardWriter)
		log.SetFlags(standardFlags)
		require.NoError(t, logFile.Close())
	})

	return env
}
