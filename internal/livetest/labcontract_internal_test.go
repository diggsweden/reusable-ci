// SPDX-FileCopyrightText: 2026 Digg - Agency for Digital Government
// SPDX-License-Identifier: EUPL-1.2 OR GPL-3.0-or-later

package livetest

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/diggsweden/reusable-ci/v3/internal/domain/errs"
	"github.com/diggsweden/reusable-ci/v3/internal/domain/provider"
)

// hexDigest is the spelling the recorded fixture hashes are written in.
func hexDigest(body []byte) string {
	sum := sha256.Sum256(body)

	return hex.EncodeToString(sum[:])
}

func writeContract(t *testing.T, body string) string {
	t.Helper()

	return writeContractBytes(t, []byte(body))
}

func writeContractBytes(t *testing.T, body []byte) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "contract.json")
	if err := os.WriteFile(path, body, 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

func setCurrentContract(t *testing.T, path string) {
	t.Helper()

	_, facts, err := readPrivateContractBound(path)
	if err != nil {
		t.Fatal(err)
	}

	t.Setenv(contractFileEnv, path)
	t.Setenv(frozenContractFactsEnv, facts)
	t.Cleanup(func() {
		activeContracts.Lock()
		defer activeContracts.Unlock()

		delete(activeContracts.byPath, path)
	})
}

func TestLoadLabContract_RejectsCaseVariantsAtEveryObjectBoundary(t *testing.T) {
	t.Parallel()

	tests := []struct{ name, from, to, reason string }{
		{name: "root", from: `"version": 2`, to: `"Version": 2`, reason: `contract is missing required key "version"`},
		{name: "generation", from: `{"id": "run-neutral-fixture"}`, to: `{"ID": "run-neutral-fixture"}`, reason: `generation is missing required key "id"`},
		{name: "endpoint", from: `"kind": "forgejo"`, to: `"Kind": "forgejo"`, reason: `endpoints[0] is missing required key "kind"`},
		{name: "later_endpoint", from: `"name": "gitea",`, to: `"Name": "gitea",`, reason: `endpoints[1] is missing required key "name"`},
		{name: "oci_registry", from: `"credential": "endpoint"`, to: `"Credential": "endpoint"`, reason: `endpoints[0].oci_registry is missing required key "credential"`},
		{name: "credential", from: `"username": "fixture-user"`, to: `"Username": "fixture-user"`, reason: `endpoints[0].credential is missing required key "username"`},
		{name: "later_credential", from: `"token": "fake-gitlab-token"`, to: `"Token": "fake-gitlab-token"`, reason: `endpoints[2].credential is missing required key "token"`},
		{name: "revocation", from: `"required": true`, to: `"Required": true`, reason: `endpoints[0].credential.revocation is missing required key "required"`},
		{name: "fulcio", from: `"issuers": [`, to: `"Issuers": [`, reason: `fulcio is missing required key "issuers"`},
		{name: "fulcio_issuer", from: `"endpoint": "forgejo"`, to: `"Endpoint": "forgejo"`, reason: `fulcio.issuers[0] is missing required key "endpoint"`},
		{name: "later_fulcio_issuer", from: `"oidc_issuer": "https://gitlab.compose.forgelab:8443"`, to: `"Oidc_Issuer": "https://gitlab.compose.forgelab:8443"`, reason: `fulcio.issuers[1] is missing required key "oidc_issuer"`},
		{name: "interfaces", from: `"credential_cleanup": {`, to: `"Credential_Cleanup": {`, reason: `interfaces contains unknown or case-mismatched key "Credential_Cleanup"`},
		{name: "cleanup", from: `"contract_file": "/tmp/neutral-targets.run-neutral-fixture.recovery.env"`, to: `"Contract_File": "/tmp/neutral-targets.run-neutral-fixture.recovery.env"`, reason: `interfaces.credential_cleanup is missing required key "contract_file"`},
		{name: "extended_fixture", from: `"protocol": "shape"`, to: `"Protocol": "shape"`, reason: `interfaces.extended_fixture_producer is missing required key "protocol"`},
		{name: "extended_fixture_command", from: `"command": "/opt/forge-lab/scripts/shape.sh"`, to: `"Command": "/opt/forge-lab/scripts/shape.sh"`, reason: `interfaces.extended_fixture_producer is missing required key "command"`},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Helper()

			t.Parallel()

			body := replaceInFixture(t, composeFixtureBody(t), testCase.from, testCase.to)
			_, err := loadLabContract(writeContract(t, body))
			requireContractRefusal(t, err, errs.ErrMalformedInput, testCase.reason)
		})
	}
}

func TestLoadLabContract_RejectsDuplicateKeysAtEveryObjectBoundary(t *testing.T) {
	t.Parallel()

	tests := []struct{ name, key, from, to string }{
		{name: "root", key: "version", from: `"version": 2,`, to: `"version": 2, "version": 2,`},
		{name: "generation", key: "id", from: `{"id": "run-neutral-fixture"}`, to: `{"id": "run-neutral-fixture", "id": "run-neutral-fixture"}`},
		{name: "endpoint", key: "name", from: `"name": "forgejo",`, to: `"name": "forgejo", "name": "forgejo",`},
		{name: "oci_registry", key: "credential", from: `"credential": "endpoint"`, to: `"credential": "endpoint", "credential": "endpoint"`},
		{name: "credential", key: "username", from: `"username": "fixture-user",`, to: `"username": "fixture-user", "username": "fixture-user",`},
		{name: "revocation", key: "required", from: `{"required": true, "generation_id": "run-neutral-fixture"}`, to: `{"required": true, "required": true, "generation_id": "run-neutral-fixture"}`},
		{name: "fulcio", key: "base_url", from: `"base_url": "https://fulcio.compose.forgelab:8443",`, to: `"base_url": "https://fulcio.compose.forgelab:8443", "base_url": "https://fulcio.compose.forgelab:8443",`},
		{name: "fulcio_issuer", key: "endpoint", from: `"endpoint": "forgejo",`, to: `"endpoint": "forgejo", "endpoint": "forgejo",`},
		{name: "interfaces", key: "credential_cleanup", from: `"credential_cleanup": {`, to: `"credential_cleanup": null, "credential_cleanup": {`},
		{name: "cleanup", key: "contract_file", from: `"contract_file": "/tmp/neutral-targets.run-neutral-fixture.recovery.env"`, to: `"contract_file": "/tmp/neutral-targets.run-neutral-fixture.recovery.env", "contract_file": "/tmp/neutral-targets.run-neutral-fixture.recovery.env"`},
		{name: "extended_fixture", key: "protocol", from: `"protocol": "shape"`, to: `"protocol": "shape", "protocol": "shape"`},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Helper()

			t.Parallel()

			body := replaceInFixture(t, composeFixtureBody(t), testCase.from, testCase.to)
			_, err := loadLabContract(writeContract(t, body))
			requireContractRefusal(t, err, errs.ErrMalformedInput, `decode live-target contract: duplicate object key "`+testCase.key+`"`)
		})
	}
}

func TestLoadLabContract_RejectsRequiredBooleanOmittedOrNull(t *testing.T) {
	t.Parallel()

	body := composeFixtureBody(t)
	if _, err := decodeLabContract([]byte(body)); err != nil {
		t.Fatalf("otherwise valid lifecycle fixture: %v", err)
	}

	for _, testCase := range []struct{ name, from, to, reason string }{
		{name: "nonexpiring_omitted", from: `"required": true, `, to: ``, reason: `endpoints[0].credential.revocation is missing required key "required"`},
		{name: "nonexpiring_null", from: `"required": true`, to: `"required": null`, reason: "endpoints[0].credential.revocation.required must be a boolean"},
		{name: "expiring_omitted", from: `"required": false, `, to: ``, reason: `endpoints[2].credential.revocation is missing required key "required"`},
		{name: "expiring_null", from: `"required": false`, to: `"required": null`, reason: "endpoints[2].credential.revocation.required must be a boolean"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			t.Helper()

			t.Parallel()

			_, err := loadLabContract(writeContract(t, replaceInFixture(t, body, testCase.from, testCase.to)))
			requireContractRefusal(t, err, errs.ErrMalformedInput, testCase.reason)
		})
	}
}

func TestLoadLabContract_RejectsInvalidUTF8(t *testing.T) {
	t.Parallel()
	body := []byte(composeFixtureBody(t))
	marker := []byte("fixture-user")
	index := bytes.Index(body, marker)

	body[index] = 0xff
	if _, err := loadLabContract(writeContractBytes(t, body)); err == nil || !strings.Contains(err.Error(), "valid UTF-8") {
		t.Fatalf("invalid UTF-8 result = %v", err)
	}
}

func fixtureBody(t *testing.T, name string) string {
	t.Helper()

	body, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}

	return string(body)
}

func composeFixtureBody(t *testing.T) string {
	t.Helper()

	return fixtureBody(t, "neutral-targets-v2-compose.json")
}

// replaceInFixture replaces the first occurrence of from, failing the test when
// the fixture no longer contains it: a stale anchor leaves the valid fixture in
// place, which turns a refusal case into a failure for the wrong reason and an
// acceptance case into a pass that proves nothing.
func replaceInFixture(t *testing.T, body, from, to string) string {
	t.Helper()

	if !strings.Contains(body, from) {
		t.Fatalf("fixture no longer contains %q", from)
	}

	return strings.Replace(body, from, to, 1)
}

// cutFixture returns body with the span from startMarker up to endMarker
// replaced by insert, failing the test when either marker is absent.
func cutFixture(t *testing.T, body, startMarker, endMarker, insert string) string {
	t.Helper()

	start := strings.Index(body, startMarker)
	if start < 0 {
		t.Fatalf("fixture no longer contains %q", startMarker)
	}

	end := strings.Index(body[start:], endMarker)
	if end < 0 {
		t.Fatalf("fixture no longer contains %q after %q", endMarker, startMarker)
	}

	return body[:start] + insert + body[start+end:]
}

// requireContractRefusal requires exactly reason, classified as sentinel, so a
// refusal from an unrelated check cannot stand in for the one under test.
func requireContractRefusal(t *testing.T, err, sentinel error, reason string) {
	t.Helper()

	if !errors.Is(err, sentinel) || err.Error() != reason+": "+sentinel.Error() {
		t.Fatalf("refusal = %v, want %q classified as %v", err, reason, sentinel)
	}
}

func TestLoadLabContract_V2CanonicalFixtures(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"neutral-targets-v2-compose.json", "neutral-targets-v2-github.json"} {
		t.Run(name, func(t *testing.T) {
			t.Helper()

			t.Parallel()

			contract, err := loadLabContract(writeContract(t, fixtureBody(t, name)))
			if err != nil {
				t.Fatalf("loadLabContract() rejected canonical fixture: %v", err)
			}

			if contract.Version != 2 || !contract.CAFile.Set || !contract.Fulcio.Set {
				t.Fatalf("required v2 projection is incomplete: %+v", contract)
			}
		})
	}
}

func TestCanonicalFixtures_MatchRecordedHashes(t *testing.T) {
	t.Parallel()

	want := map[string]string{
		"neutral-targets-v2-compose.json": "fe2f623552b2cfc6733ab004d2fd951f62846fb9a9415679d16a5eec1af0ad90",
		"neutral-targets-v2-github.json":  "65f08c37a7824c4d899b8560077fa92ccbae96af33d7c5c2b36b151c37810959",
	}
	for name, wantHash := range want {
		body := []byte(fixtureBody(t, name))
		if got := hexDigest(body); got != wantHash {
			t.Errorf("%s hash = %s, want recorded hash %s", name, got, wantHash)
		}
	}
}

func TestLoadLabContract_V1DebtIsRejectedExplicitly(t *testing.T) {
	t.Parallel()

	for _, body := range []string{
		strings.Replace(composeFixtureBody(t), `"version": 2`, `"version": 1`, 1),
		`{"version":1,"interfaces":{"credential_cleanup":"/old/v1/cleanup"}}`,
	} {
		_, err := loadLabContract(writeContract(t, body))
		if err == nil || !strings.Contains(err.Error(), "version 1 is retired") {
			t.Fatalf("v1 rejection = %v, want explicit retired-version error", err)
		}
	}
}

func TestLoadLabContract_V2VersionAndStructuralRefusals(t *testing.T) {
	t.Parallel()

	const cleanupFile = `"contract_file": "/tmp/neutral-targets.run-neutral-fixture.recovery.env"`

	tests := []struct {
		name     string
		mutate   func(*testing.T, string) string
		sentinel error
		reason   string
	}{
		{name: "version_3", mutate: func(t *testing.T, body string) string {
			t.Helper()

			return replaceInFixture(t, body, `"version": 2`, `"version": 3`)
		}, sentinel: errs.ErrValidation, reason: "live-target contract version must be 2, got 3"},
		{name: "unknown_root", mutate: func(t *testing.T, body string) string {
			t.Helper()

			return replaceInFixture(t, body, `"version": 2,`, `"version": 2, "authorization": {},`)
		}, sentinel: errs.ErrMalformedInput, reason: `contract contains unknown or case-mismatched key "authorization"`},
		{name: "unknown_nested_registry", mutate: func(t *testing.T, body string) string {
			t.Helper()

			return replaceInFixture(t, body, `"credential": "endpoint"`, `"credential": "endpoint", "scope": "push"`)
		}, sentinel: errs.ErrMalformedInput, reason: `endpoints[0].oci_registry contains unknown or case-mismatched key "scope"`},
		{name: "unknown_later_endpoint", mutate: func(t *testing.T, body string) string {
			t.Helper()

			return replaceInFixture(t, body, `"name": "gitea",`, `"name": "gitea", "extra": 1,`)
		}, sentinel: errs.ErrMalformedInput, reason: `endpoints[1] contains unknown or case-mismatched key "extra"`},
		{name: "unknown_nested_fulcio", mutate: func(t *testing.T, body string) string {
			t.Helper()

			return replaceInFixture(t, body, `"issuers": [`, `"trust_root": "x", "issuers": [`)
		}, sentinel: errs.ErrMalformedInput, reason: `fulcio contains unknown or case-mismatched key "trust_root"`},
		{name: "unknown_nested_cleanup", mutate: func(t *testing.T, body string) string {
			t.Helper()

			return replaceInFixture(t, body, cleanupFile, cleanupFile+`, "argv": []`)
		}, sentinel: errs.ErrMalformedInput, reason: `interfaces.credential_cleanup contains unknown or case-mismatched key "argv"`},
		{name: "duplicate_root", mutate: func(t *testing.T, body string) string {
			t.Helper()

			return replaceInFixture(t, body, `"version": 2,`, `"version": 2, "version": 2,`)
		}, sentinel: errs.ErrMalformedInput, reason: `decode live-target contract: duplicate object key "version"`},
		{name: "duplicate_nested", mutate: func(t *testing.T, body string) string {
			t.Helper()

			return replaceInFixture(t, body, `"credential": "endpoint"`, `"credential": "endpoint", "credential": "endpoint"`)
		}, sentinel: errs.ErrMalformedInput, reason: `decode live-target contract: duplicate object key "credential"`},
		{name: "trailing_document", mutate: func(_ *testing.T, body string) string { return body + `{}` },
			sentinel: errs.ErrMalformedInput, reason: "decode live-target contract: another JSON value follows the contract"},
		{name: "truncated", mutate: func(_ *testing.T, body string) string { return body[:len(body)/2] },
			sentinel: errs.ErrMalformedInput, reason: "decode live-target contract: unexpected EOF"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Helper()

			t.Parallel()

			_, err := loadLabContract(writeContract(t, testCase.mutate(t, composeFixtureBody(t))))
			requireContractRefusal(t, err, testCase.sentinel, testCase.reason)
		})
	}
}

func TestLoadLabContract_RejectsHistoricalEmptyInterfacesObject(t *testing.T) {
	t.Parallel()

	_, err := loadLabContract(writeContract(t, fixtureBody(t, "historical-empty-interfaces.json")))
	if err == nil || !strings.Contains(err.Error(), "at least one interface") {
		t.Fatalf("interfaces:{} rejection = %v", err)
	}
}

func TestNeutralV2Corpus(t *testing.T) {
	t.Parallel()

	var corpus struct {
		Schema  string `json:"schema"`
		Version int    `json:"version"`
		Cases   []struct {
			Name   string `json:"name"`
			Path   string `json:"path"`
			SHA256 string `json:"sha256"`
			Expect struct {
				Accepted *bool  `json:"accepted"`
				Error    string `json:"error"`
			} `json:"expect"`
		} `json:"cases"`
	}
	if err := json.Unmarshal([]byte(fixtureBody(t, "neutral-v2-corpus.json")), &corpus); err != nil {
		t.Fatal(err)
	}

	if corpus.Schema != "reusable-ci-neutral-v2-corpus" || corpus.Version != 1 || len(corpus.Cases) == 0 {
		t.Fatalf("unexpected corpus header: schema=%q version=%d cases=%d", corpus.Schema, corpus.Version, len(corpus.Cases))
	}

	errorCategories := map[string]struct {
		sentinel   error
		diagnostic string
	}{
		"size":    {sentinel: errs.ErrMalformedInput, diagnostic: "live-target contract is empty"},
		"json":    {sentinel: errs.ErrMalformedInput, diagnostic: "decode live-target contract: EOF"},
		"version": {sentinel: errs.ErrValidation, diagnostic: "live-target contract version 1 is retired; version 2 is required"},
		"shape":   {sentinel: errs.ErrMalformedInput, diagnostic: `contract is missing required key "generation"`},
	}

	for _, testCase := range corpus.Cases {
		t.Run(testCase.Name, func(t *testing.T) {
			t.Helper()

			body := []byte(fixtureBody(t, testCase.Path))
			if got := hexDigest(body); got != testCase.SHA256 {
				t.Fatalf("fixture hash = %s, want recorded hash %s", got, testCase.SHA256)
			}

			_, err := loadLabContract(writeContractBytes(t, body))
			switch {
			case testCase.Expect.Accepted != nil && *testCase.Expect.Accepted && err != nil:
				t.Fatalf("accepted fixture was rejected: %v", err)
			case testCase.Expect.Error != "":
				expectation, known := errorCategories[testCase.Expect.Error]
				if !known {
					t.Fatalf("corpus names unknown error category %q", testCase.Expect.Error)
				}

				assertNeutralV2CorpusError(t, testCase.Expect.Error, err, expectation.sentinel, expectation.diagnostic)
			case testCase.Expect.Accepted == nil || !*testCase.Expect.Accepted:
				t.Fatal("producer corpus case declares neither acceptance nor an error category")
			}
		})
	}
}

func assertNeutralV2CorpusError(t *testing.T, category string, err, sentinel error, diagnostic string) {
	t.Helper()

	if !errors.Is(err, sentinel) {
		t.Fatalf("producer %s fixture error = %v, want sentinel %v", category, err, sentinel)
	}

	if !strings.Contains(err.Error(), diagnostic) {
		t.Fatalf("producer %s fixture diagnostic = %q, want %q", category, err, diagnostic)
	}

	if classified := classifyNeutralV2CorpusDiagnostic(err); classified != category {
		t.Fatalf("producer fixture diagnostic %q classified as %q, want %q", err, classified, category)
	}
}

func classifyNeutralV2CorpusDiagnostic(err error) string {
	if err == nil {
		return ""
	}

	diagnostic := err.Error()
	switch {
	case strings.Contains(diagnostic, "live-target contract is empty"):
		return "size"
	case strings.Contains(diagnostic, "decode live-target contract: EOF"):
		return "json"
	case strings.Contains(diagnostic, "live-target contract version 1 is retired; version 2 is required"):
		return "version"
	case strings.Contains(diagnostic, `contract is missing required key "generation"`):
		return "shape"
	default:
		return ""
	}
}

func TestLoadLabContract_V2RequiredNullableDistinctions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*testing.T, string) string
		reason string
	}{
		{name: "missing_root_fulcio", mutate: func(t *testing.T, body string) string {
			t.Helper()

			return cutFixture(t, body, `  "fulcio": {`, `  "interfaces":`, "")
		}, reason: `contract is missing required key "fulcio"`},
		{name: "missing_root_ca_file", mutate: func(t *testing.T, body string) string {
			t.Helper()

			return replaceInFixture(t, body, `  "ca_file": "/opt/forge-lab/certs/forge-lab-ca.crt",
`, "")
		}, reason: `contract is missing required key "ca_file"`},
		{name: "missing_oci_registry", mutate: func(t *testing.T, body string) string {
			t.Helper()

			return cutFixture(t, body, `      "oci_registry": {`, `      "credential": {`, "")
		}, reason: `endpoints[0] is missing required key "oci_registry"`},
		{name: "null_credential", mutate: func(t *testing.T, body string) string {
			t.Helper()

			return cutFixture(t, body, `      "credential": {`, "\n    },", `      "credential": null`)
		}, reason: "endpoints[0].credential must be an object, not null"},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Helper()

			t.Parallel()

			_, err := loadLabContract(writeContract(t, testCase.mutate(t, composeFixtureBody(t))))
			requireContractRefusal(t, err, errs.ErrMalformedInput, testCase.reason)
		})
	}

	github, err := loadLabContract(writeContract(t, fixtureBody(t, "neutral-targets-v2-github.json")))
	if err != nil || github.CAFile.Value != nil || github.Fulcio.Value != nil {
		t.Fatalf("required explicit nulls were not accepted: contract=%+v err=%v", github, err)
	}

	withNullRegistry := replaceInFixture(t, composeFixtureBody(t), `"oci_registry": {
        "base_url": "https://forgejo.compose.forgelab:8443",
        "credential": "endpoint"
      }`, `"oci_registry": null`)
	if _, err := loadLabContract(writeContract(t, withNullRegistry)); err != nil {
		t.Fatalf("required explicit null oci_registry was rejected: %v", err)
	}
}

func TestLoadLabContract_V2OptionalFieldsMayBeOmittedOrNull(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*testing.T, string) string
	}{
		{name: "interfaces_omitted", mutate: func(t *testing.T, body string) string {
			t.Helper()

			withoutInterfaces := cutFixture(t, body, `  "interfaces": {`, "\n}\n", "")

			return strings.Replace(withoutInterfaces, "  },\n\n}\n", "  }\n}\n", 1)
		}},
		{name: "interfaces_null", mutate: func(t *testing.T, body string) string {
			t.Helper()

			return cutFixture(t, body, `  "interfaces": {`, "\n}\n", `  "interfaces": null`)
		}},
		{name: "git_base_url_omitted", mutate: func(t *testing.T, body string) string {
			t.Helper()

			return replaceInFixture(t, body, `      "git_base_url": "https://forgejo.compose.forgelab:8443",
`, "")
		}},
		{name: "git_base_url_null", mutate: func(t *testing.T, body string) string {
			t.Helper()

			return replaceInFixture(t, body, `"git_base_url": "https://forgejo.compose.forgelab:8443"`, `"git_base_url": null`)
		}},
		{name: "provider_id_omitted", mutate: func(t *testing.T, body string) string {
			t.Helper()

			return replaceInFixture(t, body, `        "provider_id": "101",
`, "")
		}},
		{name: "expires_at_omitted", mutate: func(t *testing.T, body string) string {
			t.Helper()

			return replaceInFixture(t, body, `        "expires_at": null,
`, "")
		}},
		{name: "expiring_revocation_generation_omitted", mutate: func(t *testing.T, body string) string {
			t.Helper()

			return replaceInFixture(t, body, `"revocation": {"required": false, "generation_id": null}`, `"revocation": {"required": false}`)
		}},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Helper()

			t.Parallel()

			if _, err := loadLabContract(writeContract(t, testCase.mutate(t, composeFixtureBody(t)))); err != nil {
				t.Fatalf("optional v2 field form was rejected: %v", err)
			}
		})
	}
}

func TestLoadLabContract_NonExpiringRevocationRequiresGeneration(t *testing.T) {
	t.Parallel()

	body := replaceInFixture(t, composeFixtureBody(t),
		`"revocation": {"required": true, "generation_id": "run-neutral-fixture"}`,
		`"revocation": {"required": true}`)
	_, err := loadLabContract(writeContract(t, body))
	requireContractRefusal(t, err, errs.ErrValidation, `endpoint "forgejo" non-expiring credential is not bound to generation "run-neutral-fixture"`)
}

func TestLoadLabContract_CapabilitiesAreASetAfterWireParsing(t *testing.T) {
	t.Parallel()

	body := replaceInFixture(t, composeFixtureBody(t),
		`"capabilities": ["artifacts", "organizations", "packages", "repositories", "workflow-runs"]`,
		`"capabilities": ["artifacts", "organizations", "artifacts", "packages", "repositories", "workflow-runs", "packages"]`)

	contract, err := loadLabContract(writeContract(t, body))
	if err != nil {
		t.Fatalf("wire parser rejected duplicate non-empty capabilities: %v", err)
	}

	endpoint, err := contract.endpointFor("forgejo", "")
	if err != nil {
		t.Fatal(err)
	}

	want := []string{"artifacts", "organizations", "packages", "repositories", "workflow-runs"}
	if !slices.Equal(endpoint.Capabilities, want) {
		t.Fatalf("normalized capabilities = %v, want %v", endpoint.Capabilities, want)
	}

	if err := endpoint.requireLiveCapabilities(); err != nil || !endpoint.hasCapability("artifacts") {
		t.Fatalf("set-like capability consumers rejected normalized capabilities: %v", err)
	}
}

func TestLoadLabContract_RejectsEmptyCapability(t *testing.T) {
	t.Parallel()

	body := replaceInFixture(t, composeFixtureBody(t), `"capabilities": ["artifacts",`, `"capabilities": ["", "artifacts",`)
	_, err := loadLabContract(writeContract(t, body))
	requireContractRefusal(t, err, errs.ErrValidation, `endpoint "forgejo" has an empty capability`)
}

func TestEndpointFor_DuplicateKindsRequireExactOperatorSelection(t *testing.T) {
	t.Parallel()

	body := replaceInFixture(t, composeFixtureBody(t), `"kind": "gitea"`, `"kind": "forgejo"`)

	contract, err := loadLabContract(writeContract(t, body))
	if err != nil {
		t.Fatalf("wire parser rejected duplicate endpoint kinds: %v", err)
	}

	if _, selectionErr := contract.endpointFor("forgejo", ""); selectionErr == nil ||
		!strings.Contains(selectionErr.Error(), "RC_LIVE_FORGEJO_ENDPOINT") {
		t.Fatalf("ambiguous kind result = %v", selectionErr)
	}

	endpoint, err := contract.endpointFor("forgejo", "gitea")
	if err != nil || endpoint.Name != "gitea" {
		t.Fatalf("exact endpoint selection = %+v, %v", endpoint, err)
	}

	if _, selectionErr := contract.endpointFor("forgejo", "missing"); selectionErr == nil {
		t.Fatal("endpoint selection invented a missing exact name")
	}
}

func TestLoadLabContract_V2URLGrammar(t *testing.T) {
	t.Parallel()

	const (
		forgejoAPI   = `https://forgejo.compose.forgelab:8443/api/v1`
		gitlabIssuer = `"oidc_issuer": "https://gitlab.compose.forgelab:8443"`
		urlRule      = " must be an HTTPS URL without userinfo, query, or fragment"
	)

	tests := []struct{ name, from, to, reason string }{
		{name: "backslash", from: forgejoAPI, to: `https://forgejo.compose.forgelab:8443\\api\\v1`, reason: "forgejo api_base_url contains whitespace, controls, or a backslash"},
		{name: "userinfo", from: forgejoAPI, to: `https://user@forgejo.compose.forgelab:8443/api/v1`, reason: "forgejo api_base_url" + urlRule},
		{name: "unsafe_port", from: forgejoAPI, to: `https://forgejo.compose.forgelab:65536/api/v1`, reason: "forgejo api_base_url has an unsafe port"},
		{name: "query", from: forgejoAPI, to: forgejoAPI + `?admin=1`, reason: "forgejo api_base_url" + urlRule},
		{name: "fragment", from: forgejoAPI, to: forgejoAPI + `#x`, reason: "forgejo api_base_url" + urlRule},
		{name: "oci_path", from: `"base_url": "https://forgejo.compose.forgelab:8443"`, to: `"base_url": "https://forgejo.compose.forgelab:8443/v2"`, reason: "forgejo oci_registry.base_url must be an HTTPS origin without a path"},
		{name: "fulcio_path", from: `https://fulcio.compose.forgelab:8443",`, to: `https://fulcio.compose.forgelab:8443/api",`, reason: "fulcio.base_url must be an HTTPS origin without a path"},
		{name: "gitea_registry_not_forge_origin", from: `"base_url": "https://gitea.compose.forgelab:8443"`, to: `"base_url": "https://registry.gitea.compose.forgelab:8443"`,
			reason: `gitea OCI registry "registry.gitea.compose.forgelab:8443" is not the accepted local relationship to "gitea.compose.forgelab:8443"`},
		{name: "unknown_fulcio_endpoint_reference", from: `"endpoint": "forgejo"`, to: `"endpoint": "missing"`, reason: `fulcio issuer references unknown endpoint "missing"`},
		{name: "later_endpoint_api_userinfo", from: `https://gitlab.compose.forgelab:8443/api/v4`, to: `https://user@gitlab.compose.forgelab:8443/api/v4`, reason: "gitlab api_base_url" + urlRule},
		{name: "later_endpoint_web_path", from: `"web_base_url": "https://gitlab.compose.forgelab:8443"`, to: `"web_base_url": "https://gitlab.compose.forgelab:8443/x"`, reason: "gitlab web_base_url must be an HTTPS origin without a path"},
		{name: "later_endpoint_git_query", from: `"git_base_url": "https://gitlab.compose.forgelab:8443"`, to: `"git_base_url": "https://gitlab.compose.forgelab:8443?x=1"`, reason: "gitlab git_base_url" + urlRule},
		{name: "later_fulcio_issuer_http", from: gitlabIssuer, to: `"oidc_issuer": "http://gitlab.compose.forgelab:8443"`, reason: "fulcio issuer gitlab" + urlRule},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Helper()

			t.Parallel()

			body := replaceInFixture(t, composeFixtureBody(t), testCase.from, testCase.to)
			_, err := loadLabContract(writeContract(t, body))
			requireContractRefusal(t, err, errs.ErrValidation, testCase.reason)
		})
	}
}

// TestLoadLabContract_V2FieldRefusalsNameTheirField changes one field of an
// otherwise valid contract at a time, including fields of later endpoints and
// both interfaces, and requires the refusal that names that field.
func TestLoadLabContract_V2FieldRefusalsNameTheirField(t *testing.T) {
	t.Parallel()

	//nolint:gosec // The fixture's synthetic GitLab credential fields.
	const gitlabCredential = `"username": "fixture-user",
        "token": "fake-gitlab-token"`

	//nolint:gosec // Synthetic fixture credentials, rewritten one field at a time.
	tests := []struct{ name, from, to, reason string }{
		{name: "generation_id", from: `{"id": "run-neutral-fixture"}`, to: `{"id": "Run_Bad"}`, reason: `generation.id "Run_Bad" is invalid`},
		{name: "ca_file_relative", from: `"ca_file": "/opt/forge-lab/certs/forge-lab-ca.crt"`, to: `"ca_file": "certs/ca.crt"`, reason: "ca_file must be a clean absolute path"},
		{name: "later_endpoint_name", from: `"name": "gitea",`, to: `"name": "Gitea",`, reason: `endpoint name "Gitea" is invalid`},
		{name: "later_endpoint_kind", from: `"kind": "gitlab"`, to: `"kind": "bitbucket"`, reason: `endpoint "gitlab" has unsupported kind "bitbucket"`},
		{name: "later_credential_token", from: `"token": "fake-gitlab-token"`, to: `"token": ""`, reason: `endpoint "gitlab" credential is incomplete`},
		{name: "later_credential_username", from: gitlabCredential, to: strings.Replace(gitlabCredential, `"fixture-user"`, `""`, 1), reason: `endpoint "gitlab" credential is incomplete`},
		{name: "later_registry_credential", from: `"base_url": "https://registry.gitlab.compose.forgelab:8443",
        "credential": "endpoint"`, to: `"base_url": "https://registry.gitlab.compose.forgelab:8443",
        "credential": "anonymous"`, reason: `endpoint "gitlab" oci_registry.credential must equal "endpoint"`},
		{name: "later_expiring_revocation_generation", from: `"revocation": {"required": false, "generation_id": null}`, to: `"revocation": {"required": false, "generation_id": "other-run"}`,
			reason: `endpoint "gitlab" expiring credential has revocation fallback metadata`},
		{name: "cleanup_command_relative", from: `"command": "/home/garga/.local/state/forge-lab/target-cleanup-run-neutral-fixture/cleanup"`, to: `"command": "cleanup"`, reason: "credential_cleanup.command must be a clean absolute path"},
		{name: "cleanup_command_sha256", from: `"` + strings.Repeat("a", 64) + `"`, to: `"AAAA"`, reason: "credential_cleanup.command_sha256 must be 64 lowercase hexadecimal characters"},
		{name: "cleanup_contract_file_relative", from: `"contract_file": "/tmp/neutral-targets.run-neutral-fixture.recovery.env"`, to: `"contract_file": "recovery.env"`, reason: "credential_cleanup.contract_file must be a clean absolute path"},
		{name: "extended_fixture_command_relative", from: `"command": "/opt/forge-lab/scripts/shape.sh"`, to: `"command": "shape.sh"`, reason: "extended_fixture_producer.command must be a clean absolute path"},
		{name: "extended_fixture_command_sha256", from: `"` + strings.Repeat("b", 64) + `"`, to: `"bbbb"`, reason: "extended_fixture_producer.command_sha256 must be 64 lowercase hexadecimal characters"},
		{name: "extended_fixture_protocol", from: `"protocol": "shape"`, to: `"protocol": "other"`, reason: `extended_fixture_producer.protocol must equal "shape"`},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Helper()

			t.Parallel()

			body := replaceInFixture(t, composeFixtureBody(t), testCase.from, testCase.to)
			_, err := loadLabContract(writeContract(t, body))
			requireContractRefusal(t, err, errs.ErrValidation, testCase.reason)
		})
	}
}

func TestLoadLabContract_V2ProjectsProducerFacts(t *testing.T) {
	t.Parallel()

	contract, err := loadLabContract(writeContract(t, composeFixtureBody(t)))
	if err != nil {
		t.Fatal(err)
	}

	forgejo, err := contract.endpointFor("forgejo", "")
	if err != nil {
		t.Fatal(err)
	}

	if forgejo.registryOrigin() != "https://forgejo.compose.forgelab:8443" || len(forgejo.Capabilities) != 5 {
		t.Fatalf("forgejo projection = %+v", forgejo)
	}

	fulcioURL, issuer, ok := contract.fulcioFor("forgejo")
	if !ok || fulcioURL != "https://fulcio.compose.forgelab:8443" || issuer != "https://forgejo.compose.forgelab:8443/api/actions" {
		t.Fatalf("Fulcio projection = %q %q %v", fulcioURL, issuer, ok)
	}

	forgeAuthorities, err := forgejo.forgeAuthorities()
	if err != nil {
		t.Fatal(err)
	}

	registryAuthorities, err := forgejo.registryAuthorities()
	if err != nil {
		t.Fatal(err)
	}

	oidcAllowed, err := oidcAuthorities(fulcioURL)
	if err != nil {
		t.Fatal(err)
	}

	forgeOrigin := []string{"https://forgejo.compose.forgelab:8443"}

	fulcioOrigin := []string{"https://fulcio.compose.forgelab:8443"}
	if !slices.Equal(forgeAuthorities, forgeOrigin) ||
		!slices.Equal(registryAuthorities, forgeOrigin) ||
		!slices.Equal(oidcAllowed, fulcioOrigin) {
		t.Fatalf("authority classes: forge=%v registry=%v OIDC=%v", forgeAuthorities, registryAuthorities, oidcAllowed)
	}

	gitlab, err := contract.endpointFor("gitlab", "")
	if err != nil {
		t.Fatal(err)
	}

	gitlabRegistryAuthorities, err := gitlab.registryAuthorities()
	if err != nil {
		t.Fatal(err)
	}

	wantGitLabRegistryAuthorities := []string{
		"https://registry.gitlab.compose.forgelab:8443",
		"https://gitlab.compose.forgelab:8443",
	}
	if !slices.Equal(gitlabRegistryAuthorities, wantGitLabRegistryAuthorities) {
		t.Fatalf("GitLab registry authorities = %v, want %v", gitlabRegistryAuthorities, wantGitLabRegistryAuthorities)
	}

	command, contractFile := contract.cleanupPair()
	if command != "/home/garga/.local/state/forge-lab/target-cleanup-run-neutral-fixture/cleanup" ||
		contractFile != "/tmp/neutral-targets.run-neutral-fixture.recovery.env" {
		t.Fatalf("cleanup pair = %q %q", command, contractFile)
	}

	if contract.Interfaces.ExtendedFixtureProducer.Protocol != extendedFixtureProtocol {
		t.Fatalf("extended fixture projection = %+v", contract.Interfaces.ExtendedFixtureProducer)
	}
}

// TestLoadLabContract_V2NormalizedProjectionIsComplete compares the whole
// normalized contract. The fixture's endpoints share a username, a credential
// name and a creation time, so the test gives each endpoint its own first: a
// projection that read endpoints[0]'s credential for every endpoint, or
// crossed two fields of one, then shows up as a wrong value rather than an
// equal one.
func TestLoadLabContract_V2NormalizedProjectionIsComplete(t *testing.T) {
	t.Parallel()

	body := composeFixtureBody(t)
	body = replaceInFixture(t, body, `"username": "fixture-user",
        "token": "fake-gitea-token",
        "provider_id": "102",
        "name": "lab-targets-run-neutral-fixture",
        "created_at": "2026-08-06T10:00:00Z"`, `"username": "gitea-user",
        "token": "fake-gitea-token",
        "provider_id": "102",
        "name": "gitea-token-name",
        "created_at": "2026-08-06T11:00:00Z"`)
	body = replaceInFixture(t, body, `"username": "fixture-user",
        "token": "fake-gitlab-token",
        "provider_id": "103",
        "name": "lab-targets-run-neutral-fixture",
        "created_at": "2026-08-06T10:00:00Z"`, `"username": "gitlab-user",
        "token": "fake-gitlab-token",
        "provider_id": "103",
        "name": "gitlab-token-name",
        "created_at": "2026-08-06T12:00:00Z"`)
	body = replaceInFixture(t, body, `"capabilities": ["artifacts", "groups", "packages", "repositories", "workflow-runs"]`,
		`"capabilities": ["workflow-runs", "groups", "artifacts", "packages", "repositories", "groups"]`)

	contract, err := loadLabContract(writeContract(t, body))
	if err != nil {
		t.Fatal(err)
	}

	text := func(value string) *string { return &value }
	generation := "run-neutral-fixture"
	caFile := "/opt/forge-lab/certs/forge-lab-ca.crt"
	registry := func(origin string) requiredNullable[labOCIRegistry] {
		return requiredNullable[labOCIRegistry]{Set: true, Value: &labOCIRegistry{BaseURL: origin, Credential: "endpoint"}}
	}
	endpoint := func(name, api string, capabilities []string, registryOrigin string, credential labCredential) labEndpoint {
		origin := "https://" + name + ".compose.forgelab:8443"

		return labEndpoint{
			Name: name, Kind: name, WebBaseURL: origin, APIBaseURL: origin + api, GitBaseURL: text(origin),
			Capabilities: capabilities, OCIRegistry: registry(registryOrigin), Credential: credential,
		}
	}
	capabilities := func(scope string) []string {
		return slices.Sorted(slices.Values(append(strings.Fields("artifacts packages repositories workflow-runs"), scope)))
	}

	//nolint:gosec // Synthetic fixture credentials the projection must carry unchanged.
	want := labContract{
		Version:    2,
		Generation: labGeneration{ID: generation},
		CAFile:     requiredNullable[string]{Set: true, Value: &caFile},
		Endpoints: []labEndpoint{
			endpoint("forgejo", "/api/v1", capabilities("organizations"), "https://forgejo.compose.forgelab:8443", labCredential{
				Username: "fixture-user", Token: "fake-forgejo-token", ProviderID: text("101"), Name: "lab-targets-run-neutral-fixture",
				CreatedAt: "2026-08-06T10:00:00Z", Revocation: &labRevocation{Required: true, GenerationID: &generation},
			}),
			endpoint("gitea", "/api/v1", capabilities("organizations"), "https://gitea.compose.forgelab:8443", labCredential{
				Username: "gitea-user", Token: "fake-gitea-token", ProviderID: text("102"), Name: "gitea-token-name",
				CreatedAt: "2026-08-06T11:00:00Z", Revocation: &labRevocation{Required: true, GenerationID: &generation},
			}),
			endpoint(string(provider.ForgeGitLab), "/api/v4", capabilities("groups"), "https://registry.gitlab.compose.forgelab:8443", labCredential{
				Username: "gitlab-user", Token: "fake-gitlab-token", ProviderID: text("103"), Name: "gitlab-token-name",
				CreatedAt: "2026-08-06T12:00:00Z", ExpiresAt: text("2026-09-05"), Revocation: &labRevocation{Required: false},
			}),
		},
		Fulcio: requiredNullable[labFulcio]{Set: true, Value: &labFulcio{
			BaseURL: "https://fulcio.compose.forgelab:8443",
			Issuers: []labFulcioIssuer{
				{Endpoint: "forgejo", OIDCIssuer: "https://forgejo.compose.forgelab:8443/api/actions"},
				{Endpoint: string(provider.ForgeGitLab), OIDCIssuer: "https://gitlab.compose.forgelab:8443"},
			},
		}},
		Interfaces: &labInterfaces{
			CredentialCleanup: &labCommandInterface{
				Command:       "/home/garga/.local/state/forge-lab/target-cleanup-run-neutral-fixture/cleanup",
				CommandSHA256: text(strings.Repeat("a", 64)),
				ContractFile:  "/tmp/neutral-targets.run-neutral-fixture.recovery.env",
			},
			ExtendedFixtureProducer: &labFixtureInterface{
				Command: "/opt/forge-lab/scripts/shape.sh", CommandSHA256: text(strings.Repeat("b", 64)), Protocol: "shape",
			},
		},
	}

	if !reflect.DeepEqual(contract, want) {
		got, gotErr := json.MarshalIndent(contract, "", "  ")
		expected, wantErr := json.MarshalIndent(want, "", "  ")
		t.Fatalf("normalized contract (%v):\n%s\nwant (%v):\n%s", gotErr, got, wantErr, expected)
	}
}

// TestLoadLabContract_FileSafety gives each file guard an otherwise valid
// contract, so the refusal it requires can only come from that guard, and
// pairs the mode and size limits with accepted values at the boundary.
func TestLoadLabContract_FileSafety(t *testing.T) {
	t.Parallel()
	body := composeFixtureBody(t)

	t.Run("relative", func(t *testing.T) {
		t.Helper()

		const relative = "testdata/neutral-targets-v2-compose.json"

		if _, err := os.Stat(relative); err != nil {
			t.Fatalf("relative control is not an existing valid contract: %v", err)
		}

		_, err := loadLabContract(relative)
		requireContractRefusal(t, err, errs.ErrValidation, `contract path "`+relative+`" must be a clean absolute path`)
	})
	t.Run("symlink", func(t *testing.T) {
		t.Helper()

		dir := t.TempDir()

		realPath := filepath.Join(dir, "real.json")
		if err := os.WriteFile(realPath, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}

		if _, err := loadLabContract(realPath); err != nil {
			t.Fatalf("link target is not a valid contract: %v", err)
		}

		link := filepath.Join(dir, "link.json")
		if err := os.Symlink(realPath, link); err != nil {
			t.Fatal(err)
		}

		_, err := loadLabContract(link)
		requireContractRefusal(t, err, errs.ErrValidation, "live-target contract must be a regular non-symlink file")
	})
	t.Run("symlink_parent", func(t *testing.T) {
		t.Helper()

		root := t.TempDir()

		realParent := filepath.Join(root, "real")
		if err := os.Mkdir(realParent, 0o700); err != nil {
			t.Fatal(err)
		}

		realPath := filepath.Join(realParent, "contract.json")
		if err := os.WriteFile(realPath, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}

		linkedParent := filepath.Join(root, "linked")
		if err := os.Symlink(realParent, linkedParent); err != nil {
			t.Fatal(err)
		}

		_, err := loadLabContract(filepath.Join(linkedParent, "contract.json"))
		requireContractRefusal(t, err, errs.ErrValidation, `contract parent "`+linkedParent+`" must not contain symbolic links`)
	})
	t.Run("permissions", func(t *testing.T) {
		t.Helper()

		path := writeContract(t, body)
		if err := os.Chmod(path, 0o400); err != nil {
			t.Fatal(err)
		}

		if _, err := loadLabContract(path); err != nil {
			t.Fatalf("owner read-only contract was rejected: %v", err)
		}

		if err := os.Chmod(path, 0o644); err != nil { //nolint:gosec // Deliberately unsafe mode must be rejected.
			t.Fatal(err)
		}

		_, err := loadLabContract(path)
		requireContractRefusal(t, err, errs.ErrValidation, "live-target contract must have mode 0400 or 0600")
	})
	t.Run("size", func(t *testing.T) {
		t.Helper()

		// Trailing whitespace keeps the padded document valid JSON, so only
		// the size guard can tell the two apart.
		atLimit := body + strings.Repeat(" ", maxContractBytes-len(body))
		if _, err := loadLabContract(writeContract(t, atLimit)); err != nil {
			t.Fatalf("valid contract of exactly %d bytes was rejected: %v", maxContractBytes, err)
		}

		_, err := loadLabContract(writeContract(t, atLimit+" "))
		requireContractRefusal(t, err, errs.ErrValidation, "live-target contract exceeds 65536 bytes")
	})
}

func TestCurrentLabContract_ReturnedValuesAreIndependent(t *testing.T) {
	body := composeFixtureBody(t)

	want, err := decodeLabContract([]byte(body))
	if err != nil {
		t.Fatal(err)
	}

	setCurrentContract(t, writeContract(t, body))

	// Exercise both the first return and cache hits, including concurrent callers.
	mutate := func() {
		got, readErr := currentLabContract()
		if readErr != nil {
			t.Error(readErr)

			return
		}

		if !reflect.DeepEqual(got, want) {
			t.Error("returned contract differs from validated fixture")

			return
		}

		got.Endpoints[0].Name = "changed"
		got.Endpoints[0].Credential.Token = "changed"
		*got.Endpoints[0].Credential.ProviderID = "999"
		*got.Endpoints[0].Credential.Revocation.GenerationID = "changed"
		*got.Endpoints[2].Credential.ExpiresAt = "changed"
		got.Endpoints[0].Capabilities[0] = "changed"
		*got.Endpoints[0].GitBaseURL = "changed"
		got.Endpoints[0].OCIRegistry.Value.BaseURL = "changed"
		*got.CAFile.Value = "changed"
		got.Fulcio.Value.Issuers[0].OIDCIssuer = "changed"
		got.Fulcio.Value.BaseURL = "changed"
		got.Interfaces.CredentialCleanup.Command = "changed"
		*got.Interfaces.CredentialCleanup.CommandSHA256 = "changed"
		got.Interfaces.ExtendedFixtureProducer.Protocol = "changed"
		*got.Interfaces.ExtendedFixtureProducer.CommandSHA256 = "changed"
	}
	mutate()
	mutate()

	var readers sync.WaitGroup
	for range 4 {
		readers.Go(mutate)
	}

	readers.Wait()

	got, err := currentLabContract()
	if err != nil || !reflect.DeepEqual(got, want) {
		t.Fatalf("caller mutation changed cached contract: err=%v", err)
	}
}

func TestCurrentLabContract_DetectsMutationAfterFirstLoad(t *testing.T) {
	body := composeFixtureBody(t)
	path := writeContract(t, body)
	setCurrentContract(t, path)

	if _, err := currentLabContract(); err != nil {
		t.Fatal(err)
	}

	changed := strings.Replace(body, `"run-neutral-fixture"`, `"run-mutated-fixture"`, 1)
	if err := os.WriteFile(path, []byte(changed), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := currentLabContract(); err == nil || !strings.Contains(err.Error(), "does not match preflight facts") {
		t.Fatalf("mutation result = %v", err)
	}
}

func TestCurrentLabContract_RejectsReplacementBeforeFirstLoad(t *testing.T) {
	body := composeFixtureBody(t)
	path := writeContract(t, body)
	setCurrentContract(t, path)

	if err := os.Rename(path, path+".old"); err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := currentLabContract(); err == nil || !strings.Contains(err.Error(), "does not match preflight facts") {
		t.Fatalf("replacement result = %v", err)
	}
}

func TestSelected_MalformedContractCannotBecomeASkip(t *testing.T) {
	body := strings.Replace(composeFixtureBody(t), `"version": 2`, `"version": 1`, 1)
	setCurrentContract(t, writeContract(t, body))
	t.Setenv("RC_LIVE_GITLAB_OWNER", "")

	recorder := &fatalRecorder{}
	if Selected(recorder, provider.ForgeGitLab) {
		t.Fatal("malformed v1 contract was selected")
	}

	if !recorder.fatal || recorder.skipped {
		t.Fatalf("malformed contract became a skip: %+v", recorder)
	}
}

func TestTokenMeta_ProjectsV2Lifecycle(t *testing.T) {
	t.Parallel()

	contract, err := loadLabContract(writeContract(t, composeFixtureBody(t)))
	if err != nil {
		t.Fatal(err)
	}

	forgejo, _ := contract.endpointFor("forgejo", "")

	meta := forgejo.tokenMeta(contract.Generation.ID)
	if !meta.revocationRequired || meta.revocationRunID != contract.Generation.ID {
		t.Fatalf("forgejo token metadata = %+v", meta)
	}

	gitlab, _ := contract.endpointFor("gitlab", "")

	now := time.Date(2026, time.August, 17, 10, 0, 0, 0, time.UTC)
	if err := validateTokenMetadata("gitlab", contract.Generation.ID, gitlab.tokenMeta(contract.Generation.ID), now); err != nil {
		t.Fatalf("canonical GitLab token lifecycle rejected: %v", err)
	}
}

// TestEndpointFor_AbsentForgeIsATypedSkip pins that a forge missing from the
// contract is reported through errNoEndpoint, which Selected turns into a
// clean skip, while an ambiguous selection stays a hard failure.
func TestEndpointFor_AbsentForgeIsATypedSkip(t *testing.T) {
	t.Parallel()

	contract := labContract{Endpoints: []labEndpoint{{Name: "gl", Kind: string(provider.ForgeGitLab)}}}

	for _, name := range []string{"", "missing"} {
		if _, err := contract.endpointFor("forgejo", name); !errors.Is(err, errNoEndpoint) {
			t.Errorf("endpointFor(forgejo, %q) = %v, want errNoEndpoint", name, err)
		}
	}

	ambiguous := labContract{Endpoints: []labEndpoint{{Name: "a", Kind: string(provider.ForgeGitLab)}, {Name: "b", Kind: string(provider.ForgeGitLab)}}}
	if _, err := ambiguous.endpointFor(string(provider.ForgeGitLab), ""); err == nil || errors.Is(err, errNoEndpoint) {
		t.Errorf("ambiguous endpointFor = %v, want a failure that is not a skip", err)
	}
}

// TestFrozenInputs_EachTamperedFactRefusesBeforeUse changes one frozen input or
// its captured facts at a time and requires the first use to refuse it: the
// contract's facts missing or for another file, the contract rewritten with the
// same length, the CA's facts missing or altered, and the CA replaced.
func TestFrozenInputs_EachTamperedFactRefusesBeforeUse(t *testing.T) {
	for name, tamper := range map[string]func(t *testing.T, contractPath string){
		"contract facts missing": func(t *testing.T, _ string) {
			t.Helper()
			t.Setenv(frozenContractFactsEnv, "")
		},
		"contract facts for another file": func(t *testing.T, _ string) {
			t.Helper()

			_, other, err := readPrivateContractBound(writeContract(t, composeFixtureBody(t)))
			if err != nil {
				t.Fatal(err)
			}

			t.Setenv(frozenContractFactsEnv, other)
		},
		"contract rewritten in place with the same length": func(t *testing.T, contractPath string) {
			t.Helper()

			body := strings.Replace(composeFixtureBody(t), `"run-neutral-fixture"`, `"run-neutral-fixturf"`, 1)
			if err := os.WriteFile(contractPath, []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
		},
	} {
		t.Run(name, func(t *testing.T) {
			path := writeContract(t, composeFixtureBody(t))
			setCurrentContract(t, path)
			tamper(t, path)

			if _, err := currentLabContract(); !errors.Is(err, errs.ErrValidation) || !strings.Contains(err.Error(), "does not match preflight facts") {
				t.Fatalf("tampered contract = %v, want the preflight facts refusal", err)
			}
		})
	}

	caPath := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(caPath, independentCAPEM(t), 0o400); err != nil {
		t.Fatal(err)
	}

	_, caFacts, err := readCAFileBound(caPath, nil)
	if err != nil {
		t.Fatal(err)
	}

	for name, facts := range map[string]string{"CA facts missing": "", "CA facts altered": caFacts + "0"} {
		if _, captureErr := captureTargetCA(Target{CAFile: caPath, CAFacts: facts}); !errors.Is(captureErr, errs.ErrValidation) ||
			!strings.Contains(captureErr.Error(), "does not match preflight facts") {
			t.Errorf("%s: capture = %v, want the preflight facts refusal", name, captureErr)
		}

		if verifyErr := verifyTargetCAPath(Target{CAFile: caPath, CAFacts: facts}); !errors.Is(verifyErr, errs.ErrValidation) {
			t.Errorf("%s: verification = %v, want a refusal", name, verifyErr)
		}
	}
}
