// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

// Package toolrecorder is one recorder for every external build tool the
// ecosystem builds drive.
//
// Each ecosystem port has its own signature: Cargo and Go take a struct, Gradle
// and Maven take variadic args with writers, npm returns captured output, Xcode
// returns an exit code. Every one of them is the same thing underneath — a
// command, in a directory, with an environment — so each ecosystem's tests grew
// their own fake with its own shape, and what one ecosystem asserted about a
// failing tool the next one did not.
//
// This normalizes them into one ordered Call list so a test can assert the
// whole sequence by exact struct equality, fail any single call by index, and
// have the tool produce the artifacts a later step expects. The recorder never
// runs anything: it is the boundary where the product stops.
package toolrecorder

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	domainbuild "github.com/diggsweden/reusable-ci/v3/internal/domain/build"
)

// Call is one recorded invocation, normalized across every port shape. Dir is
// "" when the port has no directory of its own and the tool runs in the
// process working directory.
type Call struct {
	Dir  string
	Env  []string
	Args []string
}

// Response is what the recorder does for one call: the bytes it writes back,
// the artifacts it creates, the exit code it reports and the error it returns.
type Response struct {
	// Stdout and Stderr are written to whichever writers the port supplies,
	// or returned directly by ports that capture output.
	Stdout string
	Stderr string
	// Artifacts are files the tool would have produced, written relative to
	// Dir when the port has one and to the working directory otherwise.
	Artifacts map[string]string
	// ArtifactMode is the mode those files get. Zero means owner-only read and
	// write; a compiler's output has to be set executable explicitly, because a
	// step that looks for a runnable binary should not accept one that is not.
	ArtifactMode os.FileMode
	// ExitCode is reported by ports that return one; ignored by the rest.
	ExitCode int
	Err      error
}

// Recorder implements every ecosystem tool port. The zero value records calls
// and does nothing else.
type Recorder struct {
	t *testing.T

	// Calls is the ordered record. Assert against it by exact equality.
	Calls []Call

	// respond, when set, decides the response for one call by index. It is the
	// single seam for per-index failures and for tools that must leave an
	// artifact behind.
	respond func(index int, call Call) Response
}

// New returns a recorder that answers every call successfully.
func New(t *testing.T) *Recorder {
	t.Helper()

	return &Recorder{t: t}
}

// Respond installs the per-call responder and returns the recorder, so a test
// reads as one expression.
func (r *Recorder) Respond(respond func(index int, call Call) Response) *Recorder {
	r.respond = respond

	return r
}

// FailAt makes the call at index fail with err, after any earlier calls have
// been recorded and answered. Every other call succeeds.
func (r *Recorder) FailAt(index int, err error) *Recorder {
	return r.Respond(func(current int, _ Call) Response {
		if current == index {
			return Response{Err: err}
		}

		return Response{}
	})
}

// Responder returns the installed responder, or one that always succeeds, so a
// caller can layer tool behaviour over a failure the harness already installed.
func (r *Recorder) Responder() func(index int, call Call) Response {
	if r.respond == nil {
		return func(int, Call) Response { return Response{} }
	}

	return r.respond
}

// Args returns the argument vectors alone, for assertions that care about the
// commands rather than the directories or environments they ran in.
func (r *Recorder) Args() [][]string {
	args := make([][]string, 0, len(r.Calls))
	for _, call := range r.Calls {
		args = append(args, call.Args)
	}

	return args
}

// Run implements the Cargo and Go ports, whose input carries everything.
func (r *Recorder) Run(_ context.Context, in domainbuild.GoRunInput) error {
	r.t.Helper()

	return r.record(in.Dir, in.Env, in.Args, in.Stdout, in.Stderr).Err
}

// RunInherit implements the Gradle and Maven ports, which run in the process
// working directory and stream to the caller's writers.
func (r *Recorder) RunInherit(_ context.Context, stdout, stderr io.Writer, args ...string) error {
	r.t.Helper()

	return r.record("", nil, args, stdout, stderr).Err
}

// RunInDirInherit implements the directory-scoped Gradle port.
func (r *Recorder) RunInDirInherit(_ context.Context, dir string, stdout, stderr io.Writer, args ...string) error {
	r.t.Helper()

	return r.record(dir, nil, args, stdout, stderr).Err
}

// RunCaptured implements the npm port, which returns what the tool printed
// instead of streaming it.
func (r *Recorder) RunCaptured(_ context.Context, dir string, args ...string) (string, string, error) {
	r.t.Helper()

	response := r.record(dir, nil, args, nil, nil)

	return response.Stdout, response.Stderr, response.Err
}

// RunExit implements the Xcode port, which reports the tool's exit status
// separately from a failure to start it.
func (r *Recorder) RunExit(_ context.Context, stdout, stderr io.Writer, args ...string) (int, error) {
	r.t.Helper()

	response := r.record("", nil, args, stdout, stderr)

	return response.ExitCode, response.Err
}

// EvalExpression implements Maven's property reader. The expression is recorded
// as its own call so an assertion sees it in sequence with the builds.
func (r *Recorder) EvalExpression(_ context.Context, expr string) (string, error) {
	r.t.Helper()

	response := r.record("", nil, []string{"--evaluate", expr}, nil, nil)
	if response.Err != nil {
		return "", response.Err
	}

	return strings.TrimSpace(response.Stdout), nil
}

// AssertCalls fails the test unless the recorded sequence is exactly want.
func (r *Recorder) AssertCalls(want []Call) {
	r.t.Helper()

	if len(r.Calls) != len(want) {
		r.t.Fatalf("toolrecorder: %d calls, want %d:\n got: %s\nwant: %s", len(r.Calls), len(want), format(r.Calls), format(want))
	}

	for index := range want {
		if !equal(r.Calls[index], want[index]) {
			r.t.Fatalf("toolrecorder: call %d differs:\n got: %#v\nwant: %#v", index, r.Calls[index], want[index])
		}
	}
}

func equal(left, right Call) bool {
	return left.Dir == right.Dir && slices.Equal(left.Args, right.Args) && slices.Equal(left.Env, right.Env)
}

func format(calls []Call) string {
	var out strings.Builder
	for index, call := range calls {
		_, _ = fmt.Fprintf(&out, "\n  %d: dir=%q args=%q env=%q", index, call.Dir, call.Args, call.Env)
	}

	return out.String()
}

// The ports disagree on method names and shapes for the same idea, and one type
// cannot carry two methods called Run. These views expose the recorder under
// each remaining spelling; every one of them records into the same list, so a
// test still asserts one ordered sequence across ecosystems.

// NPM returns the recorder under the npm captured-output port.
func (r *Recorder) NPM() NPMView { return NPMView{r} }

// NPMView adapts the recorder to NPMOps and NPMRunner.
type NPMView struct{ *Recorder }

// Run captures output instead of streaming it, as the npm port does.
func (v NPMView) Run(ctx context.Context, dir string, args ...string) (string, string, error) {
	return v.RunCaptured(ctx, dir, args...)
}

// RunInherit streams into the caller's writers from a directory.
func (v NPMView) RunInherit(ctx context.Context, dir string, stdout, stderr io.Writer, args ...string) error {
	return v.RunInDirInherit(ctx, dir, stdout, stderr, args...)
}

// Xcode returns the recorder under the xcodebuild port, which reports an exit
// status separately from a failure to start.
func (r *Recorder) Xcode() XcodeView { return XcodeView{r} }

// XcodeView adapts the recorder to XcodeBuildOps.
type XcodeView struct{ *Recorder }

// RunInherit reports the tool's exit status alongside any start error.
func (v XcodeView) RunInherit(ctx context.Context, stdout, stderr io.Writer, args ...string) (int, error) {
	return v.RunExit(ctx, stdout, stderr, args...)
}

// Android returns the recorder under the Android Gradle port, which carries
// both a directory and the child environment the signing credentials ride in.
func (r *Recorder) Android() AndroidView { return AndroidView{r} }

// AndroidView adapts the recorder to AndroidGradleOps.
type AndroidView struct{ *Recorder }

// RunInDirEnvInherit records the directory and environment alongside the argv.
func (v AndroidView) RunInDirEnvInherit(_ context.Context, dir string, env []string, stdout, stderr io.Writer, args ...string) error {
	v.t.Helper()

	return v.record(dir, env, args, stdout, stderr).Err
}

// Security returns the recorder under the macOS security(1) port.
func (r *Recorder) Security() SecurityView { return SecurityView{r} }

// SecurityView adapts the recorder to XcodeSecurityOps.
type SecurityView struct{ *Recorder }

// Run returns what security(1) printed.
func (v SecurityView) Run(ctx context.Context, args ...string) (string, error) {
	stdout, _, err := v.RunCaptured(ctx, "", args...)

	return stdout, err
}

// record appends one call and produces its response, writing any artifacts.
func (r *Recorder) record(dir string, env, args []string, stdout, stderr io.Writer) Response {
	r.t.Helper()

	call := Call{Dir: dir, Env: slices.Clone(env), Args: slices.Clone(args)}
	index := len(r.Calls)
	r.Calls = append(r.Calls, call)

	var response Response
	if r.respond != nil {
		response = r.respond(index, call)
	}

	r.writeArtifacts(dir, response.Artifacts, response.ArtifactMode)

	if stdout != nil && response.Stdout != "" {
		_, _ = io.WriteString(stdout, response.Stdout)
	}

	if stderr != nil && response.Stderr != "" {
		_, _ = io.WriteString(stderr, response.Stderr)
	}

	return response
}

// writeArtifacts creates the files a real tool would have left behind, so the
// step that consumes them exercises its own discovery rather than a stub.
func (r *Recorder) writeArtifacts(dir string, artifacts map[string]string, mode os.FileMode) {
	r.t.Helper()

	if mode == 0 {
		mode = 0o600
	}

	names := make([]string, 0, len(artifacts))
	for name := range artifacts {
		names = append(names, name)
	}

	slices.Sort(names)

	for _, name := range names {
		// A tool told where to put its output writes there, not under its own
		// working directory.
		path := name
		if dir != "" && !filepath.IsAbs(name) {
			path = filepath.Join(dir, name)
		}

		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil { //nolint:gosec // owned temporary build tree.
			r.t.Fatalf("toolrecorder: create %s: %v", filepath.Dir(path), err)
		}

		if err := os.WriteFile(path, []byte(artifacts[name]), mode); err != nil { //nolint:gosec // the mode is the fixture's contract.
			r.t.Fatalf("toolrecorder: write %s: %v", path, err)
		}
	}
}
