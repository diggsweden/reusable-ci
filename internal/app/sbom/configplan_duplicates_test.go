// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package sbom_test

import (
	"strings"
	"testing"

	appsbom "github.com/diggsweden/reusable-ci/v3/internal/app/sbom"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
	"github.com/stretchr/testify/require"
)

func TestGenerateArtifacts_ConfigPlanDuplicateMembers(t *testing.T) {
	const (
		artifact = `{"name":"lib","project_type":"maven","working_directory":".","sboms":"build,analyzed-artifact","effective_sboms":["build","analyzed-artifact"],"build_artifact_name":"lib-build-artifacts","build_sbom_artifact_name":"lib-build-sbom","maven":{"settings_path":"settings.xml"}}`
		raw      = `{"version":1,"artifacts":{"all":[` + artifact + `],"maven":[` + artifact + `],"cargo":[]},"pipeline_sboms":"build,analyzed-artifact","fallback_project_type":"maven","any_require_authorization":false,"sign":{"method":"gpg","imports_gpg_key":true},"git_signing":{"method":"gpg","imports_gpg_key":true},"future":{"version":1,"version":2}}`
	)

	cases := []struct{ name, raw, path string }{{"control", raw, ""}}
	for _, tc := range []struct{ first, second, path string }{
		{`"version":1`, `"VERSION":2`, "version"},
		{`"any_require_authorization":false`, `"any_require_authorization":true`, "any_require_authorization"},
		{`"cargo":[]`, `"cargo":null`, "artifacts.cargo"},
		{`"settings_path":"settings.xml"`, `"\u0073ETTINGS_PATH":"other.xml"`, "artifacts.all[0].maven.settings_path"},
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
				fsys.WriteFile("target/bom.json", []byte("{\"bomFormat\":\"CycloneDX\",\"literal\":\"bom\"}\n"))
				fsys.WriteFile("target/lib.jar", []byte("literal jar\n"))

				if seed != "" {
					fsys.WriteFile("lib-1.2.3-abc1234-build-sbom.cyclonedx.json", []byte(seed))
				}

				before := sbomPlanTree(t, fsys.Root)

				var out, stderr sbomPlanWriter

				_, _ = out.buf.WriteString(seed)
				_, _ = stderr.buf.WriteString(seed)
				syft, mvn, git := &fakeSyft{}, &sbomPlanMaven{}, &sbomPlanGit{}

				err := appsbom.GenerateArtifacts(t.Context(), syft, mvn, git, &out, &stderr, appsbom.GenerateArtifactsInput{ConfigPlanJSON: tc.raw, SBOMs: "build,analyzed-artifact"})
				if tc.path == "" {
					require.NoError(t, err)
					require.NotEmpty(t, git.calls)
					require.NotEmpty(t, mvn.calls)
					require.NotEmpty(t, syft.calls)
					require.Positive(t, out.attempts)
					require.Equal(t, "{\"bomFormat\":\"CycloneDX\",\"literal\":\"bom\"}\n", string(fsys.ReadFile("lib-1.2.3-abc1234-build-sbom.cyclonedx.json"))) //nolint:testifylint // byte identity is the claim: the harvested document is copied, never re-encoded.

					return
				}

				require.Empty(t, git.calls)
				require.Empty(t, mvn.calls)
				require.Empty(t, syft.calls)
				require.Zero(t, out.attempts)
				require.Zero(t, stderr.attempts)
				require.Equal(t, seed, out.String())
				require.Equal(t, seed, stderr.String())
				require.Equal(t, before, sbomPlanTree(t, fsys.Root))
				require.ErrorIs(t, err, errs.ErrInvalidConfig)
				require.ErrorContains(t, err, "config-plan."+tc.path+" has duplicate consumed member")
			})
		}
	}
}

func TestGenerateArtifacts_InactiveDuplicatePlan(t *testing.T) {
	t.Parallel()

	for _, layers := range []string{"none", "analyzed-container"} {
		var out, stderr sbomPlanWriter

		syft, mvn, git := &fakeSyft{}, &sbomPlanMaven{}, &sbomPlanGit{}
		err := appsbom.GenerateArtifacts(t.Context(), syft, mvn, git, &out, &stderr, appsbom.GenerateArtifactsInput{ConfigPlanJSON: `{"version":1,"version":999}`, SBOMs: layers})
		require.NoError(t, err)
		require.Empty(t, git.calls)
		require.Empty(t, mvn.calls)
		require.Empty(t, syft.calls)
		require.Zero(t, stderr.attempts)
		require.Equal(t, 1, out.attempts)
	}
}
