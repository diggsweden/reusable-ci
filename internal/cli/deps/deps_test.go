// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package deps_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/cli/deps"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testenv"
)

func TestBuild_DetectsPlatformAndWiresDependencies(t *testing.T) {
	tests := []struct {
		name   string
		github bool
		gitlab bool
		want   provider.ForgeAPI
	}{
		{name: "github", github: true, want: provider.ForgeGitHub},
		{name: "gitlab", gitlab: true, want: provider.ForgeGitLab},
		{name: "local", want: provider.ForgeLocal},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			env := testenv.New(t)
			env.Setenv("GITHUB_ACTIONS", boolEnv(testCase.github))
			env.Setenv("GITLAB_CI", boolEnv(testCase.gitlab))

			d, err := deps.Build(context.Background()) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
			if err != nil {
				t.Fatal(err)
			}

			if d.Platform != testCase.want {
				t.Errorf("Platform = %q, want %q", d.Platform, testCase.want)
			}

			if d.Provider == nil {
				t.Fatal("Provider nil")
			}

			if got := d.Provider.Name(); got != testCase.want {
				t.Errorf("Provider.Name = %q, want %q", got, testCase.want)
			}

			if d.OutputSink == nil {
				t.Errorf("OutputSink nil")
			}
		})
	}
}

func TestWithFormat_ClosesOutputSink(t *testing.T) {
	env := testenv.New(t)
	dir := env.MkdirAll("deps-with")
	outPath := filepath.Join(dir, "output")
	env.Setenv("GITHUB_OUTPUT", outPath)
	env.Setenv("GITHUB_ACTIONS", "true")

	if err := deps.WithFormat(context.Background(), output.FormatAuto, nil, func(d *deps.Deps) error {
		return d.OutputSink.Set(context.Background(), "key", "value")
	}); err != nil {
		t.Fatal(err)
	}

	body, err := os.ReadFile(outPath) //nolint:gosec // test fixture path
	if err != nil {
		t.Fatal(err)
	}

	if got := string(body); got != "key=value\n" {
		t.Errorf("output = %q", got)
	}
}

func TestBuild_GitLabWiresDotenvOutputSink(t *testing.T) {
	env := testenv.New(t)
	dir := env.MkdirAll("gitlab-output")
	outPath := filepath.Join(dir, "output.env")

	env.Setenv("GITLAB_CI", "true")
	env.Setenv("CI_OUTPUT", outPath)

	d, err := deps.Build(context.Background()) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if err != nil {
		t.Fatal(err)
	}

	if setErr := d.OutputSink.Set(context.Background(), "version-no-v", "1.2.3"); setErr != nil {
		t.Fatal(setErr)
	}

	if closeErr := d.Close(context.Background()); closeErr != nil {
		t.Fatal(closeErr)
	}

	body, err := os.ReadFile(outPath) //nolint:gosec // test fixture path
	if err != nil {
		t.Fatal(err)
	}

	if got := string(body); got != "VERSION_NO_V=1.2.3\n" {
		t.Errorf("output = %q", got)
	}
}

func TestBuild_WiresProviderSpecificSummarySink(t *testing.T) {
	tests := []struct {
		name    string
		github  bool
		gitlab  bool
		want    string
		unwants []string
	}{
		{name: "github", github: true, want: "github.md", unwants: []string{"gitlab.md"}}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		{name: "gitlab", gitlab: true, want: "gitlab.md", unwants: []string{"github.md"}},
		{name: "local", unwants: []string{"github.md", "gitlab.md"}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			env := testenv.New(t)
			dir := env.MkdirAll("summary")
			env.Setenv("GITHUB_ACTIONS", boolEnv(testCase.github))
			env.Setenv("GITLAB_CI", boolEnv(testCase.gitlab))
			env.Setenv("GITHUB_STEP_SUMMARY", filepath.Join(dir, "github.md"))
			env.Setenv("CI_SUMMARY_FILE", filepath.Join(dir, "gitlab.md"))

			d, err := deps.Build(context.Background())
			if err != nil {
				t.Fatal(err)
			}

			if err := d.SummarySink.Append(context.Background(), "summary\n"); err != nil {
				t.Fatal(err)
			}

			if testCase.want != "" {
				body, err := os.ReadFile(filepath.Join(dir, testCase.want)) //nolint:gosec // test fixture
				if err != nil {
					t.Fatal(err)
				}

				if got := string(body); got != "summary\n" {
					t.Errorf("summary = %q", got)
				}
			}

			for _, unwanted := range testCase.unwants {
				if _, err := os.Stat(filepath.Join(dir, unwanted)); err == nil {
					t.Errorf("%s was written unexpectedly", unwanted)
				}
			}
		})
	}
}

func TestBuild_LocalOutputSinkIgnoresCIOutputPaths(t *testing.T) {
	env := testenv.New(t)
	dir := env.MkdirAll("local-output")
	githubOutput := filepath.Join(dir, "github-output")
	ciOutput := filepath.Join(dir, "ci-output")

	env.Setenv("GITHUB_ACTIONS", "")
	env.Setenv("GITLAB_CI", "")
	env.Setenv("GITHUB_OUTPUT", githubOutput)
	env.Setenv("CI_OUTPUT", ciOutput)

	d, err := deps.Build(context.Background()) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
	if err != nil {
		t.Fatal(err)
	}

	if err := d.OutputSink.Set(context.Background(), "key", "value"); err != nil {
		t.Fatal(err)
	}

	if err := d.Close(context.Background()); err != nil {
		t.Fatal(err)
	}

	for _, path := range []string{githubOutput, ciOutput} {
		if _, err := os.Stat(path); err == nil {
			t.Errorf("%s was written unexpectedly", filepath.Base(path))
		}
	}
}

func boolEnv(b bool) string {
	if b {
		return "true"
	}

	return ""
}

// errOf keeps only the error of a role accessor's result.
func errOf[R any](_ R, err error) error { return err }

// TestRequireRoles_ConformanceMatrix pins every role accessor for every forge:
// a supported role returns its view, an unsupported one a typed ErrUnsupported
// naming the forge, never a stub that fails deeper in the use case. A role an
// adapter gains or loses changes this table on purpose.
func TestRequireRoles_ConformanceMatrix(t *testing.T) {
	const (
		gh = 1 << iota
		gl
		fj
	)

	roles := []struct {
		name      string
		require   func(*deps.Deps) error
		supported int
	}{
		{"token validation", func(d *deps.Deps) error { return errOf(d.RequireTokenValidator()) }, gh | gl | fj},
		{"release creation", func(d *deps.Deps) error { return errOf(d.RequireReleaseCreator()) }, gh | gl | fj},
		{"release publishing", func(d *deps.Deps) error { return errOf(d.RequireReleasePublisher()) }, gh | gl | fj},
		{"release asset upload", func(d *deps.Deps) error { return errOf(d.RequireReleaseAssetUploader()) }, gh | gl | fj},
		{"run-artifact upload", func(d *deps.Deps) error { return errOf(d.RequireRunArtifactUploader()) }, gh | fj},
		{"run-artifact download", func(d *deps.Deps) error { return errOf(d.RequireRunArtifactDownloader()) }, gh | fj},
		{"forge-packages Maven deploy", func(d *deps.Deps) error { return errOf(d.RequireForgeMavenRegistryResolver()) }, gh | gl | fj},
		{"forge-packages npm publish", func(d *deps.Deps) error { return errOf(d.RequireForgeNPMRegistryResolver()) }, gh | gl | fj},
		{"sigstore keyless signing identity", func(d *deps.Deps) error { return errOf(d.RequireSigningIdentityResolver()) }, gh | gl | fj},
		{"forge container-registry login", func(d *deps.Deps) error { return errOf(d.RequireRegistryAuthResolver()) }, gh | gl | fj},
		{"SARIF upload to Code Scanning", func(d *deps.Deps) error { return errOf(d.RequireSARIFUploader()) }, gh},
		{"container tag deletion", func(d *deps.Deps) error { return errOf(d.RequireTagDeleter()) }, gl | fj},
	}

	for _, forge := range []struct {
		name string
		bit  int
	}{{"github", gh}, {"gitlab", gl}, {"forgejo", fj}, {"local", 0}} {
		t.Run(forge.name, func(t *testing.T) {
			env := testenv.New(t)
			env.Setenv("REUSABLE_CI_PROVIDER", forge.name)

			d, err := deps.Build(context.Background()) //nolint:varnamelen // idiomatic short name (testing/http/io conventions).
			if err != nil {
				t.Fatal(err)
			}

			if string(d.Platform) != forge.name {
				t.Fatalf("Platform = %q, want %q", d.Platform, forge.name)
			}

			for _, role := range roles {
				err := role.require(d)

				switch {
				case role.supported&forge.bit != 0 && err != nil:
					t.Errorf("%s: %v, want supported", role.name, err)
				case role.supported&forge.bit == 0 && (!errors.Is(err, errs.ErrUnsupported) ||
					!strings.Contains(err.Error(), role.name) || !strings.Contains(err.Error(), `"`+forge.name+`"`)):
					t.Errorf("%s: err = %v, want ErrUnsupported naming the role and platform", role.name, err)
				}
			}

			// Always available: local returns empty metadata and has no web UI.
			if d.RepoMetadataFetcher() == nil {
				t.Error("RepoMetadataFetcher() returned nil")
			}

			if got, want := d.WebURLBuilder() != nil, forge.bit != 0; got != want {
				t.Errorf("WebURLBuilder present = %t, want %t", got, want)
			}
		})
	}
}

// failingWriter fails every write, so a sink that only emits on Close surfaces
// its error through WithFormat's deferred close.
type failingWriter struct{ err error }

func (w failingWriter) Write([]byte) (int, error) { return 0, w.err }

var errWriterFull = errors.New("deps test: writer is full") //nolint:err113 // test fixture sentinel.

// TestWithFormat_CloseIsObservable makes the close actually observable.
//
// TestWithFormat_ClosesOutputSink is named for the close but cannot see it: the
// GitHub sink appends on every Set, so the file it inspects holds the value
// whether or not Close ever ran. Deleting the deferred close leaves that test
// passing.
//
// The JSON sink is close-sensitive by construction — it buffers the outputs and
// writes the object only when closed — so it can carry the assertion without a
// fake. Two things follow from that: nothing appears unless the close happened,
// and an error from the close is the caller's error, which is what stops a
// command reporting success after failing to write the outputs the next job
// reads.
func TestWithFormat_CloseIsObservable(t *testing.T) {
	t.Run("the sink is closed, so its output is emitted", func(t *testing.T) {
		testenv.New(t)

		var out bytes.Buffer

		if err := deps.WithFormat(context.Background(), output.FormatJSON, &out, func(d *deps.Deps) error {
			return d.OutputSink.Set(context.Background(), "key", "value")
		}); err != nil {
			t.Fatal(err)
		}

		got := out.String()
		if !strings.Contains(got, `"key"`) || !strings.Contains(got, `"value"`) {
			t.Errorf("output = %q, want the JSON object the sink writes on close", got)
		}
	})

	t.Run("a close failure becomes the caller's error", func(t *testing.T) {
		testenv.New(t)

		err := deps.WithFormat(context.Background(), output.FormatJSON, failingWriter{err: errWriterFull},
			func(d *deps.Deps) error {
				return d.OutputSink.Set(context.Background(), "key", "value")
			})
		if err == nil {
			t.Fatal("WithFormat reported success while the outputs could not be written")
		}

		if !errors.Is(err, errWriterFull) {
			t.Errorf("err = %v, want the writer's own cause", err)
		}
	})

	t.Run("the body's error wins over a close failure", func(t *testing.T) {
		testenv.New(t)

		bodyErr := errors.New("deps test: the command failed") //nolint:err113 // test fixture sentinel.

		err := deps.WithFormat(context.Background(), output.FormatJSON, failingWriter{err: errWriterFull},
			func(d *deps.Deps) error {
				_ = d.OutputSink.Set(context.Background(), "key", "value")

				return bodyErr
			})
		if !errors.Is(err, bodyErr) {
			t.Errorf("err = %v, want the body's error: what the command was doing matters more than the close", err)
		}
	})
}

// TestWithFormat_SelectsTheWriterByFormat covers the two branches the close
// tests do not: which writer receives the outputs.
//
// Only JSON mode may write to the supplied writer. In every other mode the
// outputs go to the platform sink ($GITHUB_OUTPUT and friends), and the writer
// passed in is the command's stdout, which carries its human-readable result --
// a JSON object appearing there in text mode corrupts what a caller parses.
// The nil default matters for the same reason in reverse: FromCmd passes
// os.Stdout, but a library caller passing nil must still get the object
// somewhere, not a panic on a nil writer.
func TestWithFormat_SelectsTheWriterByFormat(t *testing.T) {
	t.Run("a non-JSON format leaves the writer untouched", func(t *testing.T) {
		env := testenv.New(t)

		outputFile := filepath.Join(t.TempDir(), "github-output")

		env.Setenv("GITHUB_ACTIONS", "true")
		env.Setenv("GITHUB_OUTPUT", outputFile)

		var out bytes.Buffer

		if err := deps.WithFormat(context.Background(), output.FormatAuto, &out, func(d *deps.Deps) error {
			return d.OutputSink.Set(context.Background(), "key", "value")
		}); err != nil {
			t.Fatal(err)
		}

		if out.Len() != 0 {
			t.Errorf("a non-JSON run wrote %q to the command's stdout", out.String())
		}

		body, err := os.ReadFile(outputFile) //nolint:gosec // owned temp path.
		if err != nil {
			t.Fatalf("the platform sink wrote nothing: %v", err)
		}

		if !strings.Contains(string(body), "key=value") {
			t.Errorf("platform output = %q, want the value there instead", body)
		}
	})

	t.Run("a nil writer in JSON mode falls back to stdout", func(t *testing.T) {
		testenv.New(t)

		r, w, err := os.Pipe()
		if err != nil {
			t.Fatal(err)
		}

		t.Cleanup(func() {
			_ = r.Close()
			_ = w.Close()
		})

		previous := os.Stdout
		os.Stdout = w //nolint:reassign // serial test of the documented nil-writer default.

		runErr := deps.WithFormat(context.Background(), output.FormatJSON, nil, func(d *deps.Deps) error {
			return d.OutputSink.Set(context.Background(), "key", "value")
		})

		os.Stdout = previous //nolint:reassign // restore before reading, so a failure prints normally.

		_ = w.Close()

		captured, readErr := io.ReadAll(r)
		if runErr != nil || readErr != nil {
			t.Fatalf("run = %v, read = %v", runErr, readErr)
		}

		if !strings.Contains(string(captured), `"key"`) {
			t.Errorf("stdout = %q, want the JSON object", captured)
		}
	})
}
