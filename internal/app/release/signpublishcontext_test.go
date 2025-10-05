// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/stretchr/testify/require"
)

const signPublishEnvPrefix = "EXISTING=preserve me\nRELEASE_TAG=previous-value\n"

func newSignPublishContextInput(t *testing.T) (apprelease.SignPublishContextInput, string) {
	t.Helper()

	// Even a dropped RunnerTemp argument must only create state in test-owned storage.
	fallback := t.TempDir()
	t.Setenv("TMPDIR", fallback)
	t.Chdir(t.TempDir())
	in := apprelease.SignPublishContextInput{
		ReleaseSHA:   "abcdef1234567890abcdef1234567890abcdef12",
		ReleaseTag:   "v1.2.3",
		ArtifactPath: filepath.Join(t.TempDir(), "artifact payload"),
		EnvFile:      filepath.Join(t.TempDir(), "runner.env"),
		RunnerTemp:   t.TempDir(),
	}
	require.NoError(t, os.Mkdir(in.ArtifactPath, 0o700))
	require.NoError(t, os.WriteFile(in.EnvFile, []byte(signPublishEnvPrefix), 0o600))

	return in, fallback
}

func requireSignPublishStateDir(t *testing.T, parent string) string {
	t.Helper()

	entries, err := os.ReadDir(parent)
	require.NoError(t, err)
	require.Len(t, entries, 1, "exactly one per-job state directory")
	require.True(t, strings.HasPrefix(entries[0].Name(), "reusable-ci-sign-and-publish."))
	require.NotEqual(t, "reusable-ci-sign-and-publish.", entries[0].Name())
	stateDir := filepath.Join(parent, entries[0].Name())
	info, err := os.Lstat(stateDir)
	require.NoError(t, err)
	require.True(t, info.IsDir(), "state must be a real directory, not a symlink")
	require.Equal(t, os.FileMode(0o700), info.Mode().Perm())

	children, err := os.ReadDir(stateDir)
	require.NoError(t, err)
	require.Empty(t, children)

	return stateDir
}

func TestSignPublishContext_AppendsCompleteEnvironment(t *testing.T) {
	for _, testCase := range []struct {
		name        string
		releaseSHA  string
		runnerValue string
	}{
		{name: "explicit_runner_temp", releaseSHA: "abcdef1234567890abcdef1234567890abcdef12", runnerValue: "explicit"},
		{name: "owned_tmpdir_fallback", releaseSHA: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},
		{name: "blank_runner_temp_fallback", releaseSHA: "abcdef1234567890abcdef1234567890abcdef12", runnerValue: " \t"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			in, fallback := newSignPublishContextInput(t)
			dist, runner := in.ArtifactPath, in.RunnerTemp
			parent, untouched := runner, fallback

			if testCase.runnerValue != "explicit" {
				in.RunnerTemp = testCase.runnerValue
				parent, untouched = fallback, runner
			}

			in.ReleaseSHA = " \t" + testCase.releaseSHA + "\n"
			in.ReleaseTag = "\n v1.2.3 \t"
			in.ArtifactPath = " \t" + dist + "/// \n"

			var out bytes.Buffer
			require.NoError(t, apprelease.SignPublishContext(&out, in))
			stateDir := requireSignPublishStateDir(t, parent)
			body, err := os.ReadFile(in.EnvFile)
			require.NoError(t, err)

			want := signPublishEnvPrefix +
				"RELEASE_TAG=v1.2.3\n" +
				"FORGEJO_REF_NAME=v1.2.3\n" +
				"RELEASE_SHA=" + testCase.releaseSHA + "\n" +
				"DIST_DIR=" + dist + "\n" +
				"RELEASE_FILES_MANIFEST=" + dist + "/release-files.json\n" +
				"SIGN_AND_PUBLISH_STATE_DIR=" + stateDir + "\n"
			require.Equal(t, want, string(body), "preserve the seeded prefix and append exactly six entries")
			require.Equal(t, "sign-and-publish context exported for v1.2.3 ("+testCase.releaseSHA+")\n", out.String())

			entries, err := os.ReadDir(untouched)
			require.NoError(t, err)
			require.Empty(t, entries, "state must be created under the selected temp root only")
		})
	}
}

func TestSignPublishContext_RefusesInvalidInputsWithoutSideEffects(t *testing.T) {
	tests := []struct {
		name        string
		change      func(*apprelease.SignPublishContextInput)
		errContains string
	}{
		{name: "missing_sha", change: func(in *apprelease.SignPublishContextInput) { in.ReleaseSHA = " \t" }, errContains: "release-sha must be"},
		{name: "uppercase_sha", change: func(in *apprelease.SignPublishContextInput) { in.ReleaseSHA = strings.Repeat("A", 40) }, errContains: "release-sha must be"},
		{name: "missing_tag", change: func(in *apprelease.SignPublishContextInput) { in.ReleaseTag = " \n" }, errContains: "release-tag must be stable"},
		{name: "unstable_tag", change: func(in *apprelease.SignPublishContextInput) { in.ReleaseTag = "v1.2.3-rc1" }, errContains: "release-tag must be stable"},
		{name: "missing_artifact_path", change: func(in *apprelease.SignPublishContextInput) { in.ArtifactPath = " \t" }, errContains: "artifact-path is required"},
		{name: "slash_only_artifact_path", change: func(in *apprelease.SignPublishContextInput) { in.ArtifactPath = " /// \n" }, errContains: "artifact-path is required"},
		{name: "missing_env_file", change: func(in *apprelease.SignPublishContextInput) { in.EnvFile = " \t" }, errContains: "env-file is required"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			in, fallback := newSignPublishContextInput(t)
			envFile := in.EnvFile
			testCase.change(&in)

			var out bytes.Buffer

			err := apprelease.SignPublishContext(&out, in)
			require.ErrorIs(t, err, errs.ErrUsage)
			require.ErrorContains(t, err, "sign-publish-context: "+testCase.errContains)
			require.Empty(t, out.String())

			body, err := os.ReadFile(envFile)
			require.NoError(t, err)
			require.Equal(t, signPublishEnvPrefix, string(body))

			for _, parent := range []string{in.RunnerTemp, fallback} {
				entries, readErr := os.ReadDir(parent)
				require.NoError(t, readErr)
				require.Empty(t, entries, "input refusal must precede state creation")
			}
		})
	}
}

func TestSignPublishContext_FilesystemFailures(t *testing.T) {
	tests := []struct {
		name        string
		wantErr     error
		errContains string
		wantState   bool
	}{
		{name: "missing_runner_temp", wantErr: os.ErrNotExist, errContains: "create state dir"},
		{name: "non_directory_runner_temp", wantErr: syscall.ENOTDIR, errContains: "create state dir"},
		{name: "blocked_env_parent", wantErr: syscall.ENOTDIR, errContains: "create env file dir", wantState: true},
		{name: "env_file_is_directory", wantErr: syscall.EISDIR, errContains: "append env entries", wantState: true},
		{name: "artifact_line_feed", wantErr: errs.ErrValidation, errContains: "append env entries", wantState: true},
		{name: "artifact_carriage_return", wantErr: errs.ErrValidation, errContains: "append env entries", wantState: true},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			in, fallback := newSignPublishContextInput(t)
			envFile, runner := in.EnvFile, in.RunnerTemp

			var marker string

			switch testCase.name {
			case "missing_runner_temp":
				in.RunnerTemp = filepath.Join(runner, "missing")
				_, err := os.Lstat(in.RunnerTemp)
				require.ErrorIs(t, err, os.ErrNotExist)
			case "non_directory_runner_temp":
				in.RunnerTemp = filepath.Join(runner, "not-a-directory")
				marker = in.RunnerTemp
			case "blocked_env_parent":
				marker = filepath.Join(filepath.Dir(envFile), "blocked")
				in.EnvFile = filepath.Join(marker, "runner.env")
			case "env_file_is_directory":
				in.EnvFile = filepath.Join(filepath.Dir(envFile), "env-directory")
				require.NoError(t, os.Mkdir(in.EnvFile, 0o700))
				marker = filepath.Join(in.EnvFile, "keep")
			case "artifact_line_feed":
				in.ArtifactPath += "\nINJECTED=value"
				require.NoError(t, os.Mkdir(in.ArtifactPath, 0o700))
			case "artifact_carriage_return":
				in.ArtifactPath += "\rINJECTED=value"
				require.NoError(t, os.Mkdir(in.ArtifactPath, 0o700))
			}

			if marker != "" {
				require.NoError(t, os.WriteFile(marker, []byte("keep existing bytes\n"), 0o600))
			}

			var out bytes.Buffer

			err := apprelease.SignPublishContext(&out, in)
			require.ErrorIs(t, err, testCase.wantErr)
			require.ErrorContains(t, err, "sign-publish-context: "+testCase.errContains)
			require.Empty(t, out.String())

			body, err := os.ReadFile(envFile)
			require.NoError(t, err)
			require.Equal(t, signPublishEnvPrefix, string(body), "failed append must not rewrite the seeded env file")

			if marker != "" {
				body, readErr := os.ReadFile(marker)
				require.NoError(t, readErr)
				require.Equal(t, "keep existing bytes\n", string(body))
			}

			if testCase.wantState {
				// Env failures occur after MkdirTemp. The operation is not transactional:
				// it preserves the env file but does not roll back the private state dir.
				requireSignPublishStateDir(t, runner)
			} else {
				entries, readErr := os.ReadDir(runner)
				require.NoError(t, readErr)

				if marker == "" {
					require.Empty(t, entries)
				} else {
					require.Len(t, entries, 1)
					require.Equal(t, filepath.Base(marker), entries[0].Name())
				}
			}

			entries, err := os.ReadDir(fallback)
			require.NoError(t, err)
			require.Empty(t, entries, "an explicit failing temp root must not fall back")
		})
	}
}
