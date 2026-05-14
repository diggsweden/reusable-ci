// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: CC0-1.0

package bootstrap

import (
	"bytes"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/internal/testutil/testenv"
)

type resolverCase struct {
	name               string
	script             string
	function           string
	env                map[string]string
	wantStdout         string
	wantStdoutRegex    string
	wantStderrContains string
	wantErr            bool
}

func runSourcedScript(t *testing.T, script, body string, env map[string]string) (string, string, error) {
	t.Helper()
	_ = testenv.New(t)
	cmd := exec.Command("bash", "-c", "source \"$1\"; "+body, "_", script)
	cmd.Env = append([]string{}, os.Environ()...)
	for key, value := range env {
		cmd.Env = append(cmd.Env, key+"="+value)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err := cmd.Run()
	return stdout.String(), stderr.String(), err
}

func TestInstallerResolvers(t *testing.T) {
	tests := []resolverCase{
		{
			name:            "gh_linux_amd64",
			script:          "install-gh.sh",
			function:        `printf "%s\n" "$(resolve_gh_dist)"`,
			env:             map[string]string{"OS": "Linux", "ARCH": "x86_64"},
			wantStdoutRegex: `^gh_.*_linux_amd64\.tar\.gz\n?$`,
		},
		{
			name:            "gh_linux_arm64",
			script:          "install-gh.sh",
			function:        `printf "%s\n" "$(resolve_gh_dist)"`,
			env:             map[string]string{"OS": "Linux", "ARCH": "aarch64"},
			wantStdoutRegex: `^gh_.*_linux_arm64\.tar\.gz\n?$`,
		},
		{
			name:            "gh_darwin_arm64",
			script:          "install-gh.sh",
			function:        `printf "%s\n" "$(resolve_gh_dist)"`,
			env:             map[string]string{"OS": "Darwin", "ARCH": "arm64"},
			wantStdoutRegex: `^gh_.*_macOS_arm64\.zip\n?$`,
		},
		{
			name:               "gh_unsupported",
			script:             "install-gh.sh",
			function:           `resolve_gh_dist`,
			env:                map[string]string{"OS": "FreeBSD", "ARCH": "x86_64"},
			wantErr:            true,
			wantStderrContains: "Unsupported gh platform",
		},
		{
			name:            "glab_linux_amd64",
			script:          "install-glab.sh",
			function:        `printf "%s\n" "$(resolve_glab_dist)"`,
			env:             map[string]string{"OS": "Linux", "ARCH": "x86_64"},
			wantStdoutRegex: `^glab_.*_linux_amd64\.tar\.gz\n?$`,
		},
		{
			name:            "glab_linux_arm64",
			script:          "install-glab.sh",
			function:        `printf "%s\n" "$(resolve_glab_dist)"`,
			env:             map[string]string{"OS": "Linux", "ARCH": "aarch64"},
			wantStdoutRegex: `^glab_.*_linux_arm64\.tar\.gz\n?$`,
		},
		{
			name:               "glab_unsupported",
			script:             "install-glab.sh",
			function:           `resolve_glab_dist`,
			env:                map[string]string{"OS": "FreeBSD", "ARCH": "x86_64"},
			wantErr:            true,
			wantStderrContains: "Unsupported glab platform",
		},
		{
			name:            "git_cliff_linux_amd64",
			script:          "install-git-cliff.sh",
			function:        `printf "%s\n" "$(resolve_git_cliff_dist)"`,
			env:             map[string]string{"OS": "Linux", "ARCH": "x86_64"},
			wantStdoutRegex: `^git-cliff-.*-x86_64-unknown-linux-gnu\.tar\.gz\n?$`,
		},
		{
			name:            "git_cliff_linux_arm64",
			script:          "install-git-cliff.sh",
			function:        `printf "%s\n" "$(resolve_git_cliff_dist)"`,
			env:             map[string]string{"OS": "Linux", "ARCH": "aarch64"},
			wantStdoutRegex: `^git-cliff-.*-aarch64-unknown-linux-gnu\.tar\.gz\n?$`,
		},
		{
			name:               "git_cliff_unsupported",
			script:             "install-git-cliff.sh",
			function:           `resolve_git_cliff_dist`,
			env:                map[string]string{"OS": "FreeBSD", "ARCH": "x86_64"},
			wantErr:            true,
			wantStderrContains: "Unsupported git-cliff platform",
		},
		{
			name:       "yq_linux_amd64",
			script:     "install-yq.sh",
			function:   `printf "%s\n" "$(resolve_yq_dist)"`,
			env:        map[string]string{"OS": "Linux", "ARCH": "x86_64"},
			wantStdout: "yq_linux_amd64\n",
		},
		{
			name:       "yq_linux_arm64",
			script:     "install-yq.sh",
			function:   `printf "%s\n" "$(resolve_yq_dist)"`,
			env:        map[string]string{"OS": "Linux", "ARCH": "aarch64"},
			wantStdout: "yq_linux_arm64\n",
		},
		{
			name:       "yq_darwin_arm64",
			script:     "install-yq.sh",
			function:   `printf "%s\n" "$(resolve_yq_dist)"`,
			env:        map[string]string{"OS": "Darwin", "ARCH": "arm64"},
			wantStdout: "yq_darwin_arm64\n",
		},
		{
			name:               "yq_unsupported",
			script:             "install-yq.sh",
			function:           `resolve_yq_dist`,
			env:                map[string]string{"OS": "FreeBSD", "ARCH": "x86_64"},
			wantErr:            true,
			wantStderrContains: "Unsupported yq platform",
		},
		{
			name:       "mise_linux_amd64",
			script:     "install-mise.sh",
			function:   `printf "%s\n" "$(resolve_mise_dist)"`,
			env:        map[string]string{"OS": "Linux", "ARCH": "x86_64"},
			wantStdout: "mise-v2026.5.4-linux-x64\n",
		},
		{
			name:       "mise_linux_arm64",
			script:     "install-mise.sh",
			function:   `printf "%s\n" "$(resolve_mise_dist)"`,
			env:        map[string]string{"OS": "Linux", "ARCH": "aarch64"},
			wantStdout: "mise-v2026.5.4-linux-arm64\n",
		},
		{
			name:       "mise_darwin_arm64",
			script:     "install-mise.sh",
			function:   `printf "%s\n" "$(resolve_mise_dist)"`,
			env:        map[string]string{"OS": "Darwin", "ARCH": "arm64"},
			wantStdout: "mise-v2026.5.4-macos-arm64\n",
		},
		{
			name:               "mise_unsupported",
			script:             "install-mise.sh",
			function:           `resolve_mise_dist`,
			env:                map[string]string{"OS": "FreeBSD", "ARCH": "x86_64"},
			wantErr:            true,
			wantStderrContains: "Unsupported mise platform",
		},
		{
			name:       "publiccode_linux_amd64",
			script:     "install-publiccode-parser.sh",
			function:   `printf "%s\n" "$(resolve_publiccode_parser_dist)"`,
			env:        map[string]string{"OS": "Linux", "ARCH": "x86_64"},
			wantStdout: "publiccode-parser-go_Linux_x86_64.tar.gz\n",
		},
		{
			name:       "publiccode_linux_arm64",
			script:     "install-publiccode-parser.sh",
			function:   `printf "%s\n" "$(resolve_publiccode_parser_dist)"`,
			env:        map[string]string{"OS": "Linux", "ARCH": "aarch64"},
			wantStdout: "publiccode-parser-go_Linux_arm64.tar.gz\n",
		},
		{
			name:       "publiccode_darwin_arm64",
			script:     "install-publiccode-parser.sh",
			function:   `printf "%s\n" "$(resolve_publiccode_parser_dist)"`,
			env:        map[string]string{"OS": "Darwin", "ARCH": "arm64"},
			wantStdout: "publiccode-parser-go_Darwin_arm64.tar.gz\n",
		},
		{
			name:               "publiccode_unsupported",
			script:             "install-publiccode-parser.sh",
			function:           `resolve_publiccode_parser_dist`,
			env:                map[string]string{"OS": "FreeBSD", "ARCH": "x86_64"},
			wantErr:            true,
			wantStderrContains: "Unsupported publiccode-parser platform",
		},
		{
			name:       "opengrep_linux_manylinux_amd64",
			script:     "install-opengrep.sh",
			function:   `printf "%s\n" "$(resolve_opengrep_dist)"`,
			env:        map[string]string{"OS": "Linux", "ARCH": "x86_64"},
			wantStdout: "opengrep_manylinux_x86\n",
		},
		{
			name:       "opengrep_darwin_arm64",
			script:     "install-opengrep.sh",
			function:   `printf "%s\n" "$(resolve_opengrep_dist)"`,
			env:        map[string]string{"OS": "Darwin", "ARCH": "arm64"},
			wantStdout: "opengrep_osx_arm64\n",
		},
		{
			name:               "opengrep_unsupported",
			script:             "install-opengrep.sh",
			function:           `resolve_opengrep_dist`,
			env:                map[string]string{"OS": "FreeBSD", "ARCH": "x86_64"},
			wantErr:            true,
			wantStderrContains: "Unsupported OpenGrep platform",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			stdout, stderr, err := runSourcedScript(t, testCase.script, testCase.function, testCase.env)
			if testCase.wantErr {
				if err == nil {
					t.Fatalf("expected error, got success: stdout=%q stderr=%q", stdout, stderr)
				}
				if !strings.Contains(stderr, testCase.wantStderrContains) {
					t.Fatalf("stderr = %q, want substring %q", stderr, testCase.wantStderrContains)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v\nstdout=%q\nstderr=%q", err, stdout, stderr)
			}
			if testCase.wantStdout != "" && stdout != testCase.wantStdout {
				t.Fatalf("stdout = %q, want %q", stdout, testCase.wantStdout)
			}
			if testCase.wantStdoutRegex != "" && !regexp.MustCompile(testCase.wantStdoutRegex).MatchString(stdout) {
				t.Fatalf("stdout = %q, want regex %q", stdout, testCase.wantStdoutRegex)
			}
		})
	}
}
