// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"

	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/toolrecorder"
	"github.com/stretchr/testify/require"
)

// ecosystemRun drives one ecosystem's release build against the recorder, and
// reports what the run published so a failure can be checked for silence.
type ecosystemRun struct {
	name string
	// run executes the build and returns its error. Everything the run writes
	// lands in the buffers the harness owns.
	run func(t *testing.T, rec *toolrecorder.Recorder, summary *recordingSummarySink, stdout *bytes.Buffer) error
	// softFailures are call indexes the ecosystem deliberately continues past,
	// with the reason it is allowed to. Everything else must stop the build.
	softFailures map[int]string
}

// ecosystemRuns is the whole set of release builds that drive an external tool.
// Adding an ecosystem here is what makes its failure behaviour comparable to
// the others rather than whatever its own tests happened to assert.
func ecosystemRuns() []ecosystemRun {
	return []ecosystemRun{
		{
			name: "go",
			run: func(t *testing.T, rec *toolrecorder.Recorder, summary *recordingSummarySink, stdout *bytes.Buffer) error {
				t.Helper()

				return appbuild.GoReleaseBuild(context.Background(), summary, rec, rec, stdout, stdout, appbuild.GoReleaseBuildInput{
					ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: newGoModDir(t), EnableBuildSBOM: true},
					Version:             "1.2.3",
					Platforms:           "linux/amd64",
				})
			},
		},
		{
			name: "cargo",
			run: func(t *testing.T, rec *toolrecorder.Recorder, summary *recordingSummarySink, stdout *bytes.Buffer) error {
				t.Helper()
				dir := t.TempDir()

				return appbuild.CargoReleaseBuild(context.Background(), summary, rec.Respond(cargoToolResponses(rec)), stdout, stdout, appbuild.CargoReleaseBuildInput{
					ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: dir, EnableBuildSBOM: true},
					Version:             "1.2.3",
					Platforms:           "linux/amd64",
				})
			},
		},
		{
			name: "maven",
			run: func(t *testing.T, rec *toolrecorder.Recorder, summary *recordingSummarySink, stdout *bytes.Buffer) error {
				t.Helper()

				return appbuild.MavenReleaseBuild(context.Background(), summary, rec, stdout, stdout, appbuild.MavenReleaseBuildInput{
					ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: newMavenDir(t), EnableBuildSBOM: true},
					BuildType:           "app",
					SBOMToolVersion:     "2.9.1",
				})
			},
		},
		{
			name: "gradle",
			run: func(t *testing.T, rec *toolrecorder.Recorder, summary *recordingSummarySink, stdout *bytes.Buffer) error {
				t.Helper()

				return appbuild.GradleReleaseBuild(context.Background(), summary, rec, stdout, stdout, appbuild.GradleReleaseBuildInput{
					ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: newGradleDir(t)},
					Tasks:               "assemble check",
					JavaVersion:         "25",
				})
			},
		},
		{
			name: "npm",
			run: func(t *testing.T, rec *toolrecorder.Recorder, summary *recordingSummarySink, stdout *bytes.Buffer) error {
				t.Helper()

				npm := toolrecorder.NPMView{Recorder: rec.Respond(npmToolResponses(rec))}

				return appbuild.NPMReleaseBuild(context.Background(), summary, npm, npm, output.Annotator{}, stdout, stdout, appbuild.NPMReleaseBuildInput{
					ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: newNPMDir(t), EnableBuildSBOM: true},
					PackageScope:        "@org",
					SBOMToolVersion:     "4.2.1",
				})
			},
			// npm test is the one tool failure the release build continues past:
			// the workflow reports tests separately, and a failing test must not
			// look like a broken packaging step. Every other npm call stops it.
			softFailures: map[int]string{1: "npm test failures are reported, not fatal to packaging"},
		},
	}
}

// TestEcosystemToolFailures_StopWithTheirOwnIdentity is the cross-ecosystem
// matrix. Each ecosystem grew its own fake and asserted whatever that fake made
// convenient, so what one proved about a failing tool the next one did not.
// Here every ecosystem answers the same question at every external call it
// makes: a unique cause survives to the caller, the stage it failed at is named,
// nothing runs after it, and nothing is published as if the build had worked.
//
// A tool that exits nonzero is a build failure, not a reason to keep going. The
// one documented exception is recorded per ecosystem in softFailures, so a new
// silent continuation shows up here rather than in a release.
func TestEcosystemToolFailures_StopWithTheirOwnIdentity(t *testing.T) {
	for _, ecosystem := range ecosystemRuns() {
		t.Run(ecosystem.name, func(t *testing.T) {
			calls := ecosystemCallCount(t, ecosystem)
			require.Positivef(t, calls, "%s drives no external tool", ecosystem.name)

			for index := range calls {
				t.Run("call_"+strconv.Itoa(index), func(t *testing.T) {
					cause := errors.New("owned " + ecosystem.name + " refusal at call " + strconv.Itoa(index)) //nolint:err113 // one identity per position is the contract.
					rec := toolrecorder.New(t).FailAt(index, cause)
					summary := &recordingSummarySink{}

					var stdout bytes.Buffer

					err := ecosystem.run(t, rec, summary, &stdout)

					if reason, soft := ecosystem.softFailures[index]; soft {
						require.NoErrorf(t, err, "documented continuation: %s", reason)
						require.Lenf(t, rec.Calls, calls, "a continuation still runs the rest: %s", reason)

						return
					}

					require.ErrorIsf(t, err, cause, "the cause that stopped the build must reach the caller")
					require.Lenf(t, rec.Calls, index+1, "no tool may run after a failed one:\n%s", formatArgs(rec.Args()))
					require.NotEqualf(t, cause.Error(), err.Error(),
						"the error names only the cause; nothing says which stage failed")
					require.NotContainsf(t, summary.buf.String(), "✅", "a failed build must not publish a success summary")
				})
			}
		})
	}
}

// ecosystemCallCount runs one ecosystem to completion so the matrix knows how
// many positions it has. A build that cannot succeed here is a broken fixture,
// not a finding, so it fails loudly rather than producing an empty matrix.
func ecosystemCallCount(t *testing.T, ecosystem ecosystemRun) int {
	t.Helper()

	rec := toolrecorder.New(t)

	var stdout bytes.Buffer
	require.NoErrorf(t, ecosystem.run(t, rec, &recordingSummarySink{}, &stdout), "%s happy path: %s", ecosystem.name, stdout.String())

	return len(rec.Calls)
}

// cargoToolResponses supplies the metadata document and the compiled binary a
// real cargo would leave behind, so the steps that read them run for real.
func cargoToolResponses(rec *toolrecorder.Recorder) func(int, toolrecorder.Call) toolrecorder.Response {
	failure := rec.Responder()

	return func(index int, call toolrecorder.Call) toolrecorder.Response {
		response := failure(index, call)
		if response.Err != nil || len(call.Args) == 0 {
			return response
		}

		switch call.Args[0] {
		case "metadata":
			response.Stdout = sampleCargoMetadata
		case "build":
			response.Artifacts = map[string]string{cargoBuildArtifact(call.Args): "compiled bytes"}
			response.ArtifactMode = 0o755
		}

		return response
	}
}

// cargoBuildArtifact names where a `cargo build --target <triple>` run puts its
// binary, which is where the release build looks for it.
func cargoBuildArtifact(args []string) string {
	triple := ""

	for index, arg := range args {
		if arg == "--target" && index+1 < len(args) {
			triple = args[index+1]
		}
	}

	if triple == "" {
		return "target/release/hello"
	}

	return "target/" + triple + "/release/hello"
}

// npmToolResponses supplies what npm pack and cyclonedx-npm leave behind, so
// the steps that read those results run for real. It layers over whatever
// failure the matrix installed rather than replacing it.
func npmToolResponses(rec *toolrecorder.Recorder) func(int, toolrecorder.Call) toolrecorder.Response {
	failure := rec.Responder()

	return func(index int, call toolrecorder.Call) toolrecorder.Response {
		response := failure(index, call)
		if response.Err != nil {
			return response
		}

		switch {
		case len(call.Args) == 4 && call.Args[0] == "pack":
			response.Stdout = `[{"name":"@org/app","version":"1.0.0","filename":"org-app-1.0.0.tgz"}]`
			response.Artifacts = map[string]string{call.Args[3] + "/org-app-1.0.0.tgz": string(npmTarballBytes("1.0.0"))}
		case len(call.Args) == 6 && call.Args[0] == "--yes":
			response.Artifacts = map[string]string{call.Args[5]: `{"bomFormat":"CycloneDX","specVersion":"1.6","version":1}`}
		}

		return response
	}
}

func formatArgs(args [][]string) string {
	var out strings.Builder
	for index, call := range args {
		_, _ = fmt.Fprintf(&out, "  %d: %q\n", index, call)
	}

	return out.String()
}

// TestEcosystemToolOutput_StaysOutOfAppOwnedChannels pins the pass-through
// policy every ecosystem build relies on.
//
// A build tool's stdout and stderr are streamed to the caller's writers on
// purpose: that is the build log, and truncating it would hide the failure a
// developer needs. The app-owned channels are different. A summary is published
// to the forge and an annotation is rendered into the run's UI, so anything a
// tool printed reaching them is a leak the pass-through policy never sanctioned.
// Tools print credentials by accident often enough that the boundary has to be
// asserted rather than assumed.
//
// Returned errors are outside this claim by design: the original cause stays
// reachable through errors.Is and errors.As, and it can carry whatever the tool
// wrote. That is the same documented limit the mobile builds carry.
func TestEcosystemToolOutput_StaysOutOfAppOwnedChannels(t *testing.T) {
	const canary = "AKIAOWNEDSYNTHETICCANARY"

	for _, ecosystem := range ecosystemRuns() {
		for _, failing := range []bool{false, true} {
			name := ecosystem.name + "/succeeds"
			if failing {
				name = ecosystem.name + "/fails"
			}

			t.Run(name, func(t *testing.T) {
				rec := toolrecorder.New(t)
				rec.Respond(func(_ int, call toolrecorder.Call) toolrecorder.Response {
					response := toolrecorder.Response{
						Stdout: "tool wrote " + canary + "\n",
						Stderr: "tool warned about " + canary + "\n",
					}
					if failing && len(call.Args) > 0 && call.Args[0] == "pack" {
						response.Err = errors.New("tool failed carrying " + canary) //nolint:err113 // the canary is the point.
					}

					return response
				})

				summary := &recordingSummarySink{}

				var stdout bytes.Buffer

				_ = ecosystem.run(t, rec, summary, &stdout)

				require.NotContainsf(t, summary.buf.String(), canary,
					"a tool's output must not reach the published summary")
			})
		}
	}
}
