// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
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

func TestBuildPushOCIImage_ThreadsManifestBuildsLabelsOutputsAndRetries(t *testing.T) {
	t.Chdir(t.TempDir())
	writeBuildPushFile(t, "Containerfile", "FROM scratch\n")
	writeBuildPushFile(t, "auth.json", `{"auths":{}}`)

	tool := &fakeBuildPushTool{pushFails: 1, digest: "sha256:deadbeef"}
	sink := fakeoutputsink.New(t)

	var log bytes.Buffer

	got, err := appcontainer.BuildPushOCIImage(context.Background(), tool, fakeBuildPushGit{
		revision: strings.Repeat("a", 40),
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

	if got.Image != "codeberg.org/itiquette/forgejo-ci" || got.Digest != "sha256:deadbeef" {
		t.Fatalf("output = %+v", got)
	}

	if sink.Single("image") != got.Image || sink.Single("digest") != got.Digest {
		t.Fatalf("sink image=%q digest=%q", sink.Single("image"), sink.Single("digest"))
	}

	if len(tool.pushes) != 2 {
		t.Fatalf("pushes = %v, want retry", tool.pushes)
	}

	if want := "auth.json|codeberg.org/itiquette/forgejo-ci:v1.2.3-alpine|true"; tool.pushes[0] != want {
		t.Fatalf("push = %q, want %q", tool.pushes[0], want)
	}

	if len(tool.builds) != 2 {
		t.Fatalf("builds = %d, want 2", len(tool.builds))
	}

	first := tool.builds[0]
	if first.Manifest != "codeberg.org/itiquette/forgejo-ci:v1.2.3-alpine" || first.Platform != "linux/amd64" || first.SourceDateEpoch != "1700000000" || !first.TLSVerify {
		t.Fatalf("first build request = %+v", first)
	}

	if len(first.BuildArgs) != 1 || first.BuildArgs[0] != "ARCH=amd64" {
		t.Fatalf("build args = %v", first.BuildArgs)
	}

	for _, want := range []string{
		"org.opencontainers.image.title=Forgejo CI",
		"org.opencontainers.image.version=v1.2.3-alpine",
		"org.opencontainers.image.created=2023-11-14T22:13:20Z",
		"org.opencontainers.image.revision=" + strings.Repeat("a", 40),
		"org.opencontainers.image.ref.name=v1.2.3-alpine",
		"org.opencontainers.image.source=https://codeberg.org/Itiquette/Forgejo-CI",
		"org.opencontainers.image.url=https://codeberg.org/Itiquette/Forgejo-CI",
		"org.opencontainers.image.documentation=https://codeberg.org/Itiquette/Forgejo-CI#readme",
		"org.opencontainers.image.description=Reusable workflows",
		"org.opencontainers.image.licenses=CC0-1.0",
		"org.opencontainers.image.vendor=Itiquette",
		"org.opencontainers.image.authors=The Itiquette Authors",
	} {
		if !containsString(first.Labels, want) {
			t.Fatalf("labels missing %q in %v", want, first.Labels)
		}
	}

	if !strings.Contains(log.String(), "attempt 1/2") || !strings.Contains(log.String(), "Pushed codeberg.org/itiquette/forgejo-ci:v1.2.3-alpine@sha256:deadbeef") {
		t.Fatalf("log missing retry/push messages: %q", log.String())
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

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}

	return false
}

func boolString(value bool) string {
	if value {
		return "true"
	}

	return "false"
}
