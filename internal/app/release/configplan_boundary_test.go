// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"bytes"
	"io/fs"
	"os"
	"testing"

	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/config"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/pipeline"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/projecttype"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
	"github.com/stretchr/testify/require"
)

type assemblyPlanWriter struct {
	buf      bytes.Buffer
	attempts int
}

func (w *assemblyPlanWriter) Write(body []byte) (int, error) {
	w.attempts++

	return w.buf.Write(body)
}

func (w *assemblyPlanWriter) String() string { return w.buf.String() }

func assemblyConfigPlan(t *testing.T, artifacts ...config.Artifact) pipeline.ConfigPlan {
	t.Helper()

	cfg := config.Config{Artifacts: artifacts}
	require.NoError(t, config.Derive(&cfg))
	plan := pipeline.NewConfigPlan(&cfg)
	require.NoError(t, pipeline.ValidateConfigPlan(plan))

	return plan
}

func assemblyPlanTree(t *testing.T, root string) map[string]string {
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

func TestAssemble_ConfigPlanValidationPrecedesStaging(t *testing.T) {
	for _, seed := range []string{"", "existing output\n"} {
		for _, tc := range []struct {
			name, reason string
			change       func(*pipeline.ConfigPlan)
		}{
			{name: "producer"},
			{name: "later artifact", reason: "config-plan artifacts.all[1] has an unsupported project_type", change: func(p *pipeline.ConfigPlan) { p.Artifacts.All[1].ProjectType = "unsupported" }},
			{name: "missing native projection", reason: "config-plan artifacts.go_artifact_first disagrees with artifacts.all", change: func(p *pipeline.ConfigPlan) { p.Artifacts.GoArtifactFirst = nil }},
			{name: "sign gate", reason: "config-plan sign has inconsistent resolved fields", change: func(p *pipeline.ConfigPlan) { p.Sign.ImportsGPGKey = false }},
		} {
			t.Run(tc.name+"/"+seed, func(t *testing.T) {
				fsys := testfs.NewReal(t)
				fsys.Chdir()
				fsys.WriteFile("release-artifacts/native", []byte("native\x00literal\n"))
				fsys.WriteFile("first-sbom.spdx.json", []byte("{\"literal\": 42}\n"))

				if seed != "" {
					for _, path := range []string{"release-files/assets/old", "release-files/sboms/old", ".reusable-ci/release-assembly.json"} {
						fsys.WriteFile(path, []byte(seed))
					}
				}

				plan := assemblyConfigPlan(t,
					config.Artifact{Name: "first", ProjectType: projecttype.Go},
					config.Artifact{Name: "later", ProjectType: projecttype.Go, WorkingDirectory: "later"})
				// Preserve the assembly consumer's empty-directory default too.
				plan.Artifacts.All[0].WorkingDirectory = ""
				plan.Artifacts.Go[0].WorkingDirectory = ""
				plan.Artifacts.GoArtifactFirst[0].WorkingDirectory = ""
				require.NoError(t, pipeline.ValidateConfigPlan(plan))

				if tc.change != nil {
					tc.change(&plan)
				}

				before := assemblyPlanTree(t, fsys.Root)

				var out assemblyPlanWriter

				_, _ = out.buf.WriteString(seed)

				asm, err := apprelease.Assemble(&out, apprelease.AssembleInput{
					ConfigPlanJSON: mustReleaseJSON(t, plan), ArtifactTransferPlanJSON: releaseAssemblyTransferJSON(t),
					ProjectName: "app", Version: "v1.2.3",
				})
				if tc.change != nil {
					require.ErrorIs(t, err, errs.ErrInvalidConfig)
					require.Contains(t, err.Error(), tc.reason)
					require.Nil(t, asm)
					require.Zero(t, out.attempts)
					require.Equal(t, seed, out.String())
					require.Equal(t, before, assemblyPlanTree(t, fsys.Root))

					return
				}

				require.NoError(t, err)
				require.NotNil(t, asm)
				require.Len(t, asm.Assets, 1)
				require.Len(t, asm.SBOMs, 1)
				require.Equal(t, "release-files/assets/native", asm.Assets[0].Path)
				require.Equal(t, "native\x00literal\n", string(fsys.ReadFile("release-files/assets/native")))
				require.Equal(t, "{\"literal\": 42}\n", string(fsys.ReadFile("release-files/sboms/first-sbom.spdx.json")))
				require.Contains(t, string(fsys.ReadFile(".reusable-ci/release-assembly.json")), `"source_path": "release-artifacts/native"`)
				require.Equal(t, 1, out.attempts)
				require.Equal(t, seed+"Assembled 1 release asset(s) and 1 SBOM input(s) in .reusable-ci/release-assembly.json\n", out.String())
			})
		}
	}
}
