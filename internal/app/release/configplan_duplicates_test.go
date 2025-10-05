// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"strings"
	"testing"

	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
	"github.com/stretchr/testify/require"
)

func TestAssemble_ConfigPlanDuplicateMembers(t *testing.T) {
	const (
		artifact = `{"name":"lib","project_type":"maven","working_directory":".","sboms":"none","build_artifact_name":"lib-build-artifacts","build_sbom_artifact_name":"lib-build-sbom","maven":{"settings_path":"settings.xml"}}`
		raw      = `{"version":1,"artifacts":{"all":[` + artifact + `],"maven":[` + artifact + `],"cargo":[]},"pipeline_sboms":"none","fallback_project_type":"maven","any_require_authorization":false,"sign":{"method":"gpg","imports_gpg_key":true},"git_signing":{"method":"gpg","imports_gpg_key":true},"future":{"version":1,"version":2}}`
	)

	cases := []struct{ name, raw, path string }{{"control", raw, ""}}
	for _, tc := range []struct{ first, second, path string }{
		{`"version":1`, `"ver\u0073ion":2`, "version"},
		{`"any_require_authorization":false`, `"ANY_REQUIRE_AUTHORIZATION":true`, "any_require_authorization"},
		{`"cargo":[]`, `"CARGO":null`, "artifacts.cargo"},
		{`"settings_path":"settings.xml"`, `"settings_path":"other.xml"`, "artifacts.all[0].maven.settings_path"},
	} {
		for _, pair := range []struct{ name, members string }{
			{"equal", tc.first + "," + tc.first}, {"forward", tc.first + "," + tc.second}, {"reverse", tc.second + "," + tc.first},
		} {
			cases = append(cases, struct{ name, raw, path string }{tc.path + "/" + pair.name, strings.Replace(raw, tc.first, pair.members, 1), tc.path})
		}
	}

	for _, tc := range cases {
		for _, seed := range []string{"", "existing\n"} {
			t.Run(tc.name+"/"+seed, func(t *testing.T) {
				fsys := testfs.NewReal(t)
				fsys.Chdir()
				fsys.WriteFile("release-artifacts/lib.jar", []byte("literal asset\n"))
				fsys.WriteFile("lib-sbom.spdx.json", []byte("literal sbom\n"))

				if seed != "" {
					for _, path := range []string{"release-files/assets/old", "release-files/sboms/old", ".reusable-ci/release-assembly.json"} {
						fsys.WriteFile(path, []byte(seed))
					}
				}

				before := assemblyPlanTree(t, fsys.Root)

				var out assemblyPlanWriter

				_, _ = out.buf.WriteString(seed)

				asm, err := apprelease.Assemble(&out, apprelease.AssembleInput{
					ConfigPlanJSON: tc.raw, ArtifactTransferPlanJSON: releaseAssemblyTransferJSON(t), ProjectName: "app", Version: "v1.2.3",
				})
				if tc.path == "" {
					require.NoError(t, err)
					require.NotNil(t, asm)
					require.Positive(t, out.attempts)
					require.Equal(t, "literal asset\n", string(fsys.ReadFile("release-files/assets/lib.jar")))

					return
				}
				// Observe effects even if a faulty decoder accepted the plan.
				require.Zero(t, out.attempts)
				require.Equal(t, seed, out.String())
				require.Equal(t, before, assemblyPlanTree(t, fsys.Root))
				require.Nil(t, asm)
				require.ErrorIs(t, err, errs.ErrInvalidConfig)
				require.ErrorContains(t, err, "config-plan."+tc.path+" has duplicate consumed member")
			})
		}
	}
}
