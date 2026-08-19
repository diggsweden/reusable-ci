// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"bytes"
	"context"
	"reflect"
	"slices"
	"testing"

	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
)

// cargoCallArgs returns each recorded cargo invocation's first arg.
func cargoSubcommands(calls []appbuild.CargoRunInput) []string {
	out := make([]string, 0, len(calls))
	for _, c := range calls {
		if len(c.Args) > 0 {
			out = append(out, c.Args[0])
		}
	}

	return out
}

func TestCargoReleaseBuild_RunsStepsInOrder(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		skipTests bool
		sbom      bool
		want      []string
	}{
		{
			name: "full sequence",
			sbom: true,
			want: []string{"metadata", "fetch", "test", "cyclonedx", "build"},
		},
		{
			name:      "skipping tests removes only the test step",
			skipTests: true,
			sbom:      true,
			want:      []string{"metadata", "fetch", "cyclonedx", "build"},
		},
		{
			name: "disabling the SBOM removes only the SBOM step",
			want: []string{"metadata", "fetch", "test", "build"},
		},
		{
			name:      "both skipped",
			skipTests: true,
			want:      []string{"metadata", "fetch", "build"},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			tool := &fakeCargoTool{metadata: sampleCargoMetadata, binaryNames: []string{"hello"}}

			var out bytes.Buffer

			err := appbuild.CargoReleaseBuild(context.Background(), &recordingSummarySink{}, tool, &out, &out, appbuild.CargoReleaseBuildInput{
				ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: t.TempDir(), SkipTests: tc.skipTests, EnableBuildSBOM: tc.sbom},
				Version:             "1.2.3",
				Platforms:           "linux/amd64",
			})
			if err != nil {
				t.Fatal(err)
			}

			// The exact sequence. The previous assertion searched a joined
			// string for each subcommand, so it saw neither order nor
			// repetition -- and its skip cases were satisfied by a run in
			// which nothing happened at all.
			if got := cargoSubcommands(tool.calls); !reflect.DeepEqual(got, tc.want) {
				t.Errorf("cargo subcommands = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestCargoReleaseBuild_UsesTheLockfileAsWritten asserts --locked on every
// step that resolves dependencies. Without it cargo may update Cargo.lock
// mid-release, so the artifact would not correspond to the committed
// lockfile -- and the build would still succeed, which is why this needs a
// test rather than a reviewer.
func TestCargoReleaseBuild_UsesTheLockfileAsWritten(t *testing.T) {
	t.Parallel()

	tool := &fakeCargoTool{metadata: sampleCargoMetadata, binaryNames: []string{"hello"}}

	var out bytes.Buffer

	err := appbuild.CargoReleaseBuild(context.Background(), &recordingSummarySink{}, tool, &out, &out, appbuild.CargoReleaseBuildInput{
		ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: t.TempDir()},
		Version:             "1.2.3",
		Platforms:           "linux/amd64",
	})
	if err != nil {
		t.Fatal(err)
	}

	for _, c := range tool.calls {
		if len(c.Args) == 0 || c.Args[0] == "metadata" {
			continue // metadata resolves nothing
		}

		if !slices.Contains(c.Args, "--locked") {
			t.Errorf("cargo %v ran without --locked", c.Args)
		}
	}
}
