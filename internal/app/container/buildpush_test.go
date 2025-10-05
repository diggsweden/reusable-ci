// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	domaincontainer "github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

type fakeBuildPushTool struct {
	builds []domaincontainer.BuildPushManifestBuildRequest
	pushes []string
	// events interleaves builds and pushes in call order, which the two
	// per-kind slices cannot show.
	events    []string
	pushFails int
	digest    string
}

func (f *fakeBuildPushTool) BuildManifest(_ context.Context, req domaincontainer.BuildPushManifestBuildRequest, _ io.Writer) error {
	f.builds = append(f.builds, req)
	f.events = append(f.events, "build:"+req.Platform)

	return nil
}

func (f *fakeBuildPushTool) PushManifestWithDigest(_ context.Context, authFile string, tlsVerify bool, manifest string, _ io.Writer) (string, error) {
	f.pushes = append(f.pushes, authFile+"|"+manifest+"|"+boolString(tlsVerify))
	f.events = append(f.events, "push")

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

	tool := &fakeBuildPushTool{pushFails: 1, digest: oneDigest}
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
		if got.Image != image || got.Digest != oneDigest {
			t.Errorf("result = %+v, want image %s at %s", got, image, oneDigest)
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

	t.Run("finishes every platform build before the first push", func(t *testing.T) {
		// A manifest pushed before its last platform is built publishes an
		// image that is missing that platform.
		want := []string{"build:linux/amd64", "build:linux/arm64", "push", "push"}
		if !slices.Equal(tool.events, want) {
			t.Errorf("events = %v, want %v", tool.events, want)
		}
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

		if !strings.Contains(log.String(), "Pushed "+manifest+"@"+oneDigest) {
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

		if !slices.Equal(build.BuildArgs, w.args) {
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
		slices.Sort(labels)

		if !slices.Equal(labels, wantLabels) {
			t.Errorf("build[%d] labels =\n%v\nwant\n%v", i, labels, wantLabels)
		}
	}
}

// TestBuildPushOCIImage_RejectsInvalidInputBeforeBuild covers every way the
// inputs can be refused, and requires that none of them reached the builder or
// the registry.
//
// Each row changes one field of an otherwise valid input. The rows used to
// select their change through a map of string keys against a switch that
// ignored anything it did not recognise, so a mistyped key silently produced
// the valid input and the row passed for the wrong reason -- and only three
// fields were reachable at all, which is why half the validation had no row.
func TestBuildPushOCIImage_RejectsInvalidInputBeforeBuild(t *testing.T) {
	t.Chdir(t.TempDir())
	writeBuildPushFile(t, "Containerfile", "FROM scratch\n")
	writeBuildPushFile(t, "auth.json", `{"auths":{}}`)

	for name, testCase := range map[string]struct {
		mutate func(*appcontainer.BuildPushOCIImageInput)
		want   error
	}{
		"no builds": {
			mutate: func(in *appcontainer.BuildPushOCIImageInput) { in.BuildsJSON = "[]" },
			want:   errs.ErrUsage,
		},
		"platform is not one that is built": {
			mutate: func(in *appcontainer.BuildPushOCIImageInput) { in.BuildsJSON = `[{"platform":"windows/amd64"}]` },
			want:   errs.ErrUsage,
		},
		"containerfile is missing": {
			mutate: func(in *appcontainer.BuildPushOCIImageInput) { in.Containerfile = "does/not/exist" },
			want:   errs.ErrMissingInput,
		},
		"containerfile is a directory": {
			mutate: func(in *appcontainer.BuildPushOCIImageInput) { in.Containerfile = "." },
			want:   errs.ErrMissingInput,
		},
		"no containerfile named": {
			mutate: func(in *appcontainer.BuildPushOCIImageInput) { in.Containerfile = "" },
			want:   errs.ErrUsage,
		},
		"tls-verify is neither true nor false": {
			mutate: func(in *appcontainer.BuildPushOCIImageInput) { in.TLSVerify = "maybe" },
			want:   errs.ErrUsage,
		},
		"no tag": {
			mutate: func(in *appcontainer.BuildPushOCIImageInput) { in.Tag = "" },
			want:   errs.ErrUsage,
		},
		"tag contains a space": {
			mutate: func(in *appcontainer.BuildPushOCIImageInput) { in.Tag = "v1 0" },
			want:   errs.ErrUsage,
		},
		"auth file is named but missing": {
			mutate: func(in *appcontainer.BuildPushOCIImageInput) { in.AuthFile = "no-such-auth.json" },
			want:   errs.ErrMissingInput,
		},
		"no repository": {
			mutate: func(in *appcontainer.BuildPushOCIImageInput) { in.Repository = "" },
			want:   errs.ErrUsage,
		},
		"no server url": {
			mutate: func(in *appcontainer.BuildPushOCIImageInput) { in.ServerURL = "" },
			want:   errs.ErrUsage,
		},
	} {
		t.Run(name, func(t *testing.T) {
			tool := &fakeBuildPushTool{}

			in := validBuildPushInput()
			testCase.mutate(&in)

			sink := fakeoutputsink.New(t)

			_, err := appcontainer.BuildPushOCIImage(context.Background(), tool, fakeBuildPushGit{revision: "abc", epoch: "1700000000"}, sink, io.Discard, in)
			if !errors.Is(err, testCase.want) {
				t.Errorf("err = %v, want %v", err, testCase.want)
			}

			// Refusing after a build would mean a layer had already been
			// produced, and after a push that an image was already published.
			if len(tool.events) != 0 || len(sink.Keys()) != 0 {
				t.Errorf("state touched despite invalid input: tool events=%v outputs=%v", tool.events, sink.Keys())
			}
		})
	}
}

// validBuildPushInput is the input every rejection row starts from: valid, so
// that whatever a row changes is the only reason it is refused.
func validBuildPushInput() appcontainer.BuildPushOCIImageInput {
	return appcontainer.BuildPushOCIImageInput{
		Tag:           "v0.0.0",
		Containerfile: "Containerfile",
		BuildsJSON:    `[{"platform":"linux/amd64"}]`,
		OCILabels:     domaincontainer.OCILabels{Title: "selftest"},
		TLSVerify:     "true",
		ServerURL:     "https://codeberg.org",
		Repository:    "itiquette/forgejo-ci",
	}
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

// TestBuildPushOCIImage_PreservesBuildArgumentOrderPerPlatform pins the order
// of the arguments each platform is built with.
//
// The suite's fixture gives each platform a single build-arg, so the existing
// slices.Equal comparison cannot observe order at all: reversing, sorting or
// deduplicating the list would satisfy it. Order is not cosmetic for build
// arguments — a repeated key is resolved last-one-wins by the builder, so
// reordering changes the value the image is built with, and deduplicating
// changes which of the two survives.
//
// The fixture below gives each platform four arguments, including a repeated
// key whose two values differ, and asserts the exact per-platform sequence.
func TestBuildPushOCIImage_PreservesBuildArgumentOrderPerPlatform(t *testing.T) {
	// Chdir first: writeBuildPushFile writes relative paths, and without an
	// owned working directory the fixtures land in the package source tree.
	t.Chdir(t.TempDir())
	writeBuildPushFile(t, "Containerfile", "FROM scratch\n")
	writeBuildPushFile(t, "auth.json", `{"auths":{}}`)

	tool := &fakeBuildPushTool{digest: oneDigest}

	amd64Args := []string{"BASE=alpine:3.20", "ARCH=amd64", "FEATURE=off", "FEATURE=on"}
	arm64Args := []string{"BASE=alpine:3.20", "ARCH=arm64", "FEATURE=on", "FEATURE=off"}

	buildsJSON := `[` +
		`{"platform":"linux/amd64","build-args":["BASE=alpine:3.20","ARCH=amd64","FEATURE=off","FEATURE=on"]},` +
		`{"platform":"linux/arm64","build-args":["BASE=alpine:3.20","ARCH=arm64","FEATURE=on","FEATURE=off"]}` +
		`]`

	_, err := appcontainer.BuildPushOCIImage(context.Background(), tool, fakeBuildPushGit{
		revision: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		epoch:    "1700000000",
	}, fakeoutputsink.New(t), io.Discard, appcontainer.BuildPushOCIImageInput{
		Tag:           "v1.2.3-alpine",
		Containerfile: "Containerfile",
		BuildsJSON:    buildsJSON,
		OCILabels: domaincontainer.OCILabels{
			Title: "Forgejo CI", Description: "Reusable workflows",
			Licenses: "CC0-1.0", Vendor: "Itiquette", Authors: "The Itiquette Authors",
		},
		AuthFile:      "auth.json",
		TLSVerify:     "true",
		ServerURL:     "https://codeberg.org",
		Repository:    "Itiquette/Forgejo-CI",
		RetryAttempts: 1,
		RetryDelay:    time.Nanosecond,
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(tool.builds) != 2 {
		t.Fatalf("builds = %d, want one per platform", len(tool.builds))
	}

	for i, want := range [][]string{amd64Args, arm64Args} {
		if !slices.Equal(tool.builds[i].BuildArgs, want) {
			t.Errorf("build[%d] (%s) args =\n  %v\nwant\n  %v",
				i, tool.builds[i].Platform, tool.builds[i].BuildArgs, want)
		}
	}

	// The two platforms differ only in the order of the repeated key, so a
	// builder that sorted or deduplicated would make them identical — which
	// is exactly the collapse this guards against.
	if slices.Equal(tool.builds[0].BuildArgs, tool.builds[1].BuildArgs) {
		t.Error("both platforms were built with the same argument list; per-platform order was lost")
	}
}
