// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package ci

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"maps"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"
)

type liveFixture struct {
	contract     string
	ca           string
	recovery     string
	cleanup      string
	cleanupLog   string
	verifier     string
	evidenceRoot string
	environment  []string
}

var preflightBinary string //nolint:gochecknoglobals // TestMain builds one helper shared by this package's process tests.

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "reusable-ci-live-preflight-test.")
	if err != nil {
		panic(err)
	}

	preflightBinary = filepath.Join(dir, "live-preflight")
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	command := exec.CommandContext(ctx, "go", "build", "-trimpath", "-buildvcs=false", "-o", preflightBinary, "./internal/livetest/preflight") //nolint:gosec // fixed test helper build.

	command.Dir = filepath.Join("..", "..")
	// Offline and pinned: the helper builds from the module cache the test run
	// already has, never downloads a module or a toolchain, and ignores a
	// workspace or GOFLAGS the host happens to set.
	command.Env = append(os.Environ(), "GOPROXY=off", "GOFLAGS=-mod=readonly", "GOTOOLCHAIN=local", "GOWORK=off", "CGO_ENABLED=0")
	if output, buildErr := command.CombinedOutput(); buildErr != nil {
		_ = os.RemoveAll(dir)

		panic(fmt.Sprintf("build live preflight helper: %v\n%s", buildErr, output))
	}

	// Fixtures hard-link this binary, so its mode is the mode they get. It has
	// to keep the execute bit — the whole point is that the fixtures run it —
	// and 0700 is the narrowest mode that allows that.
	if err := os.Chmod(preflightBinary, 0o700); err != nil { //nolint:gosec // an executable fixture cannot be 0600.
		_ = os.RemoveAll(dir)

		panic(fmt.Sprintf("chmod live preflight helper: %v", err))
	}

	code := m.Run()

	cancel()

	_ = os.RemoveAll(dir)

	os.Exit(code)
}

func newLiveFixture(t *testing.T) liveFixture {
	t.Helper()
	dir := t.TempDir()
	ca := filepath.Join(dir, "ca.crt")
	recovery := filepath.Join(dir, "recovery-v2.env")
	cleanupLog := filepath.Join(dir, "cleanup.log")
	cleanup := filepath.Join(dir, "cleanup")
	contract := filepath.Join(dir, "targets.json")
	verifier := filepath.Join(dir, "live-preflight")
	evidenceRoot := filepath.Join(dir, "evidence")

	writeMode(t, ca, testCertificatePEM(t), 0o600)
	writeMode(t, recovery, "recovery fixture\n", 0o600)

	cleanupBody := fmt.Sprintf(`#!/usr/bin/env bash
[[ $# -eq 1 ]] || exit 64
printf '%%s' "$1" >%q
[[ -z "${LIVE_TEST_EVENTS:-}" ]] || printf 'cleanup\n' >>"$LIVE_TEST_EVENTS"
`, cleanupLog)
	writeMode(t, cleanup, cleanupBody, 0o700)

	// Hard-link rather than copy. These tests run in parallel and each execs
	// its own verifier; writing an executable in one goroutine while another
	// forks leaves the forked child holding a write descriptor on it until
	// exec, and a concurrent exec of that file then fails with ETXTBSY
	// ("text file busy"). Linking opens no descriptor at all, so the window
	// does not exist. The link shares TestMain's inode, which nothing writes
	// to after the build; the one test that swaps the verifier renames the
	// link away and creates a new file, leaving that inode untouched.
	if err := os.Link(preflightBinary, verifier); err != nil {
		t.Fatalf("link preflight helper into the fixture: %v", err)
	}

	body, err := os.ReadFile(filepath.Join("..", "..", "internal", "livetest", "testdata", "neutral-targets-v2-compose.json"))
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	value := strings.ReplaceAll(string(body), "/opt/forge-lab/certs/forge-lab-ca.crt", ca)
	value = strings.Replace(value,
		"/home/garga/.local/state/forge-lab/target-cleanup-run-neutral-fixture/cleanup", cleanup, 1)
	value = strings.Replace(value,
		"/tmp/neutral-targets.run-neutral-fixture.recovery.env", recovery, 1)
	value = strings.Replace(value, strings.Repeat("a", sha256.Size*2),
		fmt.Sprintf("%x", sha256.Sum256([]byte(cleanupBody))), 1)
	value = strings.ReplaceAll(value, "2026-08-06T10:00:00Z", now.Add(-time.Minute).Format(time.RFC3339))
	value = strings.Replace(value, "2026-09-05", now.AddDate(0, 0, 10).Format(time.DateOnly), 1)
	writeMode(t, contract, value, 0o600)

	identity := "run=run-neutral-fixture|targets=" +
		"forgejo@https://forgejo.compose.forgelab:8443/fixture-user#resources=rc-," +
		"gitlab@https://gitlab.compose.forgelab:8443/fixture-user#resources=rc-"
	environment := liveEnvironment(t,
		"RC_LIVE_PREFLIGHT_BIN="+verifier,
		"LAB_TARGETS_FILE="+contract,
		"RC_LIVE_FORGEJO_OWNER=fixture-user",
		"RC_LIVE_GITLAB_OWNER=fixture-user",
		"LAB_RUNNER_FORGES=gitlab,forgejo",
		"RC_LIVE_CONFIRM_DESTROY=destroy-live-forge-fixtures|"+identity,
		"RC_LIVE_EVIDENCE_ROOT="+evidenceRoot,
	)

	return liveFixture{
		contract: contract, ca: ca, recovery: recovery, cleanup: cleanup,
		cleanupLog: cleanupLog, verifier: verifier, evidenceRoot: evidenceRoot, environment: environment,
	}
}

func testCertificatePEM(t *testing.T) string {
	t.Helper()

	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}

	now := time.Now().UTC()
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: "livetest fixture CA"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}

	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}))
}

func writeMode(t *testing.T, path, body string, mode os.FileMode) {
	t.Helper()

	if err := os.WriteFile(path, []byte(body), mode); err != nil { //nolint:gosec // test-owned fixture path and intentional mode.
		t.Fatal(err)
	}
}

func setFixtureCleanup(t *testing.T, fixture liveFixture, body string) {
	t.Helper()

	writeMode(t, fixture.cleanup, body, 0o700)

	contractBody, err := os.ReadFile(fixture.contract)
	if err != nil {
		t.Fatal(err)
	}

	var contract map[string]any
	if unmarshalErr := json.Unmarshal(contractBody, &contract); unmarshalErr != nil {
		t.Fatal(unmarshalErr)
	}

	interfaces := contract["interfaces"].(map[string]any)        //nolint:forcetypeassert // Controlled fixture shape.
	cleanup := interfaces["credential_cleanup"].(map[string]any) //nolint:forcetypeassert // Controlled fixture shape.
	cleanup["command_sha256"] = fmt.Sprintf("%x", sha256.Sum256([]byte(body)))

	updated, err := json.MarshalIndent(contract, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	writeMode(t, fixture.contract, string(updated)+"\n", 0o600)
}

func liveCommand(t *testing.T, args ...string) *exec.Cmd {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	t.Cleanup(cancel)

	command := exec.CommandContext(ctx, "bash", args...) //nolint:gosec // fixed local scripts with test-authored arguments.
	command.Env = liveEnvironment(t)

	return command
}

// liveEnvironment is the whole environment a live-script test starts from: the
// host PATH for the interpreters and utilities the scripts call, owned HOME and
// temporary directories, and a fixed locale, plus the test's additions. Nothing
// else from the developer's or runner's environment reaches a script:
// credentials, proxies, Go settings, XDG state, or an operator's RC_LIVE_* and
// LAB_* selections.
func liveEnvironment(t *testing.T, additions ...string) []string {
	t.Helper()

	return append([]string{"PATH=" + os.Getenv("PATH"), "HOME=" + t.TempDir(), "TMPDIR=" + t.TempDir(), "LC_ALL=C"}, additions...)
}

// TestLiveEnvironment_CarriesOnlyOwnedState plants ambient credentials, proxies,
// Go settings, XDG state and live selections in the test process and requires
// the default command environment and the fixture environment to hold exactly
// the owned names.
func TestLiveEnvironment_CarriesOnlyOwnedState(t *testing.T) { //nolint:paralleltest // Plants ambient variables with t.Setenv.
	for _, ambient := range []string{
		"GITHUB_TOKEN", "CI_JOB_TOKEN", "HTTPS_PROXY", "https_proxy", "GOFLAGS", "GOPROXY", "XDG_STATE_HOME",
		"RC_LIVE_FORGEJO_ENDPOINT", "RC_LIVE_PROFILE", "LAB_TARGETS_FILE", "SSH_AUTH_SOCK",
	} {
		t.Setenv(ambient, "ambient-"+ambient)
	}

	names := func(environment []string) []string {
		keys := make([]string, 0, len(environment))

		for _, entry := range environment {
			name, _, _ := strings.Cut(entry, "=")
			keys = append(keys, name)
		}

		slices.Sort(keys)

		return keys
	}

	if got, want := names(liveCommand(t, "true").Env), []string{"HOME", "LC_ALL", "PATH", "TMPDIR"}; !slices.Equal(got, want) {
		t.Errorf("default live command environment = %v, want %v", got, want)
	}

	want := []string{
		"HOME", "LAB_RUNNER_FORGES", "LAB_TARGETS_FILE", "LC_ALL", "PATH", "RC_LIVE_CONFIRM_DESTROY", "RC_LIVE_EVIDENCE_ROOT",
		"RC_LIVE_FORGEJO_OWNER", "RC_LIVE_GITLAB_OWNER", "RC_LIVE_PREFLIGHT_BIN", "TMPDIR",
	}
	if got := names(newLiveFixture(t).environment); !slices.Equal(got, want) {
		t.Errorf("fixture environment = %v, want %v", got, want)
	}

	for _, entry := range newLiveFixture(t).environment {
		if strings.Contains(entry, "ambient-") {
			t.Errorf("ambient value reached the fixture environment: %s", entry)
		}
	}
}

func withEnvironment(base []string, additions ...string) []string {
	environment := append([]string(nil), base...)

	return append(environment, additions...)
}

func TestValidateLiveInputs_V2PreflightFreezesValidatedInputs(t *testing.T) {
	t.Parallel()

	fixture := newLiveFixture(t)

	outputParent := t.TempDir()
	if err := os.Chmod(outputParent, 0o700); err != nil { //nolint:gosec // private test directory.
		t.Fatal(err)
	}

	output := filepath.Join(outputParent, "frozen")
	command := liveCommand(t, "validate-live-inputs.sh", "--contract", output)
	command.Env = fixture.environment

	result, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("preflight failed: %v\n%s", err, result)
	}

	frozen, err := os.ReadFile(filepath.Join(output, "targets.json"))
	if err != nil {
		t.Fatal(err)
	}

	var projection struct {
		Version    int     `json:"version"`
		CAFile     *string `json:"ca_file"`
		Interfaces struct {
			Cleanup struct {
				Command      string `json:"command"`
				ContractFile string `json:"contract_file"`
			} `json:"credential_cleanup"`
		} `json:"interfaces"`
	}
	if decodeErr := json.Unmarshal(frozen, &projection); decodeErr != nil {
		t.Fatal(decodeErr)
	}

	if projection.Version != 2 || projection.CAFile == nil || *projection.CAFile != filepath.Join(output, "ca.crt") {
		t.Fatalf("frozen projection = %+v", projection)
	}

	if projection.Interfaces.Cleanup.ContractFile != fixture.recovery {
		t.Fatalf("cleanup contract_file = %q", projection.Interfaces.Cleanup.ContractFile)
	}

	if projection.Interfaces.Cleanup.Command != filepath.Join(output, "cleanup") {
		t.Fatalf("frozen cleanup command = %q", projection.Interfaces.Cleanup.Command)
	}

	// Errorf, not Fatalf: each name is an independent claim about the
	// snapshot, and stopping at the first one hides how much of the binding is
	// missing on a run where several are.
	for _, name := range []string{"targets.json-facts", "ca.crt-facts", "cleanup-command-facts"} {
		facts, readErr := os.ReadFile(filepath.Join(output, name))
		if readErr != nil || len(facts) == 0 {
			t.Errorf("snapshot binding %s = %q, %v", name, facts, readErr)
		}
	}

	for _, name := range []string{"targets.json", "ca.crt"} {
		info, statErr := os.Stat(filepath.Join(output, name))
		if statErr != nil {
			t.Errorf("private snapshot %s: %v", name, statErr)

			continue
		}

		if info.Mode().Perm() != 0o400 {
			t.Errorf("private snapshot %s mode = %v, want 0400", name, info.Mode().Perm())
		}
	}

	cleanupInfo, err := os.Stat(filepath.Join(output, "cleanup"))
	if err != nil || cleanupInfo.Mode().Perm() != 0o500 {
		t.Fatalf("frozen cleanup mode = %v, %v", cleanupInfo, err)
	}
}

func TestValidateLiveInputs_MissingRequiredLiveCapabilityFailsBeforeSnapshot(t *testing.T) {
	t.Parallel()

	for _, capability := range []string{"repositories", "packages", "artifacts"} {
		t.Run(capability, func(t *testing.T) {
			fixture := newLiveFixture(t)

			body, err := os.ReadFile(fixture.contract)
			if err != nil {
				t.Fatal(err)
			}

			body = []byte(strings.Replace(string(body), `"`+capability+`", `, "", 1))
			writeMode(t, fixture.contract, string(body), 0o600)
			output := filepath.Join(t.TempDir(), "frozen")
			command := liveCommand(t, "validate-live-inputs.sh", "--contract", output)
			command.Env = fixture.environment

			result, err := command.CombinedOutput()
			if err == nil || !strings.Contains(string(result), "lacks required live capability") {
				t.Fatalf("missing %s result = %v\n%s", capability, err, result)
			}

			if _, err := os.Stat(output); !os.IsNotExist(err) {
				t.Fatalf("preflight wrote state before capability validation: %v", err)
			}
		})
	}
}

func TestValidateLiveInputs_HostOnlySelectionDoesNotRequireWorkflowRuns(t *testing.T) {
	t.Parallel()

	fixture := newLiveFixture(t)

	body, err := os.ReadFile(fixture.contract)
	if err != nil {
		t.Fatal(err)
	}

	body = []byte(strings.ReplaceAll(string(body), `, "workflow-runs"`, ""))
	writeMode(t, fixture.contract, string(body), 0o600)

	outputParent := t.TempDir()
	if err := os.Chmod(outputParent, 0o700); err != nil { //nolint:gosec // private test directory.
		t.Fatal(err)
	}

	output := filepath.Join(outputParent, "frozen")
	command := liveCommand(t, "validate-live-inputs.sh", "--contract", output)

	command.Env = fixture.environment
	if result, err := command.CombinedOutput(); err != nil {
		t.Fatalf("host-only contract without workflow-runs failed: %v\n%s", err, result)
	}
}

func TestValidateLiveInputs_RejectsNonCertificateCAContentBeforeSnapshot(t *testing.T) {
	t.Parallel()

	for _, suffix := range []string{"touch /tmp/exfiltrated\n", "-----BEGIN PRIVATE KEY-----\nsecret\n-----END PRIVATE KEY-----\n"} {
		fixture := newLiveFixture(t)

		body, err := os.ReadFile(fixture.ca)
		if err != nil {
			t.Fatal(err)
		}

		writeMode(t, fixture.ca, string(body)+suffix, 0o600)
		output := filepath.Join(t.TempDir(), "frozen")
		command := liveCommand(t, "validate-live-inputs.sh", "--contract", output)
		command.Env = fixture.environment

		result, err := command.CombinedOutput()
		if err == nil || !strings.Contains(string(result), "must contain only PEM CERTIFICATE blocks") {
			t.Fatalf("malicious CA result = %v\n%s", err, result)
		}

		if _, err := os.Stat(output); !os.IsNotExist(err) {
			t.Fatalf("preflight wrote state before CA validation: %v", err)
		}
	}
}

func TestRunLiveTests_CleanupTrapExecutesExactValidatedPair(t *testing.T) {
	t.Parallel()

	fixture := newLiveFixture(t)
	fakeGo, fakePath := fakeLiveTools(t, 0, 7)
	environment := withEnvironment(fixture.environment,
		"GO="+fakeGo,
		"PATH="+fakePath+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	command := liveCommand(t, "run-live-tests.sh", "focused", "TestProviderFreeTrap")
	command.Env = environment
	result, err := command.CombinedOutput()

	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 7 {
		t.Fatalf("run status = %v, want 7\n%s", err, result)
	}

	calledWith, err := os.ReadFile(fixture.cleanupLog)
	if err != nil {
		t.Fatalf("cleanup did not run: %v\n%s", err, result)
	}

	if string(calledWith) != fixture.recovery {
		t.Fatalf("cleanup argument = %q, want exact contract_file %q", calledWith, fixture.recovery)
	}
}

func TestRunLiveTests_RetainsPrivateSanitizedEvidence(t *testing.T) {
	t.Parallel()

	fixture := newLiveFixture(t)
	fakeGo, fakePath := fakeLiveTools(t, 0, 0)
	secret := "ghp_" + strings.Repeat("a", 24)

	// Each line carries a synthetic credential in a form a failing live run
	// can print; the diagnostics around them must survive redaction.
	adversaries := map[string]string{
		"Authorization: Bearer " + secret: secret,
		"git clone https://fixture-user:userinfo-secret-1@forgejo.compose.forgelab:8443/o/r":      "userinfo-secret-1",
		`{"auths":{"registry.gitlab.compose.forgelab:8443":{"auth":"YXV0aC1maWxlLXNlY3JldA=="}}}`: "YXV0aC1maWxlLXNlY3JldA==",
		"buildah login --username fixture-user --password flag-secret-2 registry":                 "flag-secret-2",
		`curl -H "X-Registry-Auth: registry-header-secret-3" https://registry.invalid`:            "registry-header-secret-3",
		"Cookie: session=cookie-secret-4; other=cookie-secret-5":                                  "cookie-secret-5",
		`"access_token": "json-secret-6"`:                                                         "json-secret-6",
		"PRIVATE-TOKEN: glpat-" + strings.Repeat("b", 20):                                         "glpat-" + strings.Repeat("b", 20),
	}
	diagnostics := []string{
		"FAIL: TestInRunner_Keyless (12.3s) HTTP 401 Unauthorized from https://gitlab.compose.forgelab:8443/api/v4/user",
		"author: Release Bot",
		"--username fixture-user",
	}

	lines := slices.Collect(maps.Keys(adversaries))
	slices.Sort(lines)

	canary := plantRetentionAdversaries(t, fixture.evidenceRoot)

	command := liveCommand(t, "run-live-tests.sh", "focused", "TestProcessFixture")
	command.Env = withEnvironment(fixture.environment,
		"GO="+fakeGo,
		"PATH="+fakePath+string(os.PathListSeparator)+os.Getenv("PATH"),
		"FAKE_GO_TEST_OUTPUT="+strings.Join(append(lines, diagnostics...), "\n"),
	)

	result, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("live run failed: %v\n%s", err, result)
	}

	evidenceDir := requireRetentionKeptOnlyItsOwn(t, fixture.evidenceRoot, canary)

	if info, statErr := os.Stat(evidenceDir); statErr != nil || info.Mode().Perm() != 0o700 {
		t.Fatalf("evidence directory mode = %v, err=%v", info, statErr)
	}

	for _, name := range []string{"metadata.txt", "source-files.sha256", "built-binaries.sha256", "live-run.log", "SHA256SUMS"} {
		info, statErr := os.Stat(filepath.Join(evidenceDir, name))
		if statErr != nil || info.Mode().Perm() != 0o400 {
			t.Errorf("%s mode = %v, err=%v", name, info, statErr)
		}
	}

	log, err := os.ReadFile(filepath.Join(evidenceDir, "live-run.log"))
	if err != nil {
		t.Fatal(err)
	}

	for line, credential := range adversaries {
		if strings.Contains(string(log), credential) {
			t.Errorf("sealed log keeps the credential from %q", line)
		}
	}

	for _, diagnostic := range diagnostics {
		if !strings.Contains(string(log), diagnostic) {
			t.Errorf("sanitizer removed the non-secret diagnostic %q", diagnostic)
		}
	}

	if strings.Count(string(log), "[REDACTED]") < len(adversaries) {
		t.Errorf("sealed log has fewer redactions than planted credentials:\n%s", log)
	}

	manifest, err := os.ReadFile(filepath.Join(evidenceDir, "built-binaries.sha256"))
	if err != nil || !strings.Contains(string(manifest), "reusable-ci-runner") || !strings.Contains(string(manifest), "credential-proxy") {
		t.Fatalf("built binary manifest = %q, err=%v", manifest, err)
	}

	sources, err := os.ReadFile(filepath.Join(evidenceDir, "source-files.sha256"))
	if err != nil || !strings.Contains(string(sources), "scripts/ci/run-live-tests.sh") {
		t.Fatalf("source manifest missing runner source: %v", err)
	}

	metadata, err := os.ReadFile(filepath.Join(evidenceDir, "metadata.txt"))
	if err != nil || !strings.Contains(string(metadata), "retention_days=30") || !strings.Contains(string(metadata), "exit_status=0") {
		t.Fatalf("evidence metadata = %q, err=%v", metadata, err)
	}

	check := exec.CommandContext(t.Context(), "sha256sum", "--check", "--quiet", "SHA256SUMS") //nolint:gosec // fixed test command and argument.

	check.Dir = evidenceDir
	if output, checkErr := check.CombinedOutput(); checkErr != nil {
		t.Fatalf("evidence checksums: %v\n%s", checkErr, output)
	}
}

func TestRunLiveTests_RequiresExplicitProfileAndFullRoad(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "profile", want: "live profile must be explicit"},
		{name: "full road", args: []string{"full"}, want: "full <compose|k3s>"},
		{name: "known road", args: []string{"full", "other"}, want: "must be compose or k3s"},
		{name: "focused filter", args: []string{"focused"}, want: "focused <go-test-run-filter>"},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			command := liveCommand(t, append([]string{"run-live-tests.sh"}, testCase.args...)...)

			output, err := command.CombinedOutput()
			if err == nil || !strings.Contains(string(output), testCase.want) {
				t.Fatalf("invalid live invocation result = %v, want %q\n%s", err, testCase.want, output)
			}
		})
	}
}

func TestRunLiveTests_V1ContractNeverArmsCleanup(t *testing.T) {
	t.Parallel()

	fixture := newLiveFixture(t)

	body, err := os.ReadFile(fixture.contract)
	if err != nil {
		t.Fatal(err)
	}

	body = []byte(strings.Replace(string(body), `"version": 2`, `"version": 1`, 1))
	writeMode(t, fixture.contract, string(body), 0o600)
	fakeGo, fakePath := fakeLiveTools(t, 0, 0)
	command := liveCommand(t, "run-live-tests.sh", "focused", "TestProcessFixture")

	command.Env = withEnvironment(fixture.environment,
		"GO="+fakeGo,
		"PATH="+fakePath+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	if result, err := command.CombinedOutput(); err == nil {
		t.Fatalf("malformed preflight succeeded\n%s", result)
	}

	if _, err := os.Stat(fixture.cleanupLog); !os.IsNotExist(err) {
		t.Fatalf("cleanup ran for malformed JSON: %v", err)
	}
}

func TestRunLiveTests_MalformedJSONNeverExecutesACommandOrLogsASecret(t *testing.T) {
	t.Parallel()

	fixture := newLiveFixture(t)

	body, err := os.ReadFile(fixture.contract)
	if err != nil {
		t.Fatal(err)
	}

	const secret = "preflight-secret-must-not-be-logged"

	value := strings.Replace(string(body), "fake-forgejo-token", secret, 1)
	value = strings.TrimSuffix(value, "}\n")
	writeMode(t, fixture.contract, value, 0o600)

	fakeGo, fakePath := fakeLiveTools(t, 0, 0)
	command := liveCommand(t, "run-live-tests.sh", "focused", "TestProcessFixture")
	command.Env = withEnvironment(fixture.environment,
		"GO="+fakeGo,
		"PATH="+fakePath+string(os.PathListSeparator)+os.Getenv("PATH"),
	)

	result, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("malformed JSON preflight succeeded\n%s", result)
	}

	if strings.Contains(string(result), secret) {
		t.Fatalf("preflight logged a credential value:\n%s", result)
	}

	if _, err := os.Stat(fixture.cleanupLog); !os.IsNotExist(err) {
		t.Fatalf("cleanup command ran for malformed JSON: %v", err)
	}
}

func TestRunLiveTests_FreezesContractBeforeMutableSourceChanges(t *testing.T) {
	t.Parallel()

	fixture := newLiveFixture(t)
	hook := filepath.Join(t.TempDir(), "mutate-source-contract")
	writeMode(t, hook, fmt.Sprintf("#!/usr/bin/env bash\nprintf 'mutated-after-preflight\\n' >>%q\n", fixture.contract), 0o700)
	fakeGo, fakePath := fakeLiveTools(t, 0, 0)
	command := liveCommand(t, "run-live-tests.sh", "focused", "TestProcessFixture")
	command.Env = withEnvironment(fixture.environment,
		"GO="+fakeGo,
		"PATH="+fakePath+string(os.PathListSeparator)+os.Getenv("PATH"),
		"FAKE_GO_BUILD_HOOK="+hook,
		"FAKE_GO_REQUIRE_FROZEN_CONTRACT=1",
		"FAKE_GO_SOURCE_CONTRACT="+fixture.contract,
	)

	result, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("run disagreed with its frozen contract after source mutation: %v\n%s", err, result)
	}

	calledWith, err := os.ReadFile(fixture.cleanupLog)
	if err != nil || string(calledWith) != fixture.recovery {
		t.Fatalf("cleanup after source mutation = %q, err=%v\n%s", calledWith, err, result)
	}
}

// ownedLiveEnvironment is the fixture environment with the Go platform and
// temporary-directory variables the runner script reads replaced by owned
// values, so an ambient GOOS or TMPDIR cannot change what is recorded.
func ownedLiveEnvironment(t *testing.T, fixture liveFixture, tmp string, additions ...string) []string {
	t.Helper()

	environment := make([]string, 0, len(fixture.environment)+len(additions)+1)

	for _, entry := range fixture.environment {
		name, _, _ := strings.Cut(entry, "=")
		if name != "GOOS" && name != "GOARCH" && name != "CGO_ENABLED" && name != "TMPDIR" {
			environment = append(environment, entry)
		}
	}

	return append(append(environment, "TMPDIR="+tmp), additions...)
}

// TestRunLiveTests_PinsTheExactGoCommandBoundary records every Go invocation
// the runner makes: the four builds with their platform environment, flags,
// outputs and packages, then the one live test command, each from the
// repository root and each exactly once. The run's private state directory is
// removed before the script exits.
func TestRunLiveTests_PinsTheExactGoCommandBoundary(t *testing.T) {
	t.Parallel()

	repo, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}

	if repo, err = filepath.EvalSymlinks(repo); err != nil {
		t.Fatal(err)
	}

	build := func(platform, output, pkg string) string {
		return repo + "|CGO_ENABLED=0 " + platform + "|build -trimpath -buildvcs=false -o STATE/" + output + " " + pkg
	}
	host, linux := "GOOS=unset GOARCH=unset", "GOOS=linux GOARCH=amd64"
	invocations := []string{
		build(host, "reusable-ci-host", "./cmd/reusable-ci"),
		build(linux, "reusable-ci-runner", "./cmd/reusable-ci"),
		build(linux, "credential-proxy", "./internal/livetest/credentialproxy"),
		build(linux, "probe-json", "./internal/livetest/probejson"),
		repo + "|CGO_ENABLED=unset GOOS=unset GOARCH=unset|test -tags=live -p 1 -count=1 -buildvcs=false -timeout=30m -v -run TestProcessFixture ./internal/livetest/...",
	}

	fixture := newLiveFixture(t)
	tmp := t.TempDir()
	argv := filepath.Join(t.TempDir(), "argv")
	fakeGo, fakePath := fakeLiveTools(t, 0, 0)
	command := liveCommand(t, "run-live-tests.sh", "focused", "TestProcessFixture")
	command.Env = ownedLiveEnvironment(t, fixture, tmp,
		"GO="+fakeGo, "PATH="+fakePath+string(os.PathListSeparator)+os.Getenv("PATH"), "FAKE_GO_ARGV="+argv)

	if result, runErr := command.CombinedOutput(); runErr != nil {
		t.Fatalf("live run: %v\n%s", runErr, result)
	}

	body, err := os.ReadFile(argv)
	if err != nil {
		t.Fatal(err)
	}

	recorded := regexp.MustCompile(regexp.QuoteMeta(tmp)+`/reusable-ci-live\.[A-Za-z0-9]{8}`).ReplaceAllString(string(body), "STATE")
	want := strings.Join(invocations, "\n") + "\n"

	if recorded != want {
		t.Fatalf("go invocations:\n%s\nwant:\n%s", recorded, want)
	}

	if leftovers, _ := filepath.Glob(filepath.Join(tmp, "reusable-ci-live.*")); len(leftovers) > 0 {
		t.Fatalf("live state left behind: %v", leftovers)
	}
}

// TestRunLiveTests_EveryArmedExitCleansUpOnceWithItsStatus runs each way an
// armed run can end. Cleanup runs exactly once and last; a missing tool stops
// the run before any Go build or test; a build or test failure keeps its own
// status; and a cleanup that is not proven turns any outcome, including a test
// failure, into status 1.
func TestRunLiveTests_EveryArmedExitCleansUpOnceWithItsStatus(t *testing.T) {
	t.Parallel()

	completed := "build\nbuild\nbuild\nbuild\ntest\ncleanup\n"

	for _, tc := range []struct {
		name          string
		build, test   int
		missingTool   bool
		cleanupFails  bool
		status        int
		events, cause string
	}{
		{name: "success", events: completed},
		{name: "test failure", test: 7, status: 7, events: completed},
		{name: "build failure", build: 8, status: 8, events: "build\ncleanup\n"},
		{name: "missing tool", missingTool: true, status: 2, events: "cleanup\n", cause: "required tools are not on PATH: cosign"},
		{name: "cleanup failure after success", cleanupFails: true, status: 1, events: completed, cause: "frozen cleanup launcher failed or exceeded its time bound"},
		{name: "cleanup failure after test failure", test: 7, cleanupFails: true, status: 1, events: completed, cause: "frozen cleanup launcher failed or exceeded its time bound"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fixture := newLiveFixture(t)
			if tc.cleanupFails {
				setFixtureCleanup(t, fixture, fmt.Sprintf(
					"#!/usr/bin/env bash\nprintf '%%s' \"$1\" >%q\n[[ -z \"${LIVE_TEST_EVENTS:-}\" ]] || printf 'cleanup\\n' >>\"$LIVE_TEST_EVENTS\"\nexit 9\n",
					fixture.cleanupLog))
			}

			fakeGo, fakePath := fakeLiveTools(t, tc.build, tc.test)
			if tc.missingTool {
				if err := os.Remove(filepath.Join(fakePath, "cosign")); err != nil {
					t.Fatal(err)
				}
			}

			events := filepath.Join(t.TempDir(), "events")
			command := liveCommand(t, "run-live-tests.sh", "focused", "TestProcessFixture")
			command.Env = ownedLiveEnvironment(t, fixture, t.TempDir(),
				"GO="+fakeGo, "PATH="+fakePath+":/usr/bin:/bin", "FAKE_GO_EVENTS="+events, "LIVE_TEST_EVENTS="+events)

			result, err := command.CombinedOutput()

			status := 0

			var exit *exec.ExitError
			if errors.As(err, &exit) {
				status = exit.ExitCode()
			} else if err != nil {
				t.Fatalf("run: %v\n%s", err, result)
			}

			if tc.cleanupFails && (strings.Contains(string(result), "refusing cleanup execution") ||
				!strings.Contains(string(result), "automatic live credential cleanup was not proven")) {
				t.Fatalf("a launched cleanup failure was reported as a refusal or without recovery instructions\n%s", result)
			}

			if status != tc.status || !strings.Contains(string(result), tc.cause) {
				t.Fatalf("status = %d, want %d with %q\n%s", status, tc.status, tc.cause, result)
			}

			body, err := os.ReadFile(events)
			if err != nil || string(body) != tc.events {
				t.Fatalf("events = %q, err=%v, want %q\n%s", body, err, tc.events, result)
			}
		})
	}
}

func TestRunLiveTests_CleanupVerifierNeedsNoUncheckedHostUtilities(t *testing.T) {
	t.Parallel()

	for _, tool := range []string{"realpath", "stat", "sha256sum"} {
		t.Run(tool, func(t *testing.T) {
			fixture := newLiveFixture(t)
			fakeGo, fakePath := fakeLiveTools(t, 0, 0)
			writeMode(t, filepath.Join(fakePath, tool), "#!/usr/bin/env sh\nexit 91\n", 0o700)
			command := liveCommand(t, "run-live-tests.sh", "focused", "TestProcessFixture")
			command.Env = withEnvironment(fixture.environment,
				"GO="+fakeGo,
				"PATH="+fakePath+string(os.PathListSeparator)+os.Getenv("PATH"),
			)

			result, _ := command.CombinedOutput()

			calledWith, err := os.ReadFile(fixture.cleanupLog)
			if err != nil || string(calledWith) != fixture.recovery {
				t.Fatalf("cleanup with failing %s = %q, err=%v\n%s", tool, calledWith, err, result)
			}
		})
	}
}

func TestRunLiveTests_PinsCleanupVerifierBeforeOwnership(t *testing.T) {
	t.Parallel()

	fixture := newLiveFixture(t)
	fakeGo, fakePath := fakeLiveTools(t, 0, 0)
	hook := filepath.Join(t.TempDir(), "replace-verifier")
	writeMode(t, hook, fmt.Sprintf("#!/usr/bin/env bash\nmv %q %q.old\nprintf '#!/usr/bin/env sh\\nexit 97\\n' >%q\nchmod 700 %q\n",
		fixture.verifier, fixture.verifier, fixture.verifier, fixture.verifier), 0o700)
	command := liveCommand(t, "run-live-tests.sh", "focused", "TestProcessFixture")
	command.Env = withEnvironment(fixture.environment,
		"GO="+fakeGo,
		"PATH="+fakePath+string(os.PathListSeparator)+os.Getenv("PATH"),
		"FAKE_GO_BUILD_HOOK="+hook,
	)

	result, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("pinned verifier run failed: %v\n%s", err, result)
	}

	calledWith, err := os.ReadFile(fixture.cleanupLog)
	if err != nil || string(calledWith) != fixture.recovery {
		t.Fatalf("pinned verifier cleanup = %q, err=%v\n%s", calledWith, err, result)
	}
}

func TestPrepareLiveTests_HelperBuildFailureCannotArmUntrustedCleanup(t *testing.T) {
	t.Parallel()

	fixture := newLiveFixture(t)
	fakeGo := filepath.Join(t.TempDir(), "go")
	writeMode(t, fakeGo, "#!/usr/bin/env sh\nexit 9\n", 0o700)
	command := liveCommand(t, "prepare-live-tests.sh", "full", "compose")
	command.Env = withEnvironment(fixture.environment, "GO="+fakeGo)

	result, err := command.CombinedOutput()
	if err == nil || !strings.Contains(string(result), "before the contract could be validated; no cleanup was armed") {
		t.Fatalf("helper-build boundary = %v\n%s", err, result)
	}

	if _, err := os.Stat(fixture.cleanupLog); !os.IsNotExist(err) {
		t.Fatalf("unvalidated cleanup executed: %v", err)
	}
}

func TestRunLiveTests_SourceCleanupReplacementStillExecutesFrozenBytes(t *testing.T) {
	t.Parallel()

	fixture := newLiveFixture(t)
	hook := filepath.Join(t.TempDir(), "replace-cleanup")
	writeMode(t, hook, fmt.Sprintf("#!/usr/bin/env bash\nprintf '#!/usr/bin/env bash\\nprintf replacement >%q\\n' >%q\nchmod 700 %q\n",
		fixture.cleanupLog, fixture.cleanup, fixture.cleanup), 0o700)
	fakeGo, fakePath := fakeLiveTools(t, 0, 0)
	command := liveCommand(t, "run-live-tests.sh", "focused", "TestProcessFixture")
	command.Env = withEnvironment(fixture.environment,
		"GO="+fakeGo,
		"PATH="+fakePath+string(os.PathListSeparator)+os.Getenv("PATH"),
		"FAKE_GO_BUILD_HOOK="+hook,
	)

	result, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("frozen cleanup failed after source replacement: %v\n%s", err, result)
	}

	calledWith, err := os.ReadFile(fixture.cleanupLog)
	if err != nil || string(calledWith) != fixture.recovery {
		t.Fatalf("source replacement executed instead of frozen bytes: %q, %v\n%s", calledWith, err, result)
	}
}

func TestRunLiveTests_RefusesCleanupContractInPlaceMutation(t *testing.T) {
	t.Parallel()

	fixture := newLiveFixture(t)
	hook := filepath.Join(t.TempDir(), "mutate-cleanup-contract")
	writeMode(t, hook, fmt.Sprintf("#!/usr/bin/env bash\nprintf 'mutated\\n' >>%q\n", fixture.recovery), 0o700)
	assertCleanupMutationRefused(t, fixture, hook)
}

func TestRunLiveTests_RefusesFrozenCleanupReplacement(t *testing.T) {
	t.Parallel()

	fixture := newLiveFixture(t)
	hook := filepath.Join(t.TempDir(), "replace-frozen-cleanup")
	writeMode(t, hook, `#!/usr/bin/env bash
frozen_cleanup="$(dirname -- "$LAB_TARGETS_FILE")/cleanup"
mv "$frozen_cleanup" "$frozen_cleanup.old"
printf '#!/usr/bin/env sh\nexit 0\n' >"$frozen_cleanup"
chmod 500 "$frozen_cleanup"
`, 0o700)
	assertCleanupMutationRefusedWithEnv(t, fixture, "FAKE_GO_TEST_HOOK="+hook)
}

func assertCleanupMutationRefused(t *testing.T, fixture liveFixture, hook string) {
	t.Helper()
	assertCleanupMutationRefusedWithEnv(t, fixture, "FAKE_GO_BUILD_HOOK="+hook)
}

func assertCleanupMutationRefusedWithEnv(t *testing.T, fixture liveFixture, hookEnv string) {
	t.Helper()
	fakeGo, fakePath := fakeLiveTools(t, 0, 0)
	command := liveCommand(t, "run-live-tests.sh", "focused", "TestProcessFixture")
	command.Env = withEnvironment(fixture.environment,
		"GO="+fakeGo,
		"PATH="+fakePath+string(os.PathListSeparator)+os.Getenv("PATH"),
		hookEnv,
	)
	result, err := command.CombinedOutput()

	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 ||
		!strings.Contains(string(result), "refusing cleanup execution") ||
		!strings.Contains(string(result), "manually inspect and invoke the original pair") {
		t.Fatalf("cleanup mutation status = %v\n%s", err, result)
	}

	if _, err := os.Stat(fixture.cleanupLog); !os.IsNotExist(err) {
		t.Fatalf("changed cleanup pair executed: %v", err)
	}
}

func fakeLiveTools(t *testing.T, buildStatus, testStatus int) (string, string) {
	t.Helper()
	dir := t.TempDir()
	fakeGo := filepath.Join(dir, "go")
	body := fmt.Sprintf(`#!/usr/bin/env bash
set -euo pipefail
[[ -z "${FAKE_GO_ARGV:-}" ]] || printf '%%s|CGO_ENABLED=%%s GOOS=%%s GOARCH=%%s|%%s\n' \
  "$PWD" "${CGO_ENABLED-unset}" "${GOOS-unset}" "${GOARCH-unset}" "$*" >>"$FAKE_GO_ARGV"
case "$1" in
build)
  [[ -z "${FAKE_GO_EVENTS:-}" ]] || printf 'build\n' >>"$FAKE_GO_EVENTS"
  if [[ -n "${FAKE_GO_BUILD_HOOK:-}" && ! -e "$FAKE_GO_BUILD_HOOK.ran" ]]; then
    : >"$FAKE_GO_BUILD_HOOK.ran"
    bash "$FAKE_GO_BUILD_HOOK"
  fi
  if ((%d != 0)); then exit %d; fi
  output=''
  while (($#)); do
    if [[ "$1" == -o ]]; then output=$2; break; fi
    shift
  done
  printf '#!/usr/bin/env sh\nexit 0\n' >"$output"
  chmod 700 "$output"
  ;;
test)
  [[ -z "${FAKE_GO_EVENTS:-}" ]] || printf 'test\n' >>"$FAKE_GO_EVENTS"
  [[ -z "${FAKE_GO_TEST_HOOK:-}" ]] || bash "$FAKE_GO_TEST_HOOK"
	[[ -z "${FAKE_GO_TEST_OUTPUT:-}" ]] || printf '%%s\n' "$FAKE_GO_TEST_OUTPUT"
  if [[ "${FAKE_GO_REQUIRE_FROZEN_CONTRACT:-}" == 1 ]]; then
    [[ "$LAB_TARGETS_FILE" != "$FAKE_GO_SOURCE_CONTRACT" ]]
    [[ "$(<"$LAB_TARGETS_FILE")" == *'"version": 2'* ]]
    [[ "$(<"$FAKE_GO_SOURCE_CONTRACT")" == *mutated-after-preflight* ]]
		[[ -n "$RC_LIVE_CONTRACT_FACTS" && -n "$RC_LIVE_CA_FACTS" ]]
		[[ -x "$RC_LIVE_CREDENTIAL_PROXY_BIN" ]]
		[[ -x "$RC_LIVE_PROBE_JSON_BIN" ]]
		[[ -x "$RC_LIVE_RUNNER_BIN" && "$RC_LIVE_RUNNER_SHA256" =~ ^[a-f0-9]{64}$ ]]
  fi
  exit %d
  ;;
*) exit 2 ;;
esac
`, buildStatus, buildStatus, testStatus)
	writeMode(t, fakeGo, body, 0o700)

	for _, tool := range []string{"cosign", "jq", "syft", "buildah", "skopeo"} {
		writeMode(t, filepath.Join(dir, tool), "#!/usr/bin/env sh\nexit 0\n", 0o700)
	}

	return fakeGo, dir
}

// TestRunLiveTests_SignalsCleanUpOnceWithTheSignalStatus sends INT and TERM to
// the whole process group while the live test phase is running, the way a
// terminal or a cancelled CI job does. The run exits with the signal's
// conventional status, runs cleanup exactly once after the test phase started,
// and leaves no process of the group behind. A launched cleanup that hangs is
// bounded inside the helper (see livetest.FrozenCleanupTimeout).
func TestRunLiveTests_SignalsCleanUpOnceWithTheSignalStatus(t *testing.T) {
	t.Parallel()

	for signal, status := range map[syscall.Signal]int{syscall.SIGINT: 130, syscall.SIGTERM: 143} {
		t.Run(signal.String(), func(t *testing.T) {
			t.Parallel()

			fixture := newLiveFixture(t)
			dir := t.TempDir()
			events, started := filepath.Join(dir, "events"), filepath.Join(dir, "started")
			hook := filepath.Join(dir, "hang-in-test")
			writeMode(t, hook, fmt.Sprintf("#!/usr/bin/env bash\n: >%q\nsleep 300\n", started), 0o700)

			fakeGo, fakePath := fakeLiveTools(t, 0, 0)
			command := liveCommand(t, "run-live-tests.sh", "focused", "TestProcessFixture")
			command.Env = withEnvironment(fixture.environment,
				"GO="+fakeGo, "PATH="+fakePath+string(os.PathListSeparator)+os.Getenv("PATH"),
				"FAKE_GO_EVENTS="+events, "LIVE_TEST_EVENTS="+events, "FAKE_GO_TEST_HOOK="+hook)
			command.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}

			var output bytes.Buffer

			command.Stdout, command.Stderr = &output, &output

			if err := command.Start(); err != nil {
				t.Fatal(err)
			}

			group := command.Process.Pid

			// A failure before the signal must not leave the hanging group behind.
			t.Cleanup(func() { _ = syscall.Kill(-group, syscall.SIGKILL) })

			waitFor(t, func() bool {
				_, statErr := os.Stat(started)

				return statErr == nil
			}, "the test phase to start")

			if err := syscall.Kill(-group, signal); err != nil {
				t.Fatal(err)
			}

			err := command.Wait()

			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != status {
				t.Fatalf("status after %s = %v, want %d\n%s", signal, err, status, output.String())
			}

			if body, readErr := os.ReadFile(events); readErr != nil || string(body) != "build\nbuild\nbuild\nbuild\ntest\ncleanup\n" {
				t.Fatalf("events after %s = %q, %v, want one cleanup after the test phase\n%s", signal, body, readErr, output.String())
			}

			waitFor(t, func() bool { return errors.Is(syscall.Kill(-group, 0), syscall.ESRCH) }, "every process of the run to exit")
		})
	}
}

// waitFor polls condition until it holds. The bound only catches a hang: the
// runner's preflight is slow when the whole suite runs in parallel.
func waitFor(t *testing.T, condition func() bool, what string) {
	t.Helper()

	deadline := time.Now().Add(time.Minute)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}

		time.Sleep(20 * time.Millisecond)
	}
}

// TestRunLiveTests_RefusesTamperedRunInputsBeforeAnyWork hands the inner run a
// tampered evidence directory or preflight helper and requires exit 2 with its
// reason, before preflight, any Go build or test, or any cleanup.
func TestRunLiveTests_RefusesTamperedRunInputsBeforeAnyWork(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		arrange func(t *testing.T, fixture liveFixture) []string
		reason  string
	}{
		"evidence directory is a symlink": {reason: "RC_LIVE_EVIDENCE_DIR must be an absolute owner-controlled, non-symlink directory",
			arrange: func(t *testing.T, _ liveFixture) []string {
				t.Helper()

				link := filepath.Join(t.TempDir(), "evidence")
				if err := os.Symlink(t.TempDir(), link); err != nil {
					t.Fatal(err)
				}

				return []string{"RC_LIVE_EVIDENCE_CAPTURED=1", "RC_LIVE_EVIDENCE_DIR=" + link}
			}},
		"evidence directory is relative": {reason: "RC_LIVE_EVIDENCE_DIR must be an absolute owner-controlled, non-symlink directory",
			arrange: func(*testing.T, liveFixture) []string {
				return []string{"RC_LIVE_EVIDENCE_CAPTURED=1", "RC_LIVE_EVIDENCE_DIR=evidence"}
			}},
		"preflight helper is a symlink": {reason: "RC_LIVE_PREFLIGHT_BIN must be an absolute regular executable",
			arrange: func(t *testing.T, fixture liveFixture) []string {
				t.Helper()

				link := filepath.Join(t.TempDir(), "live-preflight")
				if err := os.Symlink(fixture.verifier, link); err != nil {
					t.Fatal(err)
				}

				return []string{"RC_LIVE_EVIDENCE_CAPTURED=1", "RC_LIVE_EVIDENCE_DIR=" + t.TempDir(), "RC_LIVE_PREFLIGHT_BIN=" + link}
			}},
		"preflight helper is not executable": {reason: "RC_LIVE_PREFLIGHT_BIN must be an absolute regular executable",
			arrange: func(t *testing.T, _ liveFixture) []string {
				t.Helper()

				helper := filepath.Join(t.TempDir(), "live-preflight")
				writeMode(t, helper, "#!/bin/sh\nexit 0\n", 0o600)

				return []string{"RC_LIVE_EVIDENCE_CAPTURED=1", "RC_LIVE_EVIDENCE_DIR=" + t.TempDir(), "RC_LIVE_PREFLIGHT_BIN=" + helper}
			}},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			fixture := newLiveFixture(t)
			events := filepath.Join(t.TempDir(), "events")
			fakeGo, fakePath := fakeLiveTools(t, 0, 0)

			command := liveCommand(t, "run-live-tests.sh", "focused", "TestProcessFixture")
			command.Env = withEnvironment(fixture.environment, append([]string{
				"GO=" + fakeGo, "PATH=" + fakePath + string(os.PathListSeparator) + os.Getenv("PATH"),
				"FAKE_GO_EVENTS=" + events, "LIVE_TEST_EVENTS=" + events,
			}, tc.arrange(t, fixture)...)...)

			result, err := command.CombinedOutput()

			var exit *exec.ExitError
			if !errors.As(err, &exit) || exit.ExitCode() != 2 || !strings.Contains(string(result), tc.reason) {
				t.Fatalf("%s: %v, want exit 2 with %q\n%s", name, err, tc.reason, result)
			}

			if _, statErr := os.Stat(events); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("%s: work happened before the refusal: %v", name, statErr)
			}

			if _, statErr := os.Stat(fixture.cleanupLog); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("%s: cleanup ran for a refused run: %v", name, statErr)
			}
		})
	}
}

// plantRetentionAdversaries plants what evidence retention must leave alone:
// a young run, an expired directory with another name and an expired symlink
// named like a run whose target lies outside the root, next to one expired run
// it must remove. It returns a canary file inside the symlink's target.
func plantRetentionAdversaries(t *testing.T, evidenceRoot string) string {
	t.Helper()

	expiredAt, youngAt := time.Now().AddDate(0, 0, -31), time.Now().AddDate(0, 0, -29)
	outside := t.TempDir()
	canary := filepath.Join(outside, "canary")

	if err := os.WriteFile(canary, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}

	for name, at := range map[string]time.Time{
		"20260101T000000Z-1": expiredAt, "20260102T000000Z-2": youngAt, "operator-notes": expiredAt,
	} {
		dir := filepath.Join(evidenceRoot, name)
		if err := os.MkdirAll(dir, 0o700); err != nil {
			t.Fatal(err)
		}

		if err := os.Chtimes(dir, at, at); err != nil {
			t.Fatal(err)
		}
	}

	runLink := filepath.Join(evidenceRoot, "20260103T000000Z-3")
	if err := os.Symlink(outside, runLink); err != nil {
		t.Fatal(err)
	}

	if err := os.Chtimes(outside, expiredAt, expiredAt); err != nil {
		t.Fatal(err)
	}

	return canary
}

// requireRetentionKeptOnlyItsOwn requires the expired run removed, every
// adversary kept, nothing removed through the symlink, and exactly one new run
// directory, which it returns.
func requireRetentionKeptOnlyItsOwn(t *testing.T, evidenceRoot, canary string) string {
	t.Helper()

	entries, err := os.ReadDir(evidenceRoot)
	if err != nil {
		t.Fatal(err)
	}

	var runs []string

	for _, entry := range entries {
		switch entry.Name() {
		case "20260101T000000Z-1":
			t.Error("expired evidence run was retained")
		case "20260102T000000Z-2", "operator-notes", "20260103T000000Z-3":
		default:
			runs = append(runs, entry.Name())
		}
	}

	for _, kept := range []string{"20260102T000000Z-2", "operator-notes", "20260103T000000Z-3"} {
		if _, statErr := os.Lstat(filepath.Join(evidenceRoot, kept)); statErr != nil {
			t.Errorf("retention removed %s: %v", kept, statErr)
		}
	}

	if body, readErr := os.ReadFile(canary); readErr != nil || string(body) != "keep" {
		t.Errorf("retention reached through a symlink: %q, %v", body, readErr)
	}

	if len(runs) != 1 {
		t.Fatalf("new evidence runs = %v, want exactly one", runs)
	}

	return filepath.Join(evidenceRoot, runs[0])
}
