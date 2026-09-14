// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package toolchain_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/app/toolchain"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

func TestMiseExposure_PreflightPreservesState(t *testing.T) { //nolint:gocognit,gocyclo // each late refusal shares exact earlier-source, destination and PATH canaries.
	t.Parallel()

	for _, scenario := range []string{
		"regular destination", "directory destination", "unrelated symlink", "stale symlink",
		"duplicate basename", "relative directory", "non-directory", "dangling source", "cyclic source", "source directory symlink",
		"unsafe name", "unsafe directory", "linked bin home", "linked bin ancestor",
		"PATH directory", "PATH symlink", "PATH source alias", "PATH destination", "PATH missing parent", "PATH bin ancestor",
		"late rustup collision", "late rustup empty", "late rustup relative", "late rustup multiline", "late rustup missing", "late rustup non-executable",
	} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			first, later := filepath.Join(root, "first"), filepath.Join(root, "later")
			writeExecutable(t, filepath.Join(first, "early"))
			writeExecutable(t, filepath.Join(later, "tool"))
			writeToolchainFile(t, root, "PATH", "prior-path\n")
			writeToolchainFile(t, root, "canary", "unrelated bytes\n")

			binHome := filepath.Join(root, "bin")
			if err := os.Mkdir(binHome, 0o700); err != nil {
				t.Fatal(err)
			}

			linkPath := filepath.Join(binHome, "tool")
			pathFile := filepath.Join(root, "PATH")
			binPaths := first + "\n" + later
			cargoPath := ""

			switch scenario {
			case "regular destination":
				writeToolchainFile(t, binHome, "tool", "keep regular\n")
			case "directory destination":
				writeToolchainFile(t, linkPath, "child", "keep directory\n")
			case "unrelated symlink":
				exposureSymlink(t, filepath.Join(root, "canary"), linkPath)
			case "stale symlink":
				exposureSymlink(t, filepath.Join(root, "old", "tool"), linkPath)
			case "duplicate basename":
				writeExecutable(t, filepath.Join(first, "tool"))
			case "relative directory":
				binPaths = first + "\nlater"
			case "non-directory":
				binPaths = first + "\n" + filepath.Join(root, "canary")
			case "dangling source":
				exposureSymlink(t, "missing", filepath.Join(later, "z-bad"))
			case "cyclic source":
				exposureSymlink(t, "z-bad", filepath.Join(later, "z-bad"))
			case "source directory symlink":
				exposureSymlink(t, binHome, filepath.Join(later, "z-bad"))
			case "unsafe name":
				writeExecutable(t, filepath.Join(later, "z-bad\nname"))
			case "unsafe directory":
				later = filepath.Join(root, "bad:directory")
				writeExecutable(t, filepath.Join(later, "tool"))
				binPaths = first + "\n" + later
			case "linked bin home":
				binHome = filepath.Join(root, "linked-bin")
				exposureSymlink(t, filepath.Join(root, "bin"), binHome)
			case "linked bin ancestor":
				exposureSymlink(t, filepath.Join(root, "bin"), filepath.Join(root, "linked-bin"))
				binHome = filepath.Join(root, "linked-bin", "new")
			case "PATH directory":
				pathFile = binHome
			case "PATH symlink":
				pathFile = filepath.Join(root, "linked-PATH")
				exposureSymlink(t, filepath.Join(root, "canary"), pathFile)
			case "PATH source alias":
				pathFile = filepath.Join(root, "aliased-PATH")
				if err := os.Link(filepath.Join(first, "early"), pathFile); err != nil {
					t.Fatal(err)
				}
			case "PATH destination":
				pathFile = filepath.Join(binHome, "early")
			case "PATH missing parent":
				pathFile = filepath.Join(root, "absent", "PATH")
			case "PATH bin ancestor":
				pathFile = filepath.Join(root, "absent")
				binHome = filepath.Join(pathFile, "bin")
			case "late rustup collision":
				cargoPath = filepath.Join(root, "rustup", "cargo")
				writeExecutable(t, cargoPath)
				writeExecutable(t, filepath.Join(root, "rustup", "early"))
			case "late rustup relative":
				cargoPath = "relative/cargo"
			case "late rustup multiline":
				cargoPath = later + "\n" + later + "/cargo"
			case "late rustup missing":
				cargoPath = filepath.Join(root, "missing", "cargo")
			case "late rustup non-executable":
				cargoPath = filepath.Join(root, "canary")
			}

			runner := &fakeMiseRunner{responses: map[string]string{"bin-paths": binPaths}}

			if strings.HasPrefix(scenario, "late rustup") {
				writeToolchainFile(t, root, ".mise.toml", "[tools]\n'aqua:rust-lang/rustup'='1.28.2'\n")
				writeToolchainFile(t, root, "rust-toolchain.toml", "[toolchain]\nchannel='1.90.0'\n")

				runner.responses["exec\x00--no-deps\x00aqua:rust-lang/rustup\x00--\x00rustup\x00which\x00cargo"] = cargoPath
			}

			before := exposureSnapshot(t, root)

			err := toolchain.ExposeMiseTools(t.Context(), runner, nil, toolchain.ExposeMiseToolsInput{
				Root: root, BinHome: binHome, PathFile: pathFile, Locked: "false",
			})
			if err == nil {
				t.Error("invalid exposure succeeded")
			}

			if got := exposureSnapshot(t, root); got != before {
				t.Errorf("refusal changed owned state\nbefore:\n%s\nafter:\n%s", before, got)
			}

			for _, call := range runner.calls {
				if call.dir != root {
					t.Fatalf("mise root = %q, want %q", call.dir, root)
				}
			}
		})
	}
}

func TestMiseExposure_SameFileAndSourceLinks(t *testing.T) { //nolint:gocognit // the source/destination alias matrix shares rerun and byte-preservation checks.
	t.Parallel()

	for _, scenario := range []string{"same directory", "hardlink destination", "same target symlink", "relative source link", "absolute source link", "linked source directory", "same-file duplicate", "rustup source links"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			source := filepath.Join(root, "source bin")
			binHome := filepath.Join(root, "bin")
			writeExecutable(t, filepath.Join(source, "tool"))

			if err := os.Mkdir(binHome, 0o700); err != nil {
				t.Fatal(err)
			}

			binPaths := source
			wantPaths := source + "\n"

			switch scenario {
			case "same directory":
				binHome = source
			case "hardlink destination":
				if err := os.Link(filepath.Join(source, "tool"), filepath.Join(binHome, "tool")); err != nil {
					t.Fatal(err)
				}
			case "same target symlink":
				exposureSymlink(t, "../source bin/tool", filepath.Join(binHome, "tool"))
			case "relative source link", "absolute source link", "rustup source links":
				writeExecutable(t, filepath.Join(root, "runtime", "proxy"))

				target := "../runtime/proxy"
				if scenario == "absolute source link" {
					target = filepath.Join(root, "runtime", "proxy")
				}

				exposureSymlink(t, target, filepath.Join(source, "cargo"))
				exposureSymlink(t, "cargo", filepath.Join(source, "rustc"))
			case "linked source directory":
				binPaths = filepath.Join(root, "source-alias")
				exposureSymlink(t, source, binPaths)
			case "same-file duplicate":
				other := filepath.Join(root, "other")
				if err := os.Mkdir(other, 0o700); err != nil {
					t.Fatal(err)
				}

				if err := os.Link(filepath.Join(source, "tool"), filepath.Join(other, "tool")); err != nil {
					t.Fatal(err)
				}

				binPaths += "\n" + other + "\n" + source
				wantPaths += other + "\n"
			}

			runner := &fakeMiseRunner{responses: map[string]string{"bin-paths": binPaths}}

			if scenario == "rustup source links" {
				writeToolchainFile(t, root, ".mise.toml", "[tools]\n'aqua:rust-lang/rustup'='1.28.2'\n")
				writeToolchainFile(t, root, "rust-toolchain.toml", "[toolchain]\nchannel='1.90.0'\n")

				runner.responses["bin-paths"] = ""
				runner.responses["exec\x00--no-deps\x00aqua:rust-lang/rustup\x00--\x00rustup\x00which\x00cargo"] = filepath.Join(source, "cargo") + "\n"
			}

			original := exposureSnapshot(t, source)
			destination, statErr := os.Lstat(filepath.Join(binHome, "tool"))

			in := toolchain.ExposeMiseToolsInput{Root: root, BinHome: binHome, PathFile: filepath.Join(root, "PATH"), Locked: "false"}
			for count := 1; count <= 2; count++ {
				if err := toolchain.ExposeMiseTools(t.Context(), runner, nil, in); err != nil {
					t.Fatal(err)
				}

				if got := string(readFileOrFail(t, in.PathFile)); got != strings.Repeat(wantPaths, count) {
					t.Fatalf("PATH = %q", got)
				}

				if got := exposureSnapshot(t, source); got != original {
					t.Fatal("source bytes, mode or symlinks changed")
				}

				if got := string(readFileOrFail(t, filepath.Join(binHome, "tool"))); got != "#!/bin/sh\n" {
					t.Fatalf("exposed bytes = %q", got)
				}

				if statErr == nil {
					current, err := os.Lstat(filepath.Join(binHome, "tool"))
					if err != nil || !os.SameFile(destination, current) || destination.Mode() != current.Mode() {
						t.Fatal("same-file destination was replaced")
					}
				}

				if scenario == "same target symlink" {
					if target, err := os.Readlink(filepath.Join(binHome, "tool")); err != nil || target != "../source bin/tool" {
						t.Fatalf("same-file link changed: target=%q err=%v", target, err)
					}
				}

				if strings.Contains(scenario, "source link") {
					for _, name := range []string{"cargo", "rustc"} {
						if got := string(readFileOrFail(t, filepath.Join(binHome, name))); got != "#!/bin/sh\n" {
							t.Fatalf("%s bytes = %q", name, got)
						}
					}
				}
			}
		})
	}
}

func TestMiseExposure_ConflictIsValidation(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeExecutable(t, filepath.Join(root, "one", "tool"))
	writeExecutable(t, filepath.Join(root, "two", "tool"))
	runner := &fakeMiseRunner{responses: map[string]string{"bin-paths": filepath.Join(root, "one") + "\n" + filepath.Join(root, "two")}}

	err := toolchain.ExposeMiseTools(t.Context(), runner, nil, toolchain.ExposeMiseToolsInput{Root: root, BinHome: filepath.Join(root, "bin"), PathFile: filepath.Join(root, "PATH"), Locked: "false"})
	if !errors.Is(err, errs.ErrValidation) {
		t.Fatalf("conflict error = %v, want ErrValidation", err)
	}

	if _, err := os.Lstat(filepath.Join(root, "bin")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("preflight created bin home: %v", err)
	}
}

type exposureFailingRunner struct{ fakeMiseRunner }

func (f *exposureFailingRunner) Run(ctx context.Context, env []string, args ...string) (string, error) {
	output, err := f.fakeMiseRunner.Run(ctx, env, args...)
	if slices.Contains(args, "exec") {
		return "", os.ErrPermission
	}

	return output, err
}

func TestMiseExposure_RustupFailureBeforePublication(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeToolchainFile(t, root, ".mise.toml", "[tools]\n'aqua:rust-lang/rustup'='1.28.2'\n")
	writeToolchainFile(t, root, "rust-toolchain.toml", "[toolchain]\nchannel='1.90.0'\n")
	writeToolchainFile(t, root, "PATH", "prior-path\n")
	source := filepath.Join(root, "source")
	writeExecutable(t, filepath.Join(source, "tool"))
	runner := &exposureFailingRunner{fakeMiseRunner{responses: map[string]string{"bin-paths": source}}}
	before := exposureSnapshot(t, root)

	var out bytes.Buffer

	err := toolchain.ExposeMiseTools(t.Context(), runner, &out, toolchain.ExposeMiseToolsInput{
		Root: root, BinHome: filepath.Join(root, "bin"), PathFile: filepath.Join(root, "PATH"), Locked: "false",
	})
	if !errors.Is(err, os.ErrPermission) || !strings.Contains(err.Error(), "resolve rustup cargo path") {
		t.Fatalf("rustup failure = %v", err)
	}

	if len(runner.calls) != 2 || !slices.Equal(runner.calls[0].args, []string{"bin-paths"}) ||
		!slices.Equal(runner.calls[1].args, []string{"exec", "--no-deps", "aqua:rust-lang/rustup", "--", "rustup", "which", "cargo"}) {
		t.Fatalf("unexpected calls: %v", runner.calls)
	}

	if out.Len() != 0 || exposureSnapshot(t, root) != before {
		t.Fatal("rustup failure published output or changed owned state")
	}
}

func TestMiseExposure_PublicationFailurePreservesKnownGoodState(t *testing.T) {
	t.Parallel()

	if os.Geteuid() == 0 {
		t.Skip("owned permission refusals require an unprivileged process")
	}

	for _, stage := range []string{"PATH open", "link create"} {
		t.Run(stage, func(t *testing.T) {
			root := t.TempDir()
			source, binHome := filepath.Join(root, "source"), filepath.Join(root, "bin")
			writeExecutable(t, filepath.Join(source, "early"))
			writeExecutable(t, filepath.Join(source, "tool"))
			writeToolchainFile(t, root, "PATH", "prior-path\n")

			if err := os.Mkdir(binHome, 0o700); err != nil {
				t.Fatal(err)
			}

			exposureSymlink(t, filepath.Join(source, "early"), filepath.Join(binHome, "early"))
			readOnly, mode := filepath.Join(root, "PATH"), os.FileMode(0o400)

			wantStage := "open PATH file"
			if stage == "link create" {
				readOnly, mode, wantStage = binHome, 0o500, "create executable link"
			}

			if err := os.Chmod(readOnly, mode); err != nil {
				t.Fatal(err)
			}

			t.Cleanup(func() {
				if err := os.Chmod(readOnly, mode|0o200); err != nil {
					t.Error(err)
				}
			})
			before := exposureSnapshot(t, root)
			runner := &fakeMiseRunner{responses: map[string]string{"bin-paths": source}}

			var out bytes.Buffer

			err := toolchain.ExposeMiseTools(t.Context(), runner, &out, toolchain.ExposeMiseToolsInput{
				Root: root, BinHome: binHome, PathFile: filepath.Join(root, "PATH"), Locked: "false",
			})
			if !errors.Is(err, os.ErrPermission) || !strings.Contains(err.Error(), wantStage) {
				t.Fatalf("%s failure = %v", stage, err)
			}

			if out.Len() != 0 || exposureSnapshot(t, root) != before {
				t.Fatal("publication failure changed seeded PATH, existing link or executable bytes")
			}
		})
	}
}

func TestMiseExposure_DestinationSymlinkParentResolution(t *testing.T) { //nolint:gocognit // inverse hardlink canaries discriminate true and false same-file resolution with identical link spelling.
	t.Parallel()

	for _, sameFile := range []bool{false, true} {
		name := "unrelated actual target"
		if sameFile {
			name = "same-file actual target"
		}

		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			fixtures := filepath.Join(root, "fixtures")
			source := filepath.Join(fixtures, "source", "tool")

			unrelated := filepath.Join(fixtures, "unrelated", "tool")
			for _, fixture := range []struct{ path, body string }{
				{source, "selected executable canary\n"},
				{unrelated, "unrelated executable canary\n"},
			} {
				writeExecutable(t, fixture.path)

				if err := os.WriteFile(fixture.path, []byte(fixture.body), 0o755); err != nil { //nolint:gosec // owned executable canaries are never executed.
					t.Fatal(err)
				}
			}

			binHome, outside := filepath.Join(fixtures, "bin"), filepath.Join(fixtures, "outside")
			for _, dir := range []string{binHome, filepath.Join(outside, "nested")} {
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
			}

			lexicalTarget, actualTarget := source, unrelated
			if sameFile {
				lexicalTarget, actualTarget = unrelated, source
			}

			if err := os.Link(lexicalTarget, filepath.Join(binHome, "candidate")); err != nil {
				t.Fatal(err)
			}

			if err := os.Link(actualTarget, filepath.Join(outside, "candidate")); err != nil {
				t.Fatal(err)
			}

			exposureSymlink(t, filepath.Join(outside, "nested"), filepath.Join(binHome, "hop"))
			destination := filepath.Join(binHome, "tool")
			exposureSymlink(t, "hop/../candidate", destination)

			if got := string(readFileOrFail(t, destination)); got != string(readFileOrFail(t, actualTarget)) || got == string(readFileOrFail(t, lexicalTarget)) {
				t.Fatal("fixture did not distinguish actual and lexically cleaned link resolution")
			}

			originalLink, err := os.Lstat(destination)
			if err != nil {
				t.Fatal(err)
			}

			writeToolchainFile(t, root, "PATH", "prior-path\n")
			before := exposureSnapshot(t, fixtures)
			runner := &fakeMiseRunner{responses: map[string]string{"bin-paths": filepath.Dir(source)}}
			err = toolchain.ExposeMiseTools(t.Context(), runner, nil, toolchain.ExposeMiseToolsInput{
				Root: root, BinHome: binHome, PathFile: filepath.Join(root, "PATH"), Locked: "false",
			})
			wantPath := "prior-path\n"

			if sameFile {
				if err != nil {
					t.Errorf("actual same-file destination refused: %v", err)
				}

				wantPath += filepath.Dir(source) + "\n"
			} else if !errors.Is(err, errs.ErrValidation) {
				t.Errorf("unrelated actual destination error = %v, want ErrValidation", err)
			}

			if got := string(readFileOrFail(t, filepath.Join(root, "PATH"))); got != wantPath {
				t.Errorf("PATH = %q, want %q", got, wantPath)
			}

			currentLink, err := os.Lstat(destination)
			if err != nil || !os.SameFile(originalLink, currentLink) || exposureSnapshot(t, fixtures) != before {
				t.Fatal("link resolution changed executable canaries or existing destination state")
			}
		})
	}
}

func TestMiseExposure_PATHInsideNewBinHome(t *testing.T) { //nolint:gocognit // the shared combined-mise/rustup plan has positive and late-refusal controls.
	t.Parallel()

	for _, scenario := range []string{"success", "late source conflict", "late PATH collision", "late rustup conflict"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			fixtures := filepath.Join(root, "fixtures")
			first, later, cargo := filepath.Join(fixtures, "first"), filepath.Join(fixtures, "later"), filepath.Join(fixtures, "rustup")
			writeExecutable(t, filepath.Join(first, "early"))
			writeExecutable(t, filepath.Join(later, "tool"))
			writeExecutable(t, filepath.Join(cargo, "cargo"))
			writeToolchainFile(t, root, ".mise.toml", "[tools]\n'aqua:rust-lang/rustup'='1.28.2'\n")
			writeToolchainFile(t, root, "rust-toolchain.toml", "[toolchain]\nchannel='1.90.0'\n")

			switch scenario {
			case "late source conflict":
				writeExecutable(t, filepath.Join(later, "early"))
			case "late PATH collision":
				writeExecutable(t, filepath.Join(later, "PATH"))
			case "late rustup conflict":
				writeExecutable(t, filepath.Join(cargo, "tool"))
			}

			binHome := filepath.Join(root, "new-parent", "bin")
			pathFile := filepath.Join(binHome, "PATH")
			runner := &fakeMiseRunner{responses: map[string]string{
				"bin-paths": first + "\n" + later,
				"exec\x00--no-deps\x00aqua:rust-lang/rustup\x00--\x00rustup\x00which\x00cargo": filepath.Join(cargo, "cargo"),
			}}
			before, sourcesBefore := exposureSnapshot(t, root), exposureSnapshot(t, fixtures)

			err := toolchain.ExposeMiseTools(t.Context(), runner, nil, toolchain.ExposeMiseToolsInput{
				Root: root, BinHome: binHome, PathFile: pathFile, Locked: "false",
			})
			if scenario != "success" {
				if !errors.Is(err, errs.ErrValidation) {
					t.Errorf("late refusal error = %v, want ErrValidation", err)
				}

				if exposureSnapshot(t, root) != before {
					t.Fatal("late refusal created the planned PATH parent or changed owned state")
				}

				return
			}

			if err != nil {
				t.Fatalf("PATH inside new bin home: %v", err)
			}

			if got := string(readFileOrFail(t, pathFile)); got != first+"\n"+later+"\n"+cargo+"\n" {
				t.Fatalf("combined PATH = %q", got)
			}

			for _, source := range []string{filepath.Join(first, "early"), filepath.Join(later, "tool"), filepath.Join(cargo, "cargo")} {
				if target, err := os.Readlink(filepath.Join(binHome, filepath.Base(source))); err != nil || target != source {
					t.Fatalf("new bin-home link target = %q, want %q: %v", target, source, err)
				}
			}

			if exposureSnapshot(t, fixtures) != sourcesBefore {
				t.Fatal("publication changed source files")
			}
		})
	}
}

func exposureSymlink(t *testing.T, target, path string) {
	t.Helper()

	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
}

func exposureSnapshot(t *testing.T, root string) string {
	t.Helper()

	var snapshot strings.Builder

	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, err error) error {
		if err != nil {
			return err
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}

		snapshot.WriteString(path + " " + info.Mode().String() + " ")

		switch {
		case info.Mode().IsRegular():
			snapshot.Write(readFileOrFail(t, path))
		case info.Mode()&os.ModeSymlink != 0:
			target, err := os.Readlink(path)
			if err != nil {
				return err
			}

			snapshot.WriteString(target)
		}

		snapshot.WriteByte('\n')

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	return snapshot.String()
}
