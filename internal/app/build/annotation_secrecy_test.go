// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/toolrecorder"
	"github.com/stretchr/testify/require"
)

// The pass-through policy has three channels and only two were asserted.
//
// A tool's stdout and stderr are streamed to the caller on purpose — that is
// the build log. The published summary is app-owned and is already checked. The
// annotation channel is app-owned too, and it is the one an operator cannot
// avoid seeing: a `::error::` line renders into the run's UI, into the PR
// checks view, and into the notification email. If a tool's output or an
// operator's path reached it, the leak would be more visible than either of the
// other two, and nothing was checking.
//
// The claim is about a tool's STDOUT and STDERR, and the boundary is structural
// rather than a rule anyone has to remember. RunInherit streams a tool's output
// to the caller's writers; it does not capture it. So the error a failing step
// returns is an exit status wrapped by safeexec, and the one annotation that
// formats an error — the soft `npm test` warning, which continues and therefore
// has to report — can only render that status. A test asserting no error text
// ever reaches an annotation would be asserting something stronger than the
// design needs and would cost the diagnostic its exit code.
//
// The returned error stays outside the claim for the same reason it does
// everywhere else here: the original cause remains reachable through errors.Is
// and errors.As, which is what lets callers classify failures. "Nothing
// sensitive is reachable by traversing the error chain" and "the cause stays
// reachable" cannot both hold, and the cause is worth more.

// annotationCanary is shaped like a credential so a leak is unmistakable, and
// is not a real one.
const annotationCanary = "AKIAOWNEDSYNTHETICCANARY"

func TestAnnotations_CarryNoToolOutput(t *testing.T) {
	t.Parallel()

	for _, format := range []output.Format{output.FormatGitHub, output.FormatGitLab, output.FormatForgejo, output.FormatText} {
		t.Run(string(format), func(t *testing.T) {
			t.Parallel()

			rec := toolrecorder.New(t)

			// Captured before Respond: npmToolResponses wraps the recorder's
			// CURRENT responder, so building it inside the closure would wrap
			// this one and recurse.
			base := npmToolResponses(rec)

			rec.Respond(func(index int, call toolrecorder.Call) toolrecorder.Response {
				response := base(index, call)
				response.Stdout += "tool wrote " + annotationCanary + "\n"
				response.Stderr += "tool warned about " + annotationCanary + "\n"

				// `npm test` is the one failure this build continues past, so
				// it is the step that reaches the annotation channel at all.
				// The error is an exit status, not the tool's text — see the
				// note below on why that distinction is the whole claim.
				if len(call.Args) > 0 && call.Args[0] == "test" {
					response.Err = errExitStatus
				}

				return response
			})

			npm := toolrecorder.NPMView{Recorder: rec}

			var annotations, stdout bytes.Buffer

			_ = appbuild.NPMReleaseBuild(context.Background(), &recordingSummarySink{}, npm, npm,
				output.NewAnnotator(&annotations, format), &stdout, &stdout,
				appbuild.NPMReleaseBuildInput{
					ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: newNPMDir(t), EnableBuildSBOM: true},
					PackageScope:        "@org",
					SBOMToolVersion:     "4.2.1",
				})

			require.NotContainsf(t, annotations.String(), annotationCanary,
				"a %v annotation carried what the tool printed. Annotations render into the run UI, the PR "+
					"checks view and the notification email, so a credential a tool logged by accident would be "+
					"more visible there than anywhere else.\n%s", format, annotations.String())

			// The stream is where tool output belongs, and it must still be
			// there — otherwise this passes on a build that swallowed the log.
			require.Containsf(t, stdout.String(), annotationCanary,
				"the tool's output did not reach the caller's writer either; that is the build log, "+
					"and truncating it hides the failure a developer needs")
		})
	}
}

// The control. Without it every assertion above is satisfied by an annotator
// that was never written to, which is indistinguishable from one that redacts
// perfectly and a completely different state of the world.
func TestAnnotations_AreActuallyEmitted(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "package.json"), []byte(`{"name":"@other/pkg","version":"1.0.0"}`), 0o600); err != nil {
		t.Fatal(err)
	}

	var annotations bytes.Buffer

	err := appbuild.NPMMetadata(context.Background(), fakeoutputsink.New(t), &bytes.Buffer{},
		output.NewAnnotator(&annotations, output.FormatGitHub),
		appbuild.NPMMetadataInput{Dir: dir, PackageScope: "@org"})

	require.Error(t, err, "a scope mismatch must fail")
	require.NotEmpty(t, strings.TrimSpace(annotations.String()),
		"nothing was annotated, so the assertions above compare against an empty buffer")

	// And what it says is the operator's own configuration, repeated back. That
	// is the point of a diagnostic and is deliberately NOT what the secrecy
	// claim covers: the claim is about a tool's output, not about the values an
	// operator typed into artifacts.yml.
	require.Contains(t, annotations.String(), "@org")
}

// errExitStatus stands in for what a real failing tool returns through
// RunInherit: a status, not its output.
var errExitStatus = errors.New("exit status 1")

// TestAnnotations_FormatAnErrorsStatusNotItsOutput pins the one annotation that
// renders an error, so the bound above stays true by measurement rather than by
// the adapter happening not to capture output today.
func TestAnnotations_FormatAnErrorsStatusNotItsOutput(t *testing.T) {
	t.Parallel()

	rec := toolrecorder.New(t)
	base := npmToolResponses(rec)

	rec.Respond(func(index int, call toolrecorder.Call) toolrecorder.Response {
		response := base(index, call)
		response.Stdout += "tool wrote " + annotationCanary + "\n"

		if len(call.Args) > 0 && call.Args[0] == "test" {
			response.Err = errExitStatus
		}

		return response
	})

	npm := toolrecorder.NPMView{Recorder: rec}

	var annotations, stdout bytes.Buffer

	_ = appbuild.NPMReleaseBuild(context.Background(), &recordingSummarySink{}, npm, npm,
		output.NewAnnotator(&annotations, output.FormatGitHub), &stdout, &stdout,
		appbuild.NPMReleaseBuildInput{
			ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: newNPMDir(t), EnableBuildSBOM: true},
			PackageScope:        "@org",
			SBOMToolVersion:     "4.2.1",
		})

	require.Contains(t, annotations.String(), "exit status 1",
		"the soft test-failure warning must keep the status; without it an operator cannot tell what happened")
	require.NotContains(t, annotations.String(), annotationCanary,
		"the warning rendered the tool's output, not just its status")
}
