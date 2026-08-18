// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package ci

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"math/big"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type liveFixture struct {
	contract    string
	ca          string
	recovery    string
	cleanup     string
	cleanupLog  string
	verifier    string
	environment []string
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
	if output, buildErr := command.CombinedOutput(); buildErr != nil {
		_ = os.RemoveAll(dir)

		panic(fmt.Sprintf("build live preflight helper: %v\n%s", buildErr, output))
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

	writeMode(t, ca, testCertificatePEM(t), 0o600)
	writeMode(t, recovery, "recovery fixture\n", 0o600)
	writeMode(t, cleanup, fmt.Sprintf(`#!/usr/bin/env bash
[[ $# -eq 1 ]] || exit 64
printf '%%s' "$1" >%q
[[ -z "${LIVE_TEST_EVENTS:-}" ]] || printf 'cleanup\n' >>"$LIVE_TEST_EVENTS"
`, cleanupLog), 0o700)

	verifierBody, err := os.ReadFile(preflightBinary)
	if err != nil {
		t.Fatal(err)
	}

	if writeErr := os.WriteFile(verifier, verifierBody, 0o700); writeErr != nil { //nolint:gosec // Executable fixture is copied into an owner-only directory.
		t.Fatal(writeErr)
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
		"/tmp/neutral-targets.run-neutral-fixture.recovery-v2.env", recovery, 1)
	value = strings.ReplaceAll(value, "2026-08-06T10:00:00Z", now.Add(-time.Minute).Format(time.RFC3339))
	value = strings.Replace(value, "2026-09-05", now.AddDate(0, 0, 10).Format(time.DateOnly), 1)
	writeMode(t, contract, value, 0o600)

	identity := "run=run-neutral-fixture|targets=" +
		"forgejo@https://forgejo.compose.forgelab:8443/fixture-user#resources=rc-," +
		"gitlab@https://gitlab.compose.forgelab:8443/fixture-user#resources=rc-"
	environment := append(os.Environ(),
		"RC_LIVE_PREFLIGHT_BIN="+verifier,
		"LAB_TARGETS_FILE="+contract,
		"RC_LIVE_FORGEJO_OWNER=fixture-user",
		"RC_LIVE_GITLAB_OWNER=fixture-user",
		"RC_LIVE_CONFIRM_DESTROY=destroy-live-forge-fixtures|"+identity,
	)

	return liveFixture{
		contract: contract, ca: ca, recovery: recovery, cleanup: cleanup,
		cleanupLog: cleanupLog, verifier: verifier, environment: environment,
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

func liveCommand(t *testing.T, args ...string) *exec.Cmd {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	t.Cleanup(cancel)

	return exec.CommandContext(ctx, "bash", args...) //nolint:gosec // fixed local scripts with test-authored arguments.
}

func withEnvironment(base []string, additions ...string) []string {
	environment := append([]string(nil), base...)

	return append(environment, additions...)
}

func TestValidateLiveInputs_V2PreflightFreezesValidatedInputs(t *testing.T) {
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

	for _, name := range []string{"targets.json-facts", "ca.crt-facts", "cleanup-command-facts"} {
		facts, readErr := os.ReadFile(filepath.Join(output, name))
		if readErr != nil || len(facts) == 0 {
			t.Fatalf("snapshot binding %s = %q, %v", name, facts, readErr)
		}
	}

	for _, name := range []string{"targets.json", "ca.crt"} {
		info, statErr := os.Stat(filepath.Join(output, name))
		if statErr != nil || info.Mode().Perm() != 0o400 {
			t.Fatalf("private snapshot %s mode = %v, %v", name, info, statErr)
		}
	}

	cleanupInfo, err := os.Stat(filepath.Join(output, "cleanup"))
	if err != nil || cleanupInfo.Mode().Perm() != 0o500 {
		t.Fatalf("frozen cleanup mode = %v, %v", cleanupInfo, err)
	}
}

func TestValidateLiveInputs_MissingRequiredLiveCapabilityFailsBeforeSnapshot(t *testing.T) {
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
	fixture := newLiveFixture(t)
	fakeGo, fakePath := fakeLiveTools(t, 0, 7)
	environment := withEnvironment(fixture.environment,
		"GO="+fakeGo,
		"PATH="+fakePath+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	command := liveCommand(t, "run-live-tests.sh", "TestProviderFreeTrap")
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

func TestRunLiveTests_V1ContractNeverArmsCleanup(t *testing.T) {
	fixture := newLiveFixture(t)

	body, err := os.ReadFile(fixture.contract)
	if err != nil {
		t.Fatal(err)
	}

	body = []byte(strings.Replace(string(body), `"version": 2`, `"version": 1`, 1))
	writeMode(t, fixture.contract, string(body), 0o600)
	fakeGo, fakePath := fakeLiveTools(t, 0, 0)
	command := liveCommand(t, "run-live-tests.sh")

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
	command := liveCommand(t, "run-live-tests.sh")
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

func TestRunLiveTests_ProductBuildFailureRunsCleanupAfterBuild(t *testing.T) {
	fixture := newLiveFixture(t)
	events := filepath.Join(t.TempDir(), "events")
	fakeGo, fakePath := fakeLiveTools(t, 8, 0)
	command := liveCommand(t, "run-live-tests.sh")
	command.Env = withEnvironment(fixture.environment,
		"GO="+fakeGo,
		"PATH="+fakePath+string(os.PathListSeparator)+os.Getenv("PATH"),
		"FAKE_GO_EVENTS="+events,
		"LIVE_TEST_EVENTS="+events,
	)

	result, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("product build failure was lost\n%s", result)
	}

	body, readErr := os.ReadFile(events)
	if readErr != nil || string(body) != "build\ncleanup\n" {
		t.Fatalf("failure order = %q, err=%v, want build then cleanup\n%s", body, readErr, result)
	}
}

func TestRunLiveTests_FreezesContractBeforeMutableSourceChanges(t *testing.T) {
	fixture := newLiveFixture(t)
	hook := filepath.Join(t.TempDir(), "mutate-source-contract")
	writeMode(t, hook, fmt.Sprintf("#!/usr/bin/env bash\nprintf 'mutated-after-preflight\\n' >>%q\n", fixture.contract), 0o700)
	fakeGo, fakePath := fakeLiveTools(t, 0, 0)
	command := liveCommand(t, "run-live-tests.sh")
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

func TestRunLiveTests_ToolFailureRunsCleanupBeforeProductBuild(t *testing.T) {
	fixture := newLiveFixture(t)

	fakeGo, fakePath := fakeLiveTools(t, 0, 0)
	if err := os.Remove(filepath.Join(fakePath, "cosign")); err != nil {
		t.Fatal(err)
	}

	command := liveCommand(t, "run-live-tests.sh")
	command.Env = withEnvironment(fixture.environment,
		"GO="+fakeGo,
		"PATH="+fakePath+":/usr/bin:/bin",
	)

	result, err := command.CombinedOutput()
	if err == nil {
		t.Fatalf("tool preflight unexpectedly succeeded\n%s", result)
	}

	if _, err := os.ReadFile(fixture.cleanupLog); err != nil {
		t.Fatalf("tool failure did not trigger cleanup: %v\n%s", err, result)
	}
}

func TestRunLiveTests_CleanupVerifierNeedsNoUncheckedHostUtilities(t *testing.T) {
	for _, tool := range []string{"realpath", "stat", "sha256sum"} {
		t.Run(tool, func(t *testing.T) {
			fixture := newLiveFixture(t)
			fakeGo, fakePath := fakeLiveTools(t, 0, 0)
			writeMode(t, filepath.Join(fakePath, tool), "#!/usr/bin/env sh\nexit 91\n", 0o700)
			command := liveCommand(t, "run-live-tests.sh")
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
	fixture := newLiveFixture(t)
	fakeGo, fakePath := fakeLiveTools(t, 0, 0)
	hook := filepath.Join(t.TempDir(), "replace-verifier")
	writeMode(t, hook, fmt.Sprintf("#!/usr/bin/env bash\nmv %q %q.old\nprintf '#!/usr/bin/env sh\\nexit 97\\n' >%q\nchmod 700 %q\n",
		fixture.verifier, fixture.verifier, fixture.verifier, fixture.verifier), 0o700)
	command := liveCommand(t, "run-live-tests.sh")
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
	fixture := newLiveFixture(t)
	fakeGo := filepath.Join(t.TempDir(), "go")
	writeMode(t, fakeGo, "#!/usr/bin/env sh\nexit 9\n", 0o700)
	command := liveCommand(t, "prepare-live-tests.sh")
	command.Env = withEnvironment(fixture.environment, "GO="+fakeGo)

	result, err := command.CombinedOutput()
	if err == nil || !strings.Contains(string(result), "before the contract could be validated; no cleanup was armed") {
		t.Fatalf("helper-build boundary = %v\n%s", err, result)
	}

	if _, err := os.Stat(fixture.cleanupLog); !os.IsNotExist(err) {
		t.Fatalf("unvalidated cleanup executed: %v", err)
	}
}

func TestRunLiveTests_CleanupFailureOverridesSuccess(t *testing.T) {
	fixture := newLiveFixture(t)
	writeMode(t, fixture.cleanup, fmt.Sprintf("#!/usr/bin/env bash\nprintf '%%s' \"$1\" >%q\nexit 9\n", fixture.cleanupLog), 0o700)
	fakeGo, fakePath := fakeLiveTools(t, 0, 0)
	command := liveCommand(t, "run-live-tests.sh")
	command.Env = withEnvironment(fixture.environment,
		"GO="+fakeGo,
		"PATH="+fakePath+string(os.PathListSeparator)+os.Getenv("PATH"),
	)
	result, err := command.CombinedOutput()

	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 ||
		!strings.Contains(string(result), "automatic live credential cleanup was not proven") {
		t.Fatalf("cleanup failure status = %v\n%s", err, result)
	}
}

func TestRunLiveTests_SourceCleanupReplacementStillExecutesFrozenBytes(t *testing.T) {
	fixture := newLiveFixture(t)
	hook := filepath.Join(t.TempDir(), "replace-cleanup")
	writeMode(t, hook, fmt.Sprintf("#!/usr/bin/env bash\nprintf '#!/usr/bin/env bash\\nprintf replacement >%q\\n' >%q\nchmod 700 %q\n",
		fixture.cleanupLog, fixture.cleanup, fixture.cleanup), 0o700)
	fakeGo, fakePath := fakeLiveTools(t, 0, 0)
	command := liveCommand(t, "run-live-tests.sh")
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
	fixture := newLiveFixture(t)
	hook := filepath.Join(t.TempDir(), "mutate-cleanup-contract")
	writeMode(t, hook, fmt.Sprintf("#!/usr/bin/env bash\nprintf 'mutated\\n' >>%q\n", fixture.recovery), 0o700)
	assertCleanupMutationRefused(t, fixture, hook)
}

func TestRunLiveTests_RefusesFrozenCleanupReplacement(t *testing.T) {
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
	command := liveCommand(t, "run-live-tests.sh")
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
  [[ -z "${FAKE_GO_TEST_HOOK:-}" ]] || bash "$FAKE_GO_TEST_HOOK"
  if [[ "${FAKE_GO_REQUIRE_FROZEN_CONTRACT:-}" == 1 ]]; then
    [[ "$LAB_TARGETS_FILE" != "$FAKE_GO_SOURCE_CONTRACT" ]]
    [[ "$(<"$LAB_TARGETS_FILE")" == *'"version": 2'* ]]
    [[ "$(<"$FAKE_GO_SOURCE_CONTRACT")" == *mutated-after-preflight* ]]
    [[ -n "$RC_LIVE_CONTRACT_FACTS" && -n "$RC_LIVE_CA_FACTS" ]]
    [[ -x "$RC_LIVE_CREDENTIAL_PROXY_BIN" ]]
  fi
  exit %d
  ;;
*) exit 2 ;;
esac
`, buildStatus, buildStatus, testStatus)
	writeMode(t, fakeGo, body, 0o700)

	for _, tool := range []string{"cosign", "syft", "buildah", "skopeo"} {
		writeMode(t, filepath.Join(dir, tool), "#!/usr/bin/env sh\nexit 0\n", 0o700)
	}

	return fakeGo, dir
}
