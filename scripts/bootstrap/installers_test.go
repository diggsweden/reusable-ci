// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package bootstrap

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"cmp"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/container"
)

func runSourcedScript(t *testing.T, script, body string, env map[string]string) (string, string, error) {
	t.Helper()
	root := t.TempDir()

	tools := filepath.Join(root, "tools")
	if err := os.Mkdir(tools, 0o700); err != nil {
		t.Fatal(err)
	}
	// Only file utilities and explicitly supplied fixture binaries are visible;
	// no ambient installer, credentials, or shell initialization is inherited.
	for _, tool := range []string{"bash", "basename", "dirname", "uname", "mktemp", "mkdir", "rm", "cp", "mv", "chmod", "install", "tar", "gzip", "unzip", "sha256sum", "shasum", "awk", "cut", "tr", "grep", "head", "cat", "ldd"} {
		path, err := exec.LookPath(tool)
		if err != nil {
			continue
		}

		if err := os.Symlink(path, filepath.Join(tools, tool)); err != nil {
			t.Fatal(err)
		}
	}

	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Fatal(err)
	}

	if writeErr := os.WriteFile(filepath.Join(tools, "curl"), []byte("#!/usr/bin/env bash\nprintf 'unexpected download attempt\\n' >&2\nexit 97\n"), 0o700); writeErr != nil { //nolint:gosec // executable fixture inside t.TempDir().
		t.Fatal(writeErr)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, bash, "-c", "source \"$1\"; "+body, "_", script) //nolint:gosec // bounded fixture command with a closed, test-owned environment.
	cmd.WaitDelay = time.Second
	vars := map[string]string{"HOME": root, "TMPDIR": root, "CI_TEMP_DIR": root, "LC_ALL": "C", "PATH": tools}

	for key, value := range env {
		if key == "PATH" {
			value += string(os.PathListSeparator) + tools
		}

		vars[key] = value
	}

	for key, value := range vars {
		cmd.Env = append(cmd.Env, key+"="+value)
	}

	var stdout, stderr bytes.Buffer

	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	err = cmd.Run()

	return stdout.String(), stderr.String(), err
}

func TestVerifiedDownloadRejectsIntegrityFailure(t *testing.T) {
	fixture := filepath.Join(t.TempDir(), "upstream")
	if err := os.WriteFile(fixture, []byte("tampered archive\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	destination := filepath.Join(t.TempDir(), "download")
	body := `curl() { cp "$FIXTURE" "${*: -1}"; }; ci_download_verified https://example.invalid/tool.tar.gz "$DESTINATION" ` + strings.Repeat("0", 64)

	_, stderr, err := runSourcedScript(t, "install-common.sh", body, map[string]string{
		"FIXTURE":     fixture,
		"DESTINATION": destination,
	})
	if err == nil {
		t.Fatal("download with a mismatching pinned checksum succeeded")
	}

	if !strings.Contains(stderr, "SHA-256 mismatch") {
		t.Fatalf("stderr = %q, want SHA-256 mismatch", stderr)
	}

	if _, statErr := os.Stat(destination); !os.IsNotExist(statErr) {
		t.Fatalf("rejected download was not removed: %v", statErr)
	}
}

func TestVerifiedDownloadRejectsMissingPinBeforeNetwork(t *testing.T) {
	destination := filepath.Join(t.TempDir(), "download")
	marker := filepath.Join(t.TempDir(), "curl-called")
	body := `curl() { printf called >"$CURL_MARKER"; }; ci_download_verified https://example.invalid/tool "$DESTINATION" ""`

	_, stderr, err := runSourcedScript(t, "install-common.sh", body, map[string]string{
		"CURL_MARKER": marker,
		"DESTINATION": destination,
	})
	if err == nil || !strings.Contains(stderr, "pinned SHA-256 is required") {
		t.Fatalf("missing pin result: err=%v stderr=%q", err, stderr)
	}

	if _, statErr := os.Stat(marker); !os.IsNotExist(statErr) {
		t.Fatalf("curl ran before pin validation: %v", statErr)
	}
}

// pinnedInstallerCase is one installer on one platform branch. An empty os
// and arch mean Linux x86_64 with glibc.
type pinnedInstallerCase struct{ tool, url, pin, entry, os, arch, libc string }

func TestPinnedInstallers_VerifyExactDownloadBeforeInstalling(t *testing.T) {
	t.Parallel()

	for _, tc := range []pinnedInstallerCase{
		{"trivy", "https://github.com/aquasecurity/trivy/releases/download/v0.69.3/trivy_0.69.3_Linux-64bit.tar.gz", "1816b632dfe529869c740c0913e36bd1629cb7688bd5634f4a858c1d57c88b75", "trivy", "", "", ""},
		{"syft", "https://github.com/anchore/syft/releases/download/v1.45.1/syft_1.45.1_linux_amd64.tar.gz", "20c84195e24927f50a3b2269946be51f4c4abc9d2f145fee7388b4199149f716", "syft", "", "", ""},
		{"cosign", "https://github.com/sigstore/cosign/releases/download/v3.1.3/cosign-linux-amd64", "4629c757b7618056f8ddd7e2625ae9fdd94c0372a65049520bc7d9df9efc7f71", "", "", "", ""},
		{"gh", "https://github.com/cli/cli/releases/download/v2.93.0/gh_2.93.0_linux_amd64.tar.gz", "02d1290eba130e0b896f3709ffff22e1c75a51475ddb70476a85abc6b5807af0", "gh_2.93.0_linux_amd64/bin/gh", "", "", ""},
		{"glab", "https://gitlab.com/gitlab-org/cli/-/releases/v1.74.0/downloads/glab_1.74.0_linux_amd64.tar.gz", "75008e8d57825547d944a3193a66a018188b433a4ecb1cf36500ffbafe4689ff", "root/bin/glab", "", "", ""},
		{"git-cliff", "https://github.com/orhun/git-cliff/releases/download/v2.13.1/git-cliff-2.13.1-x86_64-unknown-linux-gnu.tar.gz", "9a1263f24e59a2f508c7b3d3283c9dea94a8bf697f96dbc18cc783cac6284546", "root/git-cliff", "", "", ""},
		{"yq", "https://github.com/mikefarah/yq/releases/download/v4.52.5/yq_linux_amd64", "75d893a0d5940d1019cb7cdc60001d9e876623852c31cfc6267047bc31149fa9", "", "", "", ""},
		{"mise", "https://github.com/jdx/mise/releases/download/v2026.5.4/mise-v2026.5.4-linux-x64", "96a0eefa1ad8c92461c808e6e07644f95cda830d7719895b98c819c94b0e0b1c", "", "", "", ""},
		{"publiccode-parser", "https://github.com/italia/publiccode-parser-go/releases/download/v5.3.1/publiccode-parser-go_Linux_x86_64.tar.gz", "ed5b75ebe3fc3f6c29f925d8acc132e381769690cada3b40680406b9ec64fedf", "publiccode-parser", "", "", ""},
		{"opengrep", "https://github.com/opengrep/opengrep/releases/download/v1.18.0/opengrep_manylinux_x86", "65390e16db45db1258967578c73eacac6e4ab9cd20074d7dc1aeb695e2696203", "", "", "", ""},
		{"trivy", "https://github.com/aquasecurity/trivy/releases/download/v0.69.3/trivy_0.69.3_Linux-ARM64.tar.gz", "7e3924a974e912e57b4a99f65ece7931f8079584dae12eb7845024f97087bdfd", "trivy", "Linux", "aarch64", ""},
		{"trivy", "https://github.com/aquasecurity/trivy/releases/download/v0.69.3/trivy_0.69.3_macOS-ARM64.tar.gz", "a2f2179afd4f8bb265ca3c7aefb56a666bc4a9a411663bc0f22c3549fbc643a5", "trivy", "Darwin", "arm64", ""},
		{"syft", "https://github.com/anchore/syft/releases/download/v1.45.1/syft_1.45.1_linux_arm64.tar.gz", "7df9f45cba1f6358ecfc7fac349d43b4605137001f9646b41267abe15a7c6cd7", "syft", "Linux", "aarch64", ""},
		{"syft", "https://github.com/anchore/syft/releases/download/v1.45.1/syft_1.45.1_darwin_amd64.tar.gz", "abe6e73b819f433b69ece755dc180a19c7694896062bf806f89d0e3ca5db710a", "syft", "Darwin", "x86_64", ""},
		{"cosign", "https://github.com/sigstore/cosign/releases/download/v3.1.3/cosign-linux-arm64", "c5d324e091826b0d7a78eb16fef316450b4eb9aaec045611c08ba06f5e73220a", "", "Linux", "aarch64", ""},
		{"cosign", "https://github.com/sigstore/cosign/releases/download/v3.1.3/cosign-darwin-arm64", "5cf948c2f4dfe59687bdd0b8523709067383e03982cc543475c8a7dc70e92a76", "", "Darwin", "arm64", ""},
		{"gh", "https://github.com/cli/cli/releases/download/v2.93.0/gh_2.93.0_linux_arm64.tar.gz", "c55feb33684abba57e9909737340d5b39282257c0363e1edde6785ac4a413be7", "gh_2.93.0_linux_arm64/bin/gh", "Linux", "arm64", ""},
		{"glab", "https://gitlab.com/gitlab-org/cli/-/releases/v1.74.0/downloads/glab_1.74.0_linux_arm64.tar.gz", "65f987456b884a9b230895010d24431507e4c9b48bbada4de5400d2d11b0ccee", "root/bin/glab", "Linux", "aarch64", ""},
		{"glab", "https://gitlab.com/gitlab-org/cli/-/releases/v1.74.0/downloads/glab_1.74.0_darwin_arm64.tar.gz", "cc67a9f79079ed9578cd93ca385e3df13975e2ccb03f047c0c6bf51f708af395", "root/bin/glab", "Darwin", "arm64", ""},
		{"git-cliff", "https://github.com/orhun/git-cliff/releases/download/v2.13.1/git-cliff-2.13.1-aarch64-unknown-linux-gnu.tar.gz", "9619b7f0c584229f8a2331c1905afe88bd938bdc9102926c2073836a42f02455", "root/git-cliff", "Linux", "aarch64", ""},
		{"git-cliff", "https://github.com/orhun/git-cliff/releases/download/v2.13.1/git-cliff-2.13.1-x86_64-apple-darwin.tar.gz", "6e60ae390d375cecb9d8008c49f0e724a8dfe40390b532ef5501e421d2cc8acb", "root/git-cliff", "Darwin", "amd64", ""},
		{"yq", "https://github.com/mikefarah/yq/releases/download/v4.52.5/yq_linux_arm", "dea10a4f66646160b592bb8ddaed9fc8c5f13d235474e66c47178cbf2f816a73", "", "Linux", "armv7l", ""},
		{"yq", "https://github.com/mikefarah/yq/releases/download/v4.52.5/yq_darwin_arm64", "45a12e64d4bd8a31c72ee1b889e81f1b1110e801baad3d6f030c111db0068de0", "", "Darwin", "arm64", ""},
		{"mise", "https://github.com/jdx/mise/releases/download/v2026.5.4/mise-v2026.5.4-linux-arm64", "49e2d71d72d68dcb5d554724602d69f33358e1060a0352e5b02b1afcbeecc9b6", "", "Linux", "aarch64", ""},
		{"mise", "https://github.com/jdx/mise/releases/download/v2026.5.4/mise-v2026.5.4-macos-x64", "a0f0c7119180907951542832202fe21fc8a58e78e7605a7cf92467f64848f5e1", "", "Darwin", "x86_64", ""},
		{"publiccode-parser", "https://github.com/italia/publiccode-parser-go/releases/download/v5.3.1/publiccode-parser-go_Linux_arm64.tar.gz", "556b0fe4d8a6e9d2f638baa174212930ae463da6f5911543a55f29018bea78ae", "publiccode-parser", "Linux", "aarch64", ""},
		{"publiccode-parser", "https://github.com/italia/publiccode-parser-go/releases/download/v5.3.1/publiccode-parser-go_Darwin_arm64.tar.gz", "ca7a332ddda144e7f7f5569fe207799298db5fc1fd9129cff312a9ffe658543f", "publiccode-parser", "Darwin", "arm64", ""},
		{"opengrep", "https://github.com/opengrep/opengrep/releases/download/v1.18.0/opengrep_musllinux_x86", "17768ec506bc422ccda3ca3227c2036d77fe5cf0eb6dc0fb010f34ee055c6de6", "", "Linux", "x86_64", "musl"},
		{"opengrep", "https://github.com/opengrep/opengrep/releases/download/v1.18.0/opengrep_osx_arm64", "ee851faef1a555e3389cec3ea46bd05f5c8abba2646aceaca70221eebebb96cb", "", "Darwin", "arm64", ""},
	} {
		if tc.os == "" {
			tc.os, tc.arch, tc.libc = "Linux", "x86_64", "glibc"
		}

		t.Run(strings.Join([]string{tc.tool, tc.os, tc.arch, tc.libc}, "/"), func(t *testing.T) {
			t.Parallel()

			for _, mode := range []installerMode{installMatching, installTampered, installDownloadFails} {
				t.Run(string(mode), func(t *testing.T) {
					checkPinnedInstaller(t, tc, mode)
				})
			}
		})
	}
}

type installerMode string

const (
	installMatching      installerMode = "matching checksum"
	installTampered      installerMode = "different checksum"
	installDownloadFails installerMode = "download fails midway"
)

// requireInstallerFailureCleansUp requires a refused install to report its
// reason and to leave nothing in the tool's directory: no installed binary,
// partial download, archive or extraction.
func requireInstallerFailureCleansUp(t *testing.T, mode installerMode, runErr error, stderr, installDir string) {
	t.Helper()

	// Trivy and Syft report a failed download as a failed install.
	want := "SHA-256 mismatch"
	if mode == installDownloadFails {
		want = "ERROR: Failed to "
	}

	if runErr == nil || !strings.Contains(stderr, want) {
		t.Errorf("%s: err=%v stderr=%q, want %q", mode, runErr, stderr, want)
	}

	if entries, readErr := os.ReadDir(installDir); readErr != nil && !errors.Is(readErr, os.ErrNotExist) || len(entries) != 0 {
		t.Errorf("%s left %v in %s (read error %v)", mode, entries, installDir, readErr)
	}
}

func checkPinnedInstaller(t *testing.T, tc pinnedInstallerCase, mode installerMode) {
	t.Helper()
	root, scriptDir, binDir := t.TempDir(), t.TempDir(), t.TempDir()
	fixture := filepath.Join(root, "download")
	binary := []byte("#!/usr/bin/env bash\nprintf 'GitVersion: v3.1.3\\n{\"version\":\"fixture\"}\\n'\n")
	writeFixture := func(body []byte) {
		if tc.entry != "" {
			writeInstallerTarball(t, fixture, tc.entry, body)
		} else if err := os.WriteFile(fixture, body, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writeFixture(binary)

	pin := sha256sumFile(t, fixture)
	script := "install-" + tc.tool + ".sh"

	body, err := os.ReadFile(script)
	if err != nil {
		t.Fatal(err)
	}

	if strings.Count(string(body), tc.pin) != 1 {
		t.Fatal("reviewed platform pin missing or ambiguous")
	}
	// Substitute only the reviewed literal pin in this owned copy. The real
	// download/checksum code must still validate the fixture bytes.
	body = []byte(strings.Replace(string(body), tc.pin, pin, 1))
	if writeErr := os.WriteFile(filepath.Join(scriptDir, script), body, 0o600); writeErr != nil { //nolint:gosec // fixed repository fixture copied into t.TempDir().
		t.Fatal(writeErr)
	}

	common, err := os.ReadFile("install-common.sh")
	if err != nil {
		t.Fatal(err)
	}

	if writeErr := os.WriteFile(filepath.Join(scriptDir, "install-common.sh"), common, 0o600); writeErr != nil { //nolint:gosec // fixed helper copied into t.TempDir().
		t.Fatal(writeErr)
	}

	if mode == installTampered {
		writeFixture(append(bytes.Clone(binary), []byte("# changed\n")...))
	}

	writeFakeCurl(t, binDir)

	ambientMarker := filepath.Join(root, "ambient-called")

	if tc.tool == "cosign" {
		ambient := "#!/usr/bin/env bash\nprintf called > \"$AMBIENT_MARKER\"\nprintf 'GitVersion: v3.1.3\\n'\n"
		if writeErr := os.WriteFile(filepath.Join(binDir, "cosign"), []byte(ambient), 0o700); writeErr != nil { //nolint:gosec // executable fixture inside t.TempDir().
			t.Fatal(writeErr)
		}
	}

	command := `uname() { case "$1" in -s) printf ` + tc.os + ` ;; -m) printf ` + tc.arch + ` ;; esac; }; ldd() { printf ` + tc.libc + `; }; install_` + strings.ReplaceAll(tc.tool, "-", "_")

	failDownload := ""
	if mode == installDownloadFails {
		failDownload = "1"
	}

	_, stderr, runErr := runSourcedScript(t, filepath.Join(scriptDir, script), command, map[string]string{
		"PATH": binDir, "CI_TEMP_DIR": root, "OS": tc.os, "ARCH": tc.arch,
		"INSTALLER_FIXTURE": fixture, "EXPECTED_DOWNLOAD_URL": tc.url, "AMBIENT_MARKER": ambientMarker,
		"FAKE_CURL_FAIL_AFTER_PARTIAL": failDownload,
	})
	if _, statErr := os.Stat(ambientMarker); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("ambient cosign executed: %v", statErr)
	}

	installDir := filepath.Join(root, tc.tool+"-bin")
	installed := filepath.Join(installDir, tc.tool)

	if mode != installMatching {
		requireInstallerFailureCleansUp(t, mode, runErr, stderr, installDir)

		return
	}

	if runErr != nil {
		t.Fatalf("installation failed: %v stderr=%s", runErr, stderr)
	}

	got, err := os.ReadFile(installed)
	if err != nil || !bytes.Equal(got, binary) {
		t.Errorf("installed bytes differ: %v", err)
	}

	if tc.tool == "git-cliff" {
		archives, globErr := filepath.Glob(filepath.Join(filepath.Dir(installed), "*.tar.gz"))
		if globErr != nil || len(archives) != 0 {
			t.Errorf("git-cliff archive cleanup failed: %v %v", archives, globErr)
		}
	}
}

func TestRuntimeInstallerHashesMatchExactAssets(t *testing.T) {
	tests := []struct {
		name   string
		script string
		body   string
		want   string
	}{
		{name: "cosign linux amd64", script: "install-cosign.sh", body: `resolve_cosign_sha256 cosign-linux-amd64`, want: "4629c757b7618056f8ddd7e2625ae9fdd94c0372a65049520bc7d9df9efc7f71"},
		{name: "cosign linux arm64", script: "install-cosign.sh", body: `resolve_cosign_sha256 cosign-linux-arm64`, want: "c5d324e091826b0d7a78eb16fef316450b4eb9aaec045611c08ba06f5e73220a"},
		{name: "cosign darwin amd64", script: "install-cosign.sh", body: `resolve_cosign_sha256 cosign-darwin-amd64`, want: "2347488e5d5b25336644024dfeca5601b190e91197a71a917bda44744aff106c"},
		{name: "cosign darwin arm64", script: "install-cosign.sh", body: `resolve_cosign_sha256 cosign-darwin-arm64`, want: "5cf948c2f4dfe59687bdd0b8523709067383e03982cc543475c8a7dc70e92a76"},
		{name: "gh amd64", script: "install-gh.sh", body: `resolve_gh_sha256 gh_2.93.0_linux_amd64.tar.gz`, want: "02d1290eba130e0b896f3709ffff22e1c75a51475ddb70476a85abc6b5807af0"},
		{name: "gh arm64", script: "install-gh.sh", body: `resolve_gh_sha256 gh_2.93.0_linux_arm64.tar.gz`, want: "c55feb33684abba57e9909737340d5b39282257c0363e1edde6785ac4a413be7"},
		{name: "gh darwin amd64", script: "install-gh.sh", body: `resolve_gh_sha256 gh_2.93.0_macOS_amd64.zip`, want: "009425b9d175c482037fe25181817fd6b1ea3ae1f51cfae0e18f29f33d3152ac"},
		{name: "gh darwin arm64", script: "install-gh.sh", body: `resolve_gh_sha256 gh_2.93.0_macOS_arm64.zip`, want: "a86be4e0a86c26456cf71177d6572d6f1165cf1679e532b72f7f15918ee51fd2"},
		{name: "glab amd64", script: "install-glab.sh", body: `resolve_glab_sha256 glab_1.74.0_linux_amd64.tar.gz`, want: "75008e8d57825547d944a3193a66a018188b433a4ecb1cf36500ffbafe4689ff"},
		{name: "glab arm64", script: "install-glab.sh", body: `resolve_glab_sha256 glab_1.74.0_linux_arm64.tar.gz`, want: "65f987456b884a9b230895010d24431507e4c9b48bbada4de5400d2d11b0ccee"},
		{name: "glab darwin amd64", script: "install-glab.sh", body: `resolve_glab_sha256 glab_1.74.0_darwin_amd64.tar.gz`, want: "3262db492073ac23318f43de4b59b93463b01ba9d06232d61ff1f02e10082337"},
		{name: "glab darwin arm64", script: "install-glab.sh", body: `resolve_glab_sha256 glab_1.74.0_darwin_arm64.tar.gz`, want: "cc67a9f79079ed9578cd93ca385e3df13975e2ccb03f047c0c6bf51f708af395"},
		{name: "git-cliff amd64", script: "install-git-cliff.sh", body: `resolve_git_cliff_sha256 git-cliff-2.13.1-x86_64-unknown-linux-gnu.tar.gz`, want: "9a1263f24e59a2f508c7b3d3283c9dea94a8bf697f96dbc18cc783cac6284546"},
		{name: "git-cliff arm64", script: "install-git-cliff.sh", body: `resolve_git_cliff_sha256 git-cliff-2.13.1-aarch64-unknown-linux-gnu.tar.gz`, want: "9619b7f0c584229f8a2331c1905afe88bd938bdc9102926c2073836a42f02455"},
		{name: "git-cliff darwin amd64", script: "install-git-cliff.sh", body: `resolve_git_cliff_sha256 git-cliff-2.13.1-x86_64-apple-darwin.tar.gz`, want: "6e60ae390d375cecb9d8008c49f0e724a8dfe40390b532ef5501e421d2cc8acb"},
		{name: "git-cliff darwin arm64", script: "install-git-cliff.sh", body: `resolve_git_cliff_sha256 git-cliff-2.13.1-aarch64-apple-darwin.tar.gz`, want: "21547ae4a0421164070ab75c2522864ea5565858a011fabc5f583061b20f1226"},
		{name: "yq amd64", script: "install-yq.sh", body: `resolve_yq_sha256 yq_linux_amd64`, want: "75d893a0d5940d1019cb7cdc60001d9e876623852c31cfc6267047bc31149fa9"},
		{name: "yq arm64", script: "install-yq.sh", body: `resolve_yq_sha256 yq_linux_arm64`, want: "90fa510c50ee8ca75544dbfffed10c88ed59b36834df35916520cddc623d9aaa"},
		{name: "yq arm", script: "install-yq.sh", body: `resolve_yq_sha256 yq_linux_arm`, want: "dea10a4f66646160b592bb8ddaed9fc8c5f13d235474e66c47178cbf2f816a73"},
		{name: "yq darwin amd64", script: "install-yq.sh", body: `resolve_yq_sha256 yq_darwin_amd64`, want: "6e399d1eb466860c3202d231727197fdce055888c5c7bec6964156983dd1559d"},
		{name: "yq darwin arm64", script: "install-yq.sh", body: `resolve_yq_sha256 yq_darwin_arm64`, want: "45a12e64d4bd8a31c72ee1b889e81f1b1110e801baad3d6f030c111db0068de0"},
		{name: "mise amd64", script: "install-mise.sh", body: `resolve_mise_sha256 mise-v2026.5.4-linux-x64`, want: "96a0eefa1ad8c92461c808e6e07644f95cda830d7719895b98c819c94b0e0b1c"},
		{name: "mise arm64", script: "install-mise.sh", body: `resolve_mise_sha256 mise-v2026.5.4-linux-arm64`, want: "49e2d71d72d68dcb5d554724602d69f33358e1060a0352e5b02b1afcbeecc9b6"},
		{name: "mise darwin amd64", script: "install-mise.sh", body: `resolve_mise_sha256 mise-v2026.5.4-macos-x64`, want: "a0f0c7119180907951542832202fe21fc8a58e78e7605a7cf92467f64848f5e1"},
		{name: "mise darwin arm64", script: "install-mise.sh", body: `resolve_mise_sha256 mise-v2026.5.4-macos-arm64`, want: "61e1825fc2f5ca8fab6415c726451f6fae47d9e8eb915ad345a76fcfc2ff7315"},
		{name: "publiccode amd64", script: "install-publiccode-parser.sh", body: `resolve_publiccode_parser_sha256 publiccode-parser-go_Linux_x86_64.tar.gz`, want: "ed5b75ebe3fc3f6c29f925d8acc132e381769690cada3b40680406b9ec64fedf"},
		{name: "publiccode arm64", script: "install-publiccode-parser.sh", body: `resolve_publiccode_parser_sha256 publiccode-parser-go_Linux_arm64.tar.gz`, want: "556b0fe4d8a6e9d2f638baa174212930ae463da6f5911543a55f29018bea78ae"},
		{name: "publiccode darwin amd64", script: "install-publiccode-parser.sh", body: `resolve_publiccode_parser_sha256 publiccode-parser-go_Darwin_x86_64.tar.gz`, want: "b283c916c5fface7434a530f39eadc452ea65bb9c3bc12e1446a06714905a453"},
		{name: "publiccode darwin arm64", script: "install-publiccode-parser.sh", body: `resolve_publiccode_parser_sha256 publiccode-parser-go_Darwin_arm64.tar.gz`, want: "ca7a332ddda144e7f7f5569fe207799298db5fc1fd9129cff312a9ffe658543f"},
		{name: "opengrep amd64", script: "install-opengrep.sh", body: `resolve_opengrep_sha256 opengrep_manylinux_x86`, want: "65390e16db45db1258967578c73eacac6e4ab9cd20074d7dc1aeb695e2696203"},
		{name: "opengrep arm64", script: "install-opengrep.sh", body: `resolve_opengrep_sha256 opengrep_manylinux_aarch64`, want: "6253329a98bcd804311a17d4d3dfc947722ca904f0adef64f32bc8f9120755ec"},
		{name: "opengrep musl amd64", script: "install-opengrep.sh", body: `resolve_opengrep_sha256 opengrep_musllinux_x86`, want: "17768ec506bc422ccda3ca3227c2036d77fe5cf0eb6dc0fb010f34ee055c6de6"},
		{name: "opengrep musl arm64", script: "install-opengrep.sh", body: `resolve_opengrep_sha256 opengrep_musllinux_aarch64`, want: "2d31bdef1791df2bb8402b62b2c5d2edf097de21beb4ef7ce342cc150896d951"},
		{name: "opengrep darwin amd64", script: "install-opengrep.sh", body: `resolve_opengrep_sha256 opengrep_osx_x86`, want: "2bc2b5c7ce24e9e5171c3ea0b7d86e0d412e8b7d69254dcd72de7791a03566b9"},
		{name: "opengrep darwin arm64", script: "install-opengrep.sh", body: `resolve_opengrep_sha256 opengrep_osx_arm64`, want: "ee851faef1a555e3389cec3ea46bd05f5c8abba2646aceaca70221eebebb96cb"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			stdout, stderr, err := runSourcedScript(t, testCase.script, testCase.body, nil)
			if err != nil || stdout != testCase.want {
				t.Fatalf("resolver result: stdout=%q stderr=%q err=%v, want %q", stdout, stderr, err, testCase.want)
			}
		})
	}
}

func TestRuntimeInstallersFailBeforeNetworkWhenExactAssetHasNoPin(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "curl-called")
	body := `command() {
  if [[ "$1" == -v && "$2" == gh ]]; then return 1; fi
  builtin command "$@"
}
curl() { printf called >"$CURL_MARKER"; }
resolve_gh_dist() { printf 'gh_2.93.0_macOS_ppc64.zip'; }
install_gh`

	_, stderr, err := runSourcedScript(t, "install-gh.sh", body, map[string]string{
		"OS": "Darwin", "ARCH": "ppc64", "CI_TEMP_DIR": t.TempDir(), "CURL_MARKER": marker,
	})
	if err == nil || !strings.Contains(stderr, "no pinned SHA-256") {
		t.Fatalf("unhashed asset result: err=%v stderr=%q", err, stderr)
	}

	if _, statErr := os.Stat(marker); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("network ran before hash resolution: %v", statErr)
	}
}

func TestInstallGH_DarwinUsesZipExtraction(t *testing.T) {
	if _, err := exec.LookPath("unzip"); err != nil {
		t.Skip("unzip not available")
	}

	fixture := filepath.Join(t.TempDir(), "gh.zip")
	writeGHZip(t, fixture, "gh_2.93.0_macOS_arm64")
	installDir := t.TempDir()
	body := `command() {
  if [[ "$1" == -v && "$2" == gh && ! -x "$INSTALL_DIR/gh" ]]; then return 1; fi
  builtin command "$@"
}
ci_install_dir() { printf '%s' "$INSTALL_DIR"; }
ci_download_verified() { cp "$FIXTURE" "$2"; }
install_gh`

	stdout, stderr, err := runSourcedScript(t, "install-gh.sh", body, map[string]string{
		"OS": "Darwin", "ARCH": "arm64", "INSTALL_DIR": installDir, "FIXTURE": fixture,
	})
	if err != nil {
		t.Fatalf("Darwin gh install: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	if _, err := os.Stat(filepath.Join(installDir, "gh")); err != nil {
		t.Fatalf("Darwin ZIP binary was not flattened: %v", err)
	}
}

func writeGHZip(t *testing.T, path, root string) {
	t.Helper()

	file, err := os.Create(path) //nolint:gosec // test fixture under t.TempDir().
	if err != nil {
		t.Fatal(err)
	}

	zw := zip.NewWriter(file)

	entry, err := zw.Create(root + "/bin/gh")
	if err != nil {
		t.Fatal(err)
	}

	if _, err := entry.Write([]byte("#!/usr/bin/env bash\nprintf 'gh test-version\\n'\n")); err != nil {
		t.Fatal(err)
	}

	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestInstallReusableCI_ReleaseRequiresPinnedCosignBootstrap(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "cosign-bootstrap-called")
	body := `install_cosign() { printf called >"$BOOTSTRAP_MARKER"; return 1; }; install_reusable_ci_release v3.4.5 "$INSTALL_DIR"`

	_, stderr, err := runSourcedScript(t, "install-reusable-ci.sh", body, map[string]string{
		"BOOTSTRAP_MARKER": marker,
		"INSTALL_DIR":      filepath.Join(t.TempDir(), "install"),
	})
	if err == nil || !strings.Contains(stderr, "failed to bootstrap") {
		t.Fatalf("bootstrap failure result: err=%v stderr=%q", err, stderr)
	}

	if _, statErr := os.Stat(marker); statErr != nil {
		t.Fatalf("pinned cosign bootstrap was not called: %v", statErr)
	}
}

func TestInstallReusableCIRawDownloadCallersFetchReviewedHelperSet(t *testing.T) {
	t.Parallel()

	root := filepath.Join("..", "..")

	callers := map[string]bool{
		".github/workflows/lint-megalinter.yml": true,
		".github/workflows/lint-nanolinter.yml": true,
		"templates/megalinter.yml":              true,
		"templates/nanolinter.yml":              true,
	}
	for path := range callers {
		body, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(path)))
		if err != nil {
			t.Fatal(err)
		}

		for _, helper := range []string{"install-reusable-ci.sh", "install-common.sh", "install-cosign.sh"} {
			if !strings.Contains(string(body), `"$raw/`+helper+`"`) {
				t.Errorf("%s no longer fetches the reviewed helper %s", path, helper)
			}
		}
	}

	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		ext := filepath.Ext(path)
		if entry.IsDir() || (ext != ".yml" && ext != ".yaml" && ext != ".sh") {
			return nil
		}

		body, err := os.ReadFile(path) //nolint:gosec // repository contract fixture.
		if err != nil {
			return err
		}

		text := string(body)
		if !strings.Contains(text, `"$raw/install-reusable-ci.sh"`) {
			return nil
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}

		if !callers[filepath.ToSlash(rel)] {
			t.Errorf("unreviewed raw installer caller: %s", rel)
		}

		for _, helper := range []string{"install-common.sh", "install-cosign.sh"} {
			if !strings.Contains(text, `"$raw/`+helper+`"`) {
				t.Errorf("%s downloads install-reusable-ci.sh without %s", path, helper)
			}
		}

		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}

func TestInstallReusableCI_RemoteRef_GoInstallPath(t *testing.T) {
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "go-args")
	writeFakeGo(t, binDir, logPath)
	githubPath := filepath.Join(t.TempDir(), "github-path")
	installDir := filepath.Join(t.TempDir(), "install")

	// REUSABLE_CI_USE_GO_INSTALL=1 opts out of the release-asset fast path.
	// Branch refs / SHAs implicitly land here, this test covers that
	// codepath by forcing it for a tagged ref.
	stdout, stderr, err := runSourcedScript(t, "install-reusable-ci.sh", `install_reusable_ci "$REUSABLE_CI_BINARY_REF"`, map[string]string{
		"PATH":                       binDir,
		"REUSABLE_CI_BINARY_REF":     "v9.9.9",   //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		"REUSABLE_CI_INSTALL_DIR":    installDir, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		"REUSABLE_CI_USE_GO_INSTALL": "1",
		"GITHUB_PATH":                githubPath,
	})
	if err != nil {
		t.Fatalf("install_reusable_ci: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	args, err := os.ReadFile(logPath) //nolint:gosec // test fixture
	if err != nil {
		t.Fatal(err)
	}

	if got := strings.TrimSpace(string(args)); got != "install github.com/diggsweden/reusable-ci/v3/cmd/reusable-ci@v9.9.9" {
		t.Fatalf("go args = %q", got)
	}

	pathBody, err := os.ReadFile(githubPath) //nolint:gosec // test fixture
	if err != nil {
		t.Fatal(err)
	}

	if strings.TrimSpace(string(pathBody)) != installDir {
		t.Fatalf("GITHUB_PATH = %q", pathBody)
	}

	if !strings.Contains(stdout, "reusable-ci installed successfully") {
		t.Fatalf("stdout = %s", stdout)
	}
}

//nolint:cyclop // exercises platform/arch matrix of asset names.
func TestInstallReusableCI_ReleaseAssetPath(t *testing.T) {
	if _, err := exec.LookPath("tar"); err != nil {
		t.Skip("tar not available")
	}

	if _, err := exec.LookPath("sha256sum"); err != nil {
		if _, err := exec.LookPath("shasum"); err != nil {
			t.Skip("neither sha256sum nor shasum available")
		}
	}

	// Stage release fixtures: a tarball containing a fake reusable-ci binary
	// plus a checksums.txt with the matching SHA-256.
	releaseRoot := t.TempDir()

	releaseDir := filepath.Join(releaseRoot, "v3.4.5")
	if err := os.MkdirAll(releaseDir, 0o755); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}

	tarballName := osArchTarballName(t)
	binaryBody := []byte("#!/usr/bin/env bash\nprintf 'reusable-ci test-version\\n'\n")
	tarballPath := filepath.Join(releaseDir, tarballName)
	writeInstallerTarball(t, tarballPath, "reusable-ci", binaryBody)

	checksums := sha256sumFile(t, tarballPath) + "  " + tarballName + "\n"
	if err := os.WriteFile(filepath.Join(releaseDir, "checksums.txt"), []byte(checksums), 0o644); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}

	// Signature verification is fail-closed: the release must carry a
	// checksums.txt.bundle and cosign must accept it. The fixture bundle
	// plus a fake cosign (exit 0, argv logged) keep the test hermetic
	// while exercising the signed path's plumbing.
	if err := os.WriteFile(filepath.Join(releaseDir, "checksums.txt.bundle"), []byte("{}"), 0o644); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}

	// Fake curl that returns staged fixtures by filename. Mirrors the
	// `curl -sSfL -o <dest> <url>` invocation in the script.
	binDir := t.TempDir()
	writeFakeCurl(t, binDir)

	cosignLog := filepath.Join(t.TempDir(), "cosign-args")
	writeFakeCosign(t, binDir, cosignLog)

	installDir := filepath.Join(t.TempDir(), "install")
	githubPath := filepath.Join(t.TempDir(), "github-path")

	env := map[string]string{
		"PATH":                     binDir,
		"REUSABLE_CI_BINARY_REF":   "v3.4.5",
		"REUSABLE_CI_INSTALL_DIR":  installDir,
		"RELEASE_FIXTURE_ROOT":     releaseRoot,
		"EXPECTED_COSIGN_IDENTITY": `^https://github.com/diggsweden/reusable-ci/\.github/workflows/release-binary\.yml@refs/heads/main$`,
		"EXPECTED_COSIGN_ISSUER":   "https://token.actions.githubusercontent.com",
		"GITHUB_PATH":              githubPath,
	}

	stdout, stderr, err := runSourcedScript(t, "install-reusable-ci.sh", `bootstrap_reusable_ci_cosign() { :; }; install_reusable_ci "$REUSABLE_CI_BINARY_REF"`, env)
	if err != nil {
		t.Fatalf("install_reusable_ci: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	if !strings.Contains(stdout, "Downloading reusable-ci v3.4.5") {
		t.Errorf("expected download log, got: %s", stdout)
	}

	cosignArgs, err := os.ReadFile(cosignLog) //nolint:gosec // test fixture
	if err != nil {
		t.Fatalf("cosign was not invoked: %v", err)
	}

	for _, want := range []string{"verify-blob", "--bundle", "--new-bundle-format"} {
		if !strings.Contains(string(cosignArgs), want) {
			t.Errorf("cosign argv missing %q: %s", want, cosignArgs)
		}
	}

	got, err := os.ReadFile(filepath.Join(installDir, "reusable-ci")) //nolint:gosec // test fixture
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, binaryBody) {
		t.Errorf("extracted binary mismatch")
	}

	pathBody, err := os.ReadFile(githubPath) //nolint:gosec // test fixture
	if err != nil {
		t.Fatal(err)
	}

	if strings.TrimSpace(string(pathBody)) != installDir {
		t.Fatalf("GITHUB_PATH = %q", pathBody)
	}

	rejectedDir := filepath.Join(t.TempDir(), "rejected")
	env["REUSABLE_CI_INSTALL_DIR"] = rejectedDir
	env["COSIGN_VERIFY_FAIL"] = "1"

	_, stderr, err = runSourcedScript(t, "install-reusable-ci.sh", `bootstrap_reusable_ci_cosign() { :; }; install_reusable_ci "$REUSABLE_CI_BINARY_REF"`, env)
	if err == nil || !strings.Contains(stderr, "cosign verification") {
		t.Errorf("verification refusal: err=%v stderr=%q", err, stderr)
	}

	if _, err := os.Stat(rejectedDir); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("failed verification created an install destination: %v", err)
	}
}

func TestInstallReusableCI_ReleaseRefusalsPreserveExistingState(t *testing.T) {
	t.Parallel()

	for _, phase := range []string{"success", "bootstrap", "archive download", "checksum download", "bundle download", "verification", "checksum", "partial extraction", "binary pin", "copy", "mode", "publish"} {
		t.Run(phase, func(t *testing.T) {
			t.Parallel()
			checkReleaseInstallState(t, phase)
		})
	}
}

func checkReleaseInstallState(t *testing.T, phase string) {
	t.Helper()
	root, work, installDir, binDir := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()

	releaseDir := filepath.Join(root, "v3.4.5")
	if err := os.Mkdir(releaseDir, 0o700); err != nil {
		t.Fatal(err)
	}

	dist := osArchTarballName(t)
	archive := filepath.Join(releaseDir, dist)
	binary := []byte("#!/usr/bin/env bash\nprintf 'new fixture version\\n'\n")
	writeInstallerTarball(t, archive, "reusable-ci", binary)

	checksums := sha256sumFile(t, archive) + "  " + dist + "\n"
	for name, body := range map[string]string{"checksums.txt": checksums, "checksums.txt.bundle": "{}"} {
		writeBootstrapFixture(t, filepath.Join(releaseDir, name), body)
	}

	target := filepath.Join(installDir, "reusable-ci")
	writeBootstrapFixture(t, target, "old binary")

	pathFile := filepath.Join(root, "github-path")
	writeBootstrapFixture(t, pathFile, "existing path\n")
	writeFakeCurl(t, binDir)
	writeFakeCosign(t, binDir, filepath.Join(root, "cosign-args"))
	env := map[string]string{"PATH": binDir, "TMPDIR": work, "CI_TEMP_DIR": work, "RELEASE_FIXTURE_ROOT": root, "REUSABLE_CI_INSTALL_DIR": installDir, "GITHUB_PATH": pathFile, "PHASE_MARKER": filepath.Join(root, "phase-reached")}
	body := `bootstrap_reusable_ci_cosign() { :; }; `
	wantMessage, requireMarker := "", false

	switch phase {
	case "bootstrap":
		body = `bootstrap_reusable_ci_cosign() { mkdir -p "$CI_TEMP_DIR"; printf partial >"$CI_TEMP_DIR/partial"; printf reached >"$PHASE_MARKER"; return 1; }; `
		requireMarker = true
	case "archive download", "checksum download", "bundle download":
		name := map[string]string{"archive download": dist, "checksum download": "checksums.txt", "bundle download": "checksums.txt.bundle"}[phase]
		if err := os.Remove(filepath.Join(releaseDir, name)); err != nil {
			t.Fatal(err)
		}

		wantMessage = name
	case "verification":
		env["COSIGN_VERIFY_FAIL"] = "1"
		wantMessage = "cosign verification"
	case "checksum":
		writeBootstrapFixture(t, filepath.Join(releaseDir, "checksums.txt"), strings.Repeat("0", 64)+"  "+dist+"\n")

		wantMessage = "SHA-256 mismatch"
	case "partial extraction":
		body += `tar() { printf partial >"$4/reusable-ci"; printf reached >"$PHASE_MARKER"; return 1; }; `
		wantMessage, requireMarker = "failed to extract", true
	case "binary pin":
		env["REUSABLE_CI_BINARY_SHA256"] = strings.Repeat("0", 64)
		wantMessage = "does not match REUSABLE_CI_BINARY_SHA256"
	case "copy", "mode", "publish":
		command := map[string]string{"copy": "cp", "mode": "chmod", "publish": "mv"}[phase]
		body += command + `() { printf reached >"$PHASE_MARKER"; return 1; }; `
		requireMarker = true
	}

	_, stderr, runErr := runSourcedScript(t, "install-reusable-ci.sh", body+`install_reusable_ci v3.4.5`, env)
	if phase == "success" {
		if runErr != nil {
			t.Fatalf("valid installation failed: %v %s", runErr, stderr)
		}
	} else if runErr == nil || !strings.Contains(stderr, wantMessage) {
		t.Errorf("refusal: err=%v stderr=%q", runErr, stderr)
	}

	_, markerErr := os.Stat(env["PHASE_MARKER"])
	if requireMarker && markerErr != nil {
		t.Errorf("intended failure phase was not reached: %v", markerErr)
	}

	want := []byte("old binary")
	if phase == "success" {
		want = binary
	}

	got, err := os.ReadFile(target)
	if err != nil || !bytes.Equal(got, want) {
		t.Errorf("installed target changed incorrectly: %v", err)
	}

	entries, err := os.ReadDir(installDir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "reusable-ci" {
		t.Errorf("unexpected install state: %v %v", entries, err)
	}

	entries, err = os.ReadDir(work)
	if err != nil || len(entries) != 0 {
		t.Errorf("temporary state leaked: %v %v", entries, err)
	}

	if phase == "success" {
		return
	}

	info, statErr := os.Stat(target)
	if statErr != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("refusal changed installed mode: %v", statErr)
	}

	paths, err := os.ReadFile(pathFile)
	if err != nil || string(paths) != "existing path\n" {
		t.Errorf("refusal changed runner paths: %v", err)
	}
}

func writeBootstrapFixture(t *testing.T, path, body string) {
	t.Helper()

	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestInstallReusableCI_ShellVerificationCases(t *testing.T) {
	t.Parallel()

	stdout, stderr, err := runSourcedScript(t, "install-reusable-ci_test.sh", "", nil)
	if err != nil || !strings.Contains(stdout, "18 passed, 0 failed") {
		t.Fatalf("shell verification: %v stdout=%s stderr=%s", err, stdout, stderr)
	}
}

func TestInstallerHarness_IsolatesPolicyAndCleansOwnedRoot(t *testing.T) {
	root := t.TempDir()

	tools := t.TempDir()
	if err := os.WriteFile(filepath.Join(tools, "sha256sum"), []byte("#!/bin/sh\nexit 93\n"), 0o700); err != nil { //nolint:gosec // owned executable fixture; the isolated harness must not use it.
		t.Fatal(err)
	}

	_, stderr, err := runSourcedScript(t, "install-reusable-ci_test.sh", "", map[string]string{"PATH": tools, "TMPDIR": root, "REUSABLE_CI_RELEASE_URL_BASE": "https://fixture.invalid/wrong", "REUSABLE_CI_COSIGN_IDENTITY": "wrong-policy", "REUSABLE_CI_BINARY_SHA256": "wrong-policy", "HTTP_PROXY": "https://fixture.invalid"})
	if err != nil {
		t.Fatalf("harness: %v: %s", err, stderr)
	}

	entries, err := os.ReadDir(root)
	if err != nil {
		t.Fatal(err)
	}

	if len(entries) != 0 {
		t.Errorf("harness left %d entries", len(entries))
	}
}

func TestInstallReusableCI_ReleaseAssetTamperingDetected(t *testing.T) {
	if _, err := exec.LookPath("tar"); err != nil {
		t.Skip("tar not available")
	}

	releaseRoot := t.TempDir()

	releaseDir := filepath.Join(releaseRoot, "v3.4.5")
	if err := os.MkdirAll(releaseDir, 0o755); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}

	tarballName := osArchTarballName(t)
	tarballPath := filepath.Join(releaseDir, tarballName)
	writeInstallerTarball(t, tarballPath, "reusable-ci", []byte("genuine\n"))
	// Checksums file claims a hash for unrelated content; verification must fail.
	bogusChecksum := strings.Repeat("0", 64) + "  " + tarballName + "\n"
	if err := os.WriteFile(filepath.Join(releaseDir, "checksums.txt"), []byte(bogusChecksum), 0o644); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}

	// A bundle + fake cosign get the run past the fail-closed signature
	// gate, so the test reaches the SHA-256 check it is actually about.
	if err := os.WriteFile(filepath.Join(releaseDir, "checksums.txt.bundle"), []byte("{}"), 0o644); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}

	binDir := t.TempDir()
	writeFakeCurl(t, binDir)
	writeFakeCosign(t, binDir, filepath.Join(t.TempDir(), "cosign-args"))
	// Provide a fake go so the test can prove verification failure never falls
	// back to source installation.
	logPath := filepath.Join(t.TempDir(), "go-args")
	writeFakeGo(t, binDir, logPath)

	installDir := filepath.Join(t.TempDir(), "install")

	_, stderr, err := runSourcedScript(t, "install-reusable-ci.sh", `bootstrap_reusable_ci_cosign() { :; }; install_reusable_ci "$REUSABLE_CI_BINARY_REF"`, map[string]string{
		"PATH":                         binDir,
		"REUSABLE_CI_BINARY_REF":       "v3.4.5",
		"REUSABLE_CI_INSTALL_DIR":      installDir,
		"REUSABLE_CI_RELEASE_URL_BASE": "file://" + releaseRoot,
	})
	if err == nil {
		t.Fatalf("tampered release unexpectedly installed; stderr=%s", stderr)
	}

	if !strings.Contains(stderr, "SHA-256 mismatch") {
		t.Errorf("expected SHA-256 mismatch error, got stderr: %s", stderr)
	}

	if _, err := os.Stat(logPath); !os.IsNotExist(err) {
		t.Errorf("go install was invoked after checksum failure")
	}
}

// TestInstallReusableCI_UnsignedReleaseRefused pins the fail-closed contract:
// a release without a checksums.txt.bundle terminates installation.
func TestInstallReusableCI_UnsignedReleaseRefused(t *testing.T) {
	if _, err := exec.LookPath("tar"); err != nil {
		t.Skip("tar not available")
	}

	releaseRoot := t.TempDir()

	releaseDir := filepath.Join(releaseRoot, "v3.4.5")
	if err := os.MkdirAll(releaseDir, 0o755); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}

	tarballName := osArchTarballName(t)
	tarballPath := filepath.Join(releaseDir, tarballName)
	writeInstallerTarball(t, tarballPath, "reusable-ci", []byte("genuine\n"))

	checksums := sha256sumFile(t, tarballPath) + "  " + tarballName + "\n"
	if err := os.WriteFile(filepath.Join(releaseDir, "checksums.txt"), []byte(checksums), 0o644); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}
	// Deliberately NO checksums.txt.bundle.

	binDir := t.TempDir()
	writeFakeCurl(t, binDir)
	writeFakeCosign(t, binDir, filepath.Join(t.TempDir(), "cosign-args"))

	goLog := filepath.Join(t.TempDir(), "go-args")
	writeFakeGo(t, binDir, goLog)

	installDir := filepath.Join(t.TempDir(), "install")

	stdout, stderr, err := runSourcedScript(t, "install-reusable-ci.sh", `bootstrap_reusable_ci_cosign() { :; }; install_reusable_ci "$REUSABLE_CI_BINARY_REF"`, map[string]string{
		"PATH":                         binDir,
		"REUSABLE_CI_BINARY_REF":       "v3.4.5",
		"REUSABLE_CI_INSTALL_DIR":      installDir,
		"REUSABLE_CI_RELEASE_URL_BASE": "file://" + releaseRoot,
	})
	if err == nil {
		t.Fatalf("unsigned release unexpectedly installed\nstdout=%s\nstderr=%s", stdout, stderr)
	}

	if !strings.Contains(stderr, "failed to download") || !strings.Contains(stderr, "checksums.txt.bundle") {
		t.Errorf("expected fail-closed unsigned refusal, got stderr: %s", stderr)
	}

	if _, err := os.Stat(goLog); !os.IsNotExist(err) {
		t.Errorf("go install was invoked after signature download failure")
	}
}

// osArchTarballName names the release tarball for the fixture version
// v3.4.5 on the host OS/arch (every installer test stages that version).
func osArchTarballName(t *testing.T) string {
	t.Helper()

	const ref = "v3.4.5"

	osName := "linux"
	if runtime.GOOS == "darwin" {
		osName = "darwin"
	}

	arch := "amd64"
	if runtime.GOARCH == "arm64" { //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		arch = "arm64"
	}

	return "reusable-ci_" + strings.TrimPrefix(ref, "v") + "_" + osName + "_" + arch + ".tar.gz"
}

func writeInstallerTarball(t *testing.T, path, entryName string, body []byte) {
	t.Helper()

	f, err := os.Create(path) //nolint:gosec,varnamelen // test fixture; path is under t.TempDir().
	if err != nil {
		t.Fatal(err)
	}

	defer func() { _ = f.Close() }()

	gz := gzip.NewWriter(f)

	defer func() { _ = gz.Close() }()

	tw := tar.NewWriter(gz)

	defer func() { _ = tw.Close() }()

	hdr := &tar.Header{Name: entryName, Mode: 0o755, Size: int64(len(body))}
	if err := tw.WriteHeader(hdr); err != nil {
		t.Fatal(err)
	}

	if _, err := tw.Write(body); err != nil {
		t.Fatal(err)
	}
}

func sha256sumFile(t *testing.T, path string) string {
	t.Helper()

	body, err := os.ReadFile(path) //nolint:gosec // test fixture
	if err != nil {
		t.Fatal(err)
	}

	sum := sha256.Sum256(body)

	return hex.EncodeToString(sum[:])
}

func writeFakeCurl(t *testing.T, dir string) {
	t.Helper()
	// Recognize only the configured fixture URL; never delegate to a real client.
	body := `#!/usr/bin/env bash
set -euo pipefail
dest=""
url=""
while [ $# -gt 0 ]; do
  case "$1" in
    -o) dest="$2"; shift 2 ;;
    file://*) url="$1"; shift ;;
    -*) shift ;;
    *) url="$1"; shift ;;
  esac
done
if [ -z "$dest" ] || [ -z "$url" ]; then
  echo "fake curl: missing -o or url" >&2
  exit 2
fi
if [[ -n "${INSTALLER_FIXTURE:-}" ]]; then
  [[ "$url" == "$EXPECTED_DOWNLOAD_URL" ]] || { printf 'unexpected download URL\n' >&2; exit 2; }
  src="$INSTALLER_FIXTURE"
elif [[ -n "${RELEASE_FIXTURE_ROOT:-}" ]]; then
  base=https://github.com/diggsweden/reusable-ci/releases/download/
  [[ "$url" == "$base"* ]] || { printf 'unexpected release URL\n' >&2; exit 2; }
  src="$RELEASE_FIXTURE_ROOT/${url#"$base"}"
else
  [[ "$url" == file://* ]] || { printf 'only owned file fixtures are supported\n' >&2; exit 2; }
  src="${url#file://}"
fi
if [[ -n "${FAKE_CURL_FAIL_AFTER_PARTIAL:-}" ]]; then
  head -c 3 "$src" >"$dest"
  exit 18
fi
cp "$src" "$dest"
`
	if err := os.WriteFile(filepath.Join(dir, "curl"), []byte(body), 0o755); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}
}

func TestInstallReusableCI_LocalRef(t *testing.T) {
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "go-args")
	writeFakeGo(t, binDir, logPath)
	installDir := filepath.Join(t.TempDir(), "install")

	stdout, stderr, err := runSourcedScript(t, "install-reusable-ci.sh", `install_reusable_ci local`, map[string]string{
		"PATH":                    binDir,
		"REUSABLE_CI_INSTALL_DIR": installDir,
	})
	if err != nil {
		t.Fatalf("install_reusable_ci local: %v\nstdout=%s\nstderr=%s", err, stdout, stderr)
	}

	args, err := os.ReadFile(logPath) //nolint:gosec // test fixture
	if err != nil {
		t.Fatal(err)
	}

	if got := strings.TrimSpace(string(args)); got != "install ./cmd/reusable-ci" {
		t.Fatalf("go args = %q", got)
	}
}

// writeFakeCosign records arguments, optionally checks the full verification
// contract, and returns the fixture's configured result without external I/O.
func writeFakeCosign(t *testing.T, dir, logPath string) {
	t.Helper()

	body := `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" > "` + logPath + `"
if [[ -n "${EXPECTED_COSIGN_IDENTITY:-}" ]]; then
  [[ "$#" == 9 && "$1" == verify-blob && "$2" == --bundle && "$3" == "${9}.bundle" &&
     "$4" == --new-bundle-format && "$5" == --certificate-identity-regexp &&
     "$6" == "$EXPECTED_COSIGN_IDENTITY" && "$7" == --certificate-oidc-issuer &&
     "$8" == "$EXPECTED_COSIGN_ISSUER" && "$9" == */checksums.txt && -f "$3" && -f "$9" ]] || exit 2
fi
[[ "${COSIGN_VERIFY_FAIL:-0}" != 1 ]]
`
	if err := os.WriteFile(filepath.Join(dir, "cosign"), []byte(body), 0o755); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}
}

func writeFakeGo(t *testing.T, dir, logPath string) {
	t.Helper()

	body := `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" > "` + logPath + `"
mkdir -p "$GOBIN"
cat > "$GOBIN/reusable-ci" <<'EOS'
#!/usr/bin/env bash
printf 'reusable-ci test-version\n'
EOS
chmod +x "$GOBIN/reusable-ci"
`

	path := filepath.Join(dir, "go")
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}
}

// TestInstallerResolvers_EveryPlatformAndAliasHasAPinnedAsset resolves every
// supported OS and architecture spelling for each resolver-based installer to
// its exact release asset and requires that asset to have a pinned SHA-256
// (the pin values themselves are held by TestRuntimeInstallerHashesMatchExactAssets).
// An unsupported architecture on a supported OS, and an unsupported OS, are
// refused before any pin lookup.
func TestInstallerResolvers_EveryPlatformAndAliasHasAPinnedAsset(t *testing.T) {
	t.Parallel()

	type platform struct{ os, arch, libc string }

	linuxAMD64 := []platform{{"Linux", "x86_64", ""}, {"Linux", "amd64", ""}}
	linuxARM64 := []platform{{"Linux", "aarch64", ""}, {"Linux", "arm64", ""}}
	darwinAMD64 := []platform{{"Darwin", "x86_64", ""}, {"Darwin", "amd64", ""}}
	darwinARM64 := []platform{{"Darwin", "arm64", ""}, {"Darwin", "aarch64", ""}}

	for _, tool := range []struct {
		script, resolve, pin, refusal string
		assets                        map[string][]platform
		unsupported                   []platform
	}{
		{script: "install-gh.sh", resolve: "resolve_gh_dist", pin: "resolve_gh_sha256", refusal: "ERROR: Unsupported gh platform: ", assets: map[string][]platform{
			"gh_2.93.0_linux_amd64.tar.gz": linuxAMD64, "gh_2.93.0_linux_arm64.tar.gz": linuxARM64,
			"gh_2.93.0_macOS_amd64.zip": darwinAMD64, "gh_2.93.0_macOS_arm64.zip": darwinARM64,
		}},
		{script: "install-glab.sh", resolve: "resolve_glab_dist", pin: "resolve_glab_sha256", refusal: "ERROR: Unsupported glab platform: ", assets: map[string][]platform{
			"glab_1.74.0_linux_amd64.tar.gz": linuxAMD64, "glab_1.74.0_linux_arm64.tar.gz": linuxARM64,
			"glab_1.74.0_darwin_amd64.tar.gz": darwinAMD64, "glab_1.74.0_darwin_arm64.tar.gz": darwinARM64,
		}},
		{script: "install-git-cliff.sh", resolve: "resolve_git_cliff_dist", pin: "resolve_git_cliff_sha256", refusal: "ERROR: Unsupported git-cliff platform: ", assets: map[string][]platform{
			"git-cliff-2.13.1-x86_64-unknown-linux-gnu.tar.gz": linuxAMD64, "git-cliff-2.13.1-aarch64-unknown-linux-gnu.tar.gz": linuxARM64,
			"git-cliff-2.13.1-x86_64-apple-darwin.tar.gz": darwinAMD64, "git-cliff-2.13.1-aarch64-apple-darwin.tar.gz": darwinARM64,
		}},
		{script: "install-yq.sh", resolve: "resolve_yq_dist", pin: "resolve_yq_sha256", refusal: "ERROR: Unsupported yq platform: ", assets: map[string][]platform{
			"yq_linux_amd64": linuxAMD64, "yq_linux_arm64": linuxARM64,
			"yq_linux_arm":    {{"Linux", "armv6l", ""}, {"Linux", "armv7l", ""}, {"Linux", "arm", ""}},
			"yq_darwin_amd64": darwinAMD64, "yq_darwin_arm64": darwinARM64,
		}, unsupported: []platform{{"Darwin", "armv7l", ""}}},
		{script: "install-mise.sh", resolve: "resolve_mise_dist", pin: "resolve_mise_sha256", refusal: "ERROR: Unsupported mise platform: ", assets: map[string][]platform{
			"mise-v2026.5.4-linux-x64": linuxAMD64, "mise-v2026.5.4-linux-arm64": linuxARM64,
			"mise-v2026.5.4-macos-x64": darwinAMD64, "mise-v2026.5.4-macos-arm64": darwinARM64,
		}},
		{script: "install-publiccode-parser.sh", resolve: "resolve_publiccode_parser_dist", pin: "resolve_publiccode_parser_sha256", refusal: "ERROR: Unsupported publiccode-parser platform: ", assets: map[string][]platform{
			"publiccode-parser-go_Linux_x86_64.tar.gz": linuxAMD64, "publiccode-parser-go_Linux_arm64.tar.gz": linuxARM64,
			"publiccode-parser-go_Darwin_x86_64.tar.gz": darwinAMD64, "publiccode-parser-go_Darwin_arm64.tar.gz": darwinARM64,
		}},
		{script: "install-opengrep.sh", resolve: "resolve_opengrep_dist", pin: "resolve_opengrep_sha256", refusal: "ERROR: Unsupported OpenGrep platform: ", assets: map[string][]platform{
			"opengrep_manylinux_x86": linuxAMD64, "opengrep_manylinux_aarch64": linuxARM64,
			"opengrep_musllinux_x86":     {{"Linux", "x86_64", "musl"}, {"Linux", "amd64", "musl"}},
			"opengrep_musllinux_aarch64": {{"Linux", "aarch64", "musl"}, {"Linux", "arm64", "musl"}},
			"opengrep_osx_x86":           darwinAMD64, "opengrep_osx_arm64": darwinARM64,
		}},
		{script: "install-cosign.sh", resolve: "resolve_cosign_asset", pin: "resolve_cosign_sha256", refusal: "ERROR: unsupported cosign platform: ", assets: map[string][]platform{
			"cosign-linux-amd64": linuxAMD64, "cosign-linux-arm64": linuxARM64,
			"cosign-darwin-amd64": darwinAMD64, "cosign-darwin-arm64": darwinARM64,
		}},
	} {
		t.Run(tool.script, func(t *testing.T) {
			t.Parallel()

			for asset, platforms := range tool.assets {
				for _, target := range platforms {
					libc := cmp.Or(target.libc, "glibc")
					body := `ldd() { printf ` + libc + `; }; dist="$(` + tool.resolve + `)" && printf '%s\n' "$dist" && ` + tool.pin + ` "$dist"`

					stdout, stderr, err := runSourcedScript(t, tool.script, body, map[string]string{"OS": target.os, "ARCH": target.arch})

					resolved, pin, _ := strings.Cut(stdout, "\n")
					if err != nil || resolved != asset || !container.ValidSHA256Hex(pin) {
						t.Errorf("%s/%s/%s: resolved %q with pin %q (err %v, stderr %q), want %s with a pinned SHA-256",
							target.os, target.arch, libc, resolved, pin, err, stderr, asset)
					}
				}
			}

			for _, target := range append(tool.unsupported, platform{"Linux", "riscv64", ""}, platform{"Darwin", "ppc64", ""}, platform{"FreeBSD", "x86_64", ""}) {
				stdout, stderr, err := runSourcedScript(t, tool.script, tool.resolve, map[string]string{"OS": target.os, "ARCH": target.arch})
				// cosign names the architecture after normalizing its aliases.
				if err == nil || stdout != "" || !strings.HasPrefix(stderr, tool.refusal+target.os+"/") || strings.Count(stderr, "\n") != 1 {
					t.Errorf("%s/%s: stdout %q, stderr %q, err %v, want an unsupported-platform refusal", target.os, target.arch, stdout, stderr, err)
				}
			}
		})
	}
}
