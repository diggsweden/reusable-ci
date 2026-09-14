// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package sbom_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io/fs"
	"os"
	"testing"

	appsbom "github.com/diggsweden/reusable-ci/v3/internal/app/sbom"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/pipeline"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
	"github.com/stretchr/testify/require"
)

type sbomPlanWriter struct {
	buf      bytes.Buffer
	attempts int
}

func (w *sbomPlanWriter) Write(body []byte) (int, error) {
	w.attempts++

	return w.buf.Write(body)
}

func (w *sbomPlanWriter) String() string { return w.buf.String() }

type sbomPlanGit struct{ calls [][]string }

func (g *sbomPlanGit) Run(_ context.Context, args ...string) (string, error) {
	g.calls = append(g.calls, args)

	return "abc1234", nil
}

type sbomPlanMaven struct{ calls []string }

func (m *sbomPlanMaven) EvalExpression(_ context.Context, expr string) (string, error) {
	m.calls = append(m.calls, expr)

	return "1.2.3", nil
}

func sbomConfigPlan(t *testing.T, artifacts ...config.Artifact) pipeline.ConfigPlan {
	t.Helper()

	cfg := config.Config{Artifacts: artifacts}
	require.NoError(t, config.Derive(&cfg))
	plan := pipeline.NewConfigPlan(&cfg)
	require.NoError(t, pipeline.ValidateConfigPlan(plan))

	return plan
}

func sbomPlanJSON(t *testing.T, plan pipeline.ConfigPlan) string {
	t.Helper()

	body, err := json.Marshal(plan)
	require.NoError(t, err)

	return string(body)
}

func sbomPlanTree(t *testing.T, root string) map[string]string {
	t.Helper()

	tree := map[string]string{}

	require.NoError(t, fs.WalkDir(os.DirFS(root), ".", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}

		if entry.IsDir() {
			tree[path+"/"] = "directory"

			return nil
		}

		body, err := fs.ReadFile(os.DirFS(root), path)
		tree[path] = string(body)

		return err
	}))

	return tree
}

func TestGenerateArtifacts_ConfigPlanValidationPrecedesEffects(t *testing.T) {
	for _, seed := range []string{"", "existing output\n"} {
		for _, tc := range []struct {
			name, reason string
			change       func(*pipeline.ConfigPlan)
		}{
			{name: "producer"},
			{name: "later artifact layers", reason: "config-plan artifacts.all[1].effective_sboms disagrees with sboms", change: func(p *pipeline.ConfigPlan) { p.Artifacts.All[1].EffectiveSBOMs = nil }},
			{name: "missing projection", reason: "config-plan artifacts.npm disagrees with artifacts.all", change: func(p *pipeline.ConfigPlan) { p.Artifacts.NPM = nil }},
			{name: "pipeline layers", reason: "config-plan pipeline_sboms disagrees with artifacts.all", change: func(p *pipeline.ConfigPlan) { p.PipelineSBOMs = "none" }},
		} {
			t.Run(tc.name+"/"+seed, func(t *testing.T) {
				fsys := testfs.NewReal(t)
				fsys.Chdir()
				fsys.WriteFile("first/bom.json", []byte("{\"bomFormat\":\"CycloneDX\",\"literal\":42}\n"))
				fsys.WriteFile("first/package.json", []byte(`{"name":"first","version":"1.2.3"}`))
				fsys.WriteFile("first/first.tgz", []byte("tar literal"))
				fsys.WriteFile("later/target/bom.json", []byte("{\"bomFormat\":\"CycloneDX\",\"literal\":99}\n"))

				if seed != "" {
					fsys.WriteFile("first/first-1.2.3-abc1234-build-sbom.cyclonedx.json", []byte(seed))
					fsys.WriteFile("later/later-1.2.3-abc1234-build-sbom.cyclonedx.json", []byte(seed))
				}

				plan := sbomConfigPlan(t,
					config.Artifact{Name: "first", ProjectType: projecttype.NPM, WorkingDirectory: "first", SBOMs: "build,analyzed-artifact"},
					config.Artifact{Name: "later", ProjectType: projecttype.Maven, WorkingDirectory: "later", SBOMs: "build"})
				plan.Artifacts.All[0].WorkingDirectory = ""
				plan.Artifacts.NPM[0].WorkingDirectory = ""
				require.NoError(t, pipeline.ValidateConfigPlan(plan))

				if tc.change != nil {
					tc.change(&plan)
				}

				before := sbomPlanTree(t, fsys.Root)

				var out, stderr sbomPlanWriter

				_, _ = out.buf.WriteString(seed)
				_, _ = stderr.buf.WriteString(seed)
				syft, mvn, git := &fakeSyft{}, &sbomPlanMaven{}, &sbomPlanGit{}

				err := appsbom.GenerateArtifacts(t.Context(), syft, mvn, git, &out, &stderr, appsbom.GenerateArtifactsInput{
					ConfigPlanJSON: sbomPlanJSON(t, plan), SBOMs: "build,analyzed-artifact", WorkingDir: "first",
				})
				if tc.change != nil {
					require.ErrorIs(t, err, errs.ErrInvalidConfig)
					require.Contains(t, err.Error(), tc.reason)
					require.Empty(t, syft.calls)
					require.Empty(t, mvn.calls)
					require.Empty(t, git.calls)
					require.Zero(t, out.attempts)
					require.Zero(t, stderr.attempts)
					require.Equal(t, seed, out.String())
					require.Equal(t, seed, stderr.String())
					require.Equal(t, before, sbomPlanTree(t, fsys.Root))

					return
				}

				require.NoError(t, err)
				require.Equal(t, [][]string{{"rev-parse", "--short", "HEAD"}, {"rev-parse", "--short", "HEAD"}}, git.calls)
				require.Equal(t, []string{"project.version"}, mvn.calls)
				require.Len(t, syft.calls, 1)
				require.Equal(t, fsys.Path("first/first.tgz"), syft.calls[0].target)
				require.Equal(t, "{\"bomFormat\":\"CycloneDX\",\"literal\":42}\n", string(fsys.ReadFile("first/first-1.2.3-abc1234-build-sbom.cyclonedx.json"))) //nolint:testifylint // byte identity is the claim: the harvested document is copied, never re-encoded.
				require.Equal(t, "{\"bomFormat\":\"CycloneDX\",\"literal\":99}\n", string(fsys.ReadFile("later/later-1.2.3-abc1234-build-sbom.cyclonedx.json"))) //nolint:testifylint // byte identity is the claim: the harvested document is copied, never re-encoded.
				require.Equal(t, `{"target":"`+fsys.Path("first/first.tgz")+`","format":"cyclonedx-json"}`, string(fsys.ReadFile("first/first-abc1234-analyzed-tararchive-sbom.cyclonedx.json")))
				require.Positive(t, out.attempts)
			})
		}
	}
}

func TestGenerateArtifacts_UnusedConfigPlanRemainsUnparsed(t *testing.T) {
	t.Parallel()

	for _, layers := range []string{"none", "analyzed-container"} {
		for _, raw := range []string{"", "not JSON", `{"version":999}`} {
			var out, stderr sbomPlanWriter

			syft, mvn, git := &fakeSyft{}, &sbomPlanMaven{}, &sbomPlanGit{}
			require.NoError(t, appsbom.GenerateArtifacts(t.Context(), syft, mvn, git, &out, &stderr, appsbom.GenerateArtifactsInput{ConfigPlanJSON: raw, SBOMs: layers}))
			require.Empty(t, syft.calls)
			require.Empty(t, mvn.calls)
			require.Empty(t, git.calls)
			require.Zero(t, stderr.attempts)
			require.Equal(t, 1, out.attempts)
			require.Equal(t, "No artifact-level SBOM layers requested; analyzed-container (if any) is already downloaded.\n", out.String())
		}
	}
}
