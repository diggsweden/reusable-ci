// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/ghaenv"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testenv"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
)

func TestGPGImportCommand_ForwardsResolvedPassphraseWithoutOutput(t *testing.T) {
	for _, fromFile := range []bool{false, true} {
		t.Run(map[bool]string{false: "environment", true: "file"}[fromFile], func(t *testing.T) {
			testenv.New(t)
			t.Setenv("GPG_PRIVATE_KEY", "synthetic-private-key")
			t.Setenv("GPG_PASSPHRASE", "env-secret-fixture")

			var got apprelease.GPGImportInput

			calls := 0
			command := gpgImportCommand(func(_ context.Context, _ *cli.Command, in apprelease.GPGImportInput) error {
				calls++
				got = in

				return nil
			})

			var out bytes.Buffer

			command.Writer = &out
			command.ErrWriter = &out
			args := []string{"import", "--git-user-signingkey", "--git-commit-gpgsign", "--git-config-global"}

			want := "env-secret-fixture"
			if fromFile {
				want = "file-secret-fixture"
				file := filepath.Join(t.TempDir(), "passphrase")
				require.NoError(t, os.WriteFile(file, []byte(want), 0o600))
				args = append(args, "--passphrase-file", file)
			}

			require.NoError(t, command.Run(t.Context(), args))
			require.Equal(t, 1, calls)
			require.Equal(t, apprelease.GPGImportInput{PrivateKey: "synthetic-private-key", Passphrase: want, GitUserSigningKey: true, GitCommitGPGSign: true, GitConfigGlobal: true}, got)
			require.Empty(t, out.String())
		})
	}
}

func TestProvenanceCommand_ExternalParametersReachStatement(t *testing.T) {
	for _, tc := range []struct {
		value string
		want  error
	}{{`{"fixture":{"value":42}}`, nil}, {`{`, errs.ErrMalformedInput}, {`{"image":"override"}`, errs.ErrValidation}, {`{"source":"override"}`, errs.ErrValidation}} {
		t.Run(tc.value, func(t *testing.T) {
			env := ghaenv.Setup(t)
			env.Setenv("GITHUB_REPOSITORY", "owner/repo")
			env.Setenv("GITHUB_SERVER_URL", "https://github.invalid")
			env.Setenv("GITHUB_REF_NAME", "v1.2.3")
			env.Setenv("GITHUB_REF_TYPE", "tag")
			env.Setenv("GITHUB_SHA", strings.Repeat("a", 40))
			env.Setenv("GITHUB_WORKFLOW_REF", "owner/repo/.github/workflows/release.yml@refs/tags/v1.2.3")
			env.Setenv("GITHUB_RUN_ID", "7")

			root := t.TempDir()
			t.Chdir(root)
			require.NoError(t, os.WriteFile("checksums", []byte(strings.Repeat("b", 64)+"  asset.tgz\n"), 0o600))

			args := []string{New().Name, "provenance", "--checksum-file", "checksums", "--go-sum", "", "--started-on", "2026-01-01T00:00:00Z", "--external-parameters-json", tc.value, "--output", "statement.json"}
			err := New().Run(t.Context(), args)
			require.ErrorIs(t, err, tc.want)

			if tc.want != nil {
				_, statErr := os.Stat("statement.json")
				require.ErrorIs(t, statErr, os.ErrNotExist)

				return
			}

			body, err := os.ReadFile("statement.json")
			require.NoError(t, err)

			var statement struct {
				Predicate struct {
					BuildDefinition struct {
						ExternalParameters map[string]json.RawMessage `json:"externalParameters"`
					} `json:"buildDefinition"`
				} `json:"predicate"`
			}
			require.NoError(t, json.Unmarshal(body, &statement))
			require.JSONEq(t, `{"value":42}`, string(statement.Predicate.BuildDefinition.ExternalParameters["fixture"]))
		})
	}
}

func TestPublishCommand_ForwardsEveryAssetInOrder(t *testing.T) {
	for _, manifest := range []bool{false, true} {
		t.Run(map[bool]string{false: "explicit", true: "manifest"}[manifest], func(t *testing.T) {
			testenv.New(t)
			t.Chdir(t.TempDir())
			require.NoError(t, os.Mkdir("dist", 0o700))

			for _, name := range []string{"notes.md", "z.tgz", "a.tgz", "checksums.sha256"} {
				require.NoError(t, os.WriteFile(filepath.Join("dist", name), []byte("fixture"), 0o600))
			}

			log, err := os.Create(filepath.Join(t.TempDir(), "stderr"))
			require.NoError(t, err)

			previous := os.Stderr
			os.Stderr = log

			t.Cleanup(func() { os.Stderr = previous; _ = log.Close() })

			args := []string{New().Name, "publish", "--dry-run", "--draft=" + strconv.FormatBool(manifest), "--tag", "v1.2.3", "--repository", "owner/repo", "--release-notes-file", "dist/notes.md"}
			if manifest {
				require.NoError(t, os.WriteFile("dist/release-files.json", []byte(`{"version":1,"assets":[{"name":"z.tgz","path":"dist/z.tgz","source":"fixture"},{"name":"a.tgz","path":"dist/a.tgz","source":"fixture"},{"name":"checksums.sha256","path":"dist/checksums.sha256","source":"fixture"}],"checksums":[{"name":"checksums.sha256","path":"dist/checksums.sha256","source":"fixture"}]}`), 0o600))
			} else {
				args = append(args, "--asset", "dist/z.tgz", "--asset", "dist/a.tgz", "--asset", "dist/checksums.sha256")
			}

			require.NoError(t, New().Run(t.Context(), args))

			body, err := os.ReadFile(log.Name())
			require.NoError(t, err)
			require.Contains(t, string(body), "draft="+strconv.FormatBool(manifest)+", prerelease=false")

			var assets []string

			for _, line := range strings.Split(string(body), "\n") {
				if value, ok := strings.CutPrefix(line, "[dry-run] would upload asset "); ok {
					assets = append(assets, value)
				}
			}

			require.Equal(t, []string{"dist/z.tgz", "dist/a.tgz", "dist/checksums.sha256"}, assets)
		})
	}
}
