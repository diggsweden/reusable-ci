// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/toolrecorder"
	"github.com/stretchr/testify/require"
)

// The cross-ecosystem matrix fails every external TOOL call in turn. The two
// positions it never fails are the ones the app itself owns: publishing the
// summary, and writing an annotation.
//
// Both can fail in production. A summary sink appends to the runner's step
// summary file, which can be full or read-only; an annotator writes to a stream
// that can be closed. Neither is exotic, and each raises a question the tool
// matrix cannot answer: when reporting fails, does the build report success?
// And when the tool has ALSO failed, which cause reaches the caller — the
// failure that happened, or the failure to describe it?
//
// The answer these pin is the one that keeps a release honest: a build that
// could not publish its summary has not succeeded, and when both fail the
// tool's cause wins, because that is the one an operator has to act on.

var (
	errSinkFull  = errors.New("step summary is full")   //nolint:err113 // injected identity is the contract.
	errToolBroke = errors.New("the tool itself failed") //nolint:err113 // injected identity is the contract.
)

type failingSummarySink struct {
	appends int
	err     error
}

func (s *failingSummarySink) Append(_ context.Context, _ string) error {
	s.appends++

	return s.err
}

// failingWriter fails every write, standing in for an annotation stream that
// has been closed under the run.
type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

// An npm build publishes two summary blocks: the SBOM status and the build
// block. Both are always written — the SBOM status is emitted with outcome
// "skipped" when SBOM generation is off — so either one failing must stop the
// build, and a fault at only one of them is masked by the other. That is worth
// recording because it is the opposite of what it looks like: disabling the
// SBOM does not remove a summary write, it changes what that write says.
func TestBuild_ASummarySinkFailureIsNotASuccessfulBuild(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		sbom bool
		why  string
	}{
		{name: "with SBOM generation on", sbom: true, why: "the SBOM status block reports success and is written first"},
		{name: "with SBOM generation off", sbom: false, why: "the SBOM status block still writes, reporting skipped"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := toolrecorder.New(t)
			rec.Respond(npmToolResponses(rec))

			npm := toolrecorder.NPMView{Recorder: rec}
			sink := &failingSummarySink{err: errSinkFull}

			var stdout bytes.Buffer

			err := appbuild.NPMReleaseBuild(context.Background(), sink, npm, npm, output.Annotator{}, &stdout, &stdout,
				appbuild.NPMReleaseBuildInput{
					ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: newNPMDir(t), EnableBuildSBOM: tc.sbom},
					PackageScope:        "@org",
					SBOMToolVersion:     "4.2.1",
				})

			require.Positivef(t, sink.appends, "the build never tried to publish a summary: %s", tc.why)
			require.Errorf(t, err, "the build reported success after failing to publish its summary: %s", tc.why)
			require.ErrorIsf(t, err, errSinkFull, "the sink's cause was replaced by something else: %s", tc.why)
		})
	}
}

// When the tool fails too, the tool's cause is the one that reaches the caller:
// "npm ci failed" is actionable, "could not write the summary" is a symptom of
// a run that was already over.
func TestBuild_AFailingToolOutranksAFailingSink(t *testing.T) {
	t.Parallel()

	rec := toolrecorder.New(t)
	base := npmToolResponses(rec)

	rec.Respond(func(index int, call toolrecorder.Call) toolrecorder.Response {
		response := base(index, call)
		if len(call.Args) > 0 && call.Args[0] == "ci" {
			response.Err = errToolBroke
		}

		return response
	})

	npm := toolrecorder.NPMView{Recorder: rec}
	sink := &failingSummarySink{err: errSinkFull}

	var stdout bytes.Buffer

	err := appbuild.NPMReleaseBuild(context.Background(), sink, npm, npm, output.Annotator{}, &stdout, &stdout,
		appbuild.NPMReleaseBuildInput{
			ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: newNPMDir(t), EnableBuildSBOM: true},
			PackageScope:        "@org",
			SBOMToolVersion:     "4.2.1",
		})

	require.Error(t, err)
	require.ErrorIsf(t, err, errToolBroke,
		"the caller was told about the reporting failure instead of the build failure: %v", err)
	require.Zero(t, sink.appends,
		"a failed build published a summary; the summary describes a build that did not happen")
}

// An annotation stream that cannot be written must not take the run down with
// it. Annotations are advisory — they decorate a run's UI — so losing one is
// not a reason to fail a build that otherwise succeeded, and it is certainly
// not a reason to mask a real failure.
func TestBuild_AFailingAnnotationStreamDoesNotChangeTheOutcome(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name     string
		toolFail bool
		wantErr  error
	}{
		{name: "an otherwise successful build", wantErr: nil},
		{name: "a build whose tool failed", toolFail: true, wantErr: errToolBroke},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			rec := toolrecorder.New(t)
			base := npmToolResponses(rec)

			rec.Respond(func(index int, call toolrecorder.Call) toolrecorder.Response {
				response := base(index, call)
				if tc.toolFail && len(call.Args) > 0 && call.Args[0] == "ci" {
					response.Err = errToolBroke
				}

				return response
			})

			npm := toolrecorder.NPMView{Recorder: rec}

			var stdout bytes.Buffer

			err := appbuild.NPMReleaseBuild(context.Background(), &recordingSummarySink{}, npm, npm,
				output.NewAnnotator(failingWriter{err: errors.New("stream closed")}, output.FormatGitHub), //nolint:err113 // injected.
				&stdout, &stdout, appbuild.NPMReleaseBuildInput{
					ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: newNPMDir(t), EnableBuildSBOM: true},
					PackageScope:        "@org",
					SBOMToolVersion:     "4.2.1",
				})

			if tc.wantErr == nil {
				require.NoError(t, err, "a build failed because it could not decorate the run's UI")

				return
			}

			require.ErrorIs(t, err, tc.wantErr, "the annotation failure displaced the real cause")
		})
	}
}
