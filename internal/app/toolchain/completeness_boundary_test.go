// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package toolchain_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/app/toolchain"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

func TestCacheTupleBoundary_UnambiguousKnownAnswers(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		in   toolchain.CacheDiscriminatorInput
		want string
	}{
		{toolchain.CacheDiscriminatorInput{}, "63cab8e921e41324"},
		{toolchain.CacheDiscriminatorInput{Tools: "a", InstallDevTools: "b", ExtraCachePaths: "c"}, "321cf3124838eafc"},
	} {
		if got := toolchain.CacheDiscriminator(tc.in); got != tc.want {
			t.Fatalf("got=%s want=%s", got, tc.want)
		}
	}

	seen := map[string]bool{}

	for _, tuple := range [][3]string{{"a|b", "c", ""}, {"a", "b|c", ""}, {"", "", ""}, {"|", "", ""}, {"", "|", ""}, {"a\nb", "c", ""}, {"a", "b\nc", ""}, {"\u00e5", "b", "c"}, {"a", "\u00e5", "c"}, {"a", "b", "\u00e5"}, {"a\xff", "b", "c"}, {"a\xfe", "b", "c"}} {
		in := toolchain.CacheDiscriminatorInput{Tools: tuple[0], InstallDevTools: tuple[1], ExtraCachePaths: tuple[2]}

		key := toolchain.CacheDiscriminator(in)
		if seen[key] || key != toolchain.CacheDiscriminator(in) {
			t.Fatalf("tuple collision/instability: %q", tuple)
		}

		seen[key] = true
	}
}

func TestRuntimeCompletenessBoundary_AllBackendsAndFiles(t *testing.T) { //nolint:gocognit // one table covers both filenames, every backend/runtime pair and the two-part rustup alternative.
	t.Parallel()

	for _, file := range []string{".mise.toml", "mise.toml"} {
		for _, pair := range [][2]string{{"pipx:checker", "uv"}, {"go:example.com/checker", "go"}, {"cargo:checker", "rust"}} {
			for _, place := range []string{"missing", "env", "tools", "other file"} {
				root := t.TempDir()
				writeToolchainFile(t, root, "mise.lock", "# owned lock\n")

				config := "[tools]\n'" + pair[0] + "'='1.2.3'\n"
				if place == "env" {
					config += "[env]\n" + pair[1] + "='not a runtime'\n"
				}

				if place == "tools" {
					config += pair[1] + "='1.2.3'\n"
				}

				writeToolchainFile(t, root, file, config)

				if place == "other file" {
					other := "mise.toml"
					if file == other {
						other = ".mise.toml"
					}

					writeToolchainFile(t, root, other, "[tools]\n"+pair[1]+"='1.2.3'\n")
				}

				err := toolchain.ValidateMiseInstall(toolchain.ValidateMiseInstallInput{Root: root, Locked: "true"})

				valid := place == "tools" || place == "other file"
				if valid && err != nil || !valid && !errors.Is(err, errs.ErrValidation) {
					t.Fatalf("%s %v %s err=%v", file, pair, place, err)
				}
			}
		}
	}

	for _, file := range []string{".mise.toml", "mise.toml"} {
		for _, state := range []string{"complete", "no rustup", "no toolchain", "unrelated rustup"} {
			root := t.TempDir()
			writeToolchainFile(t, root, "mise.lock", "# owned lock\n")

			config := "[tools]\n'cargo:checker'='1.2.3'\n"
			if state == "unrelated rustup" {
				config += "[env]\n"
			}

			if state != "no rustup" {
				config += "'aqua:rust-lang/rustup'='1.28.2'\n"
			}

			writeToolchainFile(t, root, file, config)

			if state != "no toolchain" {
				writeToolchainFile(t, root, "rust-toolchain.toml", "[toolchain]\nchannel='1.90.0'\n")
			}

			err := toolchain.ValidateMiseInstall(toolchain.ValidateMiseInstallInput{Root: root, Locked: "true"})
			if state == "complete" && err != nil || state != "complete" && !errors.Is(err, errs.ErrValidation) {
				t.Fatalf("%s %s err=%v", file, state, err)
			}
		}
	}
}

type failedEnumeration struct{ calls []string }

func (f *failedEnumeration) Run(_ context.Context, _ []string, args ...string) (string, error) {
	f.calls = append(f.calls, strings.Join(args, " "))

	return "", errTransientMise
}
func TestExposureBoundary_EnumerationFailurePreservesState(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeToolchainFile(t, root, "PATH", "prior-path\n")

	binHome := filepath.Join(root, "bin")
	if err := os.Mkdir(binHome, 0o700); err != nil {
		t.Fatal(err)
	}

	if err := os.Symlink("old-tool", filepath.Join(binHome, "tool")); err != nil {
		t.Fatal(err)
	}

	runner := &failedEnumeration{}

	var out bytes.Buffer

	err := toolchain.ExposeMiseTools(t.Context(), runner, &out, toolchain.ExposeMiseToolsInput{Root: root, Locked: "false", PathFile: filepath.Join(root, "PATH"), BinHome: binHome})
	if !errors.Is(err, errTransientMise) || !errors.Is(err, errs.ErrDependencyUnavailable) || len(runner.calls) != 1 || out.Len() != 0 {
		t.Fatalf("err=%v calls=%v out=%s", err, runner.calls, &out)
	}

	if string(readFileOrFail(t, filepath.Join(root, "PATH"))) != "prior-path\n" {
		t.Fatal("PATH changed")
	}

	if target, err := os.Readlink(filepath.Join(binHome, "tool")); err != nil || target != "old-tool" {
		t.Fatal("known-good link changed")
	}
}

func TestSetupPathBoundary_ExactAppendPolicyAndRefusals(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	root := t.TempDir()
	pathFile, envFile := filepath.Join(root, "PATH"), filepath.Join(root, "ENV")
	writeToolchainFile(t, root, "PATH", "prior\n")
	writeToolchainFile(t, root, "ENV", "PRIOR=value\n")

	in := toolchain.SetupMiseEnvInput{Cache: "true", BinHome: filepath.Join(root, "bin"), PathFile: pathFile, EnvFile: envFile, RunnerTemp: root}
	for _, change := range []func(*toolchain.SetupMiseEnvInput){func(in *toolchain.SetupMiseEnvInput) { in.Cache = "invalid" }, func(in *toolchain.SetupMiseEnvInput) { in.PathFile = "" }, func(in *toolchain.SetupMiseEnvInput) { in.EnvFile = "" }} {
		bad := in
		change(&bad)

		if err := toolchain.SetupMiseEnv(bad); !errors.Is(err, errs.ErrUsage) {
			t.Fatalf("err=%v", err)
		}

		if string(readFileOrFail(t, pathFile)) != "prior\n" || string(readFileOrFail(t, envFile)) != "PRIOR=value\n" {
			t.Fatal("refusal changed runner files")
		}
	}

	addition := in.BinHome + "\n" + filepath.Join(home, ".cargo/bin") + "\n"
	for count := 1; count <= 2; count++ {
		if err := toolchain.SetupMiseEnv(in); err != nil {
			t.Fatal(err)
		}

		if got := string(readFileOrFail(t, pathFile)); got != "prior\n"+strings.Repeat(addition, count) {
			t.Fatalf("PATH=%q", got)
		}

		if got := string(readFileOrFail(t, envFile)); got != "PRIOR=value\n"+strings.Repeat("MISE_CONFIG_DIR="+filepath.Join(root, "setup-toolchain-mise-config")+"\n", count) {
			t.Fatalf("ENV=%q", got)
		}
	}

	if info, err := os.Stat(in.BinHome); err != nil || !info.IsDir() {
		t.Fatalf("bin home=%v err=%v", info, err)
	}
}
