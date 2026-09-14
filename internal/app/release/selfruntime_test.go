// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package release_test

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	apprelease "github.com/diggsweden/reusable-ci/v3/internal/app/release"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
	domainrelease "github.com/diggsweden/reusable-ci/v3/internal/domain/release"
)

type selfRuntimePublishCall struct {
	repository string
	sourceRef  string
	targetSHA  string
	body       string
	spec       provider.ReleaseSpec
}

type fakeSelfRuntimePublisher struct {
	calls []selfRuntimePublishCall
}

func (f *fakeSelfRuntimePublisher) PublishSelfRuntimeCLI(_ context.Context, repository, sourceRef, targetSHA, body string, spec provider.ReleaseSpec) error {
	f.calls = append(f.calls, selfRuntimePublishCall{repository: repository, sourceRef: sourceRef, targetSHA: targetSHA, body: body, spec: spec})

	return nil
}

func TestPublishSelfRuntimeCLI_SelectsExactAssetsAndForcesPrereleasePolicy(t *testing.T) {
	t.Chdir(t.TempDir())
	writeSelfRuntimeAssets(t, "cli-dist", "3.0.0-pre", "")
	mustWrite(t, "cli-dist/metadata.json", "internal\n")
	mustWrite(t, "cli-dist/unrelated.tar.gz", "internal\n")

	publisher := &fakeSelfRuntimePublisher{}
	sha := strings.Repeat("a", 40)

	var out bytes.Buffer

	err := apprelease.PublishSelfRuntimeCLI(context.Background(), publisher, &out, apprelease.PublishSelfRuntimeCLIInput{
		Repository: domainrelease.SelfRuntimeRepository,
		SourceRef:  "refs/heads/main",
		TargetSHA:  sha,
		AssetsDir:  "cli-dist",
	})
	if err != nil {
		t.Fatal(err)
	}

	if len(publisher.calls) != 1 {
		t.Fatalf("publisher calls = %d", len(publisher.calls))
	}

	call := publisher.calls[0]
	if call.repository != domainrelease.SelfRuntimeRepository || call.sourceRef != "refs/heads/main" || call.targetSHA != sha {
		t.Fatalf("publication identity = %q from %q at %q", call.repository, call.sourceRef, call.targetSHA)
	}

	if call.spec.Tag != domainrelease.SelfRuntimeChannelTag || call.spec.Draft || !call.spec.Prerelease || call.spec.MakeLatest != provider.MakeLatestFalse {
		t.Fatalf("release policy = %+v", call.spec)
	}

	if call.body == "" {
		t.Fatal("rolling release body is empty")
	}

	basenames := make([]string, 0, len(call.spec.Assets))
	for _, asset := range call.spec.Assets {
		basenames = append(basenames, filepath.Base(asset))
	}

	slices.Sort(basenames)

	// The exact set, which is what the name claims. A count plus a
	// membership sweep left room for a file nobody listed either way --
	// and these are published, signed release assets.
	want := []string{
		"checksums.txt",
		"checksums.txt.bundle",
		"reusable-ci.intoto.json",
		"reusable-ci.intoto.jsonl",
		"reusable-ci_3.0.0-pre_darwin_amd64.tar.gz",
		"reusable-ci_3.0.0-pre_darwin_amd64.tar.gz.cdx.sbom.json",
		"reusable-ci_3.0.0-pre_darwin_arm64.tar.gz",
		"reusable-ci_3.0.0-pre_darwin_arm64.tar.gz.cdx.sbom.json",
		"reusable-ci_3.0.0-pre_linux_amd64.tar.gz",
		"reusable-ci_3.0.0-pre_linux_amd64.tar.gz.cdx.sbom.json",
		"reusable-ci_3.0.0-pre_linux_arm64.tar.gz",
		"reusable-ci_3.0.0-pre_linux_arm64.tar.gz.cdx.sbom.json",
	}
	if !slices.Equal(basenames, want) {
		t.Fatalf("selected assets =\n%v\nwant\n%v", basenames, want)
	}

	if !strings.Contains(out.String(), "12 assets") {
		t.Fatalf("output = %q", out.String())
	}
}

func TestPublishSelfRuntimeCLI_RejectsWrongRepositoryOrSHABeforeMutation(t *testing.T) {
	sha := strings.Repeat("a", 40)
	tests := map[string]apprelease.PublishSelfRuntimeCLIInput{
		"wrong repository": {
			Repository: "other/repo", SourceRef: "refs/heads/main", TargetSHA: sha, AssetsDir: "cli-dist",
		},
		"short SHA": {
			Repository: domainrelease.SelfRuntimeRepository, SourceRef: "refs/heads/main", TargetSHA: "abc123", AssetsDir: "cli-dist",
		},
		"untrusted source ref": {
			Repository: domainrelease.SelfRuntimeRepository, SourceRef: "refs/heads/untrusted", TargetSHA: sha, AssetsDir: "cli-dist",
		},
	}

	for name, in := range tests {
		t.Run(name, func(t *testing.T) {
			publisher := &fakeSelfRuntimePublisher{}

			err := apprelease.PublishSelfRuntimeCLI(context.Background(), publisher, &bytes.Buffer{}, in)
			if !errors.Is(err, errs.ErrValidation) {
				t.Fatalf("err = %v, want validation", err)
			}

			if len(publisher.calls) != 0 {
				t.Fatalf("publisher called despite invalid identity: %+v", publisher.calls)
			}
		})
	}
}

func TestPublishSelfRuntimeCLI_RequiresEveryExpectedAssetBeforeMutation(t *testing.T) {
	t.Chdir(t.TempDir())
	writeSelfRuntimeAssets(t, "cli-dist", "3.0.0-pre", "checksums.txt.bundle")

	publisher := &fakeSelfRuntimePublisher{}

	err := apprelease.PublishSelfRuntimeCLI(context.Background(), publisher, &bytes.Buffer{}, apprelease.PublishSelfRuntimeCLIInput{
		Repository: domainrelease.SelfRuntimeRepository,
		SourceRef:  "refs/heads/main",
		TargetSHA:  strings.Repeat("a", 40),
		AssetsDir:  "cli-dist",
	})
	if !errors.Is(err, errs.ErrMissingInput) || !strings.Contains(err.Error(), "checksums.txt.bundle") {
		t.Fatalf("err = %v, want missing bundle", err)
	}

	if len(publisher.calls) != 0 {
		t.Fatalf("publisher called with incomplete assets: %+v", publisher.calls)
	}
}

func writeSelfRuntimeAssets(t *testing.T, dir, version, omit string) {
	t.Helper()

	names := make([]string, 0, 12)

	for _, goos := range []string{"linux", "darwin"} {
		for _, arch := range []string{"amd64", "arm64"} {
			archive := "reusable-ci_" + version + "_" + goos + "_" + arch + ".tar.gz"
			names = append(names, archive, archive+".cdx.sbom.json")
		}
	}

	names = append(names, "checksums.txt", "checksums.txt.bundle", "reusable-ci.intoto.json", "reusable-ci.intoto.jsonl")

	for _, name := range names {
		if name != omit {
			mustWrite(t, filepath.Join(dir, name), name+"\n")
		}
	}
}
