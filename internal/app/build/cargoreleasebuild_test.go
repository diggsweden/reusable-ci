// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"bytes"
	"context"
	"strings"
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

func TestCargoReleaseBuild_RunsFullSequence(t *testing.T) {
	t.Parallel()

	tool := &fakeCargoTool{metadata: sampleCargoMetadata, binaryNames: []string{"hello"}}

	var out bytes.Buffer

	err := appbuild.CargoReleaseBuild(context.Background(), &recordingSummarySink{}, tool, &out, &out, appbuild.CargoReleaseBuildInput{
		ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: t.TempDir(), EnableBuildSBOM: true},
		Version:             "1.2.3",
		Platforms:           "linux/amd64",
	})
	if err != nil {
		t.Fatal(err)
	}

	// cargo ran: metadata (resolve), fetch, test, cyclonedx (SBOM), build.
	got := strings.Join(cargoSubcommands(tool.calls), ",")
	for _, want := range []string{"metadata", "fetch", "test", "cyclonedx", "build"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in cargo calls: %s", want, got)
		}
	}
}

func TestCargoReleaseBuild_SkipTests(t *testing.T) {
	t.Parallel()

	tool := &fakeCargoTool{metadata: sampleCargoMetadata, binaryNames: []string{"hello"}}

	err := appbuild.CargoReleaseBuild(context.Background(), &recordingSummarySink{}, tool, &bytes.Buffer{}, &bytes.Buffer{}, appbuild.CargoReleaseBuildInput{
		ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: t.TempDir(), SkipTests: true, EnableBuildSBOM: false},
		Version:             "1.2.3",
		Platforms:           "linux/amd64",
	})
	if err != nil {
		t.Fatal(err)
	}

	got := cargoSubcommands(tool.calls)
	for _, a := range got {
		if a == "test" {
			t.Errorf("cargo test ran despite --skip-tests: %v", got)
		}

		if a == "cyclonedx" {
			t.Errorf("SBOM ran despite EnableBuildSBOM=false: %v", got)
		}
	}
}
