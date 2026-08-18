// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package container_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	appcontainer "github.com/diggsweden/reusable-ci/v3/internal/app/container"
	domaincontainer "github.com/diggsweden/reusable-ci/v3/internal/domain/container"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
)

type fakeSignerImageTool struct {
	buildReqs []domaincontainer.SignerImageBuildToolRequest
	pushes    []string
	pushFails int
	raw       []byte

	removedManifest string
	createdManifest string
	adds            []domaincontainer.SignerImageManifestAddToolRequest
	manifestPushes  []string
}

func (f *fakeSignerImageTool) BuildSignerImage(_ context.Context, req domaincontainer.SignerImageBuildToolRequest, _ io.Writer) error {
	f.buildReqs = append(f.buildReqs, req)

	return nil
}

func (f *fakeSignerImageTool) PushImage(_ context.Context, _ string, _ string, dest string, _ io.Writer) error {
	f.pushes = append(f.pushes, dest)
	if f.pushFails > 0 {
		f.pushFails--

		return errors.New("temporary push failure") //nolint:err113 // test double error.
	}

	return nil
}

func (f *fakeSignerImageTool) RawManifest(_ context.Context, _ string, image string, _ io.Writer) ([]byte, error) {
	if f.raw != nil {
		return f.raw, nil
	}

	return []byte("raw manifest for " + image), nil
}

func (f *fakeSignerImageTool) RemoveManifest(_ context.Context, localManifest string, _ io.Writer) error {
	f.removedManifest = localManifest

	return nil
}

func (f *fakeSignerImageTool) CreateManifest(_ context.Context, localManifest string, _ io.Writer) error {
	f.createdManifest = localManifest

	return nil
}

func (f *fakeSignerImageTool) AddManifest(_ context.Context, req domaincontainer.SignerImageManifestAddToolRequest, _ io.Writer) error {
	f.adds = append(f.adds, req)

	return nil
}

func (f *fakeSignerImageTool) PushManifest(_ context.Context, _ string, localManifest, dest string, _ io.Writer) error {
	f.manifestPushes = append(f.manifestPushes, localManifest+" -> "+dest)

	return nil
}

// TestBuildSignerImageArch_NamesTheImageByTheDigestItPushed covers one
// architecture of the signer image: it is built, pushed with a retry, and
// described by metadata that is both returned and written to disk.
//
// The digest is asserted against a value computed outside this package. It was
// only ever checked against itself -- ref was compared to repo + "@" + digest,
// which holds just as well when the digest is empty -- and the digest is what
// identifies the image everything downstream signs and verifies.
func TestBuildSignerImageArch_NamesTheImageByTheDigestItPushed(t *testing.T) {
	t.Chdir(t.TempDir())
	writeAuthFileForSignerImage(t, "auth.json")

	// The digest is the SHA-256 of the raw manifest the registry returns.
	const (
		rawManifest = "arch manifest"
		wantDigest  = "sha256:2a0f79b4846ba2d6951708ea5defc8ac29b6d1d5afd44cc88ac251840149b46d"
		wantRepo    = "codeberg.org/itiquette/forgejo-ci-signer"
		sourceSHA   = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	)

	tool := &fakeSignerImageTool{pushFails: 1, raw: []byte(rawManifest)}

	var out bytes.Buffer

	meta, err := appcontainer.BuildSignerImageArch(context.Background(), tool, &out, appcontainer.SignerImageBuildArchInput{
		AuthFile:         "auth.json",
		Arch:             "amd64",
		SourceSHA:        sourceSHA,
		ServerURL:        "https://codeberg.org",
		Repository:       "Itiquette/Forgejo-CI",
		RepositorySuffix: "-signer",
		TagPrefix:        "signer-",
		Name:             "signer-image",
		Title:            "forgejo-ci signer",
		RetryAttempts:    2,
		RetryDelay:       time.Nanosecond,
	})
	if err != nil {
		t.Fatal(err)
	}

	wantTag := wantRepo + ":signer-" + sourceSHA + "-amd64"
	if meta.Tag != wantTag {
		t.Errorf("tag = %q, want %q", meta.Tag, wantTag)
	}

	if meta.Digest != wantDigest {
		t.Errorf("digest = %q, want the manifest's SHA-256 %q", meta.Digest, wantDigest)
	}

	if want := wantRepo + "@" + wantDigest; meta.Ref != want {
		t.Errorf("ref = %q, want %q", meta.Ref, want)
	}

	// The retry pushes the same tag again rather than moving on.
	if !reflect.DeepEqual(tool.pushes, []string{wantTag, wantTag}) {
		t.Errorf("pushes = %v, want %q twice", tool.pushes, wantTag)
	}

	if !strings.Contains(out.String(), "attempt 1/2") {
		t.Errorf("retry message missing from output: %q", out.String())
	}

	if len(tool.buildReqs) != 1 {
		t.Fatalf("build requests = %+v, want exactly one", tool.buildReqs)
	}

	build := tool.buildReqs[0]
	if build.Platform != "linux/amd64" {
		t.Errorf("built platform = %q, want linux/amd64", build.Platform)
	}

	if build.SourceURL != "https://codeberg.org/Itiquette/Forgejo-CI" {
		t.Errorf("source URL = %q", build.SourceURL)
	}

	if build.Title != "forgejo-ci signer" {
		t.Errorf("title = %q", build.Title)
	}

	// What is written to disk is what the next job reads; the returned value
	// never leaves this process.
	var disk appcontainer.SignerImageArchMetadata
	readJSONForSignerImage(t, "signer-image-arch-amd64/signer-image-amd64.json", &disk)

	if disk != *meta {
		t.Errorf("disk metadata = %+v, want %+v", disk, *meta)
	}
}

func TestAssembleSignerImageManifest_WritesOutputsAndMetadata(t *testing.T) {
	t.Chdir(t.TempDir())
	writeAuthFileForSignerImage(t, "auth.json")
	writeSignerArchMetadata(t, "amd64", "codeberg.org/itiquette/forgejo-ci-signer", strings.Repeat("1", 64))
	writeSignerArchMetadata(t, "arm64", "codeberg.org/itiquette/forgejo-ci-signer", strings.Repeat("2", 64))

	tool := &fakeSignerImageTool{raw: []byte("index manifest")}
	sink := fakeoutputsink.New(t)

	meta, err := appcontainer.AssembleSignerImageManifest(context.Background(), tool, sink, nil, io.Discard, appcontainer.SignerImageAssembleInput{
		AuthFile:         "auth.json",
		SourceSHA:        strings.Repeat("b", 40),
		ServerURL:        "https://codeberg.org",
		Repository:       "itiquette/forgejo-ci",
		RepositorySuffix: "-signer",
		TagPrefix:        "signer-",
		Name:             "signer-image",
	})
	if err != nil {
		t.Fatal(err)
	}

	wantManifest := "localhost/signer-image:manifest-" + strings.Repeat("b", 40)
	if tool.removedManifest != wantManifest || tool.createdManifest != wantManifest {
		t.Fatalf("manifest lifecycle remove=%q create=%q", tool.removedManifest, tool.createdManifest)
	}

	if len(tool.adds) != 2 || tool.adds[0].Arch != "amd64" || tool.adds[1].Arch != "arm64" {
		t.Fatalf("manifest adds = %+v", tool.adds)
	}

	if len(tool.manifestPushes) != 1 || !strings.Contains(tool.manifestPushes[0], ":signer-"+strings.Repeat("b", 40)) {
		t.Fatalf("manifest pushes = %v", tool.manifestPushes)
	}

	if sink.Single("image-ref") != meta.Ref || sink.Single("image-digest") != meta.Digest || sink.Single("image-tag") != meta.Tag {
		t.Fatalf("outputs ref=%q digest=%q tag=%q meta=%+v", sink.Single("image-ref"), sink.Single("image-digest"), sink.Single("image-tag"), meta)
	}

	var disk appcontainer.SignerImageMetadata
	readJSONForSignerImage(t, "signer-image-dist/signer-image.json", &disk)

	if disk != *meta {
		t.Fatalf("disk metadata = %+v want %+v", disk, *meta)
	}
}

func TestAssembleSignerImageManifest_RejectsWrongRepositoryRef(t *testing.T) {
	t.Chdir(t.TempDir())
	writeAuthFileForSignerImage(t, "auth.json")
	writeSignerArchMetadata(t, "amd64", "evil.example/itiquette/forgejo-ci-signer", strings.Repeat("1", 64))

	_, err := appcontainer.AssembleSignerImageManifest(context.Background(), &fakeSignerImageTool{}, fakeoutputsink.New(t), nil, io.Discard, appcontainer.SignerImageAssembleInput{
		AuthFile:         "auth.json",
		SourceSHA:        strings.Repeat("b", 40),
		ServerURL:        "https://codeberg.org",
		Repository:       "itiquette/forgejo-ci",
		RepositorySuffix: "-signer",
		TagPrefix:        "signer-",
		Name:             "signer-image",
		Archs:            []string{"amd64"},
	})
	if !errors.Is(err, errs.ErrValidation) || !strings.Contains(err.Error(), "must be under codeberg.org/itiquette/forgejo-ci-signer") {
		t.Fatalf("err = %v", err)
	}
}

func writeAuthFileForSignerImage(t *testing.T, path string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(`{"auths":{}}`), 0o600); err != nil { //nolint:gosec,mnd // test fixture.
		t.Fatal(err)
	}
}

func writeSignerArchMetadata(t *testing.T, arch, repo, digestHex string) {
	t.Helper()

	dir := "signer-image-arch-" + arch
	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec,mnd // test fixture.
		t.Fatal(err)
	}

	body, err := json.Marshal(appcontainer.SignerImageArchMetadata{
		Arch:   arch,
		Tag:    repo + ":signer-test-" + arch,
		Digest: "sha256:" + digestHex,
		Ref:    repo + "@sha256:" + digestHex,
	})
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "signer-image-"+arch+".json"), body, 0o644); err != nil { //nolint:gosec,mnd // test fixture.
		t.Fatal(err)
	}
}

func readJSONForSignerImage(t *testing.T, path string, out any) {
	t.Helper()

	body, err := os.ReadFile(path) //nolint:gosec // test fixture.
	if err != nil {
		t.Fatal(err)
	}

	if err := json.Unmarshal(body, out); err != nil {
		t.Fatal(err)
	}
}
