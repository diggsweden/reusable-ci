// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package build_test

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	appbuild "github.com/diggsweden/reusable-ci/v3/internal/app/build"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/fakeoutputsink"
	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testfs"
)

// fakeCargoTool records every Run call and synthesises responses for
// the cargo subcommands the cargo app code invokes: `metadata --no-deps`
// writes a stub JSON document to in.Stdout; `build --release …` creates
// the binary file at target/<triple>/release/<name> so locateCargoBinary
// succeeds without a real cargo on PATH.
type fakeCargoTool struct {
	calls       []appbuild.CargoRunInput
	metadata    string   // JSON to write on `metadata` calls
	binaryNames []string // candidates to write into target/release/ on `build` calls
}

func (f *fakeCargoTool) Run(_ context.Context, in appbuild.CargoRunInput) error {
	f.calls = append(f.calls, in)

	if len(in.Args) > 0 && in.Args[0] == "metadata" {
		if in.Stdout != nil {
			_, _ = io.WriteString(in.Stdout, f.metadata)
		}

		return nil
	}

	if len(in.Args) >= 5 && in.Args[0] == "build" {
		triple := ""

		for i, a := range in.Args {
			if a == "--target" && i+1 < len(in.Args) {
				triple = in.Args[i+1]
			}
		}

		if triple == "" {
			return nil
		}

		releaseDir := filepath.Join(in.Dir, "target", triple, "release")
		if err := os.MkdirAll(releaseDir, 0o755); err != nil {
			return err
		}

		for _, name := range f.binaryNames {
			path := filepath.Join(releaseDir, name)
			if err := os.WriteFile(path, []byte("ELFish"), 0o755); err != nil { //nolint:gosec // test fixture
				return err
			}
		}
	}

	return nil
}

const sampleCargoMetadata = `{
  "packages": [
    {"name": "hello", "version": "0.4.2", "targets": [{"name": "hello", "kind": ["bin"]}]}
  ],
  "workspace_members": ["hello 0.4.2"],
  "workspace_root": ".",
  "resolve": null
}`

func TestCargoMetadata_EmitsOutputs(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)

	tool := &fakeCargoTool{metadata: sampleCargoMetadata}
	sink := fakeoutputsink.New(t)

	var out bytes.Buffer
	if err := appbuild.CargoMetadata(context.Background(), tool, sink, &out, appbuild.CargoMetadataInput{Dir: fsys.Root, RefName: "v9.9.9"}); err != nil {
		t.Fatal(err)
	}

	if got := sink.Single("binary-name"); got != "hello" {
		t.Errorf("binary-name = %q", got)
	}

	if got := sink.Single("version"); got != "9.9.9" {
		t.Errorf("version = %q (expected --ref-name to win over Cargo.toml)", got)
	}

	if got := sink.Single("package"); got != "hello" {
		t.Errorf("package = %q", got)
	}
}

func TestCargoMetadata_FallsBackToCargoTomlVersion(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)

	tool := &fakeCargoTool{metadata: sampleCargoMetadata}
	sink := fakeoutputsink.New(t)

	if err := appbuild.CargoMetadata(context.Background(), tool, sink, &bytes.Buffer{}, appbuild.CargoMetadataInput{Dir: fsys.Root}); err != nil {
		t.Fatal(err)
	}

	if got := sink.Single("version"); got != "0.4.2" {
		t.Errorf("version = %q (expected Cargo.toml version)", got)
	}
}

func TestCargoFetch_RunsFetchLocked(t *testing.T) {
	t.Parallel()

	tool := &fakeCargoTool{}
	if err := appbuild.CargoFetch(context.Background(), tool, &bytes.Buffer{}, &bytes.Buffer{}, "src"); err != nil {
		t.Fatal(err)
	}

	if got := strings.Join(tool.calls[0].Args, " "); got != "fetch --locked" {
		t.Errorf("args = %q", got)
	}
}

func TestCargoTest_RunsAllTargets(t *testing.T) {
	t.Parallel()

	tool := &fakeCargoTool{}
	if err := appbuild.CargoTest(context.Background(), tool, &bytes.Buffer{}, &bytes.Buffer{}, appbuild.CargoTestInput{Dir: "src"}); err != nil {
		t.Fatal(err)
	}

	if got := strings.Join(tool.calls[0].Args, " "); got != "test --locked --all-targets" {
		t.Errorf("args = %q", got)
	}
}

func TestCargoBuildBinaries_BuildsPlatforms(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)

	tool := &fakeCargoTool{metadata: sampleCargoMetadata, binaryNames: []string{"hello"}}

	if err := appbuild.CargoBuildBinaries(context.Background(), tool, &bytes.Buffer{}, &bytes.Buffer{}, appbuild.CargoBuildBinariesInput{
		Dir:       fsys.Root,
		Platforms: "linux/amd64, linux/arm64",
		Version:   "1.2.3",
	}); err != nil {
		t.Fatal(err)
	}

	// One metadata call (binary-name resolution) + two build calls.
	if len(tool.calls) != 3 {
		t.Fatalf("expected 3 calls (metadata + 2 builds), got %d", len(tool.calls))
	}

	// Verify dist/ layout matches the Go shape.
	for _, want := range []string{
		"dist/linux-amd64/hello-linux-amd64",
		"dist/linux-arm64/hello-linux-arm64",
	} {
		if _, err := os.Stat(filepath.Join(fsys.Root, want)); err != nil {
			t.Errorf("missing %s: %v", want, err)
		}
	}

	// Verify --target was passed and used the canonical Rust triple.
	wantTriples := map[string]bool{
		"x86_64-unknown-linux-gnu":  false,
		"aarch64-unknown-linux-gnu": false,
	}

	for _, call := range tool.calls[1:] {
		for i, a := range call.Args {
			if a == "--target" && i+1 < len(call.Args) {
				if _, ok := wantTriples[call.Args[i+1]]; ok {
					wantTriples[call.Args[i+1]] = true
				}
			}
		}
	}

	for triple, seen := range wantTriples {
		if !seen {
			t.Errorf("expected build call with --target %s", triple)
		}
	}
}

func TestCargoBuildBinaries_RejectsUnknownPlatform(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)

	tool := &fakeCargoTool{metadata: sampleCargoMetadata, binaryNames: []string{"hello"}}

	err := appbuild.CargoBuildBinaries(context.Background(), tool, &bytes.Buffer{}, &bytes.Buffer{}, appbuild.CargoBuildBinariesInput{
		Dir:       fsys.Root,
		Platforms: "plan9/amd64",
		Version:   "1.2.3",
	})
	if err == nil || !strings.Contains(err.Error(), "unsupported platform") {
		t.Fatalf("err = %v", err)
	}
}

func TestCargoBuildBinaries_RequiresVersion(t *testing.T) {
	t.Parallel()
	fsys := testfs.NewReal(t)

	tool := &fakeCargoTool{metadata: sampleCargoMetadata, binaryNames: []string{"hello"}}

	err := appbuild.CargoBuildBinaries(context.Background(), tool, &bytes.Buffer{}, &bytes.Buffer{}, appbuild.CargoBuildBinariesInput{
		Dir:       fsys.Root,
		Platforms: "linux/amd64",
	})
	if err == nil || !strings.Contains(err.Error(), "version is required") {
		t.Fatalf("err = %v", err)
	}
}
