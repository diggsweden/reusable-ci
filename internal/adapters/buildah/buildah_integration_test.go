// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

//go:build integration

package buildah_test

import (
	"context"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	"github.com/google/go-containerregistry/pkg/v1/remote"

	"github.com/diggsweden/reusable-ci/v3/internal/adapters/buildah"
	"github.com/diggsweden/reusable-ci/v3/internal/adapters/ociregistry"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/container"
)

// hermeticContext writes a network-free build context: FROM scratch + COPY
// needs no base-image pull and no RUN, so it builds in an isolated vfs store
// without network, root, or user namespaces.
func hermeticContext(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, "Containerfile"),
		[]byte("FROM scratch AS export\nCOPY hello.txt /hello.txt\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("hi\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	return dir
}

// hermeticStore returns isolated buildah global flags (vfs, temp root/runroot)
// and registers a store reset so t.TempDir cleanup isn't blocked by buildah's
// rootless layer files.
func hermeticStore(t *testing.T) []string {
	t.Helper()
	store := t.TempDir()
	global := []string{
		"--storage-driver", "vfs",
		"--root", filepath.Join(store, "root"),
		"--runroot", filepath.Join(store, "runroot"),
	}
	t.Setenv("BUILDAH_ISOLATION", "chroot")
	t.Cleanup(func() {
		_ = exec.Command("buildah", append(append([]string{}, global...), "rmi", "--all", "--force")...).Run() //nolint:gosec // test-controlled args.
	})

	return global
}

func TestBuild_LoadThenLocal_WithRealBuildah(t *testing.T) {
	ctx := context.Background()
	global := hermeticStore(t)
	adapter := &buildah.Adapter{Global: global}
	cdir := hermeticContext(t)
	cfile := filepath.Join(cdir, "Containerfile")

	// Load mode: build into the isolated store under a tag.
	if err := adapter.Build(ctx, container.BuildRequest{
		Context:         cdir,
		Containerfile:   cfile,
		Mode:            container.BuildModeLoad,
		ImageRef:        "reusable-ci-buildtest:verify",
		SourceDateEpoch: "1700000000",
		Labels:          []string{"com.example.test=1"},
	}, os.Stderr); err != nil {
		t.Skipf("buildah cannot build in this environment (storage/permissions): %v", err)
	}

	images := runBuildah(t, global, "images", "--format", "{{.Name}}:{{.Tag}}")
	if !strings.Contains(images, "reusable-ci-buildtest:verify") {
		t.Errorf("built image not in store; images = %q", images)
	}

	// Local mode: export the target stage's filesystem to a directory.
	dest := t.TempDir()
	if err := adapter.Build(ctx, container.BuildRequest{
		Context:       cdir,
		Containerfile: cfile,
		Target:        "export",
		Mode:          container.BuildModeLocal,
		OutputDir:     dest,
	}, os.Stderr); err != nil {
		t.Fatalf("local-export build: %v", err)
	}

	body, err := os.ReadFile(filepath.Join(dest, "hello.txt"))
	if err != nil || strings.TrimSpace(string(body)) != "hi" {
		t.Errorf("exported file = %q, err = %v; want \"hi\"", body, err)
	}
}

// TestBuildToLayout_PushByDigest_ToInProcessRegistry is the critical proof for
// the publish path: buildah builds to an OCI layout, the ggcr adapter pushes it
// by digest to an in-process registry over plain HTTP (loopback), the returned
// digest matches the stored manifest, and — crucially — NO tag is created
// (tagless, like build-push's push-by-digest, so there is nothing to clean up).
func TestBuildToLayout_PushByDigest_ToInProcessRegistry(t *testing.T) {
	ctx := context.Background()

	srv := httptest.NewServer(registry.New())
	t.Cleanup(srv.Close)

	repo := strings.TrimPrefix(srv.URL, "http://") + "/o/r"

	adapter := &buildah.Adapter{Global: hermeticStore(t)}
	cdir := hermeticContext(t)

	layoutDir := t.TempDir()
	if err := adapter.BuildToLayout(ctx, container.BuildRequest{
		Context:         cdir,
		Containerfile:   filepath.Join(cdir, "Containerfile"),
		Mode:            container.BuildModePushByDigest,
		ImageRef:        repo,
		SourceDateEpoch: "1700000000",
	}, layoutDir, os.Stderr); err != nil {
		t.Skipf("buildah cannot build in this environment: %v", err)
	}

	digest, err := ociregistry.New().PushLayoutByDigest(ctx, layoutDir, repo)
	if err != nil {
		t.Fatalf("push by digest: %v", err)
	}

	if !strings.HasPrefix(digest, "sha256:") {
		t.Fatalf("returned digest = %q, want sha256:…", digest)
	}

	// Retrievable by digest.
	dref, err := name.NewDigest(repo+"@"+digest, name.Insecure)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := remote.Get(dref, remote.WithContext(ctx)); err != nil {
		t.Fatalf("manifest not retrievable by digest: %v", err)
	}

	// Tagless: no tag was created.
	repoRef, err := name.NewRepository(repo, name.Insecure)
	if err != nil {
		t.Fatal(err)
	}

	tags, err := remote.List(repoRef, remote.WithContext(ctx))
	if err != nil {
		t.Fatalf("list tags: %v", err)
	}

	if len(tags) != 0 {
		t.Errorf("push-by-digest created tags %v; want none (tagless)", tags)
	}
}

func runBuildah(t *testing.T, global []string, args ...string) string {
	t.Helper()

	out, err := exec.Command("buildah", append(global, args...)...).CombinedOutput() //nolint:gosec // test-controlled args.
	if err != nil {
		t.Fatalf("buildah %v: %v\n%s", args, err, out)
	}

	return string(out)
}
