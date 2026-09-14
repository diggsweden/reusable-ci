// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package toolchain_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/app/toolchain"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
)

func TestRustPin_RefusesBeforeEffects(t *testing.T) { //nolint:gocognit,gocyclo // every invalid document shares all public entries and complete no-effect assertions.
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("HOME", home)
	t.Setenv("PATH", home)
	// A valid cwd decoy must not substitute for the selected root's evidence.
	writeToolchainFile(t, home, "rust-toolchain.toml", "[toolchain]\nchannel='1.90.0'\n")
	t.Chdir(home)

	cases := []struct {
		name, body string
		want       error
	}{
		{"malformed", "[toolchain\nchannel='1.90.0'\n", errs.ErrMalformedInput},
		{"duplicate channel", "[toolchain]\nchannel='1.90.0'\nchannel='1.91.0'\n", errs.ErrMalformedInput},
		{"duplicate table", "[toolchain]\nchannel='1.90.0'\n[toolchain]\n", errs.ErrMalformedInput},
		{"wrong table type", "toolchain='1.90.0'\n", errs.ErrValidation},
		{"array table", "[[toolchain]]\nchannel='1.90.0'\n", errs.ErrValidation},
		{"integer channel", "[toolchain]\nchannel=190\n", errs.ErrValidation},
		{"boolean channel", "[toolchain]\nchannel=true\n", errs.ErrValidation},
		{"array channel", "[toolchain]\nchannel=['1.90.0']\n", errs.ErrValidation},
		{"table channel", "[toolchain.channel]\nversion='1.90.0'\n", errs.ErrValidation},
		{"empty file", "", errs.ErrValidation},
		{"comment decoy", "# [toolchain]\n# channel='1.90.0'\n", errs.ErrValidation},
		{"wrong table", "[other]\nchannel='1.90.0'\n", errs.ErrValidation},
		{"top-level channel", "channel='1.90.0'\n", errs.ErrValidation},
		{"case-sensitive table", "[Toolchain]\nchannel='1.90.0'\n", errs.ErrValidation},
		{"case-sensitive channel", "[toolchain]\nChannel='1.90.0'\n", errs.ErrValidation},
		{"missing channel", "[toolchain]\ncomponents=['clippy']\n", errs.ErrValidation},
		{"path instead of pin", "[toolchain]\npath='./local-rust'\n", errs.ErrValidation},
		{"NUL", `[toolchain]
channel="1.90.0\u0000"
`, errs.ErrValidation},
		{"DEL", `[toolchain]
channel="1.90.0\u007f"
`, errs.ErrValidation},
		{"C1 control", `[toolchain]
channel="1.90.0\u0085"
`, errs.ErrValidation},
	}
	channels := []string{
		"", "stable", "beta", "nightly", "latest", "stable-x86_64-unknown-linux-gnu", "nightly-msvc",
		"1", "1.90", "01.90.0", "1.090.0", "1.90.00", "v1.90.0", "=1.90.0", "^1.90.0", "~1.90.0",
		"1.90.0-rc1", "1.90.0-beta", "1.90.0-alpha2", "1.90.0-rc.1", "1.90.0+build",
		" 1.90.0", "1.90.0 ", "1.90. 0", "1.90.0\n", "1.90.0\r", "1.90.0\t", "1.90.0\u00a0",
		"1.90.0/extra", `1.90.0\extra`, "1.90.0:extra", "1.90.0;extra", "1.90.0@extra",
		"nightly-2026-9-01", "nightly-2026-09", "nightly-2026-13-01", "nightly-2026-02-29", "beta-2026-04-31",
		"stable-0000-01-01", "1.90.0-2026-02-30", "nightly-2024-02-29-", "1.90.0--msvc",
		"1.90.0-x86_64-unknown-linux-gnu\n", "nightly-2024-02-29-x86_64/unknown-linux-gnu",
	}

	cases = slices.Grow(cases, len(channels))
	for _, channel := range channels {
		cases = append(cases, struct {
			name, body string
			want       error
		}{strconv.Quote(channel), "[toolchain]\nchannel=" + strconv.Quote(channel) + "\n", errs.ErrValidation})
	}

	for _, tc := range cases {
		for _, locked := range []string{"false", "true"} {
			t.Run(tc.name+"/locked="+locked, func(t *testing.T) {
				root, rootErr := filepath.EvalSymlinks(t.TempDir())
				if rootErr != nil {
					t.Fatal(rootErr)
				}

				writeToolchainFile(t, root, ".mise.toml", "[tools]\nuv='0.9.0'\ngo='1.26.6'\nrust='1.90.0'\n'aqua:rust-lang/rustup'='1.28.2'\n'cargo:checker'='1.2.3'\n")
				writeToolchainFile(t, root, "mise.lock", "# owned lock\n")
				writeToolchainFile(t, root, "rust-toolchain.toml", tc.body)
				writeToolchainFile(t, root, "PATH", "prior PATH\n")
				writeToolchainFile(t, root, "ENV", "PRIOR=keep\n")

				binHome := filepath.Join(root, "bin")
				if locked == "true" {
					writeToolchainFile(t, binHome, "keep", "existing destination\n")
				}

				runner := &recordingMiseRunner{}

				var out bytes.Buffer
				out.WriteString("prior output\n")

				before := exposureSnapshot(t, root)
				for _, entry := range []struct {
					name string
					run  func() error
				}{
					{"validate", func() error {
						return toolchain.ValidateMiseInstall(toolchain.ValidateMiseInstallInput{Root: root, Locked: locked, GithubTokenPresent: true})
					}},
					{"install", func() error {
						return toolchain.InstallMiseTools(t.Context(), runner, &out, toolchain.InstallMiseToolsInput{Root: root, Locked: locked, Tools: "cargo:checker", RetryAttempts: 1, RetryDelay: -1})
					}},
					{"expose", func() error {
						return toolchain.ExposeMiseTools(t.Context(), runner, &out, toolchain.ExposeMiseToolsInput{Root: root, Locked: locked, BinHome: binHome, PathFile: filepath.Join(root, "PATH")})
					}},
				} {
					t.Run(entry.name, func(t *testing.T) {
						runner.calls = nil

						out.Reset()
						out.WriteString("prior output\n")

						got := entry.run()
						if !errors.Is(got, tc.want) || !strings.Contains(got.Error(), "rust-toolchain.toml") {
							t.Errorf("Rust pin refusal = %v, want %v naming evidence", got, tc.want)
						}

						if len(runner.calls) != 0 || out.String() != "prior output\n" {
							t.Errorf("Rust pin refusal reached effects: calls=%d output=%q", len(runner.calls), &out)
						}

						if after := exposureSnapshot(t, root); before != after {
							t.Error("Rust pin refusal changed owned bytes, modes, or entries")
						}
					})
				}
			})
		}
	}
}

func TestRustPin_ImmutableAndOptionalControls(t *testing.T) { //nolint:gocognit,gocyclo // independent request vectors and publication state cover pins and optional Rust through all entries.
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv("HOME", home)
	t.Setenv("PATH", home)

	cases := []struct {
		name, body string
		rustup     bool
	}{
		{"exact stable", "[toolchain]\nchannel='1.90.0'\n", true},
		{"additive fields", "[toolchain]\nchannel='1.90.0'\ncomponents=['clippy','rustfmt']\ntargets=['wasm32-unknown-unknown']\nprofile='minimal'\n", true},
		{"dotted key", "toolchain.channel='1.90.0'\n", true},
		{"inline table", "toolchain={channel='1.90.0', profile='minimal'}\n", true},
		{"quoted keys", "['toolchain']\n'channel'='1.90.0'\n", true},
		{"escaped stable", "[toolchain]\nchannel=\"1.90.\\u0030\"\n", true},
		{"dated nightly", "[toolchain]\nchannel='nightly-2024-02-29'\n", true},
		{"dated beta", "[toolchain]\nchannel='beta-2026-09-01'\n", true},
		{"dated stable", "[toolchain]\nchannel='stable-2026-09-01'\n", true},
		{"dated version", "[toolchain]\nchannel='1.90.0-2025-09-18'\n", true},
		{"linux host", "[toolchain]\nchannel='1.90.0-x86_64-unknown-linux-gnu'\n", true},
		{"apple host", "[toolchain]\nchannel='1.90.0-aarch64-apple-darwin'\n", true},
		{"dated host", "[toolchain]\nchannel='nightly-2024-02-29-x86_64-pc-windows-msvc'\n", true},
		{"partial host", "[toolchain]\nchannel='1.90.0-msvc'\n", true},
		{"partial arch", "[toolchain]\nchannel='1.90.0-x86_64'\n", true},
		{"no Rust", "", false},
		{"rustup without toolchain", "", true},
		{"toolchain without rustup", "[toolchain]\nchannel='1.90.0'\n", false},
	}
	for _, tc := range cases {
		for _, locked := range []string{"false", "true"} {
			t.Run(tc.name+"/locked="+locked, func(t *testing.T) {
				root, rootErr := filepath.EvalSymlinks(t.TempDir())
				if rootErr != nil {
					t.Fatal(rootErr)
				}

				if tc.rustup {
					writeToolchainFile(t, root, "mise.toml", "[tools]\n'aqua:rust-lang/rustup'='1.28.2'\n")
				}

				if tc.body != "" {
					writeToolchainFile(t, root, "rust-toolchain.toml", tc.body)
				}

				writeToolchainFile(t, root, "mise.lock", "# owned lock\n")
				writeToolchainFile(t, root, "PATH", "prior PATH\n")
				cargoBin := filepath.Join(root, "source", "bin")
				writeExecutable(t, filepath.Join(cargoBin, "cargo"))

				before := exposureSnapshot(t, root)
				if validateErr := toolchain.ValidateMiseInstall(toolchain.ValidateMiseInstallInput{Root: root, Locked: locked, GithubTokenPresent: tc.rustup}); validateErr != nil {
					t.Fatalf("immutable/optional validation: %v", validateErr)
				}

				flags := []string{}
				if locked == "true" {
					flags = append(flags, "--locked")
				}

				install := append([]string{"install"}, flags...)
				execArgs := append([]string{"exec", "--no-deps"}, flags...)
				execArgs = append(execArgs, "aqua:rust-lang/rustup", "--", "rustup")
				which := append(slices.Clone(execArgs), "which", "cargo")
				runner := &recordingMiseRunner{responses: map[string]string{strings.Join(which, "\x00"): filepath.Join(cargoBin, "cargo")}}

				var out bytes.Buffer
				if installErr := toolchain.InstallMiseTools(t.Context(), runner, &out, toolchain.InstallMiseToolsInput{Root: root, Locked: locked, RetryAttempts: 1, RetryDelay: -1}); installErr != nil {
					t.Fatalf("immutable/optional install: %v", installErr)
				}

				var want [][]string
				if tc.rustup {
					want = append(want, append(slices.Clone(install), "aqua:rust-lang/rustup"))
					if tc.body != "" {
						want = append(want, append(slices.Clone(execArgs), "show"), which)
					}
				}

				want = append(want, install)

				if out.Len() != 0 || exposureSnapshot(t, root) != before {
					t.Error("recorded install changed output or owned evidence")
				}

				binHome := filepath.Join(root, "published")
				if exposeErr := toolchain.ExposeMiseTools(t.Context(), runner, nil, toolchain.ExposeMiseToolsInput{Root: root, Locked: locked, BinHome: binHome, PathFile: filepath.Join(root, "PATH")}); exposeErr != nil {
					t.Fatalf("immutable/optional exposure: %v", exposeErr)
				}

				want = append(want, []string{"bin-paths"})
				if tc.rustup && tc.body != "" {
					want = append(want, which)

					if target, linkErr := os.Readlink(filepath.Join(binHome, "cargo")); linkErr != nil || target != filepath.Join(cargoBin, "cargo") {
						t.Errorf("Rust pin exposure target=%q err=%v", target, linkErr)
					}

					if got := string(readFileOrFail(t, filepath.Join(root, "PATH"))); got != "prior PATH\n"+cargoBin+"\n" {
						t.Errorf("Rust pin PATH=%q", got)
					}
				} else if exposureSnapshot(t, root) != before {
					t.Error("optional Rust unexpectedly published files")
				}

				if len(runner.calls) != len(want) {
					t.Fatalf("Rust pin calls=%d, want %d", len(runner.calls), len(want))
				}

				for i, call := range runner.calls {
					if call.dir != root || !slices.Equal(call.args, want[i]) {
						t.Errorf("Rust pin call[%d] root=%q args=%q; want root=%q args=%q", i, call.dir, call.args, root, want[i])
					}
				}
			})
		}
	}
}
