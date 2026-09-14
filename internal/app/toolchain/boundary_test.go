// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package toolchain_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/app/toolchain"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

type boundaryTransport func(*http.Request) (*http.Response, error)

func (f boundaryTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func TestMiseBoundary_OfficialPinnedSource(t *testing.T) {
	t.Parallel()
	archive := miseArchive(t, "owned executable fixture")
	calls := 0
	client := &http.Client{Transport: boundaryTransport(func(req *http.Request) (*http.Response, error) {
		calls++

		arch := "x64"
		if runtime.GOARCH == "arm64" {
			arch = "arm64"
		}

		want := "https://github.com/jdx/mise/releases/download/v2026.6.11/mise-v2026.6.11-linux-" + arch + "-musl.tar.gz"
		if req.Method != http.MethodGet || req.URL.String() != want || req.Header.Get("Authorization") != "" {
			t.Fatalf("unexpected request %s %s", req.Method, req.URL)
		}

		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(bytes.NewReader(archive)), Header: make(http.Header)}, nil
	})}
	dir := t.TempDir()

	path, err := toolchain.InstallMise(t.Context(), client, io.Discard, toolchain.InstallMiseInput{Version: "2026.6.11", LinuxX64SHA256: shaHexForArch(archive, "amd64"), LinuxARM64SHA256: shaHexForArch(archive, "arm64"), DestDir: dir})
	if err != nil || calls != 1 || path != filepath.Join(dir, "mise") {
		t.Fatalf("path=%s calls=%d err=%v", path, calls, err)
	}

	body, err := os.ReadFile(path)
	if err != nil || string(body) != "owned executable fixture" {
		t.Fatalf("body=%q err=%v", body, err)
	}

	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o755 {
		t.Fatalf("info=%v err=%v", info, err)
	}
}

func TestMiseBoundary_InvalidPinsBeforeEffects(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"", "latest", "2026.6", "v2026.6.11", "2026.6.11-rc1", "2026.6.11/other", "2026.6.11\n"} {
		dir := t.TempDir()

		var out bytes.Buffer

		calls := 0
		client := &http.Client{Transport: boundaryTransport(func(*http.Request) (*http.Response, error) {
			calls++

			return &http.Response{StatusCode: http.StatusBadRequest, Body: io.NopCloser(strings.NewReader("")), Header: make(http.Header)}, nil
		})}
		_, err := toolchain.InstallMise(t.Context(), client, &out, toolchain.InstallMiseInput{Version: value, LinuxX64SHA256: strings.Repeat("a", 64), LinuxARM64SHA256: strings.Repeat("b", 64), DestDir: dir})

		entries, readErr := os.ReadDir(dir)
		if !errors.Is(err, errs.ErrUsage) || calls != 0 || out.Len() != 0 || readErr != nil || len(entries) != 0 {
			t.Fatalf("version=%q err=%v calls=%d entries=%v", value, err, calls, entries)
		}
	}

	for _, arch := range []string{"amd64", "arm64"} {
		for _, pin := range []string{"", "ABCDEF", strings.Repeat("a", 63), strings.Repeat("b", 65), strings.Repeat("G", 64)} {
			in := toolchain.InstallMiseInput{Version: "2026.6.11", LinuxX64SHA256: strings.Repeat("a", 64), LinuxARM64SHA256: strings.Repeat("b", 64), DestDir: t.TempDir()}
			if arch == "amd64" {
				in.LinuxX64SHA256 = pin
			} else {
				in.LinuxARM64SHA256 = pin
			}

			client := &http.Client{Transport: boundaryTransport(func(*http.Request) (*http.Response, error) {
				t.Fatal("invalid architecture pin reached download")

				return nil, errTransientMise
			})}
			if _, err := toolchain.InstallMise(t.Context(), client, io.Discard, in); !errors.Is(err, errs.ErrUsage) {
				t.Fatalf("err=%v", err)
			}
		}
	}
}

func TestMiseBoundary_DeclaredSubsetAndTOML(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	for _, tc := range []struct {
		config, tools string
		valid         bool
	}{
		{"[tools]\n'aqua:org/tool' = {version='1.2.3'}\n'go'='1.26.6'\n", "aqua:org/tool go", true},
		{"[env]\ngo='not-a-tool'\n", "go", false},
		{"[tools]\ngo='1.26.6'\n", "go@latest", false},
		{"[tools]\ngo='1.26.6'\n", "--help", false},
		{"[tools]\ngo='1.26.6'\n", "go go", false},
		{"[tools]\ngo='1.26.6'\n", "aqua:unknown/tool", false},
		{"[tools\ngo='1.26.6'\n", "go", false},
	} {
		root := t.TempDir()
		writeToolchainFile(t, root, ".mise.toml", tc.config)
		writeToolchainFile(t, root, "mise.lock", "# owned lock\n")

		runner := &recordingMiseRunner{}

		var out bytes.Buffer

		err := toolchain.InstallMiseTools(t.Context(), runner, &out, toolchain.InstallMiseToolsInput{Root: root, Locked: "true", Tools: tc.tools, RetryAttempts: 1})
		if tc.valid {
			for _, call := range runner.calls {
				if call.dir != root {
					t.Fatalf("mise ran in %q, not reviewed root %q", call.dir, root)
				}
			}

			if err != nil || len(runner.calls) != 2 || !slices.Equal(runner.calls[0].args, []string{"install", "--locked", "go"}) || !slices.Equal(runner.calls[1].args, []string{"install", "--locked", "aqua:org/tool", "go"}) {
				t.Fatalf("err=%v calls=%v", err, runner.calls)
			}
		} else if err == nil || len(runner.calls) != 0 || out.Len() != 0 {
			t.Fatalf("err=%v calls=%v out=%s", err, runner.calls, &out)
		}
	}
}

func TestMiseBoundary_LinkedEvidenceRefusesBeforeEffects(t *testing.T) {
	t.Setenv("HOME", t.TempDir())

	for _, name := range []string{".mise.toml", "mise.toml", "mise.lock", "rust-toolchain.toml"} {
		root := t.TempDir()
		outside := t.TempDir()
		canary := filepath.Join(outside, "evidence")
		writeToolchainFile(t, outside, "evidence", "# private canary\n")

		if name != "mise.lock" {
			writeToolchainFile(t, root, "mise.lock", "# owned lock\n")
		}

		if err := os.Symlink(canary, filepath.Join(root, name)); err != nil {
			t.Fatal(err)
		}

		runner := &recordingMiseRunner{}

		var out bytes.Buffer

		for _, call := range []func() error{
			func() error {
				return toolchain.ValidateMiseInstall(toolchain.ValidateMiseInstallInput{Root: root, Locked: "true"})
			},
			func() error {
				return toolchain.InstallMiseTools(t.Context(), runner, &out, toolchain.InstallMiseToolsInput{Root: root, Locked: "true", RetryAttempts: 1})
			},
			func() error {
				return toolchain.ExposeMiseTools(t.Context(), runner, &out, toolchain.ExposeMiseToolsInput{Root: root, Locked: "true", BinHome: filepath.Join(root, "bin"), PathFile: filepath.Join(root, "PATH")})
			},
		} {
			if err := call(); !errors.Is(err, errs.ErrValidation) {
				t.Fatalf("%s: %v", name, err)
			}

			if len(runner.calls) != 0 || out.Len() != 0 {
				t.Fatal("linked evidence reached tools/output")
			}
		}

		body, err := os.ReadFile(canary)
		if err != nil || string(body) != "# private canary\n" {
			t.Fatal("outside canary changed")
		}

		if _, err := os.Stat(filepath.Join(root, "bin")); !errors.Is(err, os.ErrNotExist) {
			t.Fatal("refusal created bin directory")
		}
	}
}

type trustRecorder struct {
	calls int
	dir   string
	env   []string
	args  []string
	err   error
}

func (f *trustRecorder) Run(_ context.Context, env []string, args ...string) (string, error) {
	f.calls++
	f.dir, _ = os.Getwd()
	f.env = env
	f.args = slices.Clone(args)

	return "trusted owned config", f.err
}
func TestMiseBoundary_TrustActionAndError(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)

	for _, cause := range []error{nil, errTransientMise} {
		runner := &trustRecorder{err: cause}

		var out bytes.Buffer

		err := toolchain.TrustMiseConfig(t.Context(), runner, &out, toolchain.TrustMiseConfigInput{})
		if !errors.Is(err, cause) || runner.calls != 1 || runner.dir != dir || runner.env != nil || !reflect.DeepEqual(runner.args, []string{"trust"}) || out.String() != "trusted owned config\n" {
			t.Fatalf("err=%v runner=%+v out=%s", err, runner, &out)
		}
	}
}

func TestMiseBoundary_ParentAndInternalLinks(t *testing.T) {
	for _, parent := range []bool{false, true} {
		root := t.TempDir()
		writeToolchainFile(t, root, "mise.lock", "# valid owned lock\n")

		if parent {
			writeToolchainFile(t, root, ".mise.toml", "[tools]\ngo='1.26.6'\n")

			link := filepath.Join(t.TempDir(), "workspace")
			if err := os.Symlink(root, link); err != nil {
				t.Fatal(err)
			}

			root = link
		} else {
			writeToolchainFile(t, root, "alternate.toml", "[tools]\ngo='1.26.6'\n")

			if err := os.Symlink("alternate.toml", filepath.Join(root, ".mise.toml")); err != nil {
				t.Fatal(err)
			}
		}

		if err := toolchain.ValidateMiseInstall(toolchain.ValidateMiseInstallInput{Root: root, Locked: "true"}); !errors.Is(err, errs.ErrValidation) {
			t.Fatalf("linked evidence accepted: %v", err)
		}
	}
}

func TestMiseBoundary_MergeRejectsDuplicateToolAuthority(t *testing.T) {
	root := t.TempDir()
	writeToolchainFile(t, root, "mise.lock", "# owned lock\n")
	writeToolchainFile(t, root, ".mise.toml", "[tools]\ngo='1.26.6'\n")
	writeToolchainFile(t, root, "mise.toml", "[tools]\n'aqua:org/tool'='1.2.3'\n")
	in := toolchain.InstallMiseToolsInput{Root: root, Locked: "true", Tools: "aqua:org/tool", RetryAttempts: 1}

	runner := &recordingMiseRunner{}
	if err := toolchain.InstallMiseTools(t.Context(), runner, io.Discard, in); err != nil || len(runner.calls) != 2 {
		t.Fatalf("err=%v calls=%v", err, runner.calls)
	}

	writeToolchainFile(t, root, "mise.toml", "[tools]\ngo='other-version'\n")

	runner = &recordingMiseRunner{}
	if err := toolchain.InstallMiseTools(t.Context(), runner, io.Discard, in); !errors.Is(err, errs.ErrValidation) || len(runner.calls) != 0 {
		t.Fatalf("duplicate authority err=%v calls=%v", err, runner.calls)
	}
}
