// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"reflect"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	domaincontainer "github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

type fakeBuildPushTool struct {
	builds    []domaincontainer.BuildPushManifestBuildRequest
	pushes    []string
	pushFails int
	digest    string
}

func (f *fakeBuildPushTool) BuildManifest(_ context.Context, req domaincontainer.BuildPushManifestBuildRequest, _ io.Writer) error {
	f.builds = append(f.builds, req)

	return nil
}

func (f *fakeBuildPushTool) PushManifestWithDigest(_ context.Context, authFile string, tlsVerify bool, manifest string, _ io.Writer) (string, error) {
	f.pushes = append(f.pushes, authFile+"|"+manifest+"|"+boolString(tlsVerify))
	if f.pushFails > 0 {
		f.pushFails--

		return "", errors.New("temporary registry failure") //nolint:err113 // test double error.
	}

	return f.digest, nil
}

type fakeBuildPushGit struct {
	revision string
	epoch    string
}

func (f fakeBuildPushGit) RevParse(_ context.Context, _ string) (string, error) {
	return f.revision, nil
}

func (f fakeBuildPushGit) CommitUnixTime(_ context.Context, _ string) (string, error) {
	return f.epoch, nil
}

// TestBuildPushOCIImage_BuildsEachPlatformThenPushesTheManifest walks one
// multi-platform build through to a pushed manifest, with the first push
// failing so the retry is exercised.
//
// The claims are subtests over the one run. They were a linear sequence of
// t.Fatalf, so the first thing to break hid everything after it -- and one of
// them, that each platform is built with its own arguments, was only ever
// checked for the first platform.
func TestBuildPushOCIImage_BuildsEachPlatformThenPushesTheManifest(t *testing.T) {
	t.Chdir(t.TempDir())
	writeBuildPushFile(t, "Containerfile", "FROM scratch\n")
	writeBuildPushFile(t, "auth.json", `{"auths":{}}`)

	const (
		image    = "codeberg.org/itiquette/forgejo-ci"
		manifest = image + ":v1.2.3-alpine"
		revision = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	)

	tool := &fakeBuildPushTool{pushFails: 1, digest: "sha256:deadbeef"}
	sink := fakeoutputsink.New(t)

	var log bytes.Buffer

	got, err := appcontainer.BuildPushOCIImage(context.Background(), tool, fakeBuildPushGit{
		revision: revision,
		epoch:    "1700000000",
	}, sink, &log, appcontainer.BuildPushOCIImageInput{
		Tag:           "v1.2.3-alpine",
		Containerfile: "Containerfile",
		BuildsJSON:    `[{"platform":"linux/amd64","build-args":["ARCH=amd64"]},{"platform":"linux/arm64","build-args":["ARCH=arm64"]}]`,
		OCILabels: domaincontainer.OCILabels{
			Title:       "Forgejo CI",
			Description: "Reusable workflows",
			Licenses:    "CC0-1.0",
			Vendor:      "Itiquette",
			Authors:     "The Itiquette Authors",
		},
		AuthFile:      "auth.json",
		TLSVerify:     "true",
		ServerURL:     "https://codeberg.org",
		Repository:    "Itiquette/Forgejo-CI",
		RetryAttempts: 2,
		RetryDelay:    time.Nanosecond,
	})
	if err != nil {
		t.Fatal(err)
	}

	t.Run("reports the image and digest, and publishes them as outputs", func(t *testing.T) {
		// The forge coordinates are mixed case; an image reference is not.
		if got.Image != image || got.Digest != "sha256:deadbeef" {
			t.Errorf("result = %+v, want image %s at sha256:deadbeef", got, image)
		}

		if sink.Single("image") != got.Image || sink.Single("digest") != got.Digest {
			t.Errorf("sink image=%q digest=%q, want them to match the result", sink.Single("image"), sink.Single("digest"))
		}
	})

	t.Run("builds once per platform, each with its own arguments", func(t *testing.T) {
		if len(tool.builds) != 2 {
			t.Fatalf("builds = %d, want one per platform", len(tool.builds))
		}

		assertPlatformBuilds(t, tool.builds, manifest)
	})

	t.Run("labels every build with the same OCI metadata", func(t *testing.T) {
		assertOCILabels(t, tool.builds, revision)
	})

	t.Run("retries a failed push and says which attempt", func(t *testing.T) {
		if len(tool.pushes) != 2 {
			t.Fatalf("pushes = %v, want the failure retried once", tool.pushes)
		}

		want := "auth.json|" + manifest + "|true"
		for i, push := range tool.pushes {
			if push != want {
				t.Errorf("push[%d] = %q, want %q", i, push, want)
			}
		}

		if !strings.Contains(log.String(), "attempt 1/2") {
			t.Errorf("log does not name the attempt: %q", log.String())
		}

		if !strings.Contains(log.String(), "Pushed "+manifest+"@sha256:deadbeef") {
			t.Errorf("log does not report the push: %q", log.String())
		}
	})
}

// assertPlatformBuilds checks each platform was built with its own arguments.
// Reading only the first build cannot tell that apart from building the first
// platform twice, which is what this test exists to catch.
func assertPlatformBuilds(t *testing.T, builds []domaincontainer.BuildPushManifestBuildRequest, manifest string) {
	t.Helper()

	want := []struct {
		platform string
		args     []string
	}{
		{platform: "linux/amd64", args: []string{"ARCH=amd64"}},
		{platform: "linux/arm64", args: []string{"ARCH=arm64"}},
	}

	for i, w := range want {
		build := builds[i]
		if build.Platform != w.platform {
			t.Errorf("build[%d] platform = %q, want %q", i, build.Platform, w.platform)
		}

		if !reflect.DeepEqual(build.BuildArgs, w.args) {
			t.Errorf("build[%d] args = %v, want %v", i, build.BuildArgs, w.args)
		}

		if build.Manifest != manifest {
			t.Errorf("build[%d] manifest = %q, want %q", i, build.Manifest, manifest)
		}

		if !build.TLSVerify {
			t.Errorf("build[%d] built without TLS verification", i)
		}

		// Taken from the commit, not the clock, so the same commit rebuilds
		// to the same image.
		if build.SourceDateEpoch != "1700000000" {
			t.Errorf("build[%d] SOURCE_DATE_EPOCH = %q, want the commit time", i, build.SourceDateEpoch)
		}
	}
}

// assertOCILabels requires every build to carry exactly the expected labels. A
// label nobody asked for still ships on the published image.
func assertOCILabels(t *testing.T, builds []domaincontainer.BuildPushManifestBuildRequest, revision string) {
	t.Helper()

	if len(builds) == 0 {
		t.Fatal("no builds recorded")
	}

	wantLabels := []string{
		"org.opencontainers.image.authors=The Itiquette Authors",
		"org.opencontainers.image.created=2023-11-14T22:13:20Z",
		"org.opencontainers.image.description=Reusable workflows",
		"org.opencontainers.image.documentation=https://codeberg.org/Itiquette/Forgejo-CI#readme",
		"org.opencontainers.image.licenses=CC0-1.0",
		"org.opencontainers.image.ref.name=v1.2.3-alpine",
		"org.opencontainers.image.revision=" + revision,
		"org.opencontainers.image.source=https://codeberg.org/Itiquette/Forgejo-CI",
		"org.opencontainers.image.title=Forgejo CI",
		"org.opencontainers.image.url=https://codeberg.org/Itiquette/Forgejo-CI",
		"org.opencontainers.image.vendor=Itiquette",
		"org.opencontainers.image.version=v1.2.3-alpine",
	}

	for i, build := range builds {
		labels := slices.Clone(build.Labels)
		sort.Strings(labels)

		if !reflect.DeepEqual(labels, wantLabels) {
			t.Errorf("build[%d] labels =\n%v\nwant\n%v", i, labels, wantLabels)
		}
	}
}

func TestBuildPushOCIImage_RejectsInvalidInputBeforeBuild(t *testing.T) {
	t.Chdir(t.TempDir())
	writeBuildPushFile(t, "Containerfile", "FROM scratch\n")

	tests := []struct {
		name string
		in   appcontainer.BuildPushOCIImageInput
		want error
	}{
		{
			name: "empty builds",
			in:   validBuildPushInputWith(map[string]string{"builds": "[]"}),
			want: errs.ErrUsage,
		},
		{
			name: "missing containerfile",
			in:   validBuildPushInputWith(map[string]string{"containerfile": "does/not/exist"}),
			want: errs.ErrMissingInput,
		},
		{
			name: "bad tls",
			in:   validBuildPushInputWith(map[string]string{"tls": "maybe"}),
			want: errs.ErrUsage,
		},
		{
			name: "bad platform",
			in:   validBuildPushInputWith(map[string]string{"builds": `[{"platform":"windows/amd64"}]`}),
			want: errs.ErrUsage,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tool := &fakeBuildPushTool{}

			_, err := appcontainer.BuildPushOCIImage(context.Background(), tool, fakeBuildPushGit{revision: "abc", epoch: "1700000000"}, fakeoutputsink.New(t), io.Discard, tt.in)
			if !errors.Is(err, tt.want) {
				t.Fatalf("err = %v, want %v", err, tt.want)
			}

			if len(tool.builds) != 0 || len(tool.pushes) != 0 {
				t.Fatalf("tool invoked despite invalid input: builds=%v pushes=%v", tool.builds, tool.pushes)
			}
		})
	}
}

func validBuildPushInputWith(overrides map[string]string) appcontainer.BuildPushOCIImageInput {
	in := appcontainer.BuildPushOCIImageInput{
		Tag:           "v0.0.0",
		Containerfile: "Containerfile",
		BuildsJSON:    `[{"platform":"linux/amd64"}]`,
		OCILabels:     domaincontainer.OCILabels{Title: "selftest"},
		TLSVerify:     "true",
		ServerURL:     "https://codeberg.org",
		Repository:    "itiquette/forgejo-ci",
	}

	for key, value := range overrides {
		switch key {
		case "builds":
			in.BuildsJSON = value
		case "containerfile":
			in.Containerfile = value
		case "tls":
			in.TLSVerify = value
		}
	}

	return in
}

func writeBuildPushFile(t *testing.T, path, body string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(body), 0o600); err != nil { //nolint:gosec,mnd // test fixture.
		t.Fatal(err)
	}
}

func boolString(value bool) string {
	if value {
		return "true"
	}

	return "false"
}
