// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"maps"
	"os"
	"reflect"
	"strings"
	"testing"

	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/output"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

func TestAndroidReleaseBuild_PreflightRefusals(t *testing.T) { //nolint:gocognit // shared table checks all effects and both fresh/seeded secret destinations.
	for _, tc := range []struct {
		name   string
		change func(*appbuild.AndroidReleaseBuildInput)
		want   error
	}{
		{"missing keystore", func(in *appbuild.AndroidReleaseBuildInput) { in.KeystoreBase64 = "" }, errs.ErrPermissionDenied},
		{"empty decoded keystore", func(in *appbuild.AndroidReleaseBuildInput) { in.KeystoreBase64 = " \r\n\t" }, errs.ErrMalformedInput},
		{"malformed keystore", func(in *appbuild.AndroidReleaseBuildInput) { in.KeystoreBase64 = "%%%" }, errs.ErrMalformedInput},
		{"oversized decoded keystore", func(in *appbuild.AndroidReleaseBuildInput) {
			in.KeystoreBase64 = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("k"), (16<<20)+1))
		}, errs.ErrMalformedInput},
		{"oversized encoded keystore", func(in *appbuild.AndroidReleaseBuildInput) {
			in.KeystoreBase64 = strings.Repeat(" ", (32<<20)+1)
		}, errs.ErrMalformedInput},
		{"empty decoded properties", func(in *appbuild.AndroidReleaseBuildInput) { in.SecretsPropertiesBase64 = " \r\n\t" }, errs.ErrMalformedInput},
		{"malformed properties", func(in *appbuild.AndroidReleaseBuildInput) { in.SecretsPropertiesBase64 = "%%%" }, errs.ErrMalformedInput},
		{"oversized decoded properties", func(in *appbuild.AndroidReleaseBuildInput) {
			in.SecretsPropertiesBase64 = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("p"), (16<<20)+1))
		}, errs.ErrMalformedInput},
		{"oversized encoded properties", func(in *appbuild.AndroidReleaseBuildInput) {
			in.SecretsPropertiesBase64 = strings.Repeat(" ", (32<<20)+1)
		}, errs.ErrMalformedInput},
		{"unsigned malformed properties", func(in *appbuild.AndroidReleaseBuildInput) {
			in.EnableSigning = false
			in.SecretsPropertiesBase64 = "%%%"
		}, errs.ErrMalformedInput},
		{"missing build types", func(in *appbuild.AndroidReleaseBuildInput) { in.BuildTypes = "" }, errs.ErrValidation},
		{"invalid build types", func(in *appbuild.AndroidReleaseBuildInput) { in.BuildTypes = "notrelease" }, errs.ErrValidation},
		{"blank task override", func(in *appbuild.AndroidReleaseBuildInput) {
			in.GradleTasksOverride, in.BuildTypes = " \t\n", ""
		}, errs.ErrValidation},
		{"missing SBOM version", func(in *appbuild.AndroidReleaseBuildInput) { in.SBOMToolVersion = "" }, errs.ErrUsage},
		{"blank SBOM version", func(in *appbuild.AndroidReleaseBuildInput) { in.SBOMToolVersion = " \t\n" }, errs.ErrUsage},
	} {
		for _, destination := range []string{"fresh", "seeded"} {
			t.Run(tc.name+"/"+destination, func(t *testing.T) {
				fsys := testfs.NewReal(t)
				t.Setenv("TMPDIR", fsys.MkdirAll("fallback"))
				dir, scratch := fsys.MkdirAll("project"), fsys.MkdirAll("scratch")
				fsys.WriteFile("project/gradlew", []byte("recording fake only; never execute\n"))
				fsys.WriteFile("project/gradle.properties", []byte("versionName=1.2.3\nversionCode=42\n"))
				fsys.WriteFile("scratch/unrelated", []byte("preserve unrelated scratch bytes"))

				if destination == "seeded" {
					fsys.WriteFile("project/secrets.properties", []byte("preserve project bytes"))
					fsys.WriteFile("scratch/release.keystore", []byte("preserve scratch bytes"))
				}
				// These are synthetic canaries, never inherited host credentials.
				for _, key := range []string{"ANDROID_KEYSTORE_PATH", "ANDROID_KEYSTORE_PASSWORD", "ANDROID_KEY_ALIAS", "ANDROID_KEY_PASSWORD"} {
					t.Setenv(key, fsys.Path("synthetic-"+key))
				}

				beforeEnv, beforeTree := os.Environ(), ownedTree(t, fsys.Root)
				in := appbuild.AndroidReleaseBuildInput{
					ReleaseBuildOptions:     appbuild.ReleaseBuildOptions{Dir: dir, EnableBuildSBOM: true},
					RepoName:                "test-app",
					EnableSigning:           true,
					KeystoreBase64:          base64.StdEncoding.EncodeToString([]byte("synthetic keystore")),
					KeystorePassword:        "synthetic store password",
					KeyAlias:                "synthetic alias",
					KeyPassword:             "synthetic key password",
					SecretsPropertiesBase64: base64.StdEncoding.EncodeToString([]byte("synthetic properties")),
					TempDir:                 scratch,
					BuildTypes:              "release",
					BuildModule:             "app",
					SBOMToolVersion:         "2.3.4",
				}
				tc.change(&in)

				sink := fakeoutputsink.New(t)
				if err := sink.Set(t.Context(), "prior-output", "preserve output"); err != nil {
					t.Fatal(err)
				}

				beforeOutputs := sink.AllScalar()
				ops, summary := &recordingGradle{}, &recordingSummarySink{}

				var stdout, stderr bytes.Buffer

				err := appbuild.AndroidReleaseBuild(t.Context(), sink, summary, ops, output.NewAnnotator(&stderr, output.FormatGitHub), &stdout, &stderr, in)
				if !errors.Is(err, tc.want) {
					t.Errorf("error = %v, want %v", err, tc.want)
				}

				if !maps.Equal(sink.AllScalar(), beforeOutputs) || sink.CloseCount() != 0 || summary.buf.Len() != 0 || stdout.Len() != 0 || stderr.Len() != 0 || len(ops.calls) != 0 {
					t.Error("preflight refusal published output, logged, summarized, or called Gradle")
				}

				if !reflect.DeepEqual(beforeTree, ownedTree(t, fsys.Root)) {
					t.Error("preflight refusal changed fixture paths, bytes, or modes")
				}

				if !reflect.DeepEqual(beforeEnv, os.Environ()) {
					t.Error("preflight refusal changed process environment")
				}
			})
		}
	}
}

type androidPreflightStopSink struct {
	*fakeoutputsink.Sink
	calls int
	err   error
}

func (s *androidPreflightStopSink) Set(context.Context, string, string) error {
	s.calls++

	return s.err
}

// Valid blobs must pass preflight. The first-output stop keeps these blob-limit
// controls independent of the signed lifecycle tests.
func TestAndroidReleaseBuild_PreflightAcceptsBoundedBlobs(t *testing.T) {
	for _, name := range []string{"one byte", "wrapped base64", "16 MiB", "optional properties absent", "unsigned ignores keystore", "task override", "SBOM disabled"} {
		t.Run(name, func(t *testing.T) {
			fsys := testfs.NewReal(t)
			t.Setenv("TMPDIR", fsys.MkdirAll("fallback"))
			fsys.WriteFile("project/gradlew", []byte("inert wrapper"))
			in := appbuild.AndroidReleaseBuildInput{
				ReleaseBuildOptions:     appbuild.ReleaseBuildOptions{Dir: fsys.MkdirAll("project"), EnableBuildSBOM: true},
				RepoName:                "test-app",
				EnableSigning:           true,
				KeystoreBase64:          "aw==",
				KeystorePassword:        "synthetic store password",
				KeyAlias:                "synthetic alias",
				KeyPassword:             "synthetic key password",
				SecretsPropertiesBase64: "cA==",
				TempDir:                 fsys.MkdirAll("scratch"),
				BuildTypes:              "release",
				SBOMToolVersion:         "2.3.4",
			}

			switch name {
			case "wrapped base64":
				in.KeystoreBase64, in.SecretsPropertiesBase64 = " \tY W\r\nJ j \n", " \tZ G\r\nV m \n"
			case "16 MiB":
				in.KeystoreBase64 = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("k"), 16<<20))
				in.SecretsPropertiesBase64 = base64.StdEncoding.EncodeToString(bytes.Repeat([]byte("p"), 16<<20))
			case "optional properties absent":
				in.SecretsPropertiesBase64 = ""
			case "unsigned ignores keystore":
				in.EnableSigning, in.KeystoreBase64 = false, "%%%"
			case "task override":
				in.BuildTypes, in.GradleTasksOverride = "notrelease", "customTask"
			case "SBOM disabled":
				in.EnableBuildSBOM, in.SBOMToolVersion = false, ""
			}

			before := ownedTree(t, fsys.Root)
			cause := errors.New("stop before signing effects") //nolint:err113 // unique recording sink sentinel.
			sink := &androidPreflightStopSink{Sink: fakeoutputsink.New(t), err: cause}
			ops, summary := &recordingGradle{}, &recordingSummarySink{}

			var stdout, stderr bytes.Buffer

			err := appbuild.AndroidReleaseBuild(t.Context(), sink, summary, ops, output.NewAnnotator(&stderr, output.FormatGitHub), &stdout, &stderr, in)
			if !errors.Is(err, cause) || sink.calls != 1 {
				t.Fatalf("valid inputs did not reach first output: error=%v calls=%d", err, sink.calls)
			}

			if len(ops.calls) != 0 || summary.buf.Len() != 0 || stdout.Len() != 0 || stderr.Len() != 0 || !reflect.DeepEqual(before, ownedTree(t, fsys.Root)) {
				t.Fatal("first-output failure allowed later effects")
			}
		})
	}
}
