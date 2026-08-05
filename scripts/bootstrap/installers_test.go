// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package bootstrap

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"

	"github.com/diggsweden/reusable-ci/v3/internal/testutil/testenv"
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
	//nolint:gosec,noctx // test infra; script/body are caller-supplied test fixtures.
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

func TestSecurityToolInstallersUsePinnedReleaseAssets(t *testing.T) {
	mutableURL := regexp.MustCompile(`(?:raw\.)?github(?:usercontent)?\.com/.+/(?:main|master)/`)
	for _, script := range []string{"install-trivy.sh", "install-syft.sh"} {
		body, err := os.ReadFile(script) //nolint:gosec // repository fixture.
		if err != nil {
			t.Fatal(err)
		}
		text := string(body)
		if mutableURL.MatchString(text) {
			t.Errorf("%s contains a mutable upstream branch URL", script)
		}
		if !strings.Contains(text, "/releases/download/${") || !strings.Contains(text, "SHA256_") {
			t.Errorf("%s must download a versioned release asset with an in-repo SHA-256 pin", script)
		}
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
		"PATH":                       binDir + string(os.PathListSeparator) + os.Getenv("PATH"), //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		"REUSABLE_CI_BINARY_REF":     "v9.9.9",                                                  //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
		"REUSABLE_CI_INSTALL_DIR":    installDir,                                                //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
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
	writeReusableCITarball(t, tarballPath, binaryBody)

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
	writeFakeCurl(t, binDir, releaseRoot)

	cosignLog := filepath.Join(t.TempDir(), "cosign-args")
	writeFakeCosign(t, binDir, cosignLog)

	installDir := filepath.Join(t.TempDir(), "install")
	githubPath := filepath.Join(t.TempDir(), "github-path")

	stdout, stderr, err := runSourcedScript(t, "install-reusable-ci.sh", `install_reusable_ci "$REUSABLE_CI_BINARY_REF"`, map[string]string{
		"PATH":                         binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
		"REUSABLE_CI_BINARY_REF":       "v3.4.5",
		"REUSABLE_CI_INSTALL_DIR":      installDir,
		"REUSABLE_CI_RELEASE_URL_BASE": "file://" + releaseRoot,
		"GITHUB_PATH":                  githubPath,
	})
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
	writeReusableCITarball(t, tarballPath, []byte("genuine\n"))
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
	writeFakeCurl(t, binDir, releaseRoot)
	writeFakeCosign(t, binDir, filepath.Join(t.TempDir(), "cosign-args"))
	// Provide a fake go so the test can prove verification failure never falls
	// back to source installation.
	logPath := filepath.Join(t.TempDir(), "go-args")
	writeFakeGo(t, binDir, logPath)

	installDir := filepath.Join(t.TempDir(), "install")
	_, stderr, err := runSourcedScript(t, "install-reusable-ci.sh", `install_reusable_ci "$REUSABLE_CI_BINARY_REF"`, map[string]string{
		"PATH":                         binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
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
	writeReusableCITarball(t, tarballPath, []byte("genuine\n"))

	checksums := sha256sumFile(t, tarballPath) + "  " + tarballName + "\n"
	if err := os.WriteFile(filepath.Join(releaseDir, "checksums.txt"), []byte(checksums), 0o644); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}
	// Deliberately NO checksums.txt.bundle.

	binDir := t.TempDir()
	writeFakeCurl(t, binDir, releaseRoot)
	writeFakeCosign(t, binDir, filepath.Join(t.TempDir(), "cosign-args"))

	goLog := filepath.Join(t.TempDir(), "go-args")
	writeFakeGo(t, binDir, goLog)

	installDir := filepath.Join(t.TempDir(), "install")

	stdout, stderr, err := runSourcedScript(t, "install-reusable-ci.sh", `install_reusable_ci "$REUSABLE_CI_BINARY_REF"`, map[string]string{
		"PATH":                         binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
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

func writeReusableCITarball(t *testing.T, path string, body []byte) {
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

	hdr := &tar.Header{Name: "reusable-ci", Mode: 0o755, Size: int64(len(body))}
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

func writeFakeCurl(t *testing.T, dir, releaseRoot string) {
	t.Helper()
	// curl -sSfL -o <dest> <url>: the script always uses a file:// URL
	// pointing at releaseRoot. The fake copies the corresponding file.
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
src="${url#file://}"
cp "$src" "$dest"
`
	if err := os.WriteFile(filepath.Join(dir, "curl"), []byte(body), 0o755); err != nil { //nolint:gosec // test fixture
		t.Fatal(err)
	}

	_ = releaseRoot // referenced only via the file:// URL constructed by the script
}

func TestInstallReusableCI_LocalRef(t *testing.T) {
	binDir := t.TempDir()
	logPath := filepath.Join(t.TempDir(), "go-args")
	writeFakeGo(t, binDir, logPath)
	installDir := filepath.Join(t.TempDir(), "install")

	stdout, stderr, err := runSourcedScript(t, "install-reusable-ci.sh", `install_reusable_ci local`, map[string]string{
		"PATH":                    binDir + string(os.PathListSeparator) + os.Getenv("PATH"),
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

// writeFakeCosign stages a cosign stand-in that logs its argv and
// exits 0, so the fail-closed signature gate can be exercised
// hermetically (no keys, no Fulcio, no network).
func writeFakeCosign(t *testing.T, dir, logPath string) {
	t.Helper()

	body := `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" > "` + logPath + `"
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

func TestInstallerResolvers(t *testing.T) {
	tests := []resolverCase{
		{
			name:            "gh_linux_amd64",
			script:          "install-gh.sh",                                    //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			function:        `printf "%s\n" "$(resolve_gh_dist)"`,               //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			env:             map[string]string{"OS": "Linux", "ARCH": "x86_64"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			wantStdoutRegex: `^gh_.*_linux_amd64\.tar\.gz\n?$`,
		},
		{
			name:            "gh_linux_arm64",
			script:          "install-gh.sh",
			function:        `printf "%s\n" "$(resolve_gh_dist)"`,
			env:             map[string]string{"OS": "Linux", "ARCH": "aarch64"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			wantStdoutRegex: `^gh_.*_linux_arm64\.tar\.gz\n?$`,
		},
		{
			name:            "gh_darwin_arm64",
			script:          "install-gh.sh",
			function:        `printf "%s\n" "$(resolve_gh_dist)"`,
			env:             map[string]string{"OS": "Darwin", "ARCH": "arm64"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			wantStdoutRegex: `^gh_.*_macOS_arm64\.zip\n?$`,
		},
		{
			name:               "gh_unsupported",
			script:             "install-gh.sh",
			function:           `resolve_gh_dist`,
			env:                map[string]string{"OS": "FreeBSD", "ARCH": "x86_64"}, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			wantErr:            true,
			wantStderrContains: "Unsupported gh platform",
		},
		{
			name:            "glab_linux_amd64",
			script:          "install-glab.sh", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
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
			script:          "install-git-cliff.sh", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
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
			script:     "install-yq.sh",                      //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			function:   `printf "%s\n" "$(resolve_yq_dist)"`, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
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
			script:     "install-mise.sh",                      //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			function:   `printf "%s\n" "$(resolve_mise_dist)"`, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
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
			script:     "install-publiccode-parser.sh",                      //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
			function:   `printf "%s\n" "$(resolve_publiccode_parser_dist)"`, //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
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
			script:     "install-opengrep.sh", //nolint:goconst // test fixture / generic identifier — extracting would explode setup boilerplate.
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
