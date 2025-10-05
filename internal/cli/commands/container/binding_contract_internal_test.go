// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	domaincontainer "github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/imageledger"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provenance"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testenv"
	"github.com/stretchr/testify/require"
	"github.com/urfave/cli/v3"
)

func TestBuildBinding_CompleteInputAndSecretForms(t *testing.T) {
	for _, raw := range []string{`["id=a,src=fixture/a","id=b,src=fixture/b"]`, "id=a,src=fixture/a\nid=b,src=fixture/b"} {
		t.Run(raw, func(t *testing.T) {
			testenv.New(t)

			cmd := buildCmd()

			var got appcontainer.BuildImageInput

			cmd.Action = func(_ context.Context, parsed *cli.Command) error {
				var err error

				got, err = buildImageInputFromCmd(parsed)

				return err
			}
			require.NoError(t, cmd.Run(t.Context(), []string{cmd.Name, "--context", "ctx", "--file", "fixture.Containerfile", "--target", "compile-stage", "--platform", "linux/amd64", "--build-args", "A=1\nB=2", "--secrets", raw, "--labels", "x=y", "--source-date-epoch", "7", "--cache-repo", "registry.example/cache", "--cache-scope", "scope", "--cache-push", "--mode", "load", "--image-ref", "registry.example/app:tag", "--output-dir", "out", "--digest-key", "out-digest"}))
			require.Equal(t, appcontainer.BuildImageInput{Context: "ctx", Containerfile: "fixture.Containerfile", Target: "compile-stage", Platform: "linux/amd64", BuildArgs: []string{"A=1", "B=2"}, Secrets: []string{"id=a,src=fixture/a", "id=b,src=fixture/b"}, Labels: []string{"x=y"}, SourceDateEpoch: "7", CacheRepo: "registry.example/cache", CacheScope: "scope", CachePush: true, Mode: domaincontainer.BuildModeLoad, ImageRef: "registry.example/app:tag", OutputDir: "out", DigestKey: "out-digest"}, got)
		})
	}

	for _, raw := range []string{`[1]`, `["broken"`, `[""]`, `["line\nbreak"]`} {
		t.Run(raw, func(t *testing.T) {
			testenv.New(t)

			cmd := buildCmd()
			cmd.Action = func(_ context.Context, parsed *cli.Command) error {
				_, err := buildImageInputFromCmd(parsed)

				return err
			}
			require.Error(t, cmd.Run(t.Context(), []string{cmd.Name, "--mode", "load", "--secrets", raw}))
		})
	}
}

func TestLedgerAddBinding_PersistsCompleteEvidence(t *testing.T) {
	testenv.New(t)
	path := filepath.Join(t.TempDir(), "ledger.json")
	digest := "sha256:" + strings.Repeat("a", 64)
	pin := strings.Repeat("b", 64)
	cmd := ledgerAddCmd()
	require.NoError(t, cmd.Run(t.Context(), []string{cmd.Name, "--ledger", path, "--tag", "v1.2.3", "--role", "app", "--ref", "registry.example/app@" + digest, "--digest", digest, "--sbom", "dist/app.cyclonedx.json", "--sbom-sha256", pin, "--final-tag", "registry.example/app:v1.2.3", "--moving-tag", "registry.example/app:latest", "--candidate-tag", "registry.example/app:staging-v1.2.3", "--provenance-json", `{"fixture":42}`}))
	body, err := os.ReadFile(path)
	require.NoError(t, err)
	entries, err := imageledger.Parse(body)
	require.NoError(t, err)
	require.Equal(t, []imageledger.Entry{{ImageKind: imageledger.ImageKindRelease, Role: "app", Ref: "registry.example/app@" + digest, Digest: digest, SBOM: "dist/app.cyclonedx.json", SBOMSHA256: pin, FinalTag: "registry.example/app:v1.2.3", MovingTag: "registry.example/app:latest", CandidateTag: "registry.example/app:staging-v1.2.3", Provenance: map[string]any{"fixture": float64(42)}}}, entries)
}

func TestReleaseImageBinding_RecordsEveryExpectation(t *testing.T) {
	testenv.New(t)

	cmd := releaseImagesVerifyCmd()

	var got appcontainer.ReleaseImageVerifyExistingInput

	cmd.Action = func(_ context.Context, parsed *cli.Command) error {
		got = releaseImageVerifyExistingInputFromCmd(parsed)

		return nil
	}
	require.NoError(t, cmd.Run(t.Context(), []string{cmd.Name, "--ref", "image-value", "--cosign-public-key-path", "key.pub", "--expected-tag", "release-tag", "--expected-commit", "commit", "--expected-source", "source", "--expected-workflow", "workflow", "--expected-base-ref", "base-value", "--expected-base-input-id", "base-id", "--allow-reattest", "--identity-version", "version-value", "--identity-ref-name", "identity-ref", "--identity-source", "source-value"}))
	require.Equal(t, appcontainer.ReleaseImageVerifyExistingInput{Ref: "image-value", CosignPublicKey: "key.pub", ExpectedTag: "release-tag", ExpectedCommit: "commit", ExpectedSource: "source", ExpectedWorkflow: "workflow", ExpectedBaseRef: "base-value", ExpectedBaseInputID: "base-id", AllowReattest: true, ExpectedIdentityVersion: "version-value", ExpectedIdentityRefName: "identity-ref", ExpectedIdentitySource: "source-value"}, got)
}

var errAuditCopy = errors.New("copy fixture failure")
var errAuditDelete = errors.New("delete fixture failure")

type failingAuditRegistry struct {
	imageledger.PromotionRollbackRegistry
}

func (failingAuditRegistry) CopyTag(context.Context, string, string) error { return errAuditCopy }
func (failingAuditRegistry) DeleteTag(context.Context, string) error       { return errAuditDelete }

func TestAuditBindings_PreserveEveryMutationError(t *testing.T) {
	t.Parallel()

	base := failingAuditRegistry{}
	require.ErrorIs(t, (auditCopier{Registry: base, out: io.Discard}).CopyTag(t.Context(), "src", "dst"), errAuditCopy)
	require.ErrorIs(t, (auditDeleter{CleanupRegistry: base, out: io.Discard}).DeleteTag(t.Context(), "ref"), errAuditDelete)
	rollback := auditPromotionRollbackRegistry{PromotionRollbackRegistry: base, out: io.Discard}
	require.ErrorIs(t, rollback.CopyTag(t.Context(), "src", "dst"), errAuditCopy)
	require.ErrorIs(t, rollback.DeleteTag(t.Context(), "ref"), errAuditDelete)
}

func TestRepositoryBindings_CheckLaterEntries(t *testing.T) {
	t.Parallel()

	const repo = "registry.example/app"

	good := imageledger.Entry{Ref: repo + "@sha256:" + strings.Repeat("a", 64), FinalTag: repo + ":v1.2.3"}
	bad := good
	bad.FinalTag = "registry.example/other:v1.2.3"

	require.NoError(t, validateEntryRepositories([]imageledger.Entry{good, good}, repo))
	err := validateEntryRepositories([]imageledger.Entry{good, bad}, repo)
	require.ErrorIs(t, err, errs.ErrValidation)
	require.Contains(t, err.Error(), "entry 1")

	first := imageledger.PromotionRecord{SourceRef: good.Ref, FinalTag: good.FinalTag}
	second := first
	second.FinalTag = bad.FinalTag

	require.NoError(t, validatePromotionRecordRepositories([]imageledger.PromotionRecord{first, first}, repo))
	err = validatePromotionRecordRepositories([]imageledger.PromotionRecord{first, second}, repo)
	require.ErrorIs(t, err, errs.ErrValidation)
	require.Contains(t, err.Error(), "entry 1")
}

func TestAttestationBinding_RecordsSourceDigestAcrossRunners(t *testing.T) {
	for _, runner := range []string{"github", "forgejo", "gitlab"} {
		t.Run(runner, func(t *testing.T) {
			env := testenv.New(t)
			env.Setenv("REPOSITORY", "untrusted/repository")
			env.Setenv("CI_REPO", "untrusted/repository")
			env.Setenv("CI_COMMIT", "untrusted-commit")
			env.Setenv("REF_NAME", "untrusted-ref")

			sha := strings.Repeat("a", 40)
			prefix := "GITHUB_"

			if runner == "gitlab" {
				env.Setenv("GITLAB_CI", "true")
				env.Setenv("CI_SERVER_URL", "https://forge.invalid")
				env.Setenv("CI_PROJECT_PATH", "owner/repo")
				env.Setenv("CI_COMMIT_REF_NAME", "v1.2.3")
				env.Setenv("CI_COMMIT_SHA", sha)
			} else {
				env.Setenv("GITHUB_ACTIONS", "true")

				if runner == "forgejo" {
					prefix = "FORGEJO_"

					env.Setenv("FORGEJO_ACTIONS", "true")
				}

				env.Setenv(prefix+"SERVER_URL", "https://forge.invalid")
				env.Setenv(prefix+"REPOSITORY", "owner/repo")
				env.Setenv(prefix+"REF_NAME", "v1.2.3")
				env.Setenv(prefix+"SHA", sha)
			}

			got := provenanceFromEnv("registry.example/app@sha256:" + strings.Repeat("b", 64))
			require.Equal(t, []provenance.Dependency{{URI: "git+https://forge.invalid/owner/repo@v1.2.3", DigestType: "gitCommit", Digest: sha}}, got.ResolvedDeps)
		})
	}
}
