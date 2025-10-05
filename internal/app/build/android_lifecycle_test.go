// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"

	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testenv"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

type androidGradleCall struct {
	Context        context.Context //nolint:containedctx // this test recorder snapshots the forwarded context for identity/cancellation assertions, not production request lifetime.
	Dir            string
	Env, Args      []string
	Stdout, Stderr io.Writer
}

type androidGradleRecorder struct {
	calls []androidGradleCall
	run   func(androidGradleCall) error
}

func (r *androidGradleRecorder) RunInherit(ctx context.Context, out, stderr io.Writer, args ...string) error {
	return r.RunInDirInherit(ctx, "", out, stderr, args...)
}

func (r *androidGradleRecorder) RunInDirInherit(ctx context.Context, dir string, out, stderr io.Writer, args ...string) error {
	return r.RunInDirEnvInherit(ctx, dir, nil, out, stderr, args...)
}

func (r *androidGradleRecorder) RunInDirEnvInherit(ctx context.Context, dir string, env []string, out, stderr io.Writer, args ...string) error {
	call := androidGradleCall{Context: ctx, Dir: dir, Env: slices.Clone(env), Args: slices.Clone(args), Stdout: out, Stderr: stderr}

	r.calls = append(r.calls, call)
	if r.run != nil {
		return r.run(call)
	}

	return nil
}

func TestAndroidReleaseBuild_KnownRefusalsPreserveState(t *testing.T) {
	for _, name := range []string{"missing_wrapper", "missing_credentials", "occupied_properties"} {
		t.Run(name, func(t *testing.T) {
			fsys := testfs.NewReal(t)
			t.Setenv("TMPDIR", fsys.MkdirAll("fallback"))
			t.Setenv("ANDROID_KEYSTORE_PATH", "parent-canary")

			in := appbuild.AndroidReleaseBuildInput{
				ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: fsys.MkdirAll("project")},
				RepoName:            "mobile", BuildTypes: "release", TempDir: fsys.MkdirAll("scratch"),
			}
			if name != "missing_wrapper" {
				fsys.WriteFile("project/gradlew", []byte("inert wrapper"))
			}

			want := os.ErrNotExist

			switch name {
			case "missing_credentials":
				in.EnableSigning, in.KeystoreBase64 = true, "aw=="
				want = errs.ErrPermissionDenied
			case "occupied_properties":
				in.SecretsPropertiesBase64 = "cA=="

				fsys.WriteFile("project/secrets.properties", []byte("caller-owned"))

				want = errs.ErrValidation
			}

			before, env := ownedTree(t, fsys.Root), os.Environ()
			sink, summary, ops := fakeoutputsink.New(t), &recordingSummarySink{}, &androidGradleRecorder{}

			var out, stderr bytes.Buffer

			err := appbuild.AndroidReleaseBuild(t.Context(), sink, summary, ops, output.NewAnnotator(&stderr, output.FormatGitHub), &out, &stderr, in)
			require.ErrorIs(t, err, want)
			require.Empty(t, sink.Keys())
			require.Empty(t, ops.calls)
			require.Empty(t, summary.buf.String()+out.String()+stderr.String())
			require.Equal(t, before, ownedTree(t, fsys.Root))
			require.Equal(t, env, os.Environ())
		})
	}
}

func TestAndroidReleaseBuild_PropertiesLifetime(t *testing.T) {
	fsys := testfs.NewReal(t)
	t.Setenv("TMPDIR", fsys.MkdirAll("fallback"))
	dir := fsys.MkdirAll("project")
	fsys.WriteFile("project/gradlew", []byte("inert wrapper"))

	path := filepath.Join(dir, "secrets.properties")
	ops := &androidGradleRecorder{run: func(call androidGradleCall) error {
		require.Equal(t, dir, call.Dir)
		require.Equal(t, "p", string(fsys.ReadFile("project/secrets.properties")))

		info, err := os.Stat(path)
		require.NoError(t, err)
		require.Equal(t, os.FileMode(0o600), info.Mode().Perm())

		return nil
	}}
	err := appbuild.AndroidReleaseBuild(t.Context(), fakeoutputsink.New(t), &recordingSummarySink{}, ops, output.Annotator{}, io.Discard, io.Discard, appbuild.AndroidReleaseBuildInput{
		ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: dir}, RepoName: "mobile", BuildTypes: "release", SecretsPropertiesBase64: "cA==",
	})
	require.NoError(t, err)
	require.Len(t, ops.calls, 1)
	require.NoFileExists(t, path)
}

func TestAndroidReleaseBuild_SigningDoesNotChangeParent(t *testing.T) {
	fsys := testfs.NewReal(t)
	t.Setenv("TMPDIR", fsys.MkdirAll("fallback"))
	t.Setenv("ANDROID_KEYSTORE_PATH", "parent-canary")

	before := os.Environ()
	dir := fsys.MkdirAll("project")
	fsys.WriteFile("project/gradlew", []byte("inert wrapper"))
	err := appbuild.AndroidReleaseBuild(t.Context(), fakeoutputsink.New(t), &recordingSummarySink{}, &androidGradleRecorder{}, output.Annotator{}, io.Discard, io.Discard, appbuild.AndroidReleaseBuildInput{
		ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: dir}, RepoName: "mobile", BuildTypes: "release", EnableSigning: true, KeystoreBase64: "aw==", TempDir: fsys.MkdirAll("scratch"),
		KeystorePassword: "store password", KeyAlias: "alias", KeyPassword: "key password",
	})
	require.NoError(t, err)
	require.Equal(t, before, os.Environ())

	entries, err := os.ReadDir(fsys.Path("scratch"))
	require.NoError(t, err)
	require.Empty(t, entries)
}

type androidLifecycleSink struct {
	*fakeoutputsink.Sink
	events         *[]string
	fail           string
	cause          error
	summary        string
	contexts       []context.Context
	attempts       []string
	summaryAttempt string
	afterSet       func(string)
}

func (s *androidLifecycleSink) Set(ctx context.Context, key, value string) error {
	*s.events = append(*s.events, key)
	s.contexts = append(s.contexts, ctx)

	s.attempts = append(s.attempts, key+"="+value)
	if key == s.fail {
		return s.cause
	}

	if s.afterSet != nil {
		s.afterSet(key)
	}

	return s.Sink.Set(ctx, key, value)
}

func (s *androidLifecycleSink) Append(ctx context.Context, body string) error {
	*s.events = append(*s.events, "summary")
	s.contexts = append(s.contexts, ctx)

	s.summaryAttempt = body
	if s.fail == "summary" {
		return s.cause
	}

	s.summary += body

	return nil
}

func TestAndroidReleaseBuild_SignedCallPrefixesAndCleanup(t *testing.T) { //nolint:gocognit,gocyclo,maintidx // complete ordered effect prefixes and inside-call file checks share the same signed fixture.
	wantEvents := []string{"debug-name", "release-name", "aab-name", "sbom-name", "version", "version-code", "build", "sbom", "summary"}
	for _, fail := range append([]string{"", "v3", "sbom_missing", "missing_metadata", "sbom_and_summary", "cancel_build", "cancel_sbom"}, wantEvents...) {
		t.Run("fail_"+fail, func(t *testing.T) {
			testenv.New(t)
			fsys := testfs.NewReal(t)
			fsys.Chdir()
			t.Setenv("TMPDIR", fsys.MkdirAll("fallback"))

			for _, name := range []string{"ANDROID_KEYSTORE_PATH", "ANDROID_KEYSTORE_PASSWORD", "ANDROID_KEY_ALIAS", "ANDROID_KEY_PASSWORD"} {
				t.Setenv(name, "ambient-"+name)
			}

			parentEnv := os.Environ()

			fsys.WriteFile("gradlew", []byte("unselected CWD wrapper"))
			fsys.WriteFile("gradle.properties", []byte("versionName=wrong-cwd\nversionCode=999\n"))
			fsys.WriteFile("project/gradlew", []byte("inert selected wrapper"))
			fsys.WriteFile("project/gradle.properties", []byte("versionName=3.2.1\nversionCode=37\n"))

			if fail == "missing_metadata" {
				require.NoError(t, os.Remove(fsys.Path("project/gradle.properties")))
			}

			fsys.WriteFile("scratch/release.keystore", []byte("caller-keystore-canary"))
			fsys.WriteFile("decoy/build/reports/bom.json", []byte("unselected report"))
			decoy := ownedTree(t, fsys.Path("decoy"))
			fallbackBefore := ownedTree(t, fsys.Path("fallback"))
			in := appbuild.AndroidReleaseBuildInput{
				ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: "project/.", EnableBuildSBOM: true, SkipTests: true},
				RepoName:            "mobile", GradleTasksOverride: " assembleChosen\t:widget:bundleChosen ", BuildTypes: "ignored-invalid",
				EnableSigning: true, KeystoreBase64: base64.StdEncoding.EncodeToString([]byte("owned-keystore\x00\xff\n")),
				KeystorePassword: " store-password \r\n", KeyAlias: " alias-canary ", KeyPassword: "\tkey-password\n",
				SecretsPropertiesBase64: base64.StdEncoding.EncodeToString([]byte("token=owned-properties\n")),
				TempDir:                 "scratch/.", SBOMToolVersion: "2.3.4",
			}

			causes := map[string]error{}
			for _, step := range wantEvents {
				causes[step] = errors.New("owned " + step + " failure") //nolint:err113 // independent per-port error identities.
			}

			ctx, cancel := context.WithCancelCause(t.Context())
			defer cancel(nil)

			var events []string

			sink := &androidLifecycleSink{Sink: fakeoutputsink.New(t), events: &events, fail: fail, cause: causes[fail], summary: "caller summary\n"}
			require.NoError(t, sink.Sink.Set(ctx, "unrelated", "caller output"))
			require.NoError(t, sink.SetMultiline(ctx, "caller-lines", []string{"one", "two"}))

			if fail == "sbom_and_summary" {
				sink.fail, sink.cause = "summary", causes["summary"]
			}

			report := "project/build/reports/bom.json"
			if fail == "v3" {
				in.SBOMToolVersion, report = "3.2.1", "project/build/reports/cyclonedx/bom.json"
			}

			var (
				stdout, stderr    bytes.Buffer
				keyPath, initPath string
			)

			ops := &androidGradleRecorder{run: func(call androidGradleCall) error {
				require.Same(t, ctx, call.Context)
				require.NoError(t, call.Context.Err())
				require.Equal(t, fsys.Path("project"), call.Dir)
				require.Same(t, &stdout, call.Stdout)
				require.Same(t, &stderr, call.Stderr)
				require.Len(t, call.Env, 4)
				path := strings.TrimPrefix(call.Env[0], "ANDROID_KEYSTORE_PATH=")
				require.Equal(t, fsys.Path("scratch"), filepath.Dir(filepath.Dir(path)))
				require.NotEqual(t, fsys.Path("scratch/release.keystore"), path)

				if keyPath == "" {
					keyPath = path
				}

				require.Equal(t, keyPath, path)
				require.Equal(t, []string{"ANDROID_KEYSTORE_PATH=" + keyPath, "ANDROID_KEYSTORE_PASSWORD= store-password \r\n", "ANDROID_KEY_ALIAS= alias-canary ", "ANDROID_KEY_PASSWORD=\tkey-password\n"}, call.Env)

				for path, want := range map[string]string{keyPath: "owned-keystore\x00\xff\n", fsys.Path("project/secrets.properties"): "token=owned-properties\n"} {
					body, err := os.ReadFile(path)
					require.NoError(t, err)
					require.Equal(t, want, string(body))

					info, err := os.Stat(path)
					require.NoError(t, err)
					require.Equal(t, os.FileMode(0o600), info.Mode().Perm())
				}

				info, err := os.Stat(filepath.Dir(keyPath))
				require.NoError(t, err)
				require.Equal(t, os.FileMode(0o700), info.Mode().Perm())
				require.Equal(t, parentEnv, os.Environ())

				step := "build"
				if len(events) == 7 {
					step = "sbom"

					require.Len(t, call.Args, 3)
					initPath = call.Args[1]
					require.Equal(t, []string{"--init-script", initPath, "cyclonedxBom"}, call.Args)
					body, readErr := os.ReadFile(initPath)
					require.NoError(t, readErr)
					require.Contains(t, string(body), "org.cyclonedx:cyclonedx-gradle-plugin:"+in.SBOMToolVersion)

					if fail != "sbom_missing" {
						fsys.WriteFile(report, []byte(`{"bomFormat":"CycloneDX","specVersion":"1.6","version":1,"components":[{"type":"library","name":"owned-fixture","version":"1.0"}]}`))
					}
				} else {
					require.Equal(t, []string{"assembleChosen", ":widget:bundleChosen", "-x", "test"}, call.Args)
				}

				events = append(events, step)
				if fail == "cancel_"+step {
					cancel(causes[step])
					require.Same(t, causes[step], context.Cause(ctx))
					require.ErrorIs(t, call.Context.Err(), context.Canceled)

					return call.Context.Err()
				}

				if step == fail || (step == "sbom" && fail == "sbom_and_summary") {
					return causes[step]
				}

				_, err = io.WriteString(call.Stdout, step+" stdout\n")
				require.NoError(t, err)
				_, err = io.WriteString(call.Stderr, step+" stderr\n")
				require.NoError(t, err)

				return nil
			}}
			err := appbuild.AndroidReleaseBuild(ctx, sink, sink, ops, output.NewAnnotator(&stderr, output.FormatGitHub), &stdout, &stderr, in)
			wantPrefix := wantEvents

			switch fail {
			case "", "v3", "sbom_missing", "missing_metadata":
				require.NoError(t, err)
				require.Contains(t, stdout.String(), "build stdout\nsbom stdout\n")
				require.Contains(t, stderr.String(), "build stderr\nsbom stderr\n")
			default:
				stage := strings.TrimPrefix(fail, "cancel_")
				if fail == "sbom_and_summary" {
					stage = "sbom"
				}

				if strings.HasPrefix(fail, "cancel_") {
					require.ErrorIs(t, err, context.Canceled)
				}

				for step, cause := range causes {
					if !strings.HasPrefix(fail, "cancel_") && (step == stage || (fail == "sbom_and_summary" && step == "summary")) {
						require.ErrorIs(t, err, cause)
					} else {
						require.NotErrorIs(t, err, cause)
					}
				}

				switch stage {
				case "debug-name", "release-name", "aab-name", "sbom-name":
					require.ErrorContains(t, err, "compose artifact names: set "+stage+":")
				case "version", "version-code":
					require.ErrorContains(t, err, "resolve version:")
				case "build":
					require.ErrorContains(t, err, "gradle build:")
				case "sbom":
					require.ErrorContains(t, err, "gradle-android Build SBOM generation failed:")
				case "summary":
					require.ErrorContains(t, err, "write SBOM status:")
				}

				if stage != "sbom" {
					wantPrefix = wantEvents[:slices.Index(wantEvents, stage)+1]
				}

				if fail == "sbom_and_summary" {
					require.Equal(t, "gradle-android Build SBOM generation failed: owned sbom failure\nwrite SBOM status: owned summary failure", err.Error())
				}
			}

			require.Equal(t, wantPrefix, events)

			wantOutputs := map[string]string{"unrelated": "caller output"}

			values := []string{"mobile - APK debug", "mobile - APK release", "mobile - AAB release", "mobile - build SBOM", "3.2.1", "37"}
			if fail == "missing_metadata" {
				values[4], values[5] = "unknown", "unknown"

				require.Contains(t, stderr.String(), "::warning::gradle.properties not found, version info unavailable\n")
			}

			for index, key := range wantEvents[:6] {
				if key == fail {
					break
				}

				wantOutputs[key] = values[index]
			}

			require.Equal(t, wantOutputs, sink.AllScalar())
			require.Equal(t, []string{"one", "two"}, sink.Multiline("caller-lines"))

			for _, got := range sink.contexts {
				require.Same(t, ctx, got)
			}

			wantCalls := 0

			for _, event := range wantPrefix {
				if event == "build" || event == "sbom" {
					wantCalls++
				}
			}

			require.Len(t, ops.calls, wantCalls)

			wantSummary := ""
			if slices.Contains(wantPrefix, "summary") {
				wantSummary = "### Build SBOM\n- \u2713 CycloneDX: `" + fsys.Path(report) + "`\n"

				switch fail {
				case "sbom", "sbom_and_summary", "cancel_sbom":
					wantSummary = "### Build SBOM\n- \u26a0\ufe0f Generation failed; release blocked until the Build SBOM succeeds or is explicitly disabled\n"
				case "sbom_missing":
					wantSummary = "### Build SBOM\n- \u26a0\ufe0f Generated but file not located - check plugin output path\n"
				}
			}

			require.Equal(t, wantSummary, sink.summaryAttempt)

			if sink.fail == "summary" {
				wantSummary = ""
			}

			require.Equal(t, "caller summary\n"+wantSummary, sink.summary)

			wrapperMode := os.FileMode(0o755)
			if slices.Contains(wantEvents[:4], fail) {
				wrapperMode = 0o644
			}

			for path, want := range map[string]struct {
				body string
				mode os.FileMode
			}{
				"project/gradlew":           {"inert selected wrapper", wrapperMode},
				"gradlew":                   {"unselected CWD wrapper", 0o644},
				"gradle.properties":         {"versionName=wrong-cwd\nversionCode=999\n", 0o644},
				"project/gradle.properties": {"versionName=3.2.1\nversionCode=37\n", 0o644},
			} {
				if path == "project/gradle.properties" && fail == "missing_metadata" {
					require.NoFileExists(t, fsys.Path(path))

					continue
				}

				require.Equal(t, want.body, string(fsys.ReadFile(path)))
				info, statErr := os.Stat(fsys.Path(path))
				require.NoError(t, statErr)
				require.Equal(t, want.mode, info.Mode().Perm())
			}

			require.Equal(t, decoy, ownedTree(t, fsys.Path("decoy")))
			require.NoFileExists(t, fsys.Path("project/secrets.properties"))

			if keyPath != "" {
				_, statErr := os.Lstat(filepath.Dir(keyPath))
				require.ErrorIs(t, statErr, os.ErrNotExist)
			}

			if initPath != "" {
				require.NoFileExists(t, initPath)
			}

			entries, readErr := os.ReadDir(fsys.Path("scratch"))
			require.NoError(t, readErr)
			require.Len(t, entries, 1)
			require.Equal(t, "caller-keystore-canary", string(fsys.ReadFile("scratch/release.keystore")))
			require.Equal(t, fallbackBefore, ownedTree(t, fsys.Path("fallback")))
			require.Equal(t, parentEnv, os.Environ())

			for _, secret := range []string{in.KeystorePassword, in.KeyAlias, in.KeyPassword, "store-password", "alias-canary", "key-password", in.KeystoreBase64, in.SecretsPropertiesBase64, "owned-keystore", "owned-properties"} {
				require.NotContains(t, stdout.String()+stderr.String()+sink.summary+sink.summaryAttempt+fmt.Sprint(err)+fmt.Sprint(sink.AllScalar())+strings.Join(sink.attempts, "\n"), secret)

				for _, call := range ops.calls {
					require.NotContains(t, strings.Join(call.Args, " "), secret)
				}
			}
		})
	}
}

func TestAndroidReleaseBuild_CredentialRefusals(t *testing.T) {
	for _, field := range []string{"store", "alias", "key"} {
		for _, value := range []string{"", "synthetic\x00secret", " \t\r\n"} {
			if field != "alias" && value == " \t\r\n" {
				continue // Whitespace-only passwords are valid bytes, covered by controls.
			}

			t.Run(fmt.Sprintf("%s_%d", field, len(value)), func(t *testing.T) {
				fsys := testfs.NewReal(t)
				t.Setenv("TMPDIR", fsys.MkdirAll("fallback"))

				for _, name := range []string{"ANDROID_KEYSTORE_PATH", "ANDROID_KEYSTORE_PASSWORD", "ANDROID_KEY_ALIAS", "ANDROID_KEY_PASSWORD"} {
					t.Setenv(name, "ambient-canary-"+name)
				}

				fsys.WriteFile("project/gradlew", []byte("inert wrapper"))
				fsys.WriteFile("scratch/release.keystore", []byte("caller-canary"))
				in := appbuild.AndroidReleaseBuildInput{
					ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: fsys.Path("project")}, RepoName: "mobile", BuildTypes: "release",
					EnableSigning: true, KeystoreBase64: "aw==", KeystorePassword: "store", KeyAlias: "alias", KeyPassword: "key", TempDir: fsys.Path("scratch"), SecretsPropertiesBase64: "cA==",
				}

				switch field {
				case "store":
					in.KeystorePassword = value
				case "alias":
					in.KeyAlias = value
				case "key":
					in.KeyPassword = value
				}

				before, env := ownedTree(t, fsys.Root), os.Environ()
				sink, summary, ops := fakeoutputsink.New(t), &recordingSummarySink{}, &androidGradleRecorder{}

				var out, stderr bytes.Buffer

				err := appbuild.AndroidReleaseBuild(t.Context(), sink, summary, ops, output.NewAnnotator(&stderr, output.FormatGitHub), &out, &stderr, in)

				want := errs.ErrValidation
				if value == "" {
					want = errs.ErrPermissionDenied
				}

				require.ErrorIs(t, err, want)
				require.Empty(t, sink.Keys())
				require.Empty(t, ops.calls)
				require.Empty(t, summary.buf.String()+out.String()+stderr.String())
				require.NotContains(t, err.Error(), "synthetic")
				require.Equal(t, before, ownedTree(t, fsys.Root))
				require.Equal(t, env, os.Environ())
			})
		}
	}
}

func TestAndroidReleaseBuild_PathRefusals(t *testing.T) { //nolint:gocognit // one snapshot oracle for the known root, wrapper, metadata and secret destination refusals.
	for _, name := range []string{"project_link", "wrapper_link", "wrapper_directory", "wrapper_unreadable", "metadata_directory", "metadata_unreadable", "scratch_missing", "scratch_file", "scratch_link", "scratch_readonly", "scratch_in_project", "properties_link", "properties_directory", "project_readonly"} {
		t.Run(name, func(t *testing.T) {
			fsys := testfs.NewReal(t)
			t.Setenv("TMPDIR", fsys.MkdirAll("fallback"))
			t.Setenv("ANDROID_KEYSTORE_PATH", "ambient-canary")
			fsys.WriteFile("project/gradlew", []byte("inert wrapper"))
			fsys.WriteFile("project/gradle.properties", []byte("native=unknown\n"))
			fsys.WriteFile("scratch/caller", []byte("caller scratch bytes"))
			fsys.WriteFile("caller", []byte("caller target bytes"))
			in := appbuild.AndroidReleaseBuildInput{
				ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: fsys.Path("project")}, RepoName: "mobile", BuildTypes: "release",
				EnableSigning: true, KeystoreBase64: "aw==", KeystorePassword: "store", KeyAlias: "alias", KeyPassword: "key", TempDir: fsys.Path("scratch"), SecretsPropertiesBase64: "cA==",
			}
			want := errs.ErrValidation

			var modePath string

			mode := os.FileMode(0o500)

			switch name {
			case "project_link":
				require.NoError(t, os.Symlink(in.Dir, fsys.Path("linked")))
				in.Dir = fsys.Path("linked")
			case "wrapper_link", "wrapper_directory":
				require.NoError(t, os.Remove(fsys.Path("project/gradlew")))

				if name == "wrapper_link" {
					require.NoError(t, os.Symlink(fsys.Path("caller"), fsys.Path("project/gradlew")))
				} else {
					fsys.MkdirAll("project/gradlew")
				}
			case "wrapper_unreadable":
				modePath, mode, want = fsys.Path("project/gradlew"), 0o200, errs.ErrPermissionDenied
			case "metadata_unreadable":
				modePath, mode, want = fsys.Path("project/gradle.properties"), 0o200, errs.ErrPermissionDenied
			case "metadata_directory":
				require.NoError(t, os.Remove(fsys.Path("project/gradle.properties")))
				fsys.MkdirAll("project/gradle.properties")
			case "scratch_missing":
				in.TempDir, want = fsys.Path("absent"), os.ErrNotExist
			case "scratch_file":
				in.TempDir = fsys.Path("caller")
			case "scratch_link":
				require.NoError(t, os.Symlink(in.TempDir, fsys.Path("linked")))
				in.TempDir = fsys.Path("linked")
			case "scratch_readonly":
				modePath, want = in.TempDir, errs.ErrPermissionDenied
			case "scratch_in_project":
				in.TempDir = fsys.MkdirAll("project/scratch")
			case "properties_link":
				require.NoError(t, os.Symlink(fsys.Path("caller"), fsys.Path("project/secrets.properties")))
			case "properties_directory":
				fsys.MkdirAll("project/secrets.properties")
			case "project_readonly":
				modePath, want = in.Dir, errs.ErrPermissionDenied
			}

			before, env := ownedTree(t, fsys.Root), os.Environ()

			var oldMode os.FileMode

			if modePath != "" {
				info, err := os.Stat(modePath)
				require.NoError(t, err)

				oldMode = info.Mode().Perm()

				t.Cleanup(func() { require.NoError(t, os.Chmod(modePath, oldMode)) })
				require.NoError(t, os.Chmod(modePath, mode))
			}

			sink, summary, ops := fakeoutputsink.New(t), &recordingSummarySink{}, &androidGradleRecorder{}

			var out, stderr bytes.Buffer

			err := appbuild.AndroidReleaseBuild(t.Context(), sink, summary, ops, output.NewAnnotator(&stderr, output.FormatGitHub), &out, &stderr, in)
			require.ErrorIs(t, err, want)
			require.Empty(t, sink.Keys())
			require.Empty(t, ops.calls)
			require.Empty(t, summary.buf.String()+out.String()+stderr.String())

			if modePath != "" {
				info, err := os.Stat(modePath)
				require.NoError(t, err)
				require.Equal(t, mode, info.Mode().Perm())
				require.NoError(t, os.Chmod(modePath, oldMode))
			}

			require.Equal(t, before, ownedTree(t, fsys.Root))
			require.Equal(t, env, os.Environ())
		})
	}
}

func TestAndroidReleaseBuild_OptionalControls(t *testing.T) { //nolint:gocognit // contrasting signed, unsigned and fallback controls share one lifecycle oracle.
	for _, name := range []string{"unsigned", "existing_properties", "signed_without_properties", "fallback", "fallback_alias", "default_dir_module", "debug_aab"} {
		t.Run(name, func(t *testing.T) {
			testenv.New(t)
			fsys := testfs.NewReal(t)
			fsys.Chdir()

			fallback := fsys.MkdirAll("fallback")
			if name == "fallback_alias" {
				require.NoError(t, os.Symlink(fallback, fsys.Path("temp-alias")))
				t.Setenv("TMPDIR", fsys.Path("temp-alias"))
			} else {
				t.Setenv("TMPDIR", fallback)
			}

			t.Setenv("ANDROID_KEYSTORE_PATH", "ambient-canary")
			fsys.WriteFile("project/gradlew", []byte("inert wrapper"))
			in := appbuild.AndroidReleaseBuildInput{
				ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: "project"}, RepoName: "mobile", ProductFlavor: "demoFree", BuildModule: "widget", BuildTypes: "release,debug", IncludeAAB: true,
				EnableSigning: true, KeystoreBase64: "aw==", KeystorePassword: " \t\n", KeyAlias: " alias ", KeyPassword: "\r\n ", TempDir: fsys.MkdirAll("scratch"),
			}
			wantArgs := []string{"assembleDemoFreeDebug", "assembleDemoFreeRelease", "widget:bundleDemoFreeRelease"}

			switch name {
			case "default_dir_module":
				t.Chdir(fsys.Path("project"))

				in.Dir, in.BuildModule = "", ""
				wantArgs[2] = "app:bundleDemoFreeRelease"
			case "debug_aab":
				in.BuildTypes = "debug"
				wantArgs = []string{"assembleDemoFreeDebug"}
			}

			if name == "fallback" || name == "fallback_alias" {
				in.TempDir = ""
			}

			if name == "unsigned" || name == "existing_properties" {
				in.EnableSigning, in.KeystoreBase64, in.KeyPassword, in.KeyAlias, in.TempDir = false, "%%%", "\x00", "", fsys.Path("absent-unused")
			}

			if name == "existing_properties" {
				fsys.WriteFile("project/secrets.properties", []byte("caller properties\n"))
				require.NoError(t, os.Chmod(fsys.Path("project/secrets.properties"), 0o640)) //nolint:gosec // caller-owned broad-mode preservation fixture.
			}

			parentEnv := os.Environ()
			ops := &androidGradleRecorder{run: func(call androidGradleCall) error {
				require.Equal(t, fsys.Path("project"), call.Dir)
				require.Same(t, t.Context(), call.Context)
				require.Equal(t, wantArgs, call.Args)

				if in.EnableSigning {
					require.Len(t, call.Env, 4)
					path := strings.TrimPrefix(call.Env[0], "ANDROID_KEYSTORE_PATH=")

					wantRoot := in.TempDir
					if wantRoot == "" {
						wantRoot = fallback
					}

					require.Equal(t, wantRoot, filepath.Dir(filepath.Dir(path)))
					require.Equal(t, []string{"ANDROID_KEYSTORE_PASSWORD= \t\n", "ANDROID_KEY_ALIAS= alias ", "ANDROID_KEY_PASSWORD=\r\n "}, call.Env[1:])
				} else {
					require.Empty(t, call.Env)
				}

				return nil
			}}
			sink, summary := fakeoutputsink.New(t), &recordingSummarySink{}
			err := appbuild.AndroidReleaseBuild(t.Context(), sink, summary, ops, output.Annotator{}, io.Discard, io.Discard, in)
			require.NoError(t, err)
			require.Len(t, ops.calls, 1)
			require.Equal(t, "unknown", sink.Single("version"))
			require.Equal(t, "unknown", sink.Single("version-code"))
			require.Equal(t, "### Build SBOM\n- \u2298 Generation disabled; release continues without a build SBOM\n", summary.buf.String())

			if name == "existing_properties" {
				require.Equal(t, "caller properties\n", string(fsys.ReadFile("project/secrets.properties")))
				info, err := os.Stat(fsys.Path("project/secrets.properties"))
				require.NoError(t, err)
				require.Equal(t, os.FileMode(0o640), info.Mode().Perm())
			} else {
				require.NoFileExists(t, fsys.Path("project/secrets.properties"))
			}

			for _, dir := range []string{fallback, fsys.Path("scratch")} {
				entries, err := os.ReadDir(dir)
				require.NoError(t, err)
				require.Empty(t, entries)
			}

			require.Equal(t, parentEnv, os.Environ())
		})
	}
}

func TestAndroidReleaseBuild_CleanupErrorsPreservePrimaryAndCallerFiles(t *testing.T) { //nolint:gocognit // both primary outcomes for each owned cleanup failure.
	for _, name := range []string{"properties_replaced", "properties_unlink_failure", "keystore_remove_failure"} {
		for _, failBuild := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/build_failure_%t", name, failBuild), func(t *testing.T) {
				fsys := testfs.NewReal(t)
				t.Setenv("TMPDIR", fsys.MkdirAll("fallback"))
				fsys.WriteFile("project/gradlew", []byte("inert wrapper"))
				in := appbuild.AndroidReleaseBuildInput{
					ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: fsys.Path("project")}, RepoName: "mobile", BuildTypes: "release",
					EnableSigning: true, KeystoreBase64: "aw==", KeystorePassword: "store", KeyAlias: "alias", KeyPassword: "key", TempDir: fsys.MkdirAll("scratch"), SecretsPropertiesBase64: "cA==",
				}
				cause := errors.New("owned Gradle failure") //nolint:err113 // independent primary sentinel.
				properties := fsys.Path("project/secrets.properties")

				var keyPath string

				t.Cleanup(func() {
					require.NoError(t, os.Chmod(in.Dir, 0o700))     //nolint:gosec // restore test-owned directory for fixture cleanup.
					require.NoError(t, os.Chmod(in.TempDir, 0o700)) //nolint:gosec // restore test-owned directory for fixture cleanup.
				})

				ops := &androidGradleRecorder{run: func(call androidGradleCall) error {
					keyPath = strings.TrimPrefix(call.Env[0], "ANDROID_KEYSTORE_PATH=")

					switch name {
					case "properties_replaced":
						require.NoError(t, os.Rename(properties, fsys.Path("caller-retained-original")))
						fsys.WriteFile("project/secrets.properties", []byte("caller replacement"))
						require.NoError(t, os.Chmod(properties, 0o640)) //nolint:gosec // caller-owned broad-mode preservation fixture.
					case "properties_unlink_failure":
						require.NoError(t, os.Chmod(in.Dir, 0o500)) //nolint:gosec // remove write permission on an owned directory to force unlink failure.
					case "keystore_remove_failure":
						require.NoError(t, os.Chmod(in.TempDir, 0o500)) //nolint:gosec // remove write permission on an owned directory to force removal failure.
					}

					if failBuild {
						return cause
					}

					return nil
				}}
				err := appbuild.AndroidReleaseBuild(t.Context(), fakeoutputsink.New(t), &recordingSummarySink{}, ops, output.Annotator{}, io.Discard, io.Discard, in)
				require.Error(t, err)

				if failBuild {
					require.ErrorIs(t, err, cause)
				}

				require.Len(t, ops.calls, 1)

				if name == "properties_replaced" {
					require.ErrorIs(t, err, errs.ErrValidation)
					require.Equal(t, "caller replacement", string(fsys.ReadFile("project/secrets.properties")))

					info, statErr := os.Stat(properties)
					require.NoError(t, statErr)
					require.Equal(t, os.FileMode(0o640), info.Mode().Perm())
				} else {
					require.ErrorIs(t, err, os.ErrPermission)
				}

				if name != "keystore_remove_failure" {
					require.NoDirExists(t, filepath.Dir(keyPath))
				} else {
					require.DirExists(t, filepath.Dir(keyPath))
					require.NoFileExists(t, properties)
				}

				if name == "properties_unlink_failure" {
					require.Equal(t, "p", string(fsys.ReadFile("project/secrets.properties")))
				}
			})
		}
	}
}

func TestAndroidReleaseBuild_LatePropertiesCollisionCleansKeystoreOnly(t *testing.T) {
	fsys := testfs.NewReal(t)
	t.Setenv("TMPDIR", fsys.MkdirAll("fallback"))
	t.Setenv("ANDROID_KEYSTORE_PATH", "ambient-canary")
	fsys.WriteFile("project/gradlew", []byte("inert wrapper"))
	in := appbuild.AndroidReleaseBuildInput{
		ReleaseBuildOptions: appbuild.ReleaseBuildOptions{Dir: fsys.Path("project")}, RepoName: "mobile", BuildTypes: "release",
		EnableSigning: true, KeystoreBase64: "aw==", KeystorePassword: "store", KeyAlias: "alias", KeyPassword: "key", TempDir: fsys.MkdirAll("scratch"), SecretsPropertiesBase64: "cA==",
	}

	var events []string

	sink := &androidLifecycleSink{Sink: fakeoutputsink.New(t), events: &events, afterSet: func(key string) {
		if key == "version-code" {
			fsys.WriteFile("project/secrets.properties", []byte("late caller properties"))
		}
	}}
	ops := &androidGradleRecorder{}
	env := os.Environ()
	err := appbuild.AndroidReleaseBuild(t.Context(), sink, sink, ops, output.Annotator{}, io.Discard, io.Discard, in)
	require.ErrorIs(t, err, os.ErrExist)
	require.Equal(t, []string{"debug-name", "release-name", "aab-name", "sbom-name", "version", "version-code"}, events)
	require.Empty(t, ops.calls)
	require.Equal(t, "late caller properties", string(fsys.ReadFile("project/secrets.properties")))

	entries, err := os.ReadDir(in.TempDir)
	require.NoError(t, err)
	require.Empty(t, entries)
	require.Equal(t, env, os.Environ())
}
